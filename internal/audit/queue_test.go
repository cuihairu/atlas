package audit

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// testSink records the entries it received and the cutoffs a sweep passed it.
type testSink struct {
	logger *slog.Logger
	call   func(ctx context.Context, e Entry) error

	mu          sync.Mutex
	writes      []Entry
	seenCutoffs []time.Time
	closeCalls  int
}

func newTestSink(t *testing.T, call func(context.Context, Entry) error) *testSink {
	t.Helper()
	return &testSink{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		call:   call,
	}
}

func (s *testSink) Write(ctx context.Context, e Entry) error {
	s.mu.Lock()
	s.writes = append(s.writes, e)
	s.mu.Unlock()
	if s.call == nil {
		return nil
	}
	return s.call(ctx, e)
}

func (s *testSink) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closeCalls++
	return nil
}

func (s *testSink) Retain(ctx context.Context, before time.Time) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seenCutoffs = append(s.seenCutoffs, before)
	return 2, nil
}

// plainSink implements only Sink, so the dispatcher's Sweep must fall through
// the type assertion instead of expiring anything.
type plainSink struct{}

func (plainSink) Write(ctx context.Context, e Entry) error { return nil }
func (plainSink) Close() error                             { return nil }

// failingRetainer always fails Retain, to pin the sweep's error path.
type failingRetainer struct{}

func (failingRetainer) Write(ctx context.Context, e Entry) error { return nil }
func (failingRetainer) Close() error                             { return nil }
func (failingRetainer) Retain(ctx context.Context, _ time.Time) (int64, error) {
	return 0, errStoreGone
}

var errStoreGone = context.Canceled

type syncBuffer struct {
	sync.Mutex
	buf []byte
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.Lock()
	defer b.Unlock()
	b.buf = append(b.buf, p...)
	return len(p), nil
}

func (b *syncBuffer) String() string {
	b.Lock()
	defer b.Unlock()
	return string(b.buf)
}

func testLogger(t *testing.T) *slog.Logger {
	t.Helper()
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestDispatcherWorkerWritesAndCounters(t *testing.T) {
	var mu sync.Mutex
	var seen []string
	sink := newTestSink(t, func(_ context.Context, e Entry) error {
		mu.Lock()
		seen = append(seen, e.Path)
		mu.Unlock()
		return nil
	})
	d := NewDispatcher(sink, 8, 0, nil, testLogger(t))
	d.Submit(Entry{Path: "/v1/admin/a"})
	d.Submit(Entry{Path: "/v1/admin/b"})

	waitFor(t, "two sink writes", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(seen) == 2
	})
	if w, dr := d.Stats(); w != 2 || dr != 0 {
		t.Fatalf("stats = written %d dropped %d, want 2/0", w, dr)
	}
	if dropped := d.Drain(time.Second); dropped != 0 {
		t.Fatalf("drain dropped = %d, want 0", dropped)
	}
}

func TestDispatcherOverflowDropsOldest(t *testing.T) {
	var started atomic.Bool
	release := make(chan struct{})
	closed := false
	closeOnce := func() {
		if !closed {
			closed = true
			close(release)
		}
	}
	defer closeOnce()
	sink := newTestSink(t, func(_ context.Context, e Entry) error {
		started.Store(true)
		<-release
		return nil
	})
	d := NewDispatcher(sink, 2, 0, nil, testLogger(t))

	d.Submit(Entry{Path: "/v1/admin/0"}) // occupies the worker
	waitFor(t, "the worker to pick up entry 0", started.Load)
	d.Submit(Entry{Path: "/v1/admin/1"})
	d.Submit(Entry{Path: "/v1/admin/2"})
	d.Submit(Entry{Path: "/v1/admin/3"})
	d.Submit(Entry{Path: "/v1/admin/4"})

	// The worker holds entry 0 and the queue of two holds 3 and 4: entries 1
	// and 2 were displaced by the overflow policy.
	if w, dr := d.Stats(); w != 0 || dr != 2 {
		t.Fatalf("stats = written %d dropped %d, want 0/2", w, dr)
	}
	closeOnce()
	d.Drain(time.Second)

	sink.mu.Lock()
	writes := make([]Entry, len(sink.writes))
	copy(writes, sink.writes)
	sink.mu.Unlock()
	if len(writes) != 3 {
		t.Fatalf("sink writes = %d, want 3", len(writes))
	}
	paths := map[string]bool{}
	for _, e := range writes {
		paths[e.Path] = true
	}
	if !paths["/v1/admin/0"] || !paths["/v1/admin/3"] || !paths["/v1/admin/4"] {
		t.Fatalf("sink writes = %v, want the oldest kept plus the two newest", paths)
	}
	if w, dr := d.Stats(); w != 3 || dr != 2 {
		t.Fatalf("stats = written %d dropped %d, want 3/2", w, dr)
	}
}

