package registry

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"testing"

	"github.com/cuihairu/atlas/internal/model"
	"github.com/cuihairu/atlas/internal/store/memory"
)

func newTestService() *Service {
	mem := memory.New()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	return New(mem, mem, logger)
}

func testRequest() RegisterRequest {
	return RegisterRequest{
		ID:       "game-1001",
		Name:     "Test Server",
		Type:     "game",
		Region:   "cn-east",
		Version:  "1.0.0",
		Platform: "android",
		Endpoint: model.Endpoint{Host: "10.0.0.1", Port: 30001},
		Capacity: 2000,
	}
}

func TestRegister(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	srv, err := svc.Register(ctx, testRequest())
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if srv.ID != "game-1001" {
		t.Errorf("expected ID game-1001, got %s", srv.ID)
	}
	if srv.Status != model.StatusStarting {
		t.Errorf("expected status starting, got %s", srv.Status)
	}
}

func TestRegisterIdempotent(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	req := testRequest()
	if _, err := svc.Register(ctx, req); err != nil {
		t.Fatalf("first Register: %v", err)
	}

	// Second registration should succeed (idempotent).
	req.Name = "Updated Name"
	srv, err := svc.Register(ctx, req)
	if err != nil {
		t.Fatalf("second Register: %v", err)
	}
	if srv.Name != "Updated Name" {
		t.Errorf("expected name 'Updated Name', got %q", srv.Name)
	}
}

func TestHeartbeat(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	if _, err := svc.Register(ctx, testRequest()); err != nil {
		t.Fatalf("Register: %v", err)
	}

	// Heartbeat should succeed and promote to online.
	hb := model.Heartbeat{Players: 100, Load: 0.5}
	if _, err := svc.Heartbeat(ctx, "game-1001", hb); err != nil {
		t.Fatalf("Heartbeat: %v", err)
	}

	// Verify the server was promoted to online.
	srv, err := svc.servers.GetServer(ctx, "game-1001")
	if err != nil {
		t.Fatalf("GetServer: %v", err)
	}
	if srv.Status != model.StatusOnline {
		t.Errorf("expected status online after heartbeat, got %s", srv.Status)
	}
}

func TestHeartbeatNotFound(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	hb := model.Heartbeat{Players: 100, Load: 0.5}
	_, err := svc.Heartbeat(ctx, "nonexistent", hb)
	if !errors.Is(err, model.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got: %v", err)
	}
}

func TestUnregister(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	if _, err := svc.Register(ctx, testRequest()); err != nil {
		t.Fatalf("Register: %v", err)
	}

	if err := svc.Unregister(ctx, "game-1001"); err != nil {
		t.Fatalf("Unregister: %v", err)
	}

	// Server should be offline.
	srv, err := svc.servers.GetServer(ctx, "game-1001")
	if err != nil {
		t.Fatalf("GetServer: %v", err)
	}
	if srv.Status != model.StatusOffline {
		t.Errorf("expected status offline after unregister, got %s", srv.Status)
	}
}

func TestUnregisterNotFound(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	err := svc.Unregister(ctx, "nonexistent")
	if !errors.Is(err, model.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got: %v", err)
	}
}
func TestHeartbeatValidation(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()
	if _, err := svc.Register(ctx, testRequest()); err != nil {
		t.Fatalf("Register: %v", err)
	}

	for name, hb := range map[string]model.Heartbeat{
		"negative players": {Players: -1, Load: 0.5},
		"load above one":   {Players: 10, Load: 1.5},
		"unknown status":   {Players: 10, Load: 0.5, Status: "warp"},
	} {
		if _, err := svc.Heartbeat(ctx, "game-1001", hb); !errors.Is(err, model.ErrInvalid) {
			t.Errorf("%s: expected ErrInvalid, got %v", name, err)
		}
	}
}

func TestHeartbeatExplicitStatus(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()
	if _, err := svc.Register(ctx, testRequest()); err != nil {
		t.Fatalf("Register: %v", err)
	}
	// First heartbeat promotes starting → online regardless of the reported
	// status; the lifecycle state machine stays authoritative afterwards too:
	// an explicit hb.Status lands in runtime data only, the reported status
	// keeps mirroring the server record.
	if _, err := svc.Heartbeat(ctx, "game-1001", model.Heartbeat{Players: 5, Load: 0.2}); err != nil {
		t.Fatalf("first Heartbeat: %v", err)
	}
	status, err := svc.Heartbeat(ctx, "game-1001", model.Heartbeat{Players: 5, Load: 0.2, Status: model.StatusMaintenance})
	if err != nil {
		t.Fatalf("Heartbeat: %v", err)
	}
	if status != model.StatusOnline {
		t.Errorf("expected reported status online (lifecycle wins), got %q", status)
	}

	rt, err := svc.runtime.GetRuntime(ctx, "game-1001")
	if err != nil {
		t.Fatalf("GetRuntime: %v", err)
	}
	if rt.Status != model.StatusMaintenance {
		t.Errorf("expected runtime status maintenance, got %q", rt.Status)
	}
}

func TestRegisterConfigManagedConflict(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	mem := svc.servers.(*memory.Store)
	if err := mem.RegisterServer(ctx, &model.Server{
		ID:       "game-1001",
		Name:     "Declared",
		Region:   "cn-east",
		Endpoint: model.Endpoint{Host: "10.0.0.9", Port: 30009},
		Status:   model.StatusOnline,
		Source:   "config",
	}); err != nil {
		t.Fatalf("seed config server: %v", err)
	}

	_, err := svc.Register(ctx, testRequest())
	if !errors.Is(err, model.ErrConflict) {
		t.Fatalf("expected ErrConflict for config-managed server, got %v", err)
	}
}

func TestHeartbeatConfigReentry(t *testing.T) {
	for _, stale := range []model.ServerStatus{model.StatusSuspect, model.StatusOffline} {
		svc := newTestService()
		ctx := context.Background()

		mem := svc.servers.(*memory.Store)
		if err := mem.RegisterServer(ctx, &model.Server{
			ID:       "game-cfg",
			Name:     "Declared",
			Region:   "cn-east",
			Endpoint: model.Endpoint{Host: "10.0.0.9", Port: 30009},
			Status:   stale,
			Source:   "config",
		}); err != nil {
			t.Fatalf("seed config server (%s): %v", stale, err)
		}

		status, err := svc.Heartbeat(ctx, "game-cfg", model.Heartbeat{Players: 3, Load: 0.1})
		if err != nil {
			t.Fatalf("Heartbeat from %s: %v", stale, err)
		}
		if status != model.StatusOnline {
			t.Errorf("heartbeat from %s: expected status online, got %q", stale, status)
		}

		srv, err := svc.servers.GetServer(ctx, "game-cfg")
		if err != nil {
			t.Fatalf("GetServer: %v", err)
		}
		if srv.Status != model.StatusOnline {
			t.Errorf("expected persisted status online, got %q", srv.Status)
		}
	}
}
