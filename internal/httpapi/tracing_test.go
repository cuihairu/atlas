package httpapi

import (
	"bytes"
	"context"
	"encoding/hex"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newTraceTestLogger() (*slog.Logger, *bytes.Buffer) {
	var buf bytes.Buffer
	return slog.New(slog.NewTextHandler(&buf, nil)), &buf
}

func TestTracingPropagatesInboundRequestID(t *testing.T) {
	logger, buf := newTraceTestLogger()
	var ctxID string
	handler := Tracing(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctxID = RequestIDFromContext(r.Context())
		w.WriteHeader(http.StatusTeapot)
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/directory/accounts/1/characters", nil)
	req.Header.Set(RequestIDHeader, "gw-42.abc_123")
	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get(RequestIDHeader); got != "gw-42.abc_123" {
		t.Errorf("expected inbound id echoed, got %q", got)
	}
	if ctxID != "gw-42.abc_123" {
		t.Errorf("expected id in context, got %q", ctxID)
	}
	log := buf.String()
	if !strings.Contains(log, "request_id=gw-42.abc_123") {
		t.Errorf("expected request_id in access log, got:\n%s", log)
	}
	if !strings.Contains(log, "status=418") {
		t.Errorf("expected captured status in access log, got:\n%s", log)
	}
}

func TestTracingGeneratesRequestID(t *testing.T) {
	logger, buf := newTraceTestLogger()
	handler := Tracing(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if id := RequestIDFromContext(r.Context()); id == "" {
			t.Error("expected generated id in context")
		}
		w.Write([]byte("ok"))
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/discovery/servers", nil))

	id := rec.Header().Get(RequestIDHeader)
	if len(id) != 32 { // 16 bytes hex
		t.Errorf("expected generated 32-char hex id, got %q", id)
	}
	if !strings.Contains(buf.String(), "request_id="+id) {
		t.Errorf("expected generated id in access log, got:\n%s", buf.String())
	}
}

func TestTracingReplacesUnusableInboundID(t *testing.T) {
	for _, bad := range []string{
		"has spaces",
		"injected\nheader",
		strings.Repeat("x", 65), // too long
		"",
	} {
		logger, _ := newTraceTestLogger()
		handler := Tracing(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/v1/routing/recommended", nil)
		if bad != "" {
			req.Header.Set(RequestIDHeader, bad)
		}
		handler.ServeHTTP(rec, req)
		got := rec.Header().Get(RequestIDHeader)
		if got == bad || got == "" || len(got) != 32 {
			t.Errorf("expected unusable id %q replaced by generated hex, got %q", bad, got)
		}
	}
}

func TestTracingQuietPathSkipsLogButKeepsHeader(t *testing.T) {
	logger, buf := newTraceTestLogger()
	handler := Tracing(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"status":"ok"}`))
	}))

	for _, path := range []string{"/healthz", "/readyz", "/metrics"} {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Header().Get(RequestIDHeader) == "" {
			t.Errorf("%s: expected request id header set", path)
		}
		if buf.Len() != 0 {
			t.Errorf("%s: expected no access log for quiet path, got:\n%s", path, buf.String())
		}
	}
}

func TestTracingDefaultStatusWhenHandlerSilent(t *testing.T) {
	logger, buf := newTraceTestLogger()
	handler := Tracing(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/v1/x", nil))
	if !strings.Contains(buf.String(), "status=200") {
		t.Errorf("expected default status 200 in log, got:\n%s", buf.String())
	}
}

func TestRequestIDFromContextEmpty(t *testing.T) {
	if id := RequestIDFromContext(context.Background()); id != "" {
		t.Errorf("expected empty id from bare context, got %q", id)
	}
}

// TestSprintfTimeIDFormat pins the entropy fallback format: a 128-bit id
// rendered as hex, so downstream X-Request-ID validation accepts it.
func TestSprintfTimeIDFormat(t *testing.T) {
	id := sprintfTimeID()
	if len(id) != 32 {
		t.Fatalf("sprintfTimeID length = %d, want 32 hex chars", len(id))
	}
	if _, err := hex.DecodeString(id); err != nil {
		t.Fatalf("sprintfTimeID = %q, not valid hex: %v", id, err)
	}
}
