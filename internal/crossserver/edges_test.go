// Error-path and logging coverage for the cross-server config service:
// config-store read/write failures, the notify defaults (nil targets and
// receivers) with both bus outcomes logged, callback dispatch failure and
// summary logs, postWithRetry's floor/cancel/backoff-cap edges, postOnce
// request failures, receiver addressing degrading to all servers, and the
// match-domain index and removed-structure diff arms.
package crossserver

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/cuihairu/atlas/internal/event"
	"github.com/cuihairu/atlas/internal/model"
	"github.com/cuihairu/atlas/internal/store"
	"github.com/cuihairu/atlas/internal/store/memory"
)

var errFault = errors.New("fault injected")

// faultCfg wraps the config-store verbs so reads and writes can fail on
// demand; unset hooks delegate to the backing memory store.
type faultCfg struct {
	store.CrossServerConfigStore
	base   *memory.Store
	getFn  func(ctx context.Context) (*model.CrossServerConfig, error)
	saveFn func(ctx context.Context, cfg *model.CrossServerConfig) (*model.CrossServerConfig, error)
}

func (f *faultCfg) GetCrossServerConfig(ctx context.Context) (*model.CrossServerConfig, error) {
	if f.getFn != nil {
		return f.getFn(ctx)
	}
	return f.base.GetCrossServerConfig(ctx)
}

func (f *faultCfg) SaveCrossServerConfig(ctx context.Context, cfg *model.CrossServerConfig) (*model.CrossServerConfig, error) {
	if f.saveFn != nil {
		return f.saveFn(ctx, cfg)
	}
	return f.base.SaveCrossServerConfig(ctx, cfg)
}

// faultServers fails the fleet listing so addressing must degrade.
type faultServers struct {
	store.ServerStore
	base   *memory.Store
	listFn func(ctx context.Context, f store.ServerFilter) ([]*model.Server, error)
}

func (f *faultServers) ListServers(ctx context.Context, sf store.ServerFilter) ([]*model.Server, error) {
	if f.listFn != nil {
		return f.listFn(ctx, sf)
	}
	return f.base.ListServers(ctx, sf)
}

// syncLogBuf is a mutex-guarded log sink: callback dispatch logs from
// worker goroutines must not interleave writes.
type syncLogBuf struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncLogBuf) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncLogBuf) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// busStub toggles the bus publish outcome.
type busStub struct{ fail bool }

func (b *busStub) Name() string      { return "stub" }
func (b *busStub) Synchronous() bool { return true }

func (b *busStub) Publish(_ context.Context, _ *event.Event) error {
	if b.fail {
		return errFault
	}
	return nil
}

func (b *busStub) Subscribe(context.Context, string, event.Handler) error { return nil }
func (b *busStub) Ack(context.Context, *event.Event) error                { return nil }
func (b *busStub) Close() error                                           { return nil }

// TestConfigReadErrorsPropagate pins the strict reads: a config-store
// failure other than not-found surfaces from Get, Snapshot and the Since
// conditional pull (a center that cannot answer must not look empty).
func TestConfigReadErrorsPropagate(t *testing.T) {
	mem := memory.New()
	fc := &faultCfg{CrossServerConfigStore: mem, base: mem, getFn: func(context.Context) (*model.CrossServerConfig, error) {
		return nil, errFault
	}}
	svc := New(fc, mem, nil, nil)
	ctx := context.Background()

	if _, err := svc.Get(ctx); err == nil {
		t.Fatal("get with failing config store: expected error")
	}
	if _, err := svc.Snapshot(ctx); err == nil {
		t.Fatal("snapshot with failing config store: expected error")
	}
	if _, _, err := svc.Since(ctx, 0, ""); err == nil {
		t.Fatal("since with failing config store: expected error")
	}
}

// TestSaveStoreErrors pins the save wraps: both the pre-save snapshot read
// and the persisted write surface as errors (a config that may or may not
// have landed must be reported, never swallowed).
func TestSaveStoreErrors(t *testing.T) {
	ctx := context.Background()

	mem := memory.New()
	fc := &faultCfg{CrossServerConfigStore: mem, base: mem, getFn: func(context.Context) (*model.CrossServerConfig, error) {
		return nil, errFault
	}}
	svc := New(fc, mem, nil, nil)
	if _, err := svc.Save(ctx, testSpec()); err == nil {
		t.Fatal("save with failing snapshot read: expected error")
	}

	mem2 := memory.New()
	fc2 := &faultCfg{CrossServerConfigStore: mem2, base: mem2, saveFn: func(context.Context, *model.CrossServerConfig) (*model.CrossServerConfig, error) {
		return nil, errFault
	}}
	svc2 := New(fc2, mem2, nil, nil)
	if _, err := svc2.Save(ctx, testSpec()); err == nil {
		t.Fatal("save with failing store write: expected error")
	}
}

