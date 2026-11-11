package grpc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	pb "github.com/cuihairu/atlas/api/pb"
	atlashttpapi "github.com/cuihairu/atlas/internal/httpapi"
)

// entriesOf drains the ring through the same Recent view the REST
// GET /v1/admin/audit endpoint serves.
func entriesOf(t *testing.T, audit *atlashttpapi.AuditLog) []atlashttpapi.AuditEntry {
	t.Helper()
	return audit.Recent(0)
}

// invokeHandler runs the interceptor the way the server does: the request
// the handler would decode, plus a caller-supplied handler so tests can
// produce failing RPCs.
func invokeHandler(
	t *testing.T,
	ic grpc.UnaryServerInterceptor,
	method string,
	ctx context.Context,
	req any,
	handler grpc.UnaryHandler,
) (bool, error) {
	t.Helper()
	called := false
	_, err := ic(ctx, req, &grpc.UnaryServerInfo{FullMethod: method},
		func(c context.Context, r any) (any, error) {
			called = true
			return handler(c, r)
		})
	return called, err
}

func TestAuditHTTPStatusMapping(t *testing.T) {
	for code, want := range map[codes.Code]int{
		codes.OK:                 200,
		codes.InvalidArgument:    400,
		codes.NotFound:           404,
		codes.AlreadyExists:      409,
		codes.PermissionDenied:   403,
		codes.Unauthenticated:    401,
		codes.ResourceExhausted:  429,
		codes.FailedPrecondition: 400,
		codes.Unimplemented:      501,
		codes.Unavailable:        503,
		codes.DeadlineExceeded:   504,
		codes.Internal:           500,
		codes.Unknown:            500,
	} {
		if got := httpStatusOf(code); got != want {
			t.Errorf("httpStatusOf(%v) = %d, want %d", code, got, want)
		}
	}
}

// TestAuditInterceptor pins the field mapping: admin read RPC → GET without
// a diff, admin mutation → POST with the protojson request, handler errors
// → the REST-equivalent status, non-admin domains → no entry at all.
func TestAuditInterceptor(t *testing.T) {
	audit := atlashttpapi.NewAuditLog(100, 4096, testLogger())
	ic := UnaryAudit(audit)
	okHandler := func(c context.Context, req any) (any, error) { return "ok", nil }

	// Mutating admin RPC: POST with a protojson body.
	called, err := invokeHandler(t, ic, admMethod("SetMaintenance"),
		ctxWithPeer("10.0.0.1"),
		&pb.ServerIdRequest{ServerId: "game-1"}, okHandler)
	if !called || err != nil {
		t.Fatalf("mutating call: called=%v err=%v", called, err)
	}
	entries := entriesOf(t, audit)
	if len(entries) != 1 {
		t.Fatalf("want 1 entry, got %d", len(entries))
	}
	e := entries[0]
	if e.Method != "POST" || e.Status != 200 {
		t.Errorf("entry method/status = %s/%d, want POST/200", e.Method, e.Status)
	}
	if e.Path != admMethod("SetMaintenance") {
		t.Errorf("entry path = %s, want full method", e.Path)
	}
	if !strings.Contains(string(e.Body), "serverId") {
		t.Errorf("entry body missing protojson serverId: %s", e.Body)
	}
	if e.Actor != ":anonymous" {
		t.Errorf("actor without auth ctx = %q, want :anonymous", e.Actor)
	}

	// Read RPC: recorded like a GET, without a diff.
	if _, err := invokeHandler(t, ic, admMethod("GetStats"), context.Background(),
		&pb.GetStatsRequest{}, okHandler); err != nil {
		t.Fatalf("read call: %v", err)
	}
	entries = entriesOf(t, audit)
	if len(entries) != 2 {
		t.Fatalf("want 2 entries, got %d", len(entries))
	}
	if e := entries[1]; e.Method != "GET" || len(e.Body) != 0 {
		t.Errorf("read entry = %s body=%q, want GET without body", e.Method, e.Body)
	}

	// Handler failure: the REST-equivalent status lands in the entry.
	_, err = invokeHandler(t, ic, admMethod("Disable"), context.Background(),
		&pb.ServerIdRequest{ServerId: "game-1"},
		func(c context.Context, req any) (any, error) {
			return nil, status.Error(codes.NotFound, "not found")
		})
	if codeOf(t, err) != codes.NotFound {
		t.Fatalf("handler error swallowed: %v", err)
	}
	entries = entriesOf(t, audit)
	if e := entries[2]; e.Status != 404 {
		t.Errorf("failed call status = %d, want 404", e.Status)
	}

	// Public domain: the interceptor must not record discovery traffic.
	if _, err := invokeHandler(t, ic, discMethod("GetServer"), context.Background(),
		&pb.GetServerRequest{ServerId: "game-1"}, okHandler); err != nil {
		t.Fatalf("public call: %v", err)
	}
	if got := len(entriesOf(t, audit)); got != 3 {
		t.Errorf("public domain recorded %d entries, want still 3", got)
	}
}

