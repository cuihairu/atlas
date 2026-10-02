package atlas

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// configServer is a stand-in config center: it serves one versioned
// document and lets the test publish new versions, force pull failures,
// and count how many pulls actually happened.
type configServer struct {
	mu      sync.Mutex
	version int
	hash    string
	spec    map[string]any

	failNext atomic.Int32 // >0 makes the next N pulls fail
	dark     atomic.Bool  // while set every pull fails (unbounded)
	pulls    atomic.Int32
	started  time.Time
}

func newConfigServer() *configServer {
	return &configServer{started: time.Now()}
}

func (s *configServer) publish(t *testing.T, version int, hash string, spec map[string]any) {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.version, s.hash, s.spec = version, hash, spec
}

// handle serves the pull endpoint with HTTP conditional-get semantics.
func (s *configServer) handle(w http.ResponseWriter, r *http.Request) {
	s.pulls.Add(1)
	if s.dark.Load() {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	if n := s.failNext.Load(); n > 0 {
		s.failNext.Add(-1)
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	s.mu.Lock()
	version, hash, spec := s.version, s.hash, s.spec
	s.mu.Unlock()

	etag := `"` + hash + `"`
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "no-cache")

	if v := r.URL.Query().Get("version"); v != "" {
		if v == itoa(version) && r.URL.Query().Get("hash") == hash {
			w.WriteHeader(http.StatusNotModified)
			return
		}
	}
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
		"version": version, "hash": hash, "spec": spec,
		"updated_at": s.started.UTC().Format(time.RFC3339),
	})
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// TestConfigWatcherStartupPull pins the startup contract: the first pull
// adopts whatever the center has and hands it to OnApply.
func TestConfigWatcherStartupPull(t *testing.T) {
	cs := newConfigServer()
	cs.publish(t, 7, "hash-7", map[string]any{"features": map[string]bool{"arena": true}})

	var applied atomic.Int32
	var gotVersion atomic.Int32
	c, _ := newRESTClient(t, http.HandlerFunc(cs.handle))
	w := NewConfigWatcher(c, ConfigWatcherOptions{
		ServerID:   "game-1",
		OnApply:    func(cfg *CrossServerConfig) { applied.Add(1); gotVersion.Store(int32(cfg.Version)) },
		MaxBackoff: 5 * time.Millisecond,
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx, 2) }()

	waitFor(t, func() bool { return applied.Load() == 1 })
	if gotVersion.Load() != 7 {
		t.Errorf("applied version = %d, want 7", gotVersion.Load())
	}
	if cur := w.Current(); cur == nil || cur.Version != 7 || cur.Hash != "hash-7" {
		t.Errorf("current = %+v, want v7/hash-7", cur)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Run returned %v, want nil on cancel", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run did not return after cancel")
	}
}

// TestConfigWatcherStartupFailFast pins the other half: when the center
// cannot be reached within the startup window, Run reports it instead of
// starting a game server that silently has no coordination config.
func TestConfigWatcherStartupFailFast(t *testing.T) {
	cs := newConfigServer()
	cs.publish(t, 1, "hash-1", nil)
	cs.failNext.Store(10)

	c, _ := newRESTClient(t, http.HandlerFunc(cs.handle))
	w := NewConfigWatcher(c, ConfigWatcherOptions{MaxBackoff: time.Millisecond})

	err := w.Run(context.Background(), 2)
	if err == nil {
		t.Fatal("Run succeeded with an unreachable config center")
	}
	if w.Current() != nil {
		t.Error("a failed startup must not leave a config in hand")
	}
}

// TestConfigWatcherRuntimeFailureKeepsConfig is the availability promise:
// a config center outage mid-game must not take the server down or swap
// its config — the watcher keeps the last good version and retries.
func TestConfigWatcherRuntimeFailureKeepsConfig(t *testing.T) {
	cs := newConfigServer()
	cs.publish(t, 3, "hash-3", map[string]any{"features": map[string]bool{"boss": true}})

	var applied atomic.Int32
	c, _ := newRESTClient(t, http.HandlerFunc(cs.handle))
	w := NewConfigWatcher(c, ConfigWatcherOptions{
		OnApply:      func(*CrossServerConfig) { applied.Add(1) },
		PollInterval: 10 * time.Millisecond,
		MaxBackoff:   5 * time.Millisecond,
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx, 3) }()
	waitFor(t, func() bool { return applied.Load() == 1 })

	// A new version lands, then the center goes dark. The dark gate is
	// unbounded, so the window cannot accidentally end mid-assert the way
	// a failure budget would.
	cs.publish(t, 4, "hash-4", map[string]any{"features": map[string]bool{"boss": false}})
	cs.dark.Store(true)
	waitFor(t, func() bool {
		_, _, _, failures := w.Stats()
		return failures > 0
	})

	cur := w.Current()
	if cur == nil || cur.Version != 3 {
		t.Fatalf("current = %+v, want the last good v3", cur)
	}
	if applied.Load() != 1 {
		t.Errorf("apply count = %d, want 1 (nothing applied while the center was down)", applied.Load())
	}

	// Recovery: the center answers again and the watcher converges.
	cs.dark.Store(false)
	cs.publish(t, 5, "hash-5", nil)
	w.Notify()
	waitFor(t, func() bool { return w.Current() != nil && w.Current().Version == 5 })
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Run returned %v, want nil on cancel", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run did not return after cancel")
	}
}