// TestNotifyDefaultsAndBusLogs pins the notify contract: nil targets mean
// "*" and nil receivers mean an empty (non-nil) list, and both bus
// outcomes are visible — a lost signal carries BusError and a warn, a
// delivered one is logged as info.
func TestNotifyDefaultsAndBusLogs(t *testing.T) {
	ctx := context.Background()
	cfg := &model.CrossServerConfig{Version: 1, Hash: "h"}

	var lb syncLogBuf
	mem := memory.New()
	svc := New(mem, mem, &busStub{fail: true}, slog.New(slog.NewTextHandler(&lb, nil)))
	res := svc.Notify(ctx, cfg, nil, nil)
	if len(res.Targets) != 1 || res.Targets[0] != model.TargetAll {
		t.Fatalf("targets = %v, want [*] for nil input", res.Targets)
	}
	if res.Receivers == nil || len(res.Receivers) != 0 {
		t.Fatalf("receivers = %v, want non-nil empty", res.Receivers)
	}
	if res.BusError == "" {
		t.Fatal("bus publish failure must surface in BusError")
	}
	if !bytes.Contains([]byte(lb.String()), []byte("config update signal not delivered on bus")) {
		t.Fatalf("bus failure was not warned: %q", lb.String())
	}

	var lb2 syncLogBuf
	svc2 := New(mem, mem, &busStub{}, slog.New(slog.NewTextHandler(&lb2, nil)))
	res2 := svc2.Notify(ctx, cfg, []string{"cluster-ea"}, []string{"game-1001"})
	if res2.BusError != "" {
		t.Fatalf("successful publish must not set BusError: %q", res2.BusError)
	}
	if !bytes.Contains([]byte(lb2.String()), []byte("config update signaled on bus")) {
		t.Fatalf("bus success was not logged: %q", lb2.String())
	}
}

// TestCallbackFailureAndDispatchLogs pins the callback worker logs: a
// failed delivery is warned per target and the dispatch summary is logged
// — the fallback-to-polling contract must be observable, not silent.
func TestCallbackFailureAndDispatchLogs(t *testing.T) {
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer receiver.Close()

	var lb syncLogBuf
	mem := memory.New()
	svc := New(mem, mem, nil, slog.New(slog.NewTextHandler(&lb, nil)))
	svc.WithCallbackPolicy(250*time.Millisecond, 1, time.Millisecond)
	registerCallbackServer(t, mem, "game-9100", receiver.URL, model.StatusOnline)

	if _, err := svc.Save(context.Background(), testSpec()); err != nil {
		t.Fatalf("save: %v (a failed callback must not fail the save)", err)
	}
	if !bytes.Contains([]byte(lb.String()), []byte("config update callback failed")) {
		t.Fatalf("callback failure was not warned: %q", lb.String())
	}
	if !bytes.Contains([]byte(lb.String()), []byte("config update callbacks dispatched")) {
		t.Fatalf("dispatch summary was not logged: %q", lb.String())
	}
}

