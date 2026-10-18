package health

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	atlasmetrics "github.com/cuihairu/atlas/internal/metrics"
	"github.com/cuihairu/atlas/internal/model"
	"github.com/cuihairu/atlas/internal/store"
	"github.com/cuihairu/atlas/internal/store/memory"
)

// raceInjector runs hook just before every guarded status write — the seam
// where a concurrent writer lands between the sweep's list and its write.
type raceInjector struct {
	store.Store
	hook func()
}

func (s *raceInjector) UpdateServerStatusIf(ctx context.Context, id string, from, to model.ServerStatus) (bool, error) {
	if s.hook != nil {
		s.hook()
	}
	return s.Store.UpdateServerStatusIf(ctx, id, from, to)
}

// flakyCAS fails the first guarded status write with a store error, then
// delegates normally.
type flakyCAS struct {
	store.Store
	boom error
	used bool
}

func (s *flakyCAS) UpdateServerStatusIf(ctx context.Context, id string, from, to model.ServerStatus) (bool, error) {
	if !s.used {
		s.used = true
		return false, s.boom
	}
	return s.Store.UpdateServerStatusIf(ctx, id, from, to)
}

// flakyMark fails the first MarkMaintenanceWindowApplied, then delegates.
type flakyMark struct {
	store.Store
	boom error
	used bool
}

func (s *flakyMark) MarkMaintenanceWindowApplied(ctx context.Context, id string, previous model.ServerStatus) error {
	if !s.used {
		s.used = true
		return s.boom
	}
	return s.Store.MarkMaintenanceWindowApplied(ctx, id, previous)
}

// TestSweep_RacingWriterBeatsStaleOffline pins the compare-and-set guard on
// the sweep's status writes: the monitor decides on the snapshot it listed,
// and a writer landing between that read and the write must win. The
// unguarded write used to resurrect offline from a heartbeat that no longer
// existed — e.g. knocking a freshly re-registered server straight back out
// of the rotation.
func TestSweep_RacingWriterBeatsStaleOffline(t *testing.T) {
	mem := memory.New()
	ctx := context.Background()

	raced := false
	wrapped := &raceInjector{Store: mem, hook: func() {
		if raced {
			return
		}
		raced = true
		// A concurrent writer moves the server between the sweep's list
		// and its guarded write — e.g. a re-registration resetting the
		// dead-ish lifecycle to starting, simulated here with the same
		// store-level write the registry performs.
		if err := mem.UpdateServerStatus(ctx, "srv-1", model.StatusStarting); err != nil {
			t.Fatalf("racing write: %v", err)
		}
	}}

	mon := newTestMonitor(wrapped, 30*time.Second, 60*time.Second)
	registerServer(t, ctx, mem, "srv-1", model.StatusOnline)
	recordHeartbeat(t, ctx, mem, "srv-1", 100)

	heartbeatTime := time.Now()
	mon.now = func() time.Time { return heartbeatTime.Add(65 * time.Second) }
	if err := mon.sweep(ctx); err != nil {
		t.Fatalf("sweep: %v", err)
	}

	if got := statusOf(t, ctx, mem, "srv-1"); got != model.StatusStarting {
		t.Fatalf("status after raced sweep = %s, want starting (racing writer wins)", got)
	}
}

// TestMaintenanceWindow_MarkFailureKeepsServerUntouched pins the
// record-first ordering of window entry: the status to restore is
// persisted BEFORE the server is moved. When the mark write fails, the
// server must be untouched and the window unapplied — the old
// move-then-record order stranded the server in maintenance with an empty
// previous status, leaving window end with nothing to restore.
func TestMaintenanceWindow_MarkFailureKeepsServerUntouched(t *testing.T) {
	mem := memory.New()
	ctx := context.Background()
	wrapped := &flakyMark{Store: mem, boom: errors.New("mark failed")}
	mon := newTestMonitor(wrapped, 30*time.Second, 60*time.Second)

	registerServer(t, ctx, mem, "srv-1", model.StatusOnline)

	base := time.Now()
	win := &model.MaintenanceWindow{
		ID:       "mwin-1",
		ServerID: "srv-1",
		StartAt:  base.Add(-time.Minute),
		EndAt:    base.Add(time.Hour),
	}
	if err := mem.CreateMaintenanceWindow(ctx, win); err != nil {
		t.Fatalf("create window: %v", err)
	}

	// Sweep 1: the mark write fails — server untouched, window unapplied.
	applyWindowAt(t, mon, ctx, base)
	if got := statusOf(t, ctx, mem, "srv-1"); got != model.StatusOnline {
		t.Fatalf("status after failed mark = %s, want online (server untouched)", got)
	}
	w, err := mem.GetMaintenanceWindow(ctx, "mwin-1")
	if err != nil {
		t.Fatalf("window must survive a failed mark: %v", err)
	}
	if w.PreviousStatus != "" {
		t.Fatalf("previous status after failed mark = %q, want empty", w.PreviousStatus)
	}

	// Sweep 2: the mark succeeds and the move lands with the record.
	applyWindowAt(t, mon, ctx, base)
	if got := statusOf(t, ctx, mem, "srv-1"); got != model.StatusMaintenance {
		t.Fatalf("status on retry = %s, want maintenance", got)
	}
	if w, _ = mem.GetMaintenanceWindow(ctx, "mwin-1"); w.PreviousStatus != model.StatusOnline {
		t.Fatalf("previous status = %q, want online", w.PreviousStatus)
	}

	// Window over → restored.
	applyWindowAt(t, mon, ctx, base.Add(2*time.Hour))
	if got := statusOf(t, ctx, mem, "srv-1"); got != model.StatusOnline {
		t.Fatalf("post-window status = %s, want online (restored)", got)
	}
}

