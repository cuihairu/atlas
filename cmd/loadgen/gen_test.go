package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeAtlas implements just the endpoint shapes the SDK decodes, and counts
// hits per path so tests can assert every load stream actually fired.
type fakeAtlas struct {
	mu            sync.Mutex
	hits          map[string]int
	delay         time.Duration
	registerDelay time.Duration
	registered    map[string]bool
}

func newFakeAtlas() *fakeAtlas {
	return &fakeAtlas{hits: map[string]int{}, registered: map[string]bool{}}
}

func (f *fakeAtlas) handler(w http.ResponseWriter, r *http.Request) {
	if f.delay > 0 {
		time.Sleep(f.delay)
	}
	if r.URL.Path == "/v1/registry/servers/register" && f.registerDelay > 0 {
		// Model a boot herd that takes real time, widening the window in
		// which ungated reads would race registration.
		time.Sleep(f.registerDelay)
	}
	f.mu.Lock()
	f.hits[r.URL.Path]++
	// Track registrations so detail reads behave like real Atlas: an id
	// that has not registered yet is a genuine 404.
	if r.URL.Path == "/v1/registry/servers/register" && r.Method == http.MethodPost {
		var body struct {
			ServerID string `json:"server_id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.ServerID != "" {
			f.registered[body.ServerID] = true
		}
	}
	known := true
	if id, ok := strings.CutPrefix(r.URL.Path, "/v1/discovery/servers/"); ok && id != "" && !strings.Contains(id, "/") {
		known = f.registered[id]
	}
	f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	if !known {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"code":"SERVER_NOT_FOUND"}`)
		return
	}
	// SDK list calls unmarshal into slices; everything else into objects.
	if r.URL.Path == "/v1/discovery/servers" {
		fmt.Fprint(w, `{"servers":[]}`)
		return
	}
	fmt.Fprint(w, `{}`)
}

func (f *fakeAtlas) count(path string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.hits[path]
}

func lastErrors(e *Engine) string {
	var b strings.Builder
	for _, s := range e.flows {
		if err := s.LastError(); err != "" {
			fmt.Fprintf(&b, "%s: %s\n", s.name, err)
		}
	}
	return b.String()
}

func totalErrors(e *Engine) int64 {
	var n int64
	for _, s := range e.flows {
		n += s.Errors()
	}
	return n
}

func newTestEngine(t *testing.T, cfg Config, base string) (*Engine, *fakeAtlas) {
	t.Helper()
	fake := newFakeAtlas()
	ts := httptest.NewServer(http.HandlerFunc(fake.handler))
	t.Cleanup(ts.Close)
	cfg.PublicURL = ts.URL
	cfg.RegistryURL = ts.URL
	cfg.AdminURL = ts.URL
	cfg.Timeout = 2 * time.Second
	e, err := NewEngine(cfg)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	t.Cleanup(func() { _ = e.Close() })
	_ = base
	return e, fake
}

func TestEngineDrivesAllStreams(t *testing.T) {
	cfg := Config{
		Servers:     3,
		Duration:    500 * time.Millisecond,
		Heartbeat:   80 * time.Millisecond,
		ReadRPS:     60,
		CharRPS:     10,
		OpsEvery:    100 * time.Millisecond,
		MaxInFlight: 16, // read workers fire in phase; no saturation wanted here
		Seed:        7,
	}
	e, fake := newTestEngine(t, cfg, "")

	ctx, cancel := context.WithTimeout(context.Background(), cfg.Duration)
	rep := e.Run(ctx)
	cancel()

	if got := time.Duration(rep.Elapsed * float64(time.Second)); got < cfg.Duration-50*time.Millisecond {
		t.Errorf("elapsed %v < duration %v", got, cfg.Duration)
	}
	for path, min := range map[string]int{
		"/v1/registry/servers/register": 3, // boot registration of the whole fleet
		"/v1/discovery/servers":         5, // list reads
		"/v1/routing/recommended":       1, // recommend reads
		"/v1/directory/characters":      2, // character writes
		"/v1/admin/stats":               1, // ops trickle
	} {
		if got := fake.count(path); got < min {
			t.Errorf("%s hit %d times, want >= %d", path, got, min)
		}
	}
	// Detail reads use /v1/discovery/servers/{id}; count hits whose path has
	// one more segment than the list path.
	if e.flows[flowReadDetail].Count() < 1 {
		t.Errorf("read-detail count = %d, want >= 1", e.flows[flowReadDetail].Count())
	}
	if hb := e.flows[flowHeartbeat].Count(); hb < 9 { // 3 servers x >=3 beats in 500ms
		t.Errorf("heartbeat count = %d, want >= 9", hb)
	}
	if err := totalErrors(e); err != 0 {
		t.Errorf("stream errors = %d, want 0\n%v", err, lastErrors(e))
	}
}

