package health

import (
	"context"
	"testing"
	"time"

	"github.com/cuihairu/atlas/internal/model"
	"github.com/cuihairu/atlas/internal/store/memory"
)

// applyWindowAt runs one sweep with the monitor clock pinned at now.
func applyWindowAt(t *testing.T, mon *Monitor, ctx context.Context, at time.Time) {
	t.Helper()
	mon.now = func() time.Time { return at }
	if err := mon.sweep(ctx); err != nil {
		t.Fatalf("sweep: %v", err)
	}
}

func statusOf(t *testing.T, ctx context.Context, mem *memory.Store, id string) model.ServerStatus {
	t.Helper()
	srv, err := mem.GetServer(ctx, id)
	if err != nil {
		t.Fatalf("GetServer %s: %v", id, err)
	}
	return srv.Status
}

func TestMaintenanceWindow_AppliesAndRestores(t *testing.T) {
	mem := memory.New()
	ctx := context.Background()
	mon := newTestMonitor(mem, 30*time.Second, 60*time.Second)

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

	// Window open → server moves to maintenance, previous status recorded.
	applyWindowAt(t, mon, ctx, base)
	if got := statusOf(t, ctx, mem, "srv-1"); got != model.StatusMaintenance {
		t.Fatalf("in-window status = %s, want maintenance", got)
	}
	w, _ := mem.GetMaintenanceWindow(ctx, "mwin-1")
	if w.PreviousStatus != model.StatusOnline {
		t.Fatalf("previous status = %q, want online", w.PreviousStatus)
	}

	// Window over → status restored, window deleted.
	applyWindowAt(t, mon, ctx, base.Add(2*time.Hour))
	if got := statusOf(t, ctx, mem, "srv-1"); got != model.StatusOnline {
		t.Fatalf("post-window status = %s, want online (restored)", got)
	}
	if _, err := mem.GetMaintenanceWindow(ctx, "mwin-1"); err == nil {
		t.Fatal("expired window should be deleted")
	}
}

func TestMaintenanceWindow_ScheduledNotAppliedEarly(t *testing.T) {
	mem := memory.New()
	ctx := context.Background()
	mon := newTestMonitor(mem, 30*time.Second, 60*time.Second)

	registerServer(t, ctx, mem, "srv-1", model.StatusOnline)

	base := time.Now()
	win := &model.MaintenanceWindow{
		ID:       "mwin-future",
		ServerID: "srv-1",
		StartAt:  base.Add(time.Hour),
		EndAt:    base.Add(2 * time.Hour),
	}
	if err := mem.CreateMaintenanceWindow(ctx, win); err != nil {
		t.Fatalf("create window: %v", err)
	}

	// Before the window opens: nothing happens, window untouched.
	applyWindowAt(t, mon, ctx, base)
	if got := statusOf(t, ctx, mem, "srv-1"); got != model.StatusOnline {
		t.Fatalf("status = %s, want online before window opens", got)
	}
	if _, err := mem.GetMaintenanceWindow(ctx, "mwin-future"); err != nil {
		t.Fatalf("scheduled window should still exist: %v", err)
	}

	// Opening the window applies it.
	applyWindowAt(t, mon, ctx, base.Add(time.Hour+time.Minute))
	if got := statusOf(t, ctx, mem, "srv-1"); got != model.StatusMaintenance {
		t.Fatalf("status = %s, want maintenance once open", got)
	}
}

func TestMaintenanceWindow_OperatorMoveWins(t *testing.T) {
	mem := memory.New()
	ctx := context.Background()
	mon := newTestMonitor(mem, 30*time.Second, 60*time.Second)

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

	// Enter the window…
	applyWindowAt(t, mon, ctx, base)
	if got := statusOf(t, ctx, mem, "srv-1"); got != model.StatusMaintenance {
		t.Fatalf("in-window status = %s, want maintenance", got)
	}

	// …then an operator takes the server offline mid-window.
	if err := mem.UpdateServerStatus(ctx, "srv-1", model.StatusOffline); err != nil {
		t.Fatalf("operator disable: %v", err)
	}

	// Window end must NOT resurrect the server.
	applyWindowAt(t, mon, ctx, base.Add(2*time.Hour))
	if got := statusOf(t, ctx, mem, "srv-1"); got != model.StatusOffline {
		t.Fatalf("post-window status = %s, want offline (operator move wins)", got)
	}
}

func TestMaintenanceWindow_NonAutoManagedSkipped(t *testing.T) {
	mem := memory.New()
	ctx := context.Background()
	mon := newTestMonitor(mem, 30*time.Second, 60*time.Second)

	// A draining server and a disabled server with windows open.
	registerServer(t, ctx, mem, "srv-drain", model.StatusDraining)
	registerServer(t, ctx, mem, "srv-disabled", model.StatusDisabled)

	base := time.Now()
	for i, id := range []string{"srv-drain", "srv-disabled"} {
		win := &model.MaintenanceWindow{
			ID:       "mwin-" + id,
			ServerID: id,
			StartAt:  base.Add(-time.Minute),
			EndAt:    base.Add(time.Hour),
		}
		if err := mem.CreateMaintenanceWindow(ctx, win); err != nil {
			t.Fatalf("create window %d: %v", i, err)
		}
	}

	applyWindowAt(t, mon, ctx, base)

	if got := statusOf(t, ctx, mem, "srv-drain"); got != model.StatusDraining {
		t.Errorf("draining server = %s, want draining (operator-owned)", got)
	}
	if got := statusOf(t, ctx, mem, "srv-disabled"); got != model.StatusDisabled {
		t.Errorf("disabled server = %s, want disabled (operator-owned)", got)
	}
	// Windows are consumed either way (no per-sweep retries).
	for _, id := range []string{"srv-drain", "srv-disabled"} {
		w, err := mem.GetMaintenanceWindow(ctx, "mwin-"+id)
		if err != nil {
			t.Fatalf("window %s: %v", id, err)
		}
		if w.PreviousStatus != "" {
			t.Errorf("window %s previous = %q, want empty (nothing to restore)", id, w.PreviousStatus)
		}
	}
}
