// Package telemetry keeps the lightweight in-memory time series behind the
// admin charts (概览负载时间视图 / 消息总线积压, TODO 观测深化): fixed-rate
// ring buffers sampled from live gauges — explicitly NOT a TSDB. Restart
// clears the series; the decree allows an in-memory ring with ≥10h retention.
package telemetry

import (
	"context"
	"sort"
	"sync"
	"time"
)

// Point is one sampled value.
type Point struct {
	T time.Time `json:"t"`
	V float64   `json:"v"`
}

// Ring is a fixed-capacity circular buffer of gauges sampled at a fixed
// interval. Observe is called by the sampler tick; Window returns the points
// covering the requested past span (a full ring serves any window up to its
// retention).
type Ring struct {
	mu   sync.Mutex
	buf  []Point
	head int // next write index
	n    int // valid points (≤ cap)
}

// Observe appends v stamped now.
func (r *Ring) Observe(v float64, at time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.buf[r.head] = Point{T: at, V: v}
	r.head = (r.head + 1) % len(r.buf)
	if r.n < len(r.buf) {
		r.n++
	}
}

// Window returns the points with T >= at. Points come in chronological
// order; the caller (sampler lockstep) guarantees alignment between rings
// created by the same probe.
func (r *Ring) Window(at time.Time) []Point {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Point, 0, r.n)
	for i := 0; i < r.n; i++ {
		idx := (r.head - r.n + i + len(r.buf)) % len(r.buf)
		if !r.buf[idx].T.Before(at) {
			out = append(out, r.buf[idx])
		}
	}
	return out
}

// Sampler drives every ring at one fixed interval: each tick pulls all
// probes and lands their named gauge values in per-name rings. Because all
// rings are written in the same tick loop, rings created by the same probe
// stay index-aligned — chart merging is a zip, not a join.
type Sampler struct {
	mu       sync.Mutex
	interval time.Duration
	capacity int
	rings    map[string]*Ring
	probes   []func() map[string]float64

	tickAt func() time.Time // injectable for tests
}

// NewSampler creates a sampler ticking every interval and retaining
// retention of history (ring capacity = retention/interval, minimum 2).
func NewSampler(interval, retention time.Duration) *Sampler {
	capacity := int(retention / interval)
	if capacity < 2 {
		capacity = 2
	}
	return &Sampler{
		interval: interval,
		capacity: capacity,
		rings:    make(map[string]*Ring),
		tickAt:   time.Now,
	}
}

// Capacity reports the per-series ring capacity (retention / interval).
func (s *Sampler) Capacity() int { return s.capacity }

// Probe registers a source of named gauge values. Probes are polled on every
// tick; a probe returning no value for a name leaves that ring untouched
// (gaps, not zeroes).
func (s *Sampler) Probe(f func() map[string]float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.probes = append(s.probes, f)
}

// ring returns (creating on first sight) the ring for name.
func (s *Sampler) ring(name string) *Ring {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.rings[name]
	if !ok {
		r = &Ring{buf: make([]Point, s.Capacity())}
		s.rings[name] = r
	}
	return r
}

// Run polls the probes every interval until ctx is done. The first tick
// fires immediately so charts have data right after boot.
func (s *Sampler) Run(ctx context.Context) {
	t := time.NewTicker(s.interval)
	defer t.Stop()
	s.sample()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.sample()
		}
	}
}

func (s *Sampler) sample() {
	now := s.tickAt()
	s.mu.Lock()
	probes := make([]func() map[string]float64, len(s.probes))
	copy(probes, s.probes)
	s.mu.Unlock()

	for _, probe := range probes {
		for name, v := range probe() {
			s.ring(name).Observe(v, now)
		}
	}
}

// Series returns the window of one named series (chronological).
func (s *Sampler) Series(name string, window time.Duration) []Point {
	at := s.tickAt().Add(-window)
	return s.ring(name).Window(at)
}

// Names lists series names with the given prefix, sorted.
func (s *Sampler) Names(prefix string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.rings))
	for name := range s.rings {
		if len(name) >= len(prefix) && name[:len(prefix)] == prefix {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// Rate computes events/second over a cumulative-counter series: the delta
// between the last and first sample divided by their time span. A flat or
// too-short window means zero.
func Rate(points []Point) float64 {
	if len(points) < 2 {
		return 0
	}
	first, last := points[0], points[len(points)-1]
	span := last.T.Sub(first.T).Seconds()
	if span <= 0 {
		return 0
	}
	delta := last.V - first.V
	if delta < 0 {
		delta = last.V // counter reset (restart): rate from zero to last
	}
	return delta / span
}
