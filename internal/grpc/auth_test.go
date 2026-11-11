package grpc

import (
	"context"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"

	pb "github.com/cuihairu/atlas/api/pb"
	atlashttpapi "github.com/cuihairu/atlas/internal/httpapi"
)

// TestAuthRolesMatchREST pins the role vocabulary: the gRPC side borrows
// the REST role names, and a rename on either side must fail here, not in
// production RBAC.
func TestAuthRolesMatchREST(t *testing.T) {
	if roleAdmin != atlashttpapi.RoleAdmin || roleViewer != atlashttpapi.RoleViewer ||
		roleOperator != atlashttpapi.RoleOperator {
		t.Fatalf("gRPC role constants drifted from httpapi: %s/%s/%s",
			roleAdmin, roleOperator, roleViewer)
	}
}

// regMethod / admMethod derive full methods from the generated service
// descriptors, exactly like the server reports them — hardcoding strings
// here is how a proto package rename would fake-pass unit tests.
func regMethod(name string) string {
	return "/" + pb.RegistryService_ServiceDesc.ServiceName + "/" + name
}
func admMethod(name string) string { return "/" + pb.AdminService_ServiceDesc.ServiceName + "/" + name }
func discMethod(name string) string {
	return "/" + pb.DiscoveryService_ServiceDesc.ServiceName + "/" + name
}

// invoke runs the interceptor the way the server would and reports whether
// the wrapped handler was reached.
func invoke(t *testing.T, ic grpc.UnaryServerInterceptor, method string, ctx context.Context) (bool, error) {
	t.Helper()
	called := false
	handler := func(c context.Context, req any) (any, error) {
		called = true
		return "ok", nil
	}
	_, err := ic(ctx, nil, &grpc.UnaryServerInfo{FullMethod: method}, handler)
	return called, err
}

func ctxWithPeer(ip string) context.Context {
	ctx := context.Background()
	if ip == "" {
		return ctx
	}
	return peer.NewContext(ctx, &peer.Peer{Addr: &net.TCPAddr{IP: net.ParseIP(ip), Port: 50000}})
}

func ctxWithMD(ctx context.Context, kv ...string) context.Context {
	return metadata.NewIncomingContext(ctx, metadata.Pairs(kv...))
}

func codeOf(t *testing.T, err error) codes.Code {
	t.Helper()
	if err == nil {
		return codes.OK
	}
	return status.Code(err)
}

func TestAuthDomainClassification(t *testing.T) {
	for method, want := range map[string]string{
		regMethod("Register"):                "registry",
		admMethod("SetMaintenance"):          "admin",
		discMethod("GetServer"):              "public",
		"/directory.DirectoryService/Upsert": "public",
		"/routing.RoutingService/Recommend":  "public",
	} {
		if got := authDomain(method); got != want {
			t.Errorf("authDomain(%s) = %s, want %s", method, got, want)
		}
	}
}

func TestRegistryAuth(t *testing.T) {
	ic := UnaryAuth(AuthConfig{RegistryTokens: map[string]struct{}{"tok-1": {}}})
	ctx := ctxWithPeer("10.0.0.5")

	called, err := invoke(t, ic, regMethod("Register"),
		ctxWithMD(ctx, "authorization", "Bearer tok-1"))
	if !called || err != nil {
		t.Fatalf("valid token rejected: called=%v err=%v", called, err)
	}
	called, err = invoke(t, ic, regMethod("Heartbeat"),
		ctxWithMD(ctx, "x-atlas-token", "tok-1"))
	if !called || err != nil {
		t.Fatalf("x-atlas-token rejected: called=%v err=%v", called, err)
	}
	if _, err := invoke(t, ic, regMethod("Register"), ctx); codeOf(t, err) != codes.Unauthenticated {
		t.Errorf("missing token = %v, want Unauthenticated", err)
	}
	if _, err := invoke(t, ic, regMethod("Register"),
		ctxWithMD(ctx, "authorization", "Bearer nope")); codeOf(t, err) != codes.Unauthenticated {
		t.Errorf("invalid token = %v, want Unauthenticated", err)
	}
}

func TestAdminAuthRBAC(t *testing.T) {
	ic := UnaryAuth(AuthConfig{
		AdminKeys:  map[string]struct{}{"key-viewer": {}, "key-op": {}},
		AdminRoles: map[string]string{"key-viewer": "viewer", "key-op": "operator"},
	})
	ctx := ctxWithPeer("10.0.0.9")
	viewer := ctxWithMD(ctx, "authorization", "Bearer key-viewer")
	op := ctxWithMD(ctx, "authorization", "Bearer key-op")

	called, err := invoke(t, ic, admMethod("GetStats"), viewer)
	if !called || err != nil {
		t.Fatalf("viewer read rejected: called=%v err=%v", called, err)
	}
	if _, err := invoke(t, ic, admMethod("SetMaintenance"), viewer); codeOf(t, err) != codes.PermissionDenied {
		t.Errorf("viewer write = %v, want PermissionDenied", err)
	}
	called, err = invoke(t, ic, admMethod("RollbackMigration"), op)
	if !called || err != nil {
		t.Fatalf("operator write rejected: called=%v err=%v", called, err)
	}
	if _, err := invoke(t, ic, admMethod("GetStats"), ctx); codeOf(t, err) != codes.Unauthenticated {
		t.Errorf("missing key = %v, want Unauthenticated", err)
	}
	if _, err := invoke(t, ic, admMethod("GetStats"),
		ctxWithMD(ctx, "authorization", "Bearer nope")); codeOf(t, err) != codes.Unauthenticated {
		t.Errorf("invalid key = %v, want Unauthenticated", err)
	}
	// Key absent from the roles map acts as admin (REST parity).
	called, err = invoke(t, UnaryAuth(AuthConfig{
		AdminKeys:  map[string]struct{}{"key-admin": {}},
		AdminRoles: map[string]string{},
	}), admMethod("Disable"),
		ctxWithMD(ctx, "authorization", "Bearer key-admin"))
	if !called || err != nil {
		t.Fatalf("roleless key treated as non-admin: called=%v err=%v", called, err)
	}
}

