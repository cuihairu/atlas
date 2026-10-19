package main

import (
	"fmt"
	"math"
	"math/rand/v2"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// reservoirCap bounds per-flow samples: uniform reservoir sampling keeps the
// percentiles representative over a multi-hour run without growing memory.
const reservoirCap = 100000

// stream is one measured flow: counts plus a bounded uniform sample of
// latencies. Safe for concurrent use — every stream is written from many
// fleet goroutines.
type stream struct {
	name    string
	cap     int
	mu      sync.Mutex
	samples []time.Duration
	rng     *rand.Rand
	count   atomic.Int64
	errors  atomic.Int64
	skipped atomic.Int64
	lastErr atomic.Value // string, most recent failure
}

func newStream(name string, cap int) *stream {
	return &stream{name: name, cap: cap, rng: rand.New(rand.NewPCG(uint64(len(name)), 99))}
}

func (s *stream) Record(lat time.Duration) {
	s.count.Add(1)
	s.mu.Lock()
	if len(s.samples) < s.cap {
		s.samples = append(s.samples, lat)
	} else if i := s.rng.IntN(s.cap); i < s.cap {
		s.samples[i] = lat
	}
	s.mu.Unlock()
}

func (s *stream) Err(err error) {
	s.errors.Add(1)
	s.lastErr.Store(truncateErr(err))
}
func (s *stream) Skip() { s.skipped.Add(1) }

// LastError is the most recent failure text on this flow — the first thing
// worth reading when a soak run comes back red.
func (s *stream) LastError() string {
	v := s.lastErr.Load()
	if v == nil {
		return ""
	}
	return v.(string)
}

func truncateErr(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	if len(msg) > 200 {
		msg = msg[:200] + "…"
	}
	return msg
}

func (s *stream) Count() int64   { return s.count.Load() }
func (s *stream) Errors() int64  { return s.errors.Load() }
func (s *stream) Skipped() int64 { return s.skipped.Load() }

// Pct returns the p-th percentile by nearest rank over the sample reservoir.
func (s *stream) Pct(p int) time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.samples) == 0 {
		return 0
	}
	sorted := make([]time.Duration, len(s.samples))
	copy(sorted, s.samples)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	idx := int(math.Ceil(float64(p)/100*float64(len(sorted)))) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

// RPS is the observed call rate over the elapsed window.
func (s *stream) RPS(elapsed time.Duration) float64 {
	if elapsed <= 0 {
		return 0
	}
	return float64(s.Count()) / elapsed.Seconds()
}

// StreamView is the human/JSON view of one flow.
type StreamView struct {
	Name      string  `json:"name"`
	Count     int64   `json:"count"`
	Errors    int64   `json:"errors"`
	Skipped   int64   `json:"skipped"`
	P50       int64   `json:"p50_ms"`
	P95       int64   `json:"p95_ms"`
	P99       int64   `json:"p99_ms"`
	Max       int64   `json:"max_ms"`
	LastError string  `json:"last_error,omitempty"`
	RPS       float64 `json:"rps"`
}

type report struct {
	StartedAt time.Time    `json:"started_at"`
	Elapsed   float64      `json:"elapsed_sec"`
	Servers   int          `json:"servers"`
	Streams   []StreamView `json:"streams"`
}

var flowOrder = []string{
	flowRegister, flowHeartbeat, flowStorm,
	flowReadList, flowReadDetail, flowReadRecommend,
	flowCharWrite, flowOps,
}

func (e *Engine) report(elapsed time.Duration) *report {
	r := &report{
		StartedAt: time.Now().Add(-elapsed),
		Elapsed:   elapsed.Seconds(),
		Servers:   e.cfg.Servers,
		Streams:   make([]StreamView, 0, len(e.flows)),
	}
	for _, name := range flowOrder {
		s, ok := e.flows[name]
		if !ok {
			continue
		}
		r.Streams = append(r.Streams, StreamView{
			Name:      s.name,
			Count:     s.Count(),
			Errors:    s.Errors(),
			Skipped:   s.Skipped(),
			P50:       s.Pct(50).Milliseconds(),
			P95:       s.Pct(95).Milliseconds(),
			P99:       s.Pct(99).Milliseconds(),
			Max:       s.Pct(100).Milliseconds(),
			LastError: s.LastError(),
			RPS:       s.RPS(elapsed),
		})
	}
	return r
}

// RenderText prints an aligned one-line-per-flow table for terminals.
func (r *report) RenderText() string {
	var b strings.Builder
	fmt.Fprintf(&b, "loadgen report — %d servers, %.0fs elapsed\n", r.Servers, r.Elapsed)
	fmt.Fprintf(&b, "%-18s %10s %7s %7s %8s %8s %8s %8s %8s %s\n",
		"flow", "count", "err", "skip", "rps", "p50ms", "p95ms", "p99ms", "maxms", "last_error")
	for _, s := range r.Streams {
		fmt.Fprintf(&b, "%-18s %10d %7d %7d %8.1f %8d %8d %8d %8d %s\n",
			s.Name, s.Count, s.Errors, s.Skipped, s.RPS, s.P50, s.P95, s.P99, s.Max, s.LastError)
	}
	return b.String()
}
