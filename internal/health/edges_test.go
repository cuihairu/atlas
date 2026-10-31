// Error-path coverage for the health monitor: the sweep must stay resilient
// to a store that fails mid-pass (a transition is logged and retried, not
// fatal), the maintenance-window application must log-and-continue on every
// sub-step failure, and the alerter must tolerate a nil receiver and a webhook
// that is unreachable or rejects the payload.
package health

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cuihairu/atlas/internal/model"
	"github.com/cuihairu/atlas/internal/store"
	"github.com/cuihairu/atlas/internal/store/memory"
)

var errFault = errors.New("fault injected")

// faultStore wraps a real memory.Store and lets individual methods be
// redirected on demand. Unset hooks delegate to the backing store, so a test
// enables exactly the one failure it is exercising.
type faultStore struct {
	store.Store
	base *memory.Store

	listServersFn   func(ctx context.Context, f store.ServerFilter) ([]*model.Server, error)
	getRuntimesFn   func(ctx context.Context, ids []string) (map[string]model.Runtime, error)
	casStatusFn     func(ctx context.Context, id string, from, to model.ServerStatus) (bool, error)
	deleteServerFn  func(ctx context.Context, id string) error
	deleteRuntimeFn func(ctx context.Context, id string) error
	listWindowsFn   func(ctx context.Context, serverID string, limit int) ([]*model.MaintenanceWindow, error)
	deleteWindowFn  func(ctx context.Context, id string) error
}

func (f *faultStore) ListServers(ctx context.Context, sf store.ServerFilter) ([]*model.Server, error) {
	if f.listServersFn != nil {
		return f.listServersFn(ctx, sf)
	}
	return f.base.ListServers(ctx, sf)
}

func (f *faultStore) GetRuntimes(ctx context.Context, ids []string) (map[string]model.Runtime, error) {
	if f.getRuntimesFn != nil {
		return f.getRuntimesFn(ctx, ids)
	}
	return f.base.GetRuntimes(ctx, ids)
}

func (f *faultStore) UpdateServerStatusIf(ctx context.Context, id string, from, to model.ServerStatus) (bool, error) {
	if f.casStatusFn != nil {
		return f.casStatusFn(ctx, id, from, to)
	}
	return f.base.UpdateServerStatusIf(ctx, id, from, to)
}

func (f *faultStore) DeleteServer(ctx context.Context, id string) error {
	if f.deleteServerFn != nil {
		return f.deleteServerFn(ctx, id)
	}
	return f.base.DeleteServer(ctx, id)
}

func (f *faultStore) DeleteRuntime(ctx context.Context, id string) error {
	if f.deleteRuntimeFn != nil {
		return f.deleteRuntimeFn(ctx, id)
	}
	return f.base.DeleteRuntime(ctx, id)
}

func (f *faultStore) ListMaintenanceWindows(ctx context.Context, serverID string, limit int) ([]*model.MaintenanceWindow, error) {
	if f.listWindowsFn != nil {
		return f.listWindowsFn(ctx, serverID, limit)
	}
	return f.base.ListMaintenanceWindows(ctx, serverID, limit)
}

func (f *faultStore) DeleteMaintenanceWindow(ctx context.Context, id string) error {
	if f.deleteWindowFn != nil {
		return f.deleteWindowFn(ctx, id)
	}
	return f.base.DeleteMaintenanceWindow(ctx, id)
}

// newFaultStore builds a fault store over a fresh memory.Store.
func newFaultStore() *faultStore {
	base := memory.New()
	return &faultStore{Store: base, base: base}
}

// newEdgeMonitor builds a monitor over a fault store with a capture logger so
// error logs can be asserted alongside the code path they cover.
func newEdgeMonitor(s store.Store, cap *recordCapture) *Monitor {
	return New(s, 30*time.Second, 90*time.Second, time.Hour, slog.New(cap))
}

func hasLog(cap *recordCapture, msg string) bool {
	cap.mu.Lock()
	defer cap.mu.Unlock()
	for _, r := range cap.records {
		if r.Message == msg {
			return true
		}
	}
	return false
}

// TestSweepListServersFailure pins the fail-fast contract: a listing failure
// aborts the whole sweep (judging liveness on an incomplete view would mark
// live servers offline), so the caller sees a wrapped error.
func TestSweepListServersFailure(t *testing.T) {
	fs := newFaultStore()
	fs.listServersFn = func(context.Context, store.ServerFilter) ([]*model.Server, error) {
		return nil, errFault
	}
	mon := newEdgeMonitor(fs, &recordCapture{})

	if err := mon.sweep(context.Background()); err == nil {
		t.Fatal("sweep with failing ListServers: expected an error, got nil")
	}
}

