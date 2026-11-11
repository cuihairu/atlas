// OpenTelemetry root spans for the gRPC port (roadmap 可观测性深化): the
// RPC mirror of the REST span middleware — one server span per unary RPC,
// named by the full method ("/admin.AdminService/GetStats") with the
// rpc.* semantic conventions and the same atlas.request_id attribute the
// REST spans carry. Chain it directly after UnaryTrace so calls rejected
// by rate limiting or auth are spanned, and the request id — assigned by
// the trace interceptor — is on the span.
package grpc

import (
	"context"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/status"

	atlashttpapi "github.com/cuihairu/atlas/internal/httpapi"
	atlastracing "github.com/cuihairu/atlas/internal/tracing"
)

// UnarySpans returns an interceptor that opens the RPC server span. A nil
// tracer (no-op provider default) makes spans non-recording — the
// interceptor stays in the chain unconditionally, like the REST one.
func UnarySpans() grpc.UnaryServerInterceptor {
	tracer := atlastracing.Tracer()
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		ctx, span := tracer.Start(ctx, info.FullMethod,
			trace.WithSpanKind(trace.SpanKindServer),
			trace.WithAttributes(
				semconv.RPCSystemKey.String("grpc"),
				semconv.RPCServiceKey.String(serviceOf(info.FullMethod)),
				semconv.RPCMethodKey.String(methodOf(info.FullMethod)),
				attribute.String("atlas.request_id", atlashttpapi.RequestIDFromContext(ctx)),
			))

		resp, err := handler(ctx, req)
		span.SetAttributes(semconv.RPCGRPCStatusCodeKey.Int(int(status.Code(err))))
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
		return resp, err
	}
}

// serviceOf / methodOf split a full method "/pkg.Service/Method".
func serviceOf(fullMethod string) string {
	trimmed := strings.TrimPrefix(fullMethod, "/")
	if i := strings.LastIndex(trimmed, "/"); i >= 0 {
		return trimmed[:i]
	}
	return trimmed
}

func methodOf(fullMethod string) string {
	if i := strings.LastIndex(fullMethod, "/"); i >= 0 {
		return fullMethod[i+1:]
	}
	return fullMethod
}
