package grpc

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	otelcodes "go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	atlashttpapi "github.com/cuihairu/atlas/internal/httpapi"
)

// spanRecorder swaps the global no-op tracer for an SDK provider backed by
// an in-memory recorder, restoring the no-op on cleanup. Install it before
// chaining UnarySpans: the interceptor captures its tracer at construction.
func spanRecorder(t *testing.T) *tracetest.SpanRecorder {
	t.Helper()
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	otel.SetTracerProvider(tp)
	t.Cleanup(func() {
		otel.SetTracerProvider(trace.NewNoopTracerProvider())
		_ = tp.Shutdown(context.Background())
	})
	return rec
}

// spanAttr finds the recorded attribute under key.
func spanAttr(s sdktrace.ReadOnlySpan, key string) (attribute.Value, bool) {
	for _, kv := range s.Attributes() {
		if string(kv.Key) == key {
			return kv.Value, true
		}
	}
	return attribute.Value{}, false
}

// TestUnarySpansRecordsRPCSpan chains UnaryTrace + UnarySpans the way
// main.go does: the span carries the rpc.* semantic conventions, the
// request id assigned by the trace interceptor, and the OK status code on
// a passing handler.
func TestUnarySpansRecordsRPCSpan(t *testing.T) {
	rec := spanRecorder(t)

	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	info := &grpc.UnaryServerInfo{FullMethod: "/admin.AdminService/GetStats"}
	var seenID string
	final := func(c context.Context, req any) (any, error) {
		seenID = atlashttpapi.RequestIDFromContext(c)
		return "ok", nil
	}

	ictx, stream := streamCtx(metadata.NewIncomingContext(
		context.Background(), metadata.Pairs("x-request-id", "rpc-span-1")))
	resp, err := UnaryTrace(logger)(ictx, "req", info, func(c context.Context, req any) (any, error) {
		return UnarySpans()(c, req, info, final)
	})
	if err != nil {
		t.Fatalf("handler chain: %v", err)
	}
	if resp != "ok" || seenID != "rpc-span-1" {
		t.Fatalf("resp=%v seenID=%q, want ok/rpc-span-1", resp, seenID)
	}

	spans := rec.Ended()
	if len(spans) != 1 {
		t.Fatalf("recorded %d spans, want 1", len(spans))
	}
	s := spans[0]
	if s.Name() != "/admin.AdminService/GetStats" {
		t.Fatalf("span name = %q, want the full method", s.Name())
	}
	if s.SpanKind() != trace.SpanKindServer {
		t.Fatalf("span kind = %v, want server", s.SpanKind())
	}
	for key, want := range map[string]string{
		"rpc.system":       "grpc",
		"rpc.service":      "admin.AdminService",
		"rpc.method":       "GetStats",
		"atlas.request_id": "rpc-span-1",
	} {
		if v, ok := spanAttr(s, key); !ok || v.AsString() != want {
			t.Fatalf("attribute %s = (%v, %v), want %q", key, v, ok, want)
		}
	}
	if v, ok := spanAttr(s, "rpc.grpc.status_code"); !ok || v.AsInt64() != int64(codes.OK) {
		t.Fatalf("rpc.grpc.status_code = (%v, %v), want %d", v, ok, codes.OK)
	}
	if got := s.Status().Code; got != otelcodes.Unset {
		t.Fatalf("span status = %v on an OK RPC, want Unset", got)
	}
	// Parity with REST: the id assigned by UnaryTrace is echoed in the
	// response header metadata.
	if got := stream.headers.Get("x-request-id"); len(got) != 1 || got[0] != "rpc-span-1" {
		t.Fatalf("response metadata x-request-id = %v, want [rpc-span-1]", got)
	}
}

// TestUnarySpansMarksErrors: a failing handler flips the span to
// codes.Error and records the gRPC status code.
func TestUnarySpansMarksErrors(t *testing.T) {
	rec := spanRecorder(t)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	info := &grpc.UnaryServerInfo{FullMethod: "/directory.DirectoryService/GetCharacter"}
	final := func(c context.Context, req any) (any, error) {
		return nil, status.Error(codes.NotFound, "nope")
	}

	ictx, _ := streamCtx(context.Background())
	_, err := UnaryTrace(logger)(ictx, "req", info, func(c context.Context, req any) (any, error) {
		return UnarySpans()(c, req, info, final)
	})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("handler error = %v, want NotFound", err)
	}

	spans := rec.Ended()
	if len(spans) != 1 {
		t.Fatalf("recorded %d spans, want 1", len(spans))
	}
	s := spans[0]
	if got := s.Status().Code; got != otelcodes.Error {
		t.Fatalf("span status = %v on a NotFound RPC, want Error", got)
	}
	if !bytes.Contains([]byte(s.Status().Description), []byte("nope")) {
		t.Fatalf("span status description = %q, want the error text", s.Status().Description)
	}
	if v, ok := spanAttr(s, "rpc.grpc.status_code"); !ok || v.AsInt64() != int64(codes.NotFound) {
		t.Fatalf("rpc.grpc.status_code = (%v, %v), want %d", v, ok, codes.NotFound)
	}
}

// TestSpanMethodSplit pins the full-method splitter on the wire shape and
// one degenerate input.
func TestSpanMethodSplit(t *testing.T) {
	for _, tc := range []struct{ full, service, method string }{
		{"/admin.AdminService/GetStats", "admin.AdminService", "GetStats"},
		{"/discovery.DiscoveryService/RecommendServer", "discovery.DiscoveryService", "RecommendServer"},
		{"/Health/Check", "Health", "Check"},
		{"/Ping", "Ping", "Ping"},
	} {
		if got := serviceOf(tc.full); got != tc.service {
			t.Fatalf("serviceOf(%q) = %q, want %q", tc.full, got, tc.service)
		}
		if got := methodOf(tc.full); got != tc.method {
			t.Fatalf("methodOf(%q) = %q, want %q", tc.full, got, tc.method)
		}
	}
}
