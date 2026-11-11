package grpc

import (
	"bytes"
	"context"
	"encoding/hex"
	"log/slog"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	pb "github.com/cuihairu/atlas/api/pb"
	atlashttpapi "github.com/cuihairu/atlas/internal/httpapi"
)

// recordingStream is a minimal grpc.ServerTransportStream so unit tests
// can observe grpc.SetHeader without a live transport.
type recordingStream struct {
	headers metadata.MD
}

func (s *recordingStream) Method() string { return "" }
func (s *recordingStream) SetHeader(md metadata.MD) error {
	s.headers = metadata.Join(s.headers, md)
	return nil
}
func (s *recordingStream) SendHeader(md metadata.MD) error {
	return s.SetHeader(md)
}
func (s *recordingStream) SetTrailer(metadata.MD) error { return nil }

// streamCtx attaches a recording stream the way the gRPC server does.
func streamCtx(ctx context.Context) (context.Context, *recordingStream) {
	s := &recordingStream{}
	return grpc.NewContextWithServerTransportStream(ctx, s), s
}

// traceLogger captures slog output for asserting the access-log line.
func traceLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewTextHandler(buf, nil))
}

// traceHandler returns a handler that records the request id it saw —
// through the same context key the HTTP middleware stashes it under, so
// a handler-side rename on either transport fails here.
func traceHandler(seen *string) grpc.UnaryHandler {
	return func(c context.Context, req any) (any, error) {
		*seen = atlashttpapi.RequestIDFromContext(c)
		return "ok", nil
	}
}

// TestTraceAdoptsInboundID pins the REST parity contract: a legal inbound
// x-request-id metadata value is reused verbatim — echoed in response
// header metadata, visible to the handler through the same context key
// the HTTP middleware uses, and present in the access-log line.
func TestTraceAdoptsInboundID(t *testing.T) {
	var buf bytes.Buffer
	ic := UnaryTrace(traceLogger(&buf))
	ctx, st := streamCtx(ctxWithMD(context.Background(), "x-request-id", "gw-abc.123"))
	var seen string
	_, err := ic(ctx, nil, &grpc.UnaryServerInfo{FullMethod: discMethod("GetServer")}, traceHandler(&seen))
	if err != nil {
		t.Fatalf("trace passthrough: %v", err)
	}
	if seen != "gw-abc.123" {
		t.Errorf("handler saw id %q, want gw-abc.123", seen)
	}
	if got := st.headers.Get("x-request-id"); len(got) != 1 || got[0] != "gw-abc.123" {
		t.Errorf("echoed header = %v, want [gw-abc.123]", got)
	}
	if !strings.Contains(buf.String(), `request_id=gw-abc.123`) ||
		!strings.Contains(buf.String(), `grpc request`) ||
		!strings.Contains(buf.String(), `code=OK`) {
		t.Errorf("access log missing fields: %q", buf.String())
	}
}

// TestTraceGeneratesAndSanitizesID: no inbound id → a 128-bit hex id;
// illegal inbound values (bad charset, over the 64-byte cap — the same
// rules as the HTTP header) are replaced, never echoed.
func TestTraceGeneratesAndSanitizesID(t *testing.T) {
	for name, md := range map[string][]string{
		"missing":    nil,
		"bad chars":  {"x-request-id", "bad id!"},
		"too long":   {"x-request-id", strings.Repeat("a", 65)},
		"only empty": {"x-request-id", ""},
	} {
		t.Run(name, func(t *testing.T) {
			var buf bytes.Buffer
			ic := UnaryTrace(traceLogger(&buf))
			ctx, st := streamCtx(context.Background())
			if md != nil {
				ctx = ctxWithMD(ctx, md...)
			}
			var seen string
			if _, err := ic(ctx, nil, &grpc.UnaryServerInfo{FullMethod: discMethod("GetServer")}, traceHandler(&seen)); err != nil {
				t.Fatalf("trace passthrough: %v", err)
			}
			if len(seen) != 32 {
				t.Fatalf("generated id length = %d, want 32", len(seen))
			}
			if _, err := hex.DecodeString(seen); err != nil {
				t.Fatalf("generated id %q not hex: %v", seen, err)
			}
			if got := st.headers.Get("x-request-id"); len(got) != 1 || got[0] != seen {
				t.Errorf("echoed header = %v, want the generated id", got)
			}
		})
	}
}