// TestAuditActorFromAuthChain verifies the handoff: after UnaryAuth passes a
// call, the actor the audit layer reads carries the role and the same
// SHA-256 key fingerprint the REST ring uses for correlation.
func TestAuditActorFromAuthChain(t *testing.T) {
	key := "key-op"
	ic := UnaryAuth(AuthConfig{
		AdminKeys:  map[string]struct{}{key: {}},
		AdminRoles: map[string]string{key: "operator"},
	})
	ctx := ctxWithMD(ctxWithPeer("10.0.0.1"), "authorization", "Bearer "+key)
	var gotActor atlashttpapi.Actor
	_, err := ic(ctx, nil,
		&grpc.UnaryServerInfo{FullMethod: admMethod("Disable")},
		func(c context.Context, req any) (any, error) {
			gotActor = atlashttpapi.ActorFrom(c)
			return "ok", nil
		})
	if err != nil {
		t.Fatalf("auth rejected: %v", err)
	}
	sum := sha256.Sum256([]byte(key))
	wantFP := hex.EncodeToString(sum[:])[:12]
	if gotActor.Role != "operator" || gotActor.KeyFingerprint != wantFP {
		t.Errorf("actor = %s, want operator:%s", gotActor.String(), wantFP)
	}
}

func TestTruncateAuditBody(t *testing.T) {
	if got := truncateAuditBody([]byte(`{"a":1}`), 100); string(got) != `{"a":1}` {
		t.Errorf("small valid body mangled: %s", got)
	}
	// Truncation that breaks the JSON is wrapped as a string so the audit
	// entry itself always marshals — same rule as the REST middleware.
	got := truncateAuditBody([]byte(`{"serverId":"game-1","reason":"patch"`), 16)
	if !strings.HasPrefix(string(got), `"`) {
		t.Errorf("truncated JSON not string-wrapped: %s", got)
	}
}

// TestAuditEndToEnd drives the real bufconn server with auth + audit in the
// production order and reads the ring back like GET /v1/admin/audit would.
func TestAuditEndToEnd(t *testing.T) {
	opKey := "key-op"
	audit := atlashttpapi.NewAuditLog(100, 4096, testLogger())
	c := newTestConnAuth(t, &AuthConfig{
		AdminKeys:  map[string]struct{}{opKey: {}},
		AdminRoles: map[string]string{opKey: "operator"},
	}, audit)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if _, err := c.Registry.Register(ctx, &pb.RegisterRequest{
		ServerId: "g1", Name: "n", Region: "cn-east",
		Endpoint: &pb.Endpoint{Host: "10.0.0.1", Port: 1}, Capacity: 10,
	}); err != nil {
		t.Fatalf("register (registry unconfigured = open): %v", err)
	}
	octx := metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+opKey)
	if _, err := c.Admin.SetMaintenance(octx, &pb.ServerIdRequest{ServerId: "g1"}); err != nil {
		t.Fatalf("set maintenance: %v", err)
	}
	if _, err := c.Discovery.GetServer(ctx, &pb.GetServerRequest{ServerId: "g1"}); err != nil {
		t.Fatalf("discovery: %v", err)
	}

	entries := audit.Recent(10)
	if len(entries) != 1 {
		t.Fatalf("want exactly 1 audit entry (admin mutation only), got %d", len(entries))
	}
	e := entries[0]
	sum := sha256.Sum256([]byte(opKey))
	if e.Actor != "operator:"+hex.EncodeToString(sum[:])[:12] {
		t.Errorf("actor = %q, want operator:<fingerprint>", e.Actor)
	}
	if e.Method != "POST" || e.Status != 200 || e.Path != admMethod("SetMaintenance") {
		t.Errorf("entry = %s %s %d, want POST %s 200", e.Method, e.Path, e.Status,
			admMethod("SetMaintenance"))
	}
}