func TestAuthIPWhitelist(t *testing.T) {
	_, cidr, _ := net.ParseCIDR("10.0.0.0/24")
	ic := UnaryAuth(AuthConfig{RegistryIPs: []*net.IPNet{cidr}})

	called, err := invoke(t, ic, regMethod("Register"), ctxWithPeer("10.0.0.5"))
	if !called || err != nil {
		t.Fatalf("whitelisted ip rejected: called=%v err=%v", called, err)
	}
	if _, err := invoke(t, ic, regMethod("Register"), ctxWithPeer("10.9.9.9")); codeOf(t, err) != codes.PermissionDenied {
		t.Errorf("outside ip = %v, want PermissionDenied", err)
	}
	// No peer presented = unknown IP = denied (fail closed, like REST).
	if _, err := invoke(t, ic, regMethod("Register"), context.Background()); codeOf(t, err) != codes.PermissionDenied {
		t.Errorf("missing peer = %v, want PermissionDenied", err)
	}
}

func TestAuthUnconfiguredPassesThrough(t *testing.T) {
	ic := UnaryAuth(AuthConfig{})
	for _, method := range []string{regMethod("Register"), admMethod("Disable"), discMethod("GetServer")} {
		called, err := invoke(t, ic, method, context.Background())
		if !called || err != nil {
			t.Errorf("unconfigured %s = called=%v err=%v, want passthrough", method, called, err)
		}
	}
}

// Public methods stay open even with both guard sets configured.
func TestAuthPublicStaysOpen(t *testing.T) {
	ic := UnaryAuth(AuthConfig{
		RegistryTokens: map[string]struct{}{"tok": {}},
		AdminKeys:      map[string]struct{}{"key": {}},
	})
	called, err := invoke(t, ic, discMethod("GetServer"), context.Background())
	if !called || err != nil {
		t.Errorf("public method blocked: called=%v err=%v", called, err)
	}
}

// TestAuthEndToEnd drives a real bufconn server with the guards attached:
// this is the test that catches a full-method-prefix drift (e.g. a proto
// package rename) that unit tests on fake UnaryServerInfo would fake-pass.
func TestAuthEndToEnd(t *testing.T) {
	viewerKey, opKey, token := "key-viewer", "key-op", "srctok"
	c := newTestConnAuth(t, &AuthConfig{
		RegistryTokens: map[string]struct{}{token: {}},
		AdminKeys:      map[string]struct{}{viewerKey: {}, opKey: {}},
		AdminRoles:     map[string]string{viewerKey: "viewer", opKey: "operator"},
	}, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Registry Register without a token: rejected before the service runs.
	if _, err := c.Registry.Register(ctx, &pb.RegisterRequest{ServerId: "g1"}); status.Code(err) != codes.Unauthenticated {
		t.Errorf("register without token: want Unauthenticated, got %v", err)
	}
	// The X-Atlas-Token path registers for real.
	tctx := metadata.AppendToOutgoingContext(ctx, "x-atlas-token", token)
	if _, err := c.Registry.Register(tctx, &pb.RegisterRequest{
		ServerId: "g1", Name: "n", Region: "cn-east",
		Endpoint: &pb.Endpoint{Host: "10.0.0.1", Port: 1}, Capacity: 10,
	}); err != nil {
		t.Fatalf("register with token: %v", err)
	}
	// Admin write without a key: rejected; with the operator key: passes.
	if _, err := c.Admin.Disable(ctx, &pb.ServerIdRequest{ServerId: "g1"}); status.Code(err) != codes.Unauthenticated {
		t.Errorf("disable without key: want Unauthenticated, got %v", err)
	}
	octx := metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+opKey)
	if _, err := c.Admin.SetMaintenance(octx, &pb.ServerIdRequest{ServerId: "g1"}); err != nil {
		t.Fatalf("disable with operator key: %v", err)
	}
	// The viewer key reads fine but cannot write.
	vctx := metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+viewerKey)
	if _, err := c.Admin.GetStats(vctx, &pb.GetStatsRequest{}); err != nil {
		t.Fatalf("stats with viewer key: %v", err)
	}
	if _, err := c.Admin.Disable(vctx, &pb.ServerIdRequest{ServerId: "g1"}); status.Code(err) != codes.PermissionDenied {
		t.Errorf("viewer disable: want PermissionDenied, got %v", err)
	}
	// Discovery stays open without credentials.
	if _, err := c.Discovery.GetServer(ctx, &pb.GetServerRequest{ServerId: "g1"}); err != nil {
		t.Fatalf("discovery without credentials: %v", err)
	}
}
