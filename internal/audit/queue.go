package audit

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

// Sink persists one audit entry. A single background worker drives the
// writes, so a Sink only needs to be correct on one goroutine at a time.
type Sink interface {
	Write(ctx context.Context, e Entry) error
	Close() error
}

// Retainer can expire persisted entries. Only sinks that keep their own
// history implement it (the SQL sink); the JSONL sink is rotated by ops and
// does not.
type Retainer interface {
	Retain(ctx context.Context, before time.Time) (int64, error)
}

// Observer reports sink activity into the Prometheus vocabulary. Implementers
// must be nil-receiver safe; a nil Observer is replaced with a no-op so
// wiring an AuditLog without metrics stays free.
type Observer interface {
	ObserveAuditWrite(d time.Duration)
	ObserveAuditWritten()
	ObserveAuditDropped()
	ObserveAuditRetention(n int64)
}

type nopObserver struct{}

func (nopObserver) ObserveAuditWrite(time.Duration) {}
func (nopObserver) ObserveAuditWritten()            {}
func (nopObserver) ObserveAuditDropped()            {}
func (nopObserver) ObserveAuditRetention(int64)     {}

func nopOr(obs Observer) Observer {
	if obs == nil {
		return nopObserver{}
	}
	return obs
}

// Dispatcher is the async boundary between the request path and the audit
// sink: an Admin request never waits on a database or a disk write. Overflow
// drops the oldest queued entry and counts it — never the newest (a recent
// record is what an incident review wants) and never a caller wait.
type Dispatcher struct {
	sink   Sink
	queue  chan Entry
	done   chan struct{}
	once   sync.Once
	mu     sync.Mutex // serializes Submit against Drain's queue close
	closed bool

	logger *slog.Logger
	obs    Observer

	written atomic.Int64
	dropped atomic.Int64

	retention     time.Duration
	sweepInterval time.Duration

	// retryAfter paces a worker that cannot reach the sink (tests shorten it).
	retryAfter time.Duration
}

// NewDispatcher starts the worker (and, when retention is enabled, the
// retention sweep). A non-positive queue size falls back to 1000 entries.
func NewDispatcher(sink Sink, size int, retention time.Duration, obs Observer, logger *slog.Logger) *Dispatcher {
	if size <= 0 {
		size = 1000
	}
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	d := &Dispatcher{
		sink:          sink,
		queue:         make(chan Entry, size),
		done:          make(chan struct{}),
		logger:        logger,
		obs:           nopOr(obs),
		retention:     retention,
		sweepInterval: sweepInterval(retention),
		retryAfter:    time.Millisecond * 100,
	}
	go d.run()
	if d.sweepInterval > 0 {
		go d.sweepLoop()
	}
	return d
}

// sweepInterval spaces retention sweeps apart. The window is in days, so an
// hourly sweep is plenty and keeps the DELETE off the request path.
func sweepInterval(retention time.Duration) time.Duration {
	if retention <= 0 {
		return 0
	}
	return time.Hour
}

// Submit enqueues an entry without blocking. See the type comment for the
// drop policy. Submitting after Drain counts the entry as dropped instead of
// panicking on a closed queue — a late in-flight request must not take the
// process down at shutdown.
func (d *Dispatcher) Submit(e Entry) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		d.dropped.Add(1)
		d.obs.ObserveAuditDropped()
		return
	}
	if len(d.queue) >= cap(d.queue) {
		select {
		case old := <-d.queue:
			d.drop(old, "queue full, oldest entry dropped")
		default:
		}
	}
	select {
	case d.queue <- e:
	default:
		// Filled by another Submit between the two checks.
		d.drop(e, "queue full, entry dropped")
	}
}

func (d *Dispatcher) drop(e Entry, reason string) {
	d.dropped.Add(1)
	d.obs.ObserveAuditDropped()
	d.logger.Warn("audit sink dropped entry", "reason", reason, "path", e.Path, "actor", e.Actor)
}

// run drains the queue onto the sink.
func (d *Dispatcher) run() {
	defer close(d.done)
	for e := range d.queue {
		start := time.Now()
		err := d.sink.Write(context.Background(), e)
		if err != nil {
			// One retry covers the connection-reset and pool-exhaustion cases
			// without a retry loop of its own.
			time.Sleep(d.retryAfter)
			err = d.sink.Write(context.Background(), e)
		}
		if err != nil {
			// A failed write means this entry is not in the archive, so it is
			// counted as dropped: the drop counter stays the truth about what
			// the archive holds.
			d.logger.Error("audit sink write failed", "error", err, "path", e.Path)
			d.drop(e, "sink write failed")
		} else {
			d.written.Add(1)
			d.obs.ObserveAuditWritten()
		}
		d.obs.ObserveAuditWrite(time.Since(start))
	}
}

// Sweep expires persisted entries older than the retention window once. The
// dispatcher calls it on an interval; it is exported so tests (and future
// admin tooling) can run it on demand. Sinks without Retainer, and a
// non-positive window, make it a no-op.
func (d *Dispatcher) Sweep(ctx context.Context) (int64, error) {
	r, ok := d.sink.(Retainer)
	if !ok || d.retention <= 0 {
		return 0, nil
	}
	n, err := r.Retain(ctx, time.Now().Add(-d.retention))
	if err != nil {
		d.logger.Warn("audit retention sweep failed", "error", err)
		return 0, err
	}
	if n > 0 {
		d.logger.Info("audit retention sweep", "deleted", n, "older_than", d.retention)
	}
	d.obs.ObserveAuditRetention(n)
	return n, nil
}

func (d *Dispatcher) sweepLoop() {
	t := time.NewTimer(d.sweepInterval)
	defer t.Stop()
	for {
		select {
		case <-d.done:
			return
		case <-t.C:
			_, _ = d.Sweep(context.Background())
		}
	}
}

// Drain stops the dispatcher, flushing whatever is still queued up to the
// timeout. Call it at shutdown, before the store connection closes. The
// returned count is the number of entries dropped because the flush timed
// out (an approximation: the worker may still drain a few in the background).
func (d *Dispatcher) Drain(timeout time.Duration) int64 {
	d.mu.Lock()
	d.once.Do(func() {
		d.closed = true
		close(d.queue)
	})
	d.mu.Unlock()
	var dropped int64
	select {
	case <-d.done:
	case <-time.After(timeout):
		dropped = int64(len(d.queue))
		d.logger.Warn("audit sink drain timed out", "timeout", timeout, "dropped", dropped)
	}
	if dropped > 0 {
		d.dropped.Add(dropped)
		d.obs.ObserveAuditDropped()
	}
	return dropped
}

// Stats reports the counters since startup (for tests and admin tooling).
func (d *Dispatcher) Stats() (written, dropped int64) {
	return d.written.Load(), d.dropped.Load()
}
