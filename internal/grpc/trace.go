// Request-level tracing for the gRPC port (docs/api.md §请求追踪): the
// correlation handle REST assigns on the three HTTP listeners extends to
// :9090 — the inbound x-request-id metadata is adopted when present
// (identical charset/cap as the HTTP header), generated otherwise, echoed
// in the response metadata, attached to the handler context, and logged
// with method/status/latency on completion.
//
// Like the REST middleware, this is a correlation handle, not a span tree:
// one id greps across REST and gRPC hop logs when callers reuse it. The
// interceptor sits outermost in main.go's chain so RPCs rejected by rate
// limiting or auth are traced too.
package grpc

import (
	"context"
	"log/slog"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	atlashttpapi "github.com/cuihairu/atlas/internal/httpapi"
)

// requestIDMetadata is the gRPC metadata key mirroring the REST
// X-Request-ID header; gRPC metadata keys are lowercase on the wire.
const requestIDMetadata = "x-request-id"

// UnaryTrace returns an interceptor that assigns/propagates a request id
// and logs RPC completion. Chain it before rate limit and auth so
// rejections carry the id. A nil logger disables it (dev/tests).
func UnaryTrace(logger *slog.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if logger == nil {
			return handler(ctx, req)
		}
		id := requestIDFromMetadata(ctx)
		if id == "" {
			id = atlashttpapi.NewRequestID()
		}
		// Echo regardless of the call's outcome: header metadata set here
		// is sent with the response even when the handler (or an inner
		// interceptor) returns an error status.
		_ = grpc.SetHeader(ctx, metadata.Pairs(requestIDMetadata, id))
		ctx = atlashttpapi.WithRequestID(ctx, id)

		start := time.Now()
		resp, err := handler(ctx, req)
		logger.Info("grpc request",
			"request_id", id,
			"method", info.FullMethod,
			"code", status.Code(err).String(),
			"duration_ms", float64(time.Since(start).Microseconds())/1000,
		)
		return resp, err
	}
}

// requestIDFromMetadata reads and sanitizes the inbound id, mirroring the
// REST middleware's header handling.
func requestIDFromMetadata(ctx context.Context) string {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ""
	}
	for _, v := range md.Get(requestIDMetadata) {
		if id := atlashttpapi.SanitizeRequestID(v); id != "" {
			return id
		}
	}
	return ""
}
