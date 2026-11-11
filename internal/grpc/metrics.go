// Request counting for the gRPC transport: atlas_admin_requests_total is
// fed only by the REST middleware wrapping the :8082 admin mux
// (cmd/atlas main.go), so without a counterpart the ten Admin RPCs
// (Disable / SetMaintenance / RollbackMigration / …) were invisible to
// the counter while their REST twins counted — the same transport
// asymmetry the auth / audit / rate-limit interceptors closed. Labels
// stay in one vocabulary so both transports graph as a single series:
// endpoint is the gRPC full method (the path-shaped key the audit ring
// and rate limiter already use), status is the HTTP code httpStatusOf
// maps the gRPC code onto (Unauthenticated → 401, PermissionDenied →
// 403, ResourceExhausted → 429, …).
//
// Attach OUTSIDE the rate-limit/auth chain — main.go appends this
// interceptor first — mirroring the REST order where RequestCounter
// wraps limiter → auth → audit: rejected calls must count, so a
// 401/403/429 on :9090 shows up exactly like one on :8082.
//
// Discovery and Registry RPCs are deliberately not counted: REST does
// not feed them into this counter either (discovery counts in the
// service layer via atlas_discovery_requests_total, registry has its
// own write histograms) — parity means mirroring the REST metric
// surface, not growing it.
package grpc

import (
	"context"
	"strconv"

	"google.golang.org/grpc"
	"google.golang.org/grpc/status"

	"github.com/cuihairu/atlas/internal/metrics"
)

// UnaryMetrics counts Admin-domain RPCs into m.AdminRequests after the
// handler (and every inner interceptor) has produced its status. A nil
// metrics set passes straight through.
func UnaryMetrics(m *metrics.Metrics) grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req any,
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (any, error) {
		if m == nil || authDomain(info.FullMethod) != "admin" {
			return handler(ctx, req)
		}
		resp, err := handler(ctx, req)
		m.AdminRequests.WithLabelValues(
			info.FullMethod,
			strconv.Itoa(httpStatusOf(status.Code(err))),
		).Inc()
		return resp, err
	}
}