func TestEngineStormReregistersWholeFleet(t *testing.T) {
	cfg := Config{
		Servers:     3,
		Duration:    220 * time.Millisecond,
		Heartbeat:   60 * time.Millisecond,
		StormEvery:  90 * time.Millisecond,
		MaxInFlight: 8,
		Seed:        11,
	}
	e, _ := newTestEngine(t, cfg, "")

	ctx, cancel := context.WithTimeout(context.Background(), cfg.Duration)
	e.Run(ctx)
	cancel()

	if got := e.flows[flowStorm].Count(); got < int64(cfg.Servers) {
		t.Errorf("storm re-registers = %d, want >= %d (whole fleet re-registered)", got, cfg.Servers)
	}
	if hb := e.flows[flowHeartbeat].Count(); hb < 6 {
		t.Errorf("heartbeat count = %d, want >= 6", hb)
	}
	if err := totalErrors(e); err != 0 {
		t.Errorf("stream errors = %d, want 0\n%v", err, lastErrors(e))
	}
}

// TestEngineReadsWaitForFleetBoot pins the harness behaviour found missing in
// the 2026-10-20 HA smoke: read/char/ops streams must not start until the
// whole fleet has registered, or reads race boot and surface honest-looking
// SERVER_NOT_FOUND noise in the report.
func TestEngineReadsWaitForFleetBoot(t *testing.T) {
	cfg := Config{
		Servers:     10,
		Duration:    400 * time.Millisecond,
		Heartbeat:   100 * time.Millisecond,
		ReadRPS:     2000, // reads would certainly outrun boot without the gate
		MaxInFlight: 64,
		Seed:        5,
	}
	e, fake := newTestEngine(t, cfg, "")
	fake.registerDelay = 30 * time.Millisecond

	ctx, cancel := context.WithTimeout(context.Background(), cfg.Duration)
	e.Run(ctx)
	cancel()

	if err := totalErrors(e); err != 0 {
		t.Fatalf("stream errors = %d, want 0 (reads must wait for fleet boot)\n%v", err, lastErrors(e))
	}
	if got := fake.count("/v1/registry/servers/register"); got < cfg.Servers {
		t.Errorf("register hits = %d, want >= %d", got, cfg.Servers)
	}
	if e.flows[flowReadDetail].Count() == 0 {
		t.Error("read-detail never fired after fleet gate opened")
	}
}

func TestEngineAdmissionControlSkipsNotErrors(t *testing.T) {
	fake := newFakeAtlas()
	fake.delay = 50 * time.Millisecond // every call slower than the tick rate
	ts := httptest.NewServer(http.HandlerFunc(fake.handler))
	t.Cleanup(ts.Close)

	cfg := Config{
		PublicURL:   ts.URL,
		RegistryURL: ts.URL,
		Servers:     1,
		Duration:    150 * time.Millisecond,
		Heartbeat:   200 * time.Millisecond,
		ReadRPS:     1000, // 1ms ticks against a 50ms handler, 1 slot
		MaxInFlight: 1,
		Timeout:     2 * time.Second,
		Seed:        3,
	}
	e, err := NewEngine(cfg)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	t.Cleanup(func() { _ = e.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), cfg.Duration)
	e.Run(ctx)
	cancel()

	var skipped int64
	for _, s := range e.flows {
		skipped += s.Skipped()
	}
	if skipped == 0 {
		t.Error("expected skipped > 0 when in-flight cap saturates")
	}
	if err := totalErrors(e); err != 0 {
		t.Errorf("stream errors = %d, want 0 (saturation is a skip, not an error)\n%v", err, lastErrors(e))
	}
}