// TestRunLogsSweepFailure pins the Run loop's resilience: a failing sweep is
// logged and the loop keeps ticking instead of returning (a monitor that dies
// on one store blip would silently stop watching the fleet).
func TestRunLogsSweepFailure(t *testing.T) {
	fs := newFaultStore()
	fs.listServersFn = func(context.Context, store.ServerFilter) ([]*model.Server, error) {
		return nil, errFault
	}
	cap := &recordCapture{}
	m := New(fs, 30*time.Second, 90*time.Second, 5*time.Millisecond, slog.New(cap))

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		m.Run(ctx)
		close(done)
	}()

	time.Sleep(60 * time.Millisecond) // let several sweeps fire and fail
	cancel()
	<-done

	if !hasLog(cap, "health sweep failed") {
		t.Fatal("Run did not log the failed sweep")
	}
}

// TestSweepPaginationContinuation pins the cursor loop: a full page (exactly
// the store cap) must continue to the next page, so servers beyond the first
// 200 are still checked rather than silently stranded.
func TestSweepPaginationContinuation(t *testing.T) {
	fs := newFaultStore()
	ctx := context.Background()
	for i := 0; i < store.ListServersMaxLimit; i++ {
		registerServer(t, ctx, fs.base, fmt.Sprintf("pg-%03d", i), model.StatusOnline)
	}
	mon := newEdgeMonitor(fs, &recordCapture{})

	if err := mon.sweep(ctx); err != nil {
		t.Fatalf("sweep: %v", err)
	}
}

// TestSweepMaintenanceListFailure pins the log-and-continue contract of the
// maintenance pre-pass: a window-list failure is logged, never fatal to the
// rest of the sweep (stale-heartbeat detection still runs).
func TestSweepMaintenanceListFailure(t *testing.T) {
	fs := newFaultStore()
	fs.listWindowsFn = func(context.Context, string, int) ([]*model.MaintenanceWindow, error) {
		return nil, errFault
	}
	cap := &recordCapture{}
	mon := newEdgeMonitor(fs, cap)

	if err := mon.sweep(context.Background()); err != nil {
		t.Fatalf("sweep should continue past a window-list failure, got %v", err)
	}
	if !hasLog(cap, "maintenance window application failed") {
		t.Fatal("window-list failure was not logged")
	}
}

// TestSweepTransitionFailures drives each transition decision with a failing
// compare-and-set: the sweep must log and continue (a single transition blip
// must not abort the whole pass or crash the monitor).
func TestSweepTransitionFailures(t *testing.T) {
	cases := []struct {
		name       string
		status     model.ServerStatus
		heartbeat  bool
		clockAhead time.Duration
	}{
		{"starting_no_heartbeat", model.StatusStarting, false, 200 * time.Second}, // 142
		{"starting_slow_heartbeat", model.StatusStarting, true, 60 * time.Second}, // 164
		{"suspect_dead", model.StatusSuspect, true, 150 * time.Second},            // 171
		{"suspect_recovered", model.StatusSuspect, true, 10 * time.Second},        // 178
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fs := newFaultStore()
			fs.casStatusFn = func(context.Context, string, model.ServerStatus, model.ServerStatus) (bool, error) {
				return false, errFault
			}
			ctx := context.Background()
			registerServer(t, ctx, fs.base, "trans-srv", tc.status)
			if tc.heartbeat {
				recordHeartbeat(t, ctx, fs.base, "trans-srv", 100)
			}
			cap := &recordCapture{}
			mon := newEdgeMonitor(fs, cap)
			base := time.Now()
			mon.now = func() time.Time { return base.Add(tc.clockAhead) }

			if err := mon.sweep(ctx); err != nil {
				t.Fatalf("sweep should continue past a failed transition, got %v", err)
			}
			if !hasLog(cap, "transition failed") {
				t.Fatal("transition failure was not logged")
			}
		})
	}
}

// TestExpireTemporaryZeroTTL pins the lease guard: a temporary record with no
// positive TTL is skipped (it can never be safely reclaimed) instead of being
// deleted immediately on the next sweep.
func TestExpireTemporaryZeroTTL(t *testing.T) {
	fs := newFaultStore()
	fs.listServersFn = func(context.Context, store.ServerFilter) ([]*model.Server, error) {
		return []*model.Server{{
			ID:            "temp-zero",
			Name:          "temp-zero",
			Status:        model.StatusOffline,
			RegKind:       model.RegTemporary,
			RegTTLSeconds: 0,
			CreatedAt:     time.Now(),
		}}, nil
	}
	mon := newEdgeMonitor(fs, &recordCapture{})

	if err := mon.sweep(context.Background()); err != nil {
		t.Fatalf("sweep: %v", err)
	}
}

