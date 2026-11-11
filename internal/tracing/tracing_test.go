package tracing

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// restoreNoop puts the global tracer provider back to the no-op after a
// test that swapped it, so package tests stay independent of order.
func restoreNoop(t *testing.T) {
	t.Cleanup(func() { otel.SetTracerProvider(trace.NewNoopTracerProvider()) })
}

// TestSetupNoopWithoutEndpoint pins the disabled-by-default contract: no
// endpoint means no pipeline, no shutdown, and spans from the global
// tracer are non-recording.
func TestSetupNoopWithoutEndpoint(t *testing.T) {
	shutdown, err := Setup(context.Background(), Options{SampleRatio: 1.0})
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	if shutdown != nil {
		t.Fatal("shutdown func returned for disabled tracing")
	}
	_, span := Tracer().Start(context.Background(), "noop")
	defer span.End()
	if span.IsRecording() {
		t.Fatal("span is recording under the no-op provider")
	}
}

// TestSetupRejectsInvalidEndpoint: malformed URLs fail fast at startup —
// they are config errors, not runtime conditions like an unreachable
// receiver.
func TestSetupRejectsInvalidEndpoint(t *testing.T) {
	for _, endpoint := range []string{"nohost", "http://", "http://%zz"} {
		shutdown, err := Setup(context.Background(), Options{Endpoint: endpoint})
		if err == nil {
			t.Fatalf("Setup(%q): expected error", endpoint)
		}
		if !strings.Contains(err.Error(), "invalid ATLAS_OTLP_ENDPOINT") {
			t.Fatalf("Setup(%q): error %q does not name the env var", endpoint, err)
		}
		if shutdown != nil {
			t.Fatalf("Setup(%q): shutdown returned alongside error", endpoint)
		}
	}
}

// TestSetupExportsSpansOverOTLP walks the real pipeline end to end: Setup
// against a local receiver, one root span, shutdown flush — the receiver
// must have taken the POST at the spliced OTLP path with a non-empty
// protobuf body.
func TestSetupExportsSpansOverOTLP(t *testing.T) {
	restoreNoop(t)

	var mu sync.Mutex
	var paths []string
	var bodyLen int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		paths = append(paths, r.URL.Path)
		bodyLen = len(body)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	// A path on the endpoint (collector behind a reverse proxy) must
	// become the signal base path with /v1/traces spliced on.
	shutdown, err := Setup(context.Background(), Options{
		Endpoint:    srv.URL + "/otel",
		ServiceName: "atlas-test",
		Logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	if shutdown == nil {
		t.Fatal("no shutdown func for enabled tracing")
	}

	_, span := Tracer().Start(context.Background(), "e2e-root")
	span.SetAttributes(attribute.String("atlas.request_id", "otlp-e2e"))
	span.End()
	if err := shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown flush: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(paths) != 1 {
		t.Fatalf("receiver got %d requests, want 1 (%v)", len(paths), paths)
	}
	if paths[0] != "/otel/v1/traces" {
		t.Fatalf("export path = %q, want /otel/v1/traces", paths[0])
	}
	if bodyLen == 0 {
		t.Fatal("export body is empty — no span payload arrived")
	}
}

// TestSetupClampsSampleRatio: values outside (0,1] fall back to 1.0, and
// the startup log line reports the effective ratio.
func TestSetupClampsSampleRatio(t *testing.T) {
	restoreNoop(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()

	for _, tc := range []struct {
		in   float64
		want string
	}{
		{5, "sample_ratio=1"},
		{0, "sample_ratio=1"},
		{-1, "sample_ratio=1"},
		{0.25, "sample_ratio=0.25"},
	} {
		var logs bytes.Buffer
		shutdown, err := Setup(context.Background(), Options{
			Endpoint:    srv.URL,
			SampleRatio: tc.in,
			Logger:      slog.New(slog.NewTextHandler(&logs, nil)),
		})
		if err != nil {
			t.Fatalf("Setup(ratio %v): %v", tc.in, err)
		}
		if !strings.Contains(logs.String(), tc.want) {
			t.Fatalf("Setup(ratio %v): log missing %s:\n%s", tc.in, tc.want, logs.String())
		}
		if err := shutdown(context.Background()); err != nil {
			t.Fatalf("Setup(ratio %v): shutdown: %v", tc.in, err)
		}
	}
}
