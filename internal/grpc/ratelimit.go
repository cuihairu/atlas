// Token-bucket rate limiting for the gRPC transport, sharing the exact
// limiter instance the REST listeners use (docs/security.md §4): without
// this layer a caller could dodge ATLAS_RATE_LIMITS by switching to :9090
// — registry registration floods included.
//
// Mount scope mirrors REST exactly: Registry and Admin domains are
// limited, the Public services are not (the REST public port :8080 is
// unthrottled too). Path keys are gRPC full methods ("/atlas.v1.
// RegistryService/Register"), so rules prefix-match on service/method
// names; traffic that matches no rule falls through to the default bucket
// (or passes when no default is configured — same allow-rule as REST).
//
// Chain position mirrors REST too: the limiter sits OUTSIDE auth (cheap
// per-IP rejection before any key check), so attach this interceptor
// before UnaryAuth.
package grpc

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	atlashttpapi "github.com/cuihairu/atlas/internal/httpapi"
)

// UnaryRateLimit returns the interceptor enforcing the shared token
// buckets on the Registry and Admin domains. Callers rejected here get
// ResourceExhausted with the same RATE_LIMITED wording the REST 429
// carries (gRPC has no Retry-After header; back off on the code).
func UnaryRateLimit(limiter *atlashttpapi.RateLimiter) grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req any,
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (any, error) {
		if limiter == nil {
			return handler(ctx, req)
		}
		switch authDomain(info.FullMethod) {
		case "registry", "admin":
			if !limiter.Allow(info.FullMethod, peerIP(ctx)) {
				return nil, status.Error(codes.ResourceExhausted,
					"RATE_LIMITED: too many requests for this endpoint; retry later")
			}
		}
		return handler(ctx, req)
	}
}
