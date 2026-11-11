// Package tracingtest installs an in-memory span recorder over the global
// tracer provider so service- and middleware-level tests can assert spans
// (names, parents, attributes) without an exporter. The service packages'
// span tests share this; install BEFORE building middleware or calling
// code that starts spans.
package tracingtest

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

// Recorder wraps the in-memory span recorder.
type Recorder struct {
	*tracetest.SpanRecorder
}

// Install swaps the global no-op provider for an SDK provider backed by an
// in-memory recorder and restores the no-op on cleanup.
func Install(t *testing.T) *Recorder {
	t.Helper()
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	otel.SetTracerProvider(tp)
	t.Cleanup(func() {
		otel.SetTracerProvider(trace.NewNoopTracerProvider())
		_ = tp.Shutdown(context.Background())
	})
	return &Recorder{rec}
}

// Attr finds the attribute recorded under key on a span.
func (r *Recorder) Attr(span sdktrace.ReadOnlySpan, key string) (attribute.Value, bool) {
	for _, kv := range span.Attributes() {
		if string(kv.Key) == key {
			return kv.Value, true
		}
	}
	return attribute.Value{}, false
}
