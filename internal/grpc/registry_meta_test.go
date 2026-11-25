package grpc

import (
	"context"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/cuihairu/atlas/api/pb"
	"github.com/cuihairu/atlas/internal/model"
)

// TestRegister_GrcpMetaFields pins the gRPC RegisterRequest parity fields
// that mirror registry.RegisterRequest (the REST register body): notify
// declaration, runtime seeding (players / started_at) and lifecycle class.
// A dropped mapping here silently narrows the gRPC surface relative to REST,
// which is the exact regression guarded by todo.md's "gRPC RegisterRequest subset" item.
func TestRegister_GrpcMetaFields(t *testing.T) {
	c := newTestConn(t)
	ctx := context.Background()

	started := time.Now().Add(-2 * time.Hour).UTC().Truncate(time.Second)
	_, err := c.Registry.Register(ctx, &pb.RegisterRequest{
		ServerId:          "gmeta-1",
		Name:              "Meta Server",
		Type:              "game",
		Region:            "cn-east",
		Version:           "1.0.0",
		Platform:          "android",
		Endpoint:          &pb.Endpoint{Host: "10.0.0.1", Port: 30001},
		Capacity:          1000,
		NotifyMode:        "callback",
		NotifyCallbackUrl: "https://cfg.example.com/hook",
		Players:           137,
		StartedAt:         started.Format(time.RFC3339),
		RegKind:           model.RegTemporary,
		TtlSeconds:        3600,
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	srv, err := c.Mem.GetServer(ctx, "gmeta-1")
	if err != nil {
		t.Fatalf("GetServer: %v", err)
	}
	if srv.NotifyMode != "callback" {
		t.Errorf("NotifyMode = %q, want callback", srv.NotifyMode)
	}
	if srv.NotifyCallbackURL != "https://cfg.example.com/hook" {
		t.Errorf("NotifyCallbackURL = %q, want https://cfg.example.com/hook", srv.NotifyCallbackURL)
	}
	if srv.RegKind != model.RegTemporary {
		t.Errorf("RegKind = %q, want %q", srv.RegKind, model.RegTemporary)
	}
	if srv.RegTTLSeconds != 3600 {
		t.Errorf("RegTTLSeconds = %d, want 3600", srv.RegTTLSeconds)
	}
	if srv.StartedAt == nil || !srv.StartedAt.Equal(started) {
		t.Errorf("StartedAt = %v, want %v", srv.StartedAt, started)
	}

	rt, err := c.Mem.GetRuntime(ctx, "gmeta-1")
	if err != nil {
		t.Fatalf("GetRuntime: %v", err)
	}
	if rt.Players != 137 {
		t.Errorf("runtime players = %d, want 137 (seeded from register)", rt.Players)
	}
}

// TestRegister_InvalidStartedAt pins the started_at parsing: a malformed
// RFC 3339 value is INVALID_ARGUMENT, not silently dropped.
func TestRegister_InvalidStartedAt(t *testing.T) {
	c := newTestConn(t)
	ctx := context.Background()

	_, err := c.Registry.Register(ctx, &pb.RegisterRequest{
		ServerId:  "gmeta-bad",
		Name:      "Bad",
		Type:      "game",
		Region:    "cn-east",
		Endpoint:  &pb.Endpoint{Host: "10.0.0.1", Port: 30001},
		Capacity:  100,
		StartedAt: "not-a-time",
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("Register with bad started_at: code = %v, want %v (err=%v)", status.Code(err), codes.InvalidArgument, err)
	}
}

// TestRegister_DefaultStartedAt pins that an empty started_at leaves the
// service to default it to registration time (REST parity: zero means now).
func TestRegister_DefaultStartedAt(t *testing.T) {
	c := newTestConn(t)
	ctx := context.Background()

	before := time.Now().Add(-time.Minute)
	if _, err := c.Registry.Register(ctx, &pb.RegisterRequest{
		ServerId: "gmeta-def",
		Name:     "Default",
		Type:     "game",
		Region:   "cn-east",
		Endpoint: &pb.Endpoint{Host: "10.0.0.1", Port: 30001},
		Capacity: 100,
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	srv, err := c.Mem.GetServer(ctx, "gmeta-def")
	if err != nil {
		t.Fatalf("GetServer: %v", err)
	}
	if srv.StartedAt == nil {
		t.Fatal("StartedAt = nil, want defaulted to registration time")
	}
	if srv.StartedAt.Before(before) {
		t.Errorf("StartedAt = %v, want >= %v (defaulted to now)", srv.StartedAt, before)
	}
}