// TestEngineBootRetriesThroughAdmissionSkips pins the 1k-scale lesson from
// the 2026-10-20 local run: at fleet sizes above the in-flight cap the boot
// herd saturates the semaphore, and a skipped boot register must be retried
// by the virtual server — otherwise the fleet never fully exists and its
// heartbeats 404 against a registry that never learned the id.
func TestEngineBootRetriesThroughAdmissionSkips(t *testing.T) {
	cfg := Config{
		Servers:     40,
		Duration:    2 * time.Second,
		Heartbeat:   100 * time.Millisecond,
		ReadRPS:     50,
		MaxInFlight: 2, // deliberately far below the fleet size
		Seed:        9,
	}
	e, fake := newTestEngine(t, cfg, "")
	fake.registerDelay = 20 * time.Millisecond

	ctx, cancel := context.WithTimeout(context.Background(), cfg.Duration)
	e.Run(ctx)
	cancel()

	if got := fake.count("/v1/registry/servers/register"); got < cfg.Servers {
		t.Errorf("register hits = %d, want >= %d (every server must eventually register)", got, cfg.Servers)
	}
	if err := totalErrors(e); err != 0 {
		t.Errorf("stream errors = %d, want 0 (no phantom heartbeats against unregistered ids)\n%v", err, lastErrors(e))
	}
	// The fleet-ready gate must open once the whole fleet is registered, not
	// at teardown: reads parked on the gate only fire after boot completes.
	if got := e.flows[flowReadDetail].Count(); got == 0 {
		t.Error("read-detail never fired (fleet-ready gate did not open at boot)")
	}
}

// TestEngineReadRateDeliveredUnderLatency pins the 5k-scale lesson from the
// 2026-10-20 local run: the read stream is fleet-scaled, so its rate must be
// delivered under realistic per-call latency — a single issuing goroutine
// tops out at 1/latency and undershoots the target 7x at the 5k point.
func TestEngineReadRateDeliveredUnderLatency(t *testing.T) {
	cfg := Config{
		Servers:     10,
		Duration:    1 * time.Second,
		Heartbeat:   200 * time.Millisecond,
		ReadRPS:     200,
		ReadWorkers: 16,
		MaxInFlight: 256,
		Seed:        13,
	}
	e, fake := newTestEngine(t, cfg, "")
	fake.delay = 10 * time.Millisecond // every read takes 10ms server-side

	ctx, cancel := context.WithTimeout(context.Background(), cfg.Duration)
	rep := e.Run(ctx)
	cancel()

	var reads int64
	for _, f := range []string{flowReadList, flowReadDetail, flowReadRecommend} {
		reads += e.flows[f].Count()
	}
	if reads < 140 {
		t.Errorf("read total = %d over %.1fs, want >= 140 (70%% of the 200 rps target under 10ms latency)",
			reads, rep.Elapsed)
	}
	if err := totalErrors(e); err != 0 {
		t.Errorf("stream errors = %d, want 0\n%v", err, lastErrors(e))
	}
}

func TestPickReadKindMatchesMixWeights(t *testing.T) {
	rng := rand.New(rand.NewPCG(42, 0))
	const n = 200000
	counts := make([]int, len(readKinds))
	for i := 0; i < n; i++ {
		counts[pickReadKind(rng)]++
	}
	var totalWeight int64
	for _, k := range readKinds {
		totalWeight += k.weight
	}
	for i, k := range readKinds {
		want := float64(k.weight) / float64(totalWeight)
		got := float64(counts[i]) / n
		if got < want*0.95 || got > want*1.05 {
			t.Errorf("read mix %q = %.3f, want %.3f ±5%%", k.flow, got, want)
		}
	}
}
