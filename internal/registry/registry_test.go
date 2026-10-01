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