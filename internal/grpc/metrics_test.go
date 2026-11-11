package grpc

import (
	"context"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	pb "github.com/cuihairu/atlas/api/pb"
	"github.com/cuihairu/atlas/internal/metrics"
	"github.com/cuihairu/atlas/internal/store/memory"
)

// adminCount reads one labeled series of atlas_admin_requests_total.
func adminCount(t *testing.T, m *metrics.Metrics, endpoint, code string) float64 {
	t.Helper()
	var out dto.Metric
	if err := m.AdminRequests.WithLabelValues(endpoint, code).Write(&out); err != nil {
		t.Fatalf("counter write: %v", err)
	}
	return out.GetCounter().GetValue()
}

// adminTotal sums every created series — for asserting that a call did
// NOT touch the counter without pre-creating the series it would need.
func adminTotal(t *testing.T, m *metrics.Metrics) float64 {
	t.Helper()
	ch := make(chan prometheus.Metric, 64)
	go func() {
		m.AdminRequests.Collect(ch)
		close(ch)
	}()
	sum := 0.0
	for pm := range ch {
		var out dto.Metric
		if err := pm.Write(&out); err != nil {
			t.Fatalf("metric write: %v", err)
		}
		sum += out.GetCounter().GetValue()
	}
	return sum
}

// TestUnaryMetricsCountsAdmin pins the label vocabulary: endpoint is the
// descriptor-derived full method (a proto rename writes a different
// series than this test reads → red), status is httpStatusOf's HTTP
// mapping, and non-admin domains never touch the counter.
func TestUnaryMetricsCountsAdmin(t *testing.T) {
	m := metrics.New(memory.New())
	ic := UnaryMetrics(m)
	ctx := ctxWithPeer("10.0.0.5")

	if _, err := invoke(t, ic, admMethod("GetStats"), ctx); err != nil {
		t.Fatalf("admin read: %v", err)
	}
	if got := adminCount(t, m, admMethod("GetStats"), "200"); got != 1 {
		t.Errorf("success count = %v, want 1", got)
	}

	// Handler failure maps through the same httpStatusOf the audit ring
	// uses: NotFound → 404.
	_, err := ic(ctx, nil, &grpc.UnaryServerInfo{FullMethod: admMethod("Disable")},
		func(context.Context, any) (any, error) {
			return nil, status.Error(codes.NotFound, "nope")
		})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("handler error = %v, want NotFound", err)
	}
	if got := adminCount(t, m, admMethod("Disable"), "404"); got != 1 {
		t.Errorf("not-found count = %v, want 1", got)
	}

	// Registry and Discovery RPCs are not part of this counter's REST
	// surface either — parity means mirroring, not growing.
	before := adminTotal(t, m)
	if _, err := invoke(t, ic, regMethod("Register"), ctx); err != nil {
		t.Fatalf("register (unconfigured = open): %v", err)
	}
	if _, err := invoke(t, ic, discMethod("GetServer"), ctx); err != nil {
		t.Fatalf("discovery (public): %v", err)
	}
	if after := adminTotal(t, m); after != before {
		t.Errorf("non-admin RPC counted: total %v → %v", before, after)
	}

	// Nil metrics set passes through without panicking.
	called, err := invoke(t, UnaryMetrics(nil), admMethod("GetStats"), ctx)
	if !called || err != nil {
		t.Errorf("nil metrics = called=%v err=%v, want passthrough", called, err)
	}
}

// TestMetricsEndToEnd drives the bufconn server with the production chain
// order (metrics outside auth, like RequestCounter wrapping
// limiter → auth → audit on REST) and proves rejected calls count: a
// 401/403 on :9090 must appear in the counter exactly like one on :8082.
func TestMetricsEndToEnd(t *testing.T) {
	m := metrics.New(memory.New())
	viewerKey := "key-viewer"
	c := newTestConnAuth(t, &AuthConfig{
		AdminKeys:  map[string]struct{}{viewerKey: {}},
		AdminRoles: map[string]string{viewerKey: "viewer"},
	}, nil, UnaryMetrics(m))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Register something so discovery below has a live target.
	if _, err := c.Registry.Register(ctx, &pb.RegisterRequest{
		ServerId: "g1", Name: "n", Region: "cn-east",
		Endpoint: &pb.Endpoint{Host: "10.0.0.1", Port: 1}, Capacity: 10,
	}); err != nil {
		t.Fatalf("register (registry unconfigured = open): %v", err)
	}
	before := adminTotal(t, m)

	// Authenticated read → 200.
	vctx := metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+viewerKey)
	if _, err := c.Admin.GetStats(vctx, &pb.GetStatsRequest{}); err != nil {
		t.Fatalf("stats with viewer key: %v", err)
	}
	if got := adminCount(t, m, admMethod("GetStats"), "200"); got != 1 {
		t.Errorf("GetStats 200 count = %v, want 1", got)
	}

	// Missing key → Unauthenticated, counted as 401 by the outer
	// metrics interceptor even though auth rejected before the handler.
	if _, err := c.Admin.Disable(ctx, &pb.ServerIdRequest{ServerId: "g1"}); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("disable without key: want Unauthenticated, got %v", err)
	}
	if got := adminCount(t, m, admMethod("Disable"), "401"); got != 1 {
		t.Errorf("Disable 401 count = %v, want 1", got)
	}

	// Viewer write → PermissionDenied, counted as 403.
	if _, err := c.Admin.Disable(vctx, &pb.ServerIdRequest{ServerId: "g1"}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("viewer disable: want PermissionDenied, got %v", err)
	}
	if got := adminCount(t, m, admMethod("Disable"), "403"); got != 1 {
		t.Errorf("Disable 403 count = %v, want 1", got)
	}

	// Discovery (public) and the successful register above left the
	// counter untouched: net change is exactly the three admin calls.
	if got := adminTotal(t, m) - before; got != 3 {
		t.Errorf("admin counter delta = %v, want 3 (only admin RPCs)", got)
	}
	if _, err := c.Discovery.GetServer(ctx, &pb.GetServerRequest{ServerId: "g1"}); err != nil {
		t.Fatalf("discovery without credentials: %v", err)
	}
	if got := adminTotal(t, m); got != before+3 {
		t.Errorf("discovery counted: total = %v, want %v", got, before+3)
	}
}