func TestDispatcherRetriesAFailedWriteOnce(t *testing.T) {
	var mu sync.Mutex
	var attempts int
	sink := newTestSink(t, func(_ context.Context, e Entry) error {
		mu.Lock()
		attempts++
		n := attempts
		mu.Unlock()
		if n == 1 {
			return errStoreGone
		}
		return nil
	})
	d := NewDispatcher(sink, 8, 0, nil, testLogger(t))
	d.retryAfter = time.Millisecond
	d.Submit(Entry{Path: "/v1/admin/a"})

	waitFor(t, "the retried write", func() bool { w, _ := d.Stats(); return w == 1 })
	mu.Lock()
	n := attempts
	mu.Unlock()
	if n != 2 {
		t.Fatalf("attempts = %d, want 2 (one retry)", n)
	}
	d.Drain(time.Second)
}

func TestDispatcherCountsDroppedWhenTheSinkFails(t *testing.T) {
	var buf syncBuffer
	sink := newTestSink(t, func(_ context.Context, e Entry) error { return errStoreGone })
	d := NewDispatcher(sink, 8, 0, nil, slog.New(slog.NewTextHandler(&buf, nil)))
	d.retryAfter = time.Millisecond
	d.Submit(Entry{Path: "/v1/admin/a"})

	waitFor(t, "the failed write counted as dropped", func() bool { return d.dropped.Load() == 1 })
	if w, dr := d.Stats(); w != 0 || dr != 1 {
		t.Fatalf("stats = written %d dropped %d, want 0/1", w, dr)
	}
	if s := buf.String(); !strings.Contains(s, "audit sink write failed") {
		t.Fatalf("failure not logged: %q", s)
	}
}

func TestDispatcherDrainTimeoutCountsPending(t *testing.T) {
	var started atomic.Bool
	release := make(chan struct{})
	closed := false
	closeOnce := func() {
		if !closed {
			closed = true
			close(release)
		}
	}
	defer closeOnce()
	sink := newTestSink(t, func(_ context.Context, e Entry) error {
		started.Store(true)
		<-release
		return nil
	})
	d := NewDispatcher(sink, 8, 0, nil, testLogger(t))
	d.Submit(Entry{Path: "/v1/admin/0"})
	waitFor(t, "the worker to pick up entry 0", started.Load)
	d.Submit(Entry{Path: "/v1/admin/1"})
	d.Submit(Entry{Path: "/v1/admin/2"})

	// The worker is blocked on entry 0, so a flush times out and counts the
	// two queued entries.
	if dropped := d.Drain(50 * time.Millisecond); dropped != 2 {
		t.Fatalf("drain dropped = %d, want 2", dropped)
	}
	if w, dr := d.Stats(); w != 0 || dr != 2 {
		t.Fatalf("stats = written %d dropped %d, want 0/2", w, dr)
	}
	// Release the worker: the next flush drains the queue, and a further
	// drain is a no-op once the worker has exited.
	closeOnce()
	if dropped := d.Drain(2 * time.Second); dropped != 0 {
		t.Fatalf("second drain dropped = %d, want 0", dropped)
	}
	if dropped := d.Drain(time.Second); dropped != 0 {
		t.Fatalf("third drain dropped = %d, want 0", dropped)
	}
	if w, _ := d.Stats(); w != 3 {
		t.Fatalf("written = %d, want 3 (entry 0 plus the two queued)", w)
	}
}