// TestTraceMultiValueMetadata: if a client sends several values one legal
// value wins over empty/garbage siblings.
func TestTraceMultiValueMetadata(t *testing.T) {
	var buf bytes.Buffer
	ic := UnaryTrace(traceLogger(&buf))
	md := metadata.New(map[string]string{"x-request-id": "bad id!"})
	md.Append("x-request-id", "", "keep-me.1")
	ctx, _ := streamCtx(metadata.NewIncomingContext(context.Background(), md))
	var seen string
	if _, err := ic(ctx, nil, &grpc.UnaryServerInfo{FullMethod: discMethod("GetServer")}, traceHandler(&seen)); err != nil {
		t.Fatalf("trace passthrough: %v", err)
	}
	if seen != "keep-me.1" {
		t.Errorf("id = %q, want keep-me.1", seen)
	}
}

// TestTraceLogsErrorStatus: a failing handler keeps its error, and the
// access log records the mapped code while the header is still echoed.
func TestTraceLogsErrorStatus(t *testing.T) {
	var buf bytes.Buffer
	ic := UnaryTrace(traceLogger(&buf))
	ctx, st := streamCtx(context.Background())
	_, err := ic(ctx, nil, &grpc.UnaryServerInfo{FullMethod: admMethod("GetMigration")},
		func(context.Context, any) (any, error) {
			return nil, status.Error(codes.NotFound, "gone")
		})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("error = %v, want NotFound passthrough", err)
	}
	if !strings.Contains(buf.String(), `code=NotFound`) {
		t.Errorf("access log missing code=NotFound: %q", buf.String())
	}
	if got := st.headers.Get("x-request-id"); len(got) != 1 || got[0] == "" {
		t.Errorf("echoed header = %v, want a generated id even on error", got)
	}
}

// TestTraceNilLogger: tests/dev harnesses build servers without a logger;
// the interceptor must degrade to a plain passthrough.
func TestTraceNilLogger(t *testing.T) {
	ic := UnaryTrace(nil)
	called, err := invoke(t, ic, discMethod("GetServer"), context.Background())
	if !called || err != nil {
		t.Fatalf("nil logger = called=%v err=%v, want passthrough", called, err)
	}
}

// TestTraceEndToEnd drives the production chain order (trace outermost,
// then rate limit) over bufconn: a successful call echoes the adopted id
// in real response header metadata, and the rejected-by-rate-limit call
// is still logged under the same id — the guarantee the REST tracing
// middleware gives by wrapping the limiter. Registry Register is the
// limited method (registry domain — Discovery is public and never
// limited, mirroring the unthrottled REST port :8080).
func TestTraceEndToEnd(t *testing.T) {
	var buf bytes.Buffer
	lim := testLimiter(t, regMethod("Register")+"=1:1", "")
	c := newTestConnAuth(t, nil, nil, UnaryTrace(traceLogger(&buf)), UnaryRateLimit(lim))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cctx := metadata.AppendToOutgoingContext(ctx, "x-request-id", "e2e-trace-1")
	var respMD metadata.MD
	if _, err := c.Registry.Register(cctx, &pb.RegisterRequest{
		ServerId: "g1", Name: "n", Region: "cn-east",
		Endpoint: &pb.Endpoint{Host: "10.0.0.1", Port: 1}, Capacity: 10,
	}, grpc.Header(&respMD)); err != nil {
		t.Fatalf("first register: %v", err)
	}
	if got := respMD.Get("x-request-id"); len(got) != 1 || got[0] != "e2e-trace-1" {
		t.Errorf("response header x-request-id = %v, want [e2e-trace-1]", got)
	}

	if _, err := c.Registry.Register(cctx, &pb.RegisterRequest{
		ServerId: "g2", Name: "n", Region: "cn-east",
		Endpoint: &pb.Endpoint{Host: "10.0.0.1", Port: 2}, Capacity: 10,
	}); status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("second register: want ResourceExhausted, got %v", err)
	}

	if n := strings.Count(buf.String(), `request_id=e2e-trace-1`); n != 2 {
		t.Errorf("traced calls with adopted id = %d, want 2 (rejected call traced too)", n)
	}
	if !strings.Contains(buf.String(), `code=OK`) || !strings.Contains(buf.String(), `code=ResourceExhausted`) {
		t.Errorf("log missing outcome codes: %q", buf.String())
	}
}
