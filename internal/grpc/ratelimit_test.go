package grpc

import (
	"context"
	"strings"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"

	pb "github.com/cuihairu/atlas/api/pb"
	atlashttpapi "github.com/cuihairu/atlas/internal/httpapi"
)

// testLimiter parses rules the way main.go does from ATLAS_RATE_LIMITS.
func testLimiter(t *testing.T, rules, def string) *atlashttpapi.RateLimiter {
	t.Helper()
	parsed, err := atlashttpapi.ParseRateLimitRules(rules)
	if err != nil {
		t.Fatalf("parse rules: %v", err)
	}
	var defRule atlashttpapi.RateRule
	if def != "" {
		var err error
		if defRule, err = atlashttpapi.ParseRateDefault(def); err != nil {
			t.Fatalf("parse default: %v", err)
		}
	}
	return atlashttpapi.NewRateLimiter(parsed, defRule)
}

// TestRateLimitDomainScope pins the mount scope against REST: Registry and
// Admin full methods are bucketed, Public traffic is never touched even
// when a default rule would deny everything.
func TestRateLimitDomainScope(t *testing.T) {
	lim := testLimiter(t, "/atlas.v1.RegistryService/Register=1:1", "1:1")
	ic := UnaryRateLimit(lim)
	okHandler := func(c context.Context, req any) (any, error) { return "ok", nil }
	ctx := ctxWithPeer("10.0.0.7")

	// First Register consumes the only token; the second must be limited.
	if _, err := invokeHandler(t, ic, regMethod("Register"), ctx,
		&pb.RegisterRequest{ServerId: "g1"}, okHandler); err != nil {
		t.Fatalf("first register: %v", err)
	}
	_, err := invokeHandler(t, ic, regMethod("Register"), ctx,
		&pb.RegisterRequest{ServerId: "g1"}, okHandler)
	if codeOf(t, err) != codes.ResourceExhausted {
		t.Fatalf("second register = %v, want ResourceExhausted", err)
	}
	if !strings.Contains(err.Error(), "RATE_LIMITED") {
		t.Errorf("message lacks RATE_LIMITED wording: %v", err)
	}

	// Admin domain is covered by the default rule (own bucket per method).
	if _, err := invokeHandler(t, ic, admMethod("GetStats"), ctx,
		&pb.GetStatsRequest{}, okHandler); err != nil {
		t.Fatalf("first stats: %v", err)
	}
	if _, err := invokeHandler(t, ic, admMethod("GetStats"), ctx,
		&pb.GetStatsRequest{}, okHandler); codeOf(t, err) != codes.ResourceExhausted {
		t.Fatalf("second stats = %v, want ResourceExhausted (default rule)", err)
	}

	// Public domain: never limited, even with a deny-all default active.
	for i := 0; i < 5; i++ {
		if _, err := invokeHandler(t, ic, discMethod("GetServer"), ctx,
			&pb.GetServerRequest{ServerId: "g1"}, okHandler); err != nil {
			t.Fatalf("public call %d limited: %v", i, err)
		}
	}
}

// TestRateLimitOutsideAuth pins the chain position against the REST mux:
// the limiter rejects before the auth interceptor runs, so an exhausted
// bucket answers RATE_LIMITED even to a caller holding valid credentials.
func TestRateLimitOutsideAuth(t *testing.T) {
	lim := testLimiter(t, "/atlas.v1.AdminService/=1:1", "")
	authIC := UnaryAuth(AuthConfig{
		AdminKeys: map[string]struct{}{"key-op": {}},
	})
	rateIC := UnaryRateLimit(lim)
	ctx := ctxWithMD(ctxWithPeer("10.0.0.7"), "authorization", "Bearer key-op")
	info := &grpc.UnaryServerInfo{FullMethod: admMethod("GetStats")}
	handler := func(c context.Context, req any) (any, error) { return "ok", nil }
	authed := func(c context.Context, req any) (any, error) {
		return authIC(c, req, info, handler)
	}

	if _, err := rateIC(ctx, nil, info, authed); err != nil {
		t.Fatalf("first call with valid key: %v", err)
	}
	_, err := rateIC(ctx, nil, info, authed)
	if codeOf(t, err) != codes.ResourceExhausted {
		t.Errorf("exhausted bucket with valid key = %v, want ResourceExhausted (limiter outside auth)", err)
	}
}