// TestMaintenanceWindow_EntryMoveRetried pins recovery from the other half
// of a torn window entry: the mark succeeded but the move to maintenance
// failed. The next sweep must retry the move off the recorded previous
// status — and window end must still restore it.
func TestMaintenanceWindow_EntryMoveRetried(t *testing.T) {
	mem := memory.New()
	ctx := context.Background()
	wrapped := &flakyCAS{Store: mem, boom: errors.New("move failed")}
	mon := newTestMonitor(wrapped, 30*time.Second, 60*time.Second)

	registerServer(t, ctx, mem, "srv-1", model.StatusOnline)

	base := time.Now()
	win := &model.MaintenanceWindow{
		ID:       "mwin-1",
		ServerID: "srv-1",
		StartAt:  base.Add(-time.Minute),
		EndAt:    base.Add(time.Hour),
	}
	if err := mem.CreateMaintenanceWindow(ctx, win); err != nil {
		t.Fatalf("create window: %v", err)
	}

	// Sweep 1: the move fails, but the window is recorded — recovery is
	// now possible because the previous status is on the window, not lost.
	applyWindowAt(t, mon, ctx, base)
	if got := statusOf(t, ctx, mem, "srv-1"); got != model.StatusOnline {
		t.Fatalf("status after failed move = %s, want online", got)
	}
	w, err := mem.GetMaintenanceWindow(ctx, "mwin-1")
	if err != nil {
		t.Fatalf("window must survive a failed move: %v", err)
	}
	if w.PreviousStatus != model.StatusOnline {
		t.Fatalf("previous status after failed move = %q, want online (recorded anyway)", w.PreviousStatus)
	}

	// Sweep 2: the pending-entry retry moves the server.
	applyWindowAt(t, mon, ctx, base.Add(time.Second))
	if got := statusOf(t, ctx, mem, "srv-1"); got != model.StatusMaintenance {
		t.Fatalf("status after retry = %s, want maintenance", got)
	}

	// Window over → restored.
	applyWindowAt(t, mon, ctx, base.Add(2*time.Hour))
	if got := statusOf(t, ctx, mem, "srv-1"); got != model.StatusOnline {
		t.Fatalf("post-window status = %s, want online (restored)", got)
	}
}

// TestMaintenanceWindow_RestoreRaceConsumesWindow pins the guarded restore
// at window end: when the live record moved on between the sweep's list
// and the restore write (another writer intervened), the window is
// consumed instead of being retried forever, and the newer status stands.
func TestMaintenanceWindow_RestoreRaceConsumesWindow(t *testing.T) {
	mem := memory.New()
	ctx := context.Background()

	entryDone := false
	fired := false
	wrapped := &raceInjector{Store: mem, hook: func() {
		if !entryDone || fired {
			return
		}
		fired = true
		// Another writer moves the server right before the guarded
		// restore, so the restore's compare-and-set must miss.
		if err := mem.UpdateServerStatus(ctx, "srv-1", model.StatusDraining); err != nil {
			t.Fatalf("racing write: %v", err)
		}
	}}

	mon := newTestMonitor(wrapped, 30*time.Second, 60*time.Second)
	registerServer(t, ctx, mem, "srv-1", model.StatusOnline)

	base := time.Now()
	win := &model.MaintenanceWindow{
		ID:       "mwin-1",
		ServerID: "srv-1",
		StartAt:  base.Add(-time.Minute),
		EndAt:    base.Add(time.Hour),
	}
	if err := mem.CreateMaintenanceWindow(ctx, win); err != nil {
		t.Fatalf("create window: %v", err)
	}

	// Enter the window cleanly, then arm the racer for the end-of-window
	// restore.
	applyWindowAt(t, mon, ctx, base)
	if got := statusOf(t, ctx, mem, "srv-1"); got != model.StatusMaintenance {
		t.Fatalf("in-window status = %s, want maintenance", got)
	}
	entryDone = true

	// Window over: the restore loses the race, the window is consumed.
	applyWindowAt(t, mon, ctx, base.Add(2*time.Hour))
	if got := statusOf(t, ctx, mem, "srv-1"); got != model.StatusDraining {
		t.Fatalf("post-window status = %s, want draining (racing writer wins)", got)
	}
	if _, err := mem.GetMaintenanceWindow(ctx, "mwin-1"); err == nil {
		t.Fatal("window must be consumed after a raced restore, not retried forever")
	}
}

func TestSweep_FailedStatusWriteDoesNotCountTransition(t *testing.T) {
	mem := memory.New()
	ctx := context.Background()
	wrapped := &flakyCAS{Store: mem, boom: errors.New("status write failed")}
	mm := atlasmetrics.New(mem)
	mon := newTestMonitor(wrapped, 30*time.Second, 60*time.Second).WithMetrics(mm)

	registerServer(t, ctx, mem, "srv-1", model.StatusOnline)
	recordHeartbeat(t, ctx, mem, "srv-1", 100)
	heartbeatTime := time.Now()
	mon.now = func() time.Time { return heartbeatTime.Add(65 * time.Second) }
	if err := mon.sweep(ctx); err != nil {
		t.Fatalf("sweep: %v", err)
	}

	if got := statusOf(t, ctx, mem, "srv-1"); got != model.StatusOnline {
		t.Fatalf("status after failed write = %s, want online", got)
	}
	response := httptest.NewRecorder()
	mm.Handler().ServeHTTP(response, httptest.NewRequest("GET", "/metrics", nil))
	if strings.Contains(response.Body.String(), `atlas_health_transitions_total{from="online",to="offline"}`) {
		t.Fatalf("failed status write was counted as a health transition:\n%s", response.Body.String())
	}
}
