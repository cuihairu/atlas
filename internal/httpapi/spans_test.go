package httpapi

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	otelcodes "go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/cuihairu/atlas/internal/tracing/tracingtest"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// TestSpansRenameToRoutePattern pins the low-cardinality contract: the
// span opens under the raw method name and is renamed to the matched
// ServeMux pattern once routing completes, with the semantic-convention
// attributes and the atlas request id attached.
func TestSpansRenameToRoutePattern(t *testing.T) {
	// Install BEFORE building middleware: Spans() captures its tracer at
	// construction.
	rec := tracingtest.Install(t)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/arena/servers/{id}", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h := Tracing(discardLogger())(Spans()(mux))

	req := httptest.NewRequest(http.MethodGet, "/v1/arena/servers/alpha-1", nil)
	req.Header.Set(RequestIDHeader, "span-test-1")
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", res.Code)
	}
	spans := rec.Ended()
	if len(spans) != 1 {
		t.Fatalf("recorded %d spans, want 1", len(spans))
	}
	s := spans[0]
	if s.Name() != "GET /v1/arena/servers/{id}" {
		t.Fatalf("span name = %q, want the route pattern", s.Name())
	}
	if s.SpanKind() != trace.SpanKindServer {
		t.Fatalf("span kind = %v, want server", s.SpanKind())
	}
	for key, want := range map[string]string{
		"http.request.method":       "GET",
		"http.route":                "GET /v1/arena/servers/{id}",
		"url.path":                  "/v1/arena/servers/alpha-1",
		"atlas.request_id":          "span-test-1",
		"http.response.status_code": "",
	} {
		v, ok := rec.Attr(s, key)
		if !ok {
			t.Fatalf("span missing attribute %s", key)
		}
		if key == "http.response.status_code" {
			if got := v.AsInt64(); got != 200 {
				t.Fatalf("%s = %d, want 200", key, got)
			}
			continue
		}
		if v.AsString() != want {
			t.Fatalf("%s = %q, want %q", key, v.AsString(), want)
		}
	}
	if got := s.Status().Code; got != otelcodes.Unset {
		t.Fatalf("span status = %v on a 200, want Unset", got)
	}
	if res.Header().Get(RequestIDHeader) != "span-test-1" {
		t.Fatal("Tracing middleware did not echo the inbound request id")
	}
}

// TestSpansMarkServerErrors: 5xx responses flip the span to codes.Error so
// trace backends can flag them; 4xx stay Unset (client-caused, per the
// semconv rule the middleware follows).
func TestSpansMarkServerErrors(t *testing.T) {
	rec := tracingtest.Install(t)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/arena/servers", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	h := Tracing(discardLogger())(Spans()(mux))

	res := httptest.NewRecorder()
	h.ServeHTTP(res, httptest.NewRequest(http.MethodPost, "/v1/arena/servers", nil))

	spans := rec.Ended()
	if len(spans) != 1 {
		t.Fatalf("recorded %d spans, want 1", len(spans))
	}
	s := spans[0]
	if got := s.Status().Code; got != otelcodes.Error {
		t.Fatalf("span status = %v on a 500, want Error", got)
	}
	if s.Status().Description != "Internal Server Error" {
		t.Fatalf("span status description = %q", s.Status().Description)
	}
	if v, _ := rec.Attr(s, "http.response.status_code"); v.AsInt64() != 500 {
		t.Fatalf("http.response.status_code = %d, want 500", v.AsInt64())
	}
}

// TestSpansSkipQuietPaths: the timer-probed endpoints stay out of trace
// streams, same filter as the access log.
func TestSpansSkipQuietPaths(t *testing.T) {
	rec := tracingtest.Install(t)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {})
	mux.HandleFunc("GET /metrics", func(w http.ResponseWriter, r *http.Request) {})
	h := Tracing(discardLogger())(Spans()(mux))

	for _, path := range []string{"/healthz", "/readyz", "/metrics"} {
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, path, nil))
	}
	if got := len(rec.Ended()); got != 0 {
		t.Fatalf("quiet paths produced %d spans, want 0", got)
	}
}

// TestSpansUnmatchedRoute: no matched pattern means the span keeps the
// method-name placeholder and carries no http.route, so backends still
// group the 404 noise by method instead of raw path cardinality.
func TestSpansUnmatchedRoute(t *testing.T) {
	rec := tracingtest.Install(t)

	mux := http.NewServeMux()
	h := Tracing(discardLogger())(Spans()(mux))

	res := httptest.NewRecorder()
	h.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/v1/nope", nil))

	spans := rec.Ended()
	if len(spans) != 1 {
		t.Fatalf("recorded %d spans, want 1", len(spans))
	}
	s := spans[0]
	if s.Name() != http.MethodGet {
		t.Fatalf("span name = %q, want placeholder %q", s.Name(), http.MethodGet)
	}
	if _, ok := rec.Attr(s, "http.route"); ok {
		t.Fatal("unmatched request has an http.route attribute")
	}
	if v, _ := rec.Attr(s, "http.response.status_code"); v.AsInt64() != http.StatusNotFound {
		t.Fatalf("http.response.status_code = %d, want 404", v.AsInt64())
	}
}