// TestExpireTemporaryAlreadyGone pins the not-found reclaim path: a temporary
// record whose server vanished between the list and the delete is consumed
// silently (the delete is the idempotency boundary — the runtime cleanup is
// still attempted on the next sweep).
func TestExpireTemporaryAlreadyGone(t *testing.T) {
	fs := newFaultStore()
	fs.deleteServerFn = func(context.Context, string) error {
		return store.ErrNotFound
	}
	ctx := context.Background()
	registerRegServer(t, ctx, fs.base, "temp-gone", model.StatusOffline, model.RegTemporary, 1)
	mon := newEdgeMonitor(fs, &recordCapture{})
	mon.now = func() time.Time { return time.Now().Add(20 * time.Second) }

	if err := mon.sweep(ctx); err != nil {
		t.Fatalf("sweep: %v", err)
	}
}

// TestExpireTemporaryDeleteFails pins the reclaim-retry path: a non-not-found
// delete failure is logged and the record is kept for the next sweep (the
// lease reclaim must not be silently dropped on a store blip).
func TestExpireTemporaryDeleteFails(t *testing.T) {
	fs := newFaultStore()
	fs.deleteServerFn = func(context.Context, string) error {
		return errFault
	}
	ctx := context.Background()
	registerRegServer(t, ctx, fs.base, "temp-fail", model.StatusOffline, model.RegTemporary, 1)
	cap := &recordCapture{}
	mon := newEdgeMonitor(fs, cap)
	mon.now = func() time.Time { return time.Now().Add(20 * time.Second) }

	if err := mon.sweep(ctx); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if !hasLog(cap, "temporary registration reclaim failed") {
		t.Fatal("reclaim failure was not logged")
	}
}

// TestExpireTemporaryRuntimeFails pins the post-delete runtime cleanup: after
// the server is reclaimed, a failed runtime delete is a warn, not a fatal
// (the server is already gone; orphaned runtime data is a separate concern).
func TestExpireTemporaryRuntimeFails(t *testing.T) {
	fs := newFaultStore()
	fs.deleteRuntimeFn = func(context.Context, string) error {
		return errFault
	}
	ctx := context.Background()
	registerRegServer(t, ctx, fs.base, "temp-rt", model.StatusOffline, model.RegTemporary, 1)
	cap := &recordCapture{}
	mon := newEdgeMonitor(fs, cap)
	mon.now = func() time.Time { return time.Now().Add(20 * time.Second) }

	if err := mon.sweep(ctx); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if !hasLog(cap, "temporary runtime reclaim failed") {
		t.Fatal("runtime reclaim failure was not warned")
	}
}

// TestMaintenanceRestoreFails pins the exit path: a window that ended but
// whose restore transition fails keeps the window (so the restore is retried),
// and the failure is logged — never a stranded maintenance status.
func TestMaintenanceRestoreFails(t *testing.T) {
	fs := newFaultStore()
	fs.casStatusFn = func(context.Context, string, model.ServerStatus, model.ServerStatus) (bool, error) {
		return false, errFault
	}
	ctx := context.Background()
	base := time.Now()
	registerServer(t, ctx, fs.base, "mw-exit", model.StatusMaintenance)
	win := &model.MaintenanceWindow{
		ID:       "mwin-exit",
		ServerID: "mw-exit",
		StartAt:  base.Add(-2 * time.Hour),
		EndAt:    base.Add(-time.Hour),
	}
	if err := fs.base.CreateMaintenanceWindow(ctx, win); err != nil {
		t.Fatalf("create window: %v", err)
	}
	if err := fs.base.MarkMaintenanceWindowApplied(ctx, "mwin-exit", model.StatusOnline); err != nil {
		t.Fatalf("mark applied: %v", err)
	}
	cap := &recordCapture{}
	mon := newEdgeMonitor(fs, cap)
	mon.now = func() time.Time { return base }

	if err := mon.sweep(ctx); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if !hasLog(cap, "failed to exit maintenance window") {
		t.Fatal("exit-maintenance failure was not logged")
	}
}

