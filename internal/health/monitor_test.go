package health

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/cuihairu/atlas/internal/model"
	"github.com/cuihairu/atlas/internal/store"
	"github.com/cuihairu/atlas/internal/store/memory"
)

func newTestMonitor(mem store.Store, suspectAfter, offlineAfter time.Duration) *Monitor {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	return New(mem, suspectAfter, offlineAfter, time.Hour, logger) // interval doesn't matter for sweep tests
}

func registerServer(t *testing.T, ctx context.Context, mem *memory.Store, id string, status model.ServerStatus) {
	t.Helper()
	srv := &model.Server{
		ID:       id,
		Name:     id,
		Type:     "game",
		Region:   "cn-east",
		Version:  "1.0.0",
		Platform: "android",
		Endpoint: model.Endpoint{Host: "10.0.0.1", Port: 30001},
		Capacity: 2000,
		Status:   status,
	}
	if err := mem.RegisterServer(ctx, srv); err != nil {
		t.Fatalf("register server %s: %v", id, err)
	}
}

func recordHeartbeat(t *testing.T, ctx context.Context, mem *memory.Store, id string, players int) {
	t.Helper()
	hb := model.Heartbeat{Players: players, Load: 0.5, Status: model.StatusOnline}
	if err := mem.RecordHeartbeat(ctx, id, hb); err != nil {
		t.Fatalf("record heartbeat for %s: %v", id, err)
	}
}

func setRuntimeAge(mem *memory.Store, id string, age time.Duration) {
	// The memory store uses time.Now() internally, so we need to work around
	// this. We'll delete and re-add with a synthetic last-seen-at.
	// Actually, the memory store sets LastSeenAt = time.Now() on RecordHeartbeat.
	// For testing, we can manipulate the runtime by writing directly through the store.
	// The cleanest approach: just test with the monitor's now() function.
	//
	// Since we control `now`, we set it far in the future relative to the
	// heartbeat's LastSeenAt (which is time.Now() at record time).
}

func TestSweep_OnlineToSuspect(t *testing.T) {
	mem := memory.New()
	ctx := context.Background()
	suspectAfter := 30 * time.Second
	offlineAfter := 60 * time.Second

	mon := newTestMonitor(mem, suspectAfter, offlineAfter)

	registerServer(t, ctx, mem, "srv-1", model.StatusOnline)
	recordHeartbeat(t, ctx, mem, "srv-1", 100)

	// Set now to 35 seconds in the future (past suspect threshold).
	heartbeatTime := time.Now()
	mon.now = func() time.Time { return heartbeatTime.Add(35 * time.Second) }

	if err := mon.sweep(ctx); err != nil {
		t.Fatalf("sweep: %v", err)
	}

	srv, err := mem.GetServer(ctx, "srv-1")
	if err != nil {
		t.Fatalf("GetServer: %v", err)
	}
	if srv.Status != model.StatusSuspect {
		t.Errorf("expected suspect, got %s", srv.Status)
	}
}

func TestSweep_OnlineToOffline(t *testing.T) {
	mem := memory.New()
	ctx := context.Background()

	mon := newTestMonitor(mem, 30*time.Second, 60*time.Second)

	registerServer(t, ctx, mem, "srv-1", model.StatusOnline)
	recordHeartbeat(t, ctx, mem, "srv-1", 100)

	heartbeatTime := time.Now()
	mon.now = func() time.Time { return heartbeatTime.Add(65 * time.Second) }

	if err := mon.sweep(ctx); err != nil {
		t.Fatalf("sweep: %v", err)
	}

	srv, err := mem.GetServer(ctx, "srv-1")
	if err != nil {
		t.Fatalf("GetServer: %v", err)
	}
	if srv.Status != model.StatusOffline {
		t.Errorf("expected offline, got %s", srv.Status)
	}
}

func TestSweep_SuspectRecovered(t *testing.T) {
	mem := memory.New()
	ctx := context.Background()

	mon := newTestMonitor(mem, 30*time.Second, 60*time.Second)

	registerServer(t, ctx, mem, "srv-1", model.StatusSuspect)
	// Record a recent heartbeat (only 5 seconds ago).
	recordHeartbeat(t, ctx, mem, "srv-1", 100)

	heartbeatTime := time.Now()
	mon.now = func() time.Time { return heartbeatTime.Add(5 * time.Second) }

	if err := mon.sweep(ctx); err != nil {
		t.Fatalf("sweep: %v", err)
	}

	srv, err := mem.GetServer(ctx, "srv-1")
	if err != nil {
		t.Fatalf("GetServer: %v", err)
	}
	if srv.Status != model.StatusOnline {
		t.Errorf("expected online (recovered), got %s", srv.Status)
	}
}

func TestSweep_SuspectToOffline(t *testing.T) {
	mem := memory.New()
	ctx := context.Background()

	mon := newTestMonitor(mem, 30*time.Second, 60*time.Second)

	registerServer(t, ctx, mem, "srv-1", model.StatusSuspect)
	recordHeartbeat(t, ctx, mem, "srv-1", 100)

	heartbeatTime := time.Now()
	mon.now = func() time.Time { return heartbeatTime.Add(65 * time.Second) }

	if err := mon.sweep(ctx); err != nil {
		t.Fatalf("sweep: %v", err)
	}

	srv, err := mem.GetServer(ctx, "srv-1")
	if err != nil {
		t.Fatalf("GetServer: %v", err)
	}
	if srv.Status != model.StatusOffline {
		t.Errorf("expected offline, got %s", srv.Status)
	}
}

func TestSweep_MaintenanceNotTouched(t *testing.T) {
	mem := memory.New()
	ctx := context.Background()

	mon := newTestMonitor(mem, 30*time.Second, 60*time.Second)

	registerServer(t, ctx, mem, "srv-1", model.StatusMaintenance)
	recordHeartbeat(t, ctx, mem, "srv-1", 100)

	heartbeatTime := time.Now()
	mon.now = func() time.Time { return heartbeatTime.Add(5 * time.Minute) }

	if err := mon.sweep(ctx); err != nil {
		t.Fatalf("sweep: %v", err)
	}

	srv, err := mem.GetServer(ctx, "srv-1")
	if err != nil {
		t.Fatalf("GetServer: %v", err)
	}
	if srv.Status != model.StatusMaintenance {
		t.Errorf("expected maintenance (unchanged), got %s", srv.Status)
	}
}

func TestSweep_DisabledNotTouched(t *testing.T) {
	mem := memory.New()
	ctx := context.Background()

	mon := newTestMonitor(mem, 30*time.Second, 60*time.Second)

	registerServer(t, ctx, mem, "srv-1", model.StatusDisabled)
	recordHeartbeat(t, ctx, mem, "srv-1", 100)

	heartbeatTime := time.Now()
	mon.now = func() time.Time { return heartbeatTime.Add(5 * time.Minute) }

	if err := mon.sweep(ctx); err != nil {
		t.Fatalf("sweep: %v", err)
	}

	srv, err := mem.GetServer(ctx, "srv-1")
	if err != nil {
		t.Fatalf("GetServer: %v", err)
	}
	if srv.Status != model.StatusDisabled {
		t.Errorf("expected disabled (unchanged), got %s", srv.Status)
	}
}