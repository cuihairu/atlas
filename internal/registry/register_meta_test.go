package registry

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/cuihairu/atlas/internal/store/memory"
)

func newMetaService() (*Service, *memory.Store) {
	mem := memory.New()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	return New(mem, mem, logger), mem
}

// TestRegister_MetadataSeedsRuntime covers the v0.1.20 register metadata
// extension: initial players seed the runtime, and started_at is persisted
// (explicit) or defaulted to now.
func TestRegister_MetadataSeedsRuntime(t *testing.T) {
	svc, mem := newMetaService()
	ctx := context.Background()

	started := time.Now().Add(-2 * time.Hour)
	req := testRequest()
	req.Players = 137
	req.StartedAt = &started

	srv, err := svc.Register(ctx, req)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if srv.StartedAt == nil || !srv.StartedAt.Equal(started) {
		t.Errorf("StartedAt = %v, want %v", srv.StartedAt, started)
	}

	rt, err := mem.GetRuntime(ctx, req.ID)
	if err != nil {
		t.Fatalf("GetRuntime: %v", err)
	}
	if rt.Players != 137 {
		t.Errorf("runtime players = %d, want 137 (seeded from register)", rt.Players)
	}
}

func TestRegister_StartedAtDefaultsToNow(t *testing.T) {
	svc, _ := newMetaService()
	ctx := context.Background()

	before := time.Now()
	srv, err := svc.Register(ctx, testRequest())
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if srv.StartedAt == nil || srv.StartedAt.Before(before.Add(-time.Second)) {
		t.Errorf("StartedAt = %v, want ~now", srv.StartedAt)
	}
}

// Re-registration (process restart) refreshes started_at and re-seeds the
// runtime player count.
func TestReregister_RefreshesStartedAt(t *testing.T) {
	svc, mem := newMetaService()
	ctx := context.Background()

	first := time.Now().Add(-24 * time.Hour)
	req := testRequest()
	req.StartedAt = &first
	if _, err := svc.Register(ctx, req); err != nil {
		t.Fatalf("first register: %v", err)
	}

	second := time.Now()
	req.StartedAt = &second
	req.Players = 42
	srv, err := svc.Register(ctx, req)
	if err != nil {
		t.Fatalf("re-register: %v", err)
	}
	if srv.StartedAt == nil || !srv.StartedAt.Equal(second) {
		t.Errorf("StartedAt = %v, want refreshed %v", srv.StartedAt, second)
	}

	rt, _ := mem.GetRuntime(ctx, req.ID)
	if rt.Players != 42 {
		t.Errorf("players after re-register = %d, want 42", rt.Players)
	}
}
