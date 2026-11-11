// OpenTelemetry root spans for the REST listeners (roadmap 可观测性深化):
// every request that survives the quiet-path filter gets one server span.
// The span starts under a placeholder name and is renamed to the matched
// ServeMux pattern ("GET /v1/discovery/servers/{id}") once the mux has
// routed — patterns are the low-cardinality name trace backends can group
// on; raw paths with entity ids are kept in the http.route/url.path
// attributes instead. Disabled-by-default: with no OTLP endpoint the
// global provider is the no-op and spans come out non-recording, so the
// middleware is mounted unconditionally and costs nothing until enabled.
package httpapi

import (
	"net/http"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
	"go.opentelemetry.io/otel/trace"

	atlastracing "github.com/cuihairu/atlas/internal/tracing"
)

// Spans returns middleware that opens the request root span. Mount it
// directly inside Tracing so rejections from inner auth/rate-limit layers
// are spanned too and the request id lands as a span attribute.
func Spans() func(http.Handler) http.Handler {
	tracer := atlastracing.Tracer()
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if quietPath(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}

			ctx, span := tracer.Start(r.Context(), r.Method,
				trace.WithSpanKind(trace.SpanKindServer),
				trace.WithAttributes(
					semconv.HTTPRequestMethodKey.String(r.Method),
					semconv.URLPathKey.String(r.URL.Path),
				))
			rec := &traceStatusWriter{ResponseWriter: w, status: http.StatusOK}
			// Keep the derived request: WithContext returns a shallow
			// copy and ServeMux stamps the matched pattern on the copy it
			// dispatches — the outer r stays blank.
			rw := r.WithContext(ctx)
			next.ServeHTTP(rec, rw)

			if rw.Pattern != "" {
				span.SetName(rw.Pattern)
				span.SetAttributes(semconv.HTTPRoute(rw.Pattern))
			}
			span.SetAttributes(
				semconv.HTTPResponseStatusCode(rec.status),
				attribute.String("atlas.request_id", RequestIDFromContext(ctx)),
			)
			if rec.status >= 500 {
				span.SetStatus(codes.Error, http.StatusText(rec.status))
			}
			span.End()
		})
	}
}
