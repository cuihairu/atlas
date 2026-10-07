// Package tracing wires the OpenTelemetry trace pipeline (roadmap
// 可观测性深化, the span-tree slice): when ATLAS_OTLP_ENDPOINT is set,
// root spans opened at the REST listeners and the gRPC port export over
// OTLP/HTTP to whatever receiver the deployment runs (Jaeger, Tempo, a
// vendor collector — the protocol is the commitment, not a backend).
// Unset stays the global no-op provider: spans are non-recording and the
// pipeline costs nothing, mirroring the gRPC TLS precedent (unset =
// plaintext historical behavior).
//
// Sampling is parent-based TraceIDRatio so an upstream caller's sampling
// decision (W3C traceparent) wins; the ratio only governs freshly rooted
// traces. Metrics stay on Prometheus — traces get OTLP, no second metrics
// backend.
package tracing

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
	"go.opentelemetry.io/otel/trace"
)

// Options configures Setup. Endpoint "" (the default) selects the no-op
// provider.
type Options struct {
	// Endpoint is the OTLP/HTTP receiver base URL. http:// exports in
	// plaintext, https:// upgrades the exporter transport to TLS.
	Endpoint string

	// SampleRatio governs freshly rooted traces, (0,1]; out-of-range
	// values fall back to 1.0. Ignored in no-op mode.
	SampleRatio float64

	// ServiceName lands in resource.service.name so receivers can tell
	// Atlas apart from other producers on the same collector.
	ServiceName string

	// Logger receives one info line when the pipeline comes up.
	Logger *slog.Logger
}

// Setup initializes the global tracer provider. It returns nil when
// tracing stays disabled (no shutdown needed); otherwise the returned
// func flushes and shuts the exporter down — call it on the shutdown path
// so trailing spans land before exit. An unreachable endpoint is NOT an
// error here: the exporter batches and retries in the background, matching
// how the metrics endpoint tolerates a missing scraper. Malformed URLs and
// unusable TLS schemes fail fast instead (config errors, not runtime
// conditions).
func Setup(ctx context.Context, opts Options) (func(context.Context) error, error) {
	if opts.Endpoint == "" {
		return nil, nil
	}
	u, err := url.Parse(opts.Endpoint)
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("tracing: invalid ATLAS_OTLP_ENDPOINT %q", opts.Endpoint)
	}

	var exporterOpts []otlptracehttp.Option
	exporterOpts = append(exporterOpts, otlptracehttp.WithEndpoint(u.Host))
	if u.Scheme != "https" {
		exporterOpts = append(exporterOpts, otlptracehttp.WithInsecure())
	}
	// A path on the endpoint (collector behind a reverse proxy) becomes the
	// signal base path; the SDK appends nothing, so splice the standard
	// /v1/traces onto it ourselves.
	if u.Path != "" && u.Path != "/" {
		exporterOpts = append(exporterOpts,
			otlptracehttp.WithURLPath(strings.TrimRight(u.Path, "/")+"/v1/traces"))
	}
	exporter, err := otlptracehttp.New(ctx, exporterOpts...)
	if err != nil {
		return nil, fmt.Errorf("tracing: otlp exporter: %w", err)
	}

	name := opts.ServiceName
	if name == "" {
		name = "atlas"
	}
	ratio := opts.SampleRatio
	if ratio <= 0 || ratio > 1 {
		ratio = 1.0
	}
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceName(name),
		)),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(ratio))),
	)
	otel.SetTracerProvider(tp)

	if opts.Logger != nil {
		opts.Logger.Info("otel trace export enabled",
			"endpoint", opts.Endpoint,
			"sample_ratio", ratio,
		)
	}
	return func(ctx context.Context) error {
		shutdownCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		return tp.Shutdown(shutdownCtx)
	}, nil
}

// Tracer is the single tracer name every Atlas span uses, so receivers
// see one instrumentation scope.
func Tracer() trace.Tracer { return otel.Tracer("github.com/cuihairu/atlas") }