// TestPostWithRetryFloorsAndCancellation pins the retry policy edges: a
// zero-attempt config floors to one try, a canceled context during backoff
// returns the context error instead of retrying forever, and the backoff
// doubling respects the configured cap.
func TestPostWithRetryFloorsAndCancellation(t *testing.T) {
	ctx := context.Background()

	var mu sync.Mutex
	hits := 0
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		hits++
		mu.Unlock()
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer receiver.Close()
	target := callbackTarget{id: "game-r", url: receiver.URL}

	mem := memory.New()
	floored := New(mem, mem, nil, nil).WithCallbackPolicy(250*time.Millisecond, 0, time.Millisecond)
	if err := floored.postWithRetry(ctx, target, []byte("{}")); err == nil {
		t.Fatal("floored retry against a 500 receiver: expected error")
	}
	mu.Lock()
	if hits != 1 {
		t.Fatalf("attempts after floor = %d, want exactly 1", hits)
	}
	mu.Unlock()

	canceled := New(mem, mem, nil, nil) // attempts 3, default backoff
	cctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := canceled.postWithRetry(cctx, target, []byte("{}")); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled during backoff = %v, want context.Canceled", err)
	}

	capped := New(mem, mem, nil, nil).WithCallbackPolicy(250*time.Millisecond, 3, time.Millisecond)
	capped.maxBackoff = time.Millisecond // in-package: cap below the doubled backoff
	mu.Lock()
	hits = 0
	mu.Unlock()
	if err := capped.postWithRetry(ctx, target, []byte("{}")); err == nil {
		t.Fatal("capped retry against a 500 receiver: expected error")
	}
	mu.Lock()
	defer mu.Unlock()
	if hits != 3 {
		t.Fatalf("attempts with capped backoff = %d, want 3 (fast, cap engaged)", hits)
	}
}

// TestPostOnceRequestErrors pins the two request-construction failures: a
// URL that cannot be parsed and a delivery that cannot connect.
func TestPostOnceRequestErrors(t *testing.T) {
	svc := New(memory.New(), memory.New(), nil, nil)
	ctx := context.Background()

	if err := svc.postOnce(ctx, callbackTarget{id: "bad", url: "http://exa mple.com"}, []byte("{}")); err == nil {
		t.Fatal("unparseable callback URL: expected error")
	}
	if err := svc.postOnce(ctx, callbackTarget{id: "dead", url: "http://127.0.0.1:1"}, []byte("{}")); err == nil {
		t.Fatal("unreachable callback URL: expected error")
	}
}

// TestReceiversDegradedToAll pins the addressing fallback: when the fleet
// listing fails, the signal addresses everyone (a lost signal must
// over-address rather than under-address) and the degradation is warned.
func TestReceiversDegradedToAll(t *testing.T) {
	mem := memory.New()
	fs := &faultServers{ServerStore: mem, base: mem, listFn: func(context.Context, store.ServerFilter) ([]*model.Server, error) {
		return nil, errFault
	}}
	var lb syncLogBuf
	svc := New(mem, fs, nil, slog.New(slog.NewTextHandler(&lb, nil)))

	out := svc.receivers(context.Background(), testSpec(), testSpec(), []string{"cluster-ea"})
	if len(out) != 1 || out[0] != model.TargetAll {
		t.Fatalf("degraded addressing = %v, want [*]", out)
	}
	if !bytes.Contains([]byte(lb.String()), []byte("receiver addressing degraded to all servers")) {
		t.Fatalf("degradation was not warned: %q", lb.String())
	}

	// The same listing failure inside callback dispatch is reported per
	// result (no callbacks can be planned), not silently skipped.
	cb := svc.dispatchCallbacks(context.Background(), &model.CrossServerConfig{Version: 1}, []string{"cluster-ea"})
	if len(cb.Errors) != 1 || !bytes.Contains([]byte(cb.Errors[0]), []byte("list servers: ")) {
		t.Fatalf("callback errors = %v, want one list-servers failure", cb.Errors)
	}
}

// TestTargetMemberIndexCoversMatchDomains pins the third membership source:
// match-domain ids index their servers like clusters and groups do.
func TestTargetMemberIndexCoversMatchDomains(t *testing.T) {
	spec := testSpec()
	spec.MatchDomains = []model.CrossServerMatchDomain{{ID: "md-1", Servers: []string{"game-1001"}}}

	idx := targetMemberIndex(spec)
	if !idx["md-1"]["game-1001"] {
		t.Fatalf("match-domain membership missing: %v", idx["md-1"])
	}
}

// TestDiffTargetsRemovedStructures pins the removal arms: a cluster or
// group that disappears between configs is itself a change subscribers
// must hear about.
func TestDiffTargetsRemovedStructures(t *testing.T) {
	old := testSpec()
	updated := testSpec()
	updated.Topology.Clusters[0].ID = "cluster-zh"
	updated.Groups[0].ID = "season-2"

	got := diffTargets(old, updated)
	has := func(id string) bool {
		for _, t := range got {
			if t == id {
				return true
			}
		}
		return false
	}
	if !has("cluster-ea") || !has("season-1") {
		t.Fatalf("removed structures not targeted: %v", got)
	}
}