// TestConfigWatcherVersionMonotonic pins the anti-rollback rule: a
// delayed response carrying an older version is refused, a repeated
// version is a no-op (idempotent re-delivery), and only a strictly newer
// snapshot reaches OnApply.
func TestConfigWatcherVersionMonotonic(t *testing.T) {
	cs := newConfigServer()
	cs.publish(t, 5, "hash-5", nil)

	var applied []int
	var mu sync.Mutex
	c, _ := newRESTClient(t, http.HandlerFunc(cs.handle))
	w := NewConfigWatcher(c, ConfigWatcherOptions{
		OnApply: func(cfg *CrossServerConfig) {
			mu.Lock()
			applied = append(applied, cfg.Version)
			mu.Unlock()
		},
	})
	ctx := context.Background()

	if appliedNow, err := w.Pull(ctx); err != nil || !appliedNow {
		t.Fatalf("first pull: applied=%v err=%v", appliedNow, err)
	}

	// Same version again → idempotent, no second apply.
	appliedNow, err := w.Pull(ctx)
	if err != nil {
		t.Fatalf("repeat pull: %v", err)
	}
	if appliedNow {
		t.Error("same version was applied twice")
	}
	_, idempotent, _, _ := w.Stats()
	if idempotent != 1 {
		t.Errorf("idempotent count = %d, want 1", idempotent)
	}

	// An older version must not roll the server back.
	cs.publish(t, 2, "hash-2", nil)
	appliedNow, err = w.Pull(ctx)
	if err != nil {
		t.Fatalf("stale pull: %v", err)
	}
	if appliedNow {
		t.Error("a stale version was applied")
	}
	if cur := w.Current(); cur.Version != 5 {
		t.Errorf("version rolled back to %d, want 5", cur.Version)
	}
	if _, _, skipped, _ := w.Stats(); skipped != 1 {
		t.Errorf("skipped count = %d, want 1", skipped)
	}

	// A newer version applies.
	cs.publish(t, 6, "hash-6", nil)
	if appliedNow, err = w.Pull(ctx); err != nil || !appliedNow {
		t.Fatalf("newer pull: applied=%v err=%v", appliedNow, err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(applied) != 2 || applied[0] != 5 || applied[1] != 6 {
		t.Errorf("applied versions = %v, want [5 6]", applied)
	}
}

// TestConfigWatcherNotifyCoalesces checks the signal path: Notify wakes
// the watcher, and a burst of signals collapses into a single apply.
func TestConfigWatcherNotifyCoalesces(t *testing.T) {
	cs := newConfigServer()
	cs.publish(t, 1, "hash-1", nil)

	var applied atomic.Int32
	c, _ := newRESTClient(t, http.HandlerFunc(cs.handle))
	w := NewConfigWatcher(c, ConfigWatcherOptions{
		OnApply:      func(*CrossServerConfig) { applied.Add(1) },
		PollInterval: time.Hour, // signals only
		MaxBackoff:   5 * time.Millisecond,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx, 1) }()
	waitFor(t, func() bool { return applied.Load() == 1 })

	pullsBefore := cs.pulls.Load()
	cs.publish(t, 2, "hash-2", nil)
	for i := 0; i < 50; i++ {
		w.Notify() // burst
	}
	waitFor(t, func() bool { return w.Current() != nil && w.Current().Version == 2 })

	// Coalescing: far fewer pulls than signals.
	if pulls := cs.pulls.Load() - pullsBefore; pulls > 5 {
		t.Errorf("pulls = %d for 50 signals, want coalescing", pulls)
	}
	if applied.Load() != 2 {
		t.Errorf("apply count = %d, want 2", applied.Load())
	}
}

// TestFetchCrossServerConfigUnpublished pins the fail-fast signal: an
// unpublished center is a distinct, actionable condition, not a generic
// transport error, so a booting server can decide what to do.
func TestFetchCrossServerConfigUnpublished(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/crossserver/config", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
			"error": map[string]any{"code": "CONFIG_NOT_FOUND", "message": "no cross-server config has been published yet"},
		})
	})
	c, _ := newRESTClient(t, mux)

	_, err := c.FetchCrossServerConfig(context.Background())
	if err == nil {
		t.Fatal("expected an error for an unpublished config")
	}
	if !errors.Is(err, ErrConfigNotPublished) {
		t.Errorf("error = %v, want ErrConfigNotPublished", err)
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("condition not met within 3s")
}