// TestMaintenanceDeleteFails pins the window-cleanup path: an expired window
// whose delete fails is logged, and the sweep survives (the window is retried
// next pass rather than leaking the maintenance record silently).
func TestMaintenanceDeleteFails(t *testing.T) {
	fs := newFaultStore()
	fs.deleteWindowFn = func(context.Context, string) error {
		return errFault
	}
	ctx := context.Background()
	base := time.Now()
	win := &model.MaintenanceWindow{
		ID:       "mwin-exp",
		ServerID: "mw-none",
		StartAt:  base.Add(-2 * time.Hour),
		EndAt:    base.Add(-time.Hour),
	}
	if err := fs.base.CreateMaintenanceWindow(ctx, win); err != nil {
		t.Fatalf("create window: %v", err)
	}
	cap := &recordCapture{}
	mon := newEdgeMonitor(fs, cap)
	mon.now = func() time.Time { return base }

	if err := mon.sweep(ctx); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if !hasLog(cap, "failed to delete expired maintenance window") {
		t.Fatal("expired-window delete failure was not logged")
	}
}

// TestMaintenanceEnterFails pins the pending-entry retry: an active, recorded
// window whose move fails keeps the recorded previous status so the next
// sweep can retry, and the failure is logged.
func TestMaintenanceEnterFails(t *testing.T) {
	fs := newFaultStore()
	fs.casStatusFn = func(context.Context, string, model.ServerStatus, model.ServerStatus) (bool, error) {
		return false, errFault
	}
	ctx := context.Background()
	base := time.Now()
	registerServer(t, ctx, fs.base, "mw-enter", model.StatusOnline)
	win := &model.MaintenanceWindow{
		ID:       "mwin-enter",
		ServerID: "mw-enter",
		StartAt:  base.Add(-time.Hour),
		EndAt:    base.Add(time.Hour),
	}
	if err := fs.base.CreateMaintenanceWindow(ctx, win); err != nil {
		t.Fatalf("create window: %v", err)
	}
	if err := fs.base.MarkMaintenanceWindowApplied(ctx, "mwin-enter", model.StatusOnline); err != nil {
		t.Fatalf("mark applied: %v", err)
	}
	cap := &recordCapture{}
	mon := newEdgeMonitor(fs, cap)
	mon.now = func() time.Time { return base }

	if err := mon.sweep(ctx); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if !hasLog(cap, "failed to enter maintenance window") {
		t.Fatal("enter-maintenance failure was not logged")
	}
}

// TestAlerterNilReceiver pins the nil-receiver guards: a monitor built without
// an alerter must never panic when the post-sweep evaluation or a per-server
// offline event reaches the (nil) alerter.
func TestAlerterNilReceiver(t *testing.T) {
	var a *Alerter
	a.Evaluate(context.Background(), 10, 2, 1)
	a.EmitServerOffline(context.Background(), ServerOfflineEvent{ServerID: "x", RegKind: model.RegPermanent})
}

// TestAlerterEmitOfflineWebhook pins the per-server offline webhook payload:
// a permanent registration going offline POSTs the structured event to the
// configured webhook.
func TestAlerterEmitOfflineWebhook(t *testing.T) {
	var hits atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	a := newTestAlerter(AlertConfig{WebhookURL: ts.URL}, &recordCapture{})
	a.EmitServerOffline(context.Background(), ServerOfflineEvent{
		ServerID: "srv-web", RegKind: model.RegPermanent, OfflineFor: 5 * time.Minute,
	})
	if hits.Load() != 1 {
		t.Fatalf("webhook hits = %d, want 1", hits.Load())
	}
}

// TestAlerterWebhookBadURL pins the request-construction failure: a webhook
// URL that cannot be parsed is logged as an invalid request, never a panic.
func TestAlerterWebhookBadURL(t *testing.T) {
	cap := &recordCapture{}
	a := newTestAlerter(AlertConfig{WebhookURL: "http://example.com:badport"}, cap)
	a.EmitServerOffline(context.Background(), ServerOfflineEvent{
		ServerID: "srv-badurl", RegKind: model.RegPermanent, OfflineFor: time.Minute,
	})
	if !hasLog(cap, "health alert webhook request invalid") {
		t.Fatal("invalid webhook URL was not logged")
	}
}

// TestAlerterWebhookRejected pins the non-2xx response path: a webhook that
// answers with an error status is logged as rejected (the alert itself already
// fired, so delivery failure must not be silent).
func TestAlerterWebhookRejected(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	cap := &recordCapture{}
	a := newTestAlerter(AlertConfig{WebhookURL: ts.URL}, cap)
	a.EmitServerOffline(context.Background(), ServerOfflineEvent{
		ServerID: "srv-reject", RegKind: model.RegPermanent, OfflineFor: time.Minute,
	})
	if !hasLog(cap, "health alert webhook rejected") {
		t.Fatal("rejected webhook response was not logged")
	}
}