func TestDispatcherDrainClosesTheWorker(t *testing.T) {
	sink := newTestSink(t, func(_ context.Context, e Entry) error { return nil })
	d := NewDispatcher(sink, 4, 0, nil, testLogger(t))
	d.Drain(50 * time.Millisecond)

	// Submit after a drain must not hang and must not be written.
	done := make(chan struct{})
	go func() {
		d.Submit(Entry{Path: "/v1/admin/late"})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Submit after Drain blocked")
	}
	if w, dr := d.Stats(); w != 0 || dr != 1 {
		t.Fatalf("stats = written %d dropped %d, want 0/1 (the late entry is counted as dropped)", w, dr)
	}
}

func TestDispatcherSweepExpiresRetention(t *testing.T) {
	var buf syncBuffer
	sink := newTestSink(t, nil)
	// The hourly sweep goroutine never fires inside the test; the sweep is
	// driven directly below.
	d := NewDispatcher(sink, 8, 90*24*time.Hour, nil,
		slog.New(slog.NewTextHandler(&buf, nil)))

	before := time.Now().Add(-40 * 24 * time.Hour)
	n, err := d.Sweep(context.Background())
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if n != 2 {
		t.Fatalf("sweep deleted = %d, want 2", n)
	}
	sink.mu.Lock()
	cutoffs := make([]time.Time, len(sink.seenCutoffs))
	copy(cutoffs, sink.seenCutoffs)
	sink.mu.Unlock()
	if len(cutoffs) != 1 || !cutoffs[0].Before(before) {
		t.Fatalf("cutoffs = %v, want one cutoff roughly now-90d", cutoffs)
	}
	if s := buf.String(); !strings.Contains(s, "audit retention sweep") {
		t.Fatalf("sweep not logged: %q", s)
	}
}

func TestDispatcherSweepNoopWithoutRetainer(t *testing.T) {
	d := NewDispatcher(plainSink{}, 8, 90*24*time.Hour, nil, testLogger(t))
	if n, err := d.Sweep(context.Background()); err != nil || n != 0 {
		t.Fatalf("sweep = %d %v, want a no-op", n, err)
	}

	d0 := NewDispatcher(plainSink{}, 8, 0, nil, testLogger(t))
	if n, err := d0.Sweep(context.Background()); err != nil || n != 0 {
		t.Fatalf("sweep with no retention = %d %v, want a no-op", n, err)
	}
}

func TestDispatcherSweepErrorIsReported(t *testing.T) {
	var buf syncBuffer
	d := NewDispatcher(failingRetainer{}, 8, 90*24*time.Hour, nil,
		slog.New(slog.NewTextHandler(&buf, nil)))
	if _, err := d.Sweep(context.Background()); err == nil {
		t.Fatal("sweep with a failing sink returned a nil error")
	}
	if s := buf.String(); !strings.Contains(s, "audit retention sweep failed") {
		t.Fatalf("sweep failure not logged: %q", s)
	}
}

type observer struct {
	written, dropped, retained, latency atomic.Int64
}

func (o *observer) ObserveAuditWrite(time.Duration) { o.latency.Add(1) }
func (o *observer) ObserveAuditWritten()            { o.written.Add(1) }
func (o *observer) ObserveAuditDropped()            { o.dropped.Add(1) }
func (o *observer) ObserveAuditRetention(int64)     { o.retained.Add(1) }

func TestDispatcherReportsToTheObserver(t *testing.T) {
	o := &observer{}
	sink := newTestSink(t, nil)
	d := NewDispatcher(sink, 8, 90*24*time.Hour, o, testLogger(t))
	d.Submit(Entry{Path: "/v1/admin/a"})
	waitFor(t, "one sink write", func() bool { return o.written.Load() == 1 })
	d.drop(Entry{Path: "/v1/admin/x"}, "test")
	if n, _ := d.Sweep(context.Background()); n == 0 {
		t.Fatal("sweep returned no deletions")
	}
	if o.written.Load() != 1 || o.dropped.Load() != 1 || o.retained.Load() != 1 || o.latency.Load() != 1 {
		t.Fatalf("observer = written %d dropped %d retained %d latency %d, want all 1",
			o.written.Load(), o.dropped.Load(), o.retained.Load(), o.latency.Load())
	}
}
