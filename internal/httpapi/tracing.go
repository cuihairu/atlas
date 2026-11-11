// Request-level tracing (roadmap 可观测性深化, first slice): every request
// that reaches one of the three HTTP listeners gets a request id — taken
// from an inbound X-Request-ID when the caller supplies one, generated
// otherwise — echoed back on the response, stored in the request context,
// and logged with method/path/status/latency on completion.
//
// The id is a correlation handle, not a distributed-tracing span tree:
// gateways (APISIX/nginx) can propagate the same header and operators can
// grep a single request across hop logs. /healthz, /readyz and /metrics are
// probed on a timer and would drown the access log, so they get the header
// but no log line.
package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"time"
)

// RequestIDHeader is the inbound/outbound correlation header.
const RequestIDHeader = "X-Request-ID"

// maxRequestIDLen bounds accepted inbound ids; anything longer (or with
// unexpected characters — injection into log pipelines) is replaced by a
// generated one.
const maxRequestIDLen = 64

type requestIDCtxKey struct{}

// RequestIDFromContext returns the request id attached by the Tracing
// middleware ("" when absent).
func RequestIDFromContext(ctx context.Context) string {
	id, _ := ctx.Value(requestIDCtxKey{}).(string)
	return id
}

// WithRequestID attaches a request id to the context; the gRPC trace
// interceptor uses it so handlers see the same correlation handle on both
// transports.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDCtxKey{}, id)
}

// Tracing returns middleware that assigns/propagates a request id and logs
// request completion. Wrap it outermost so rejections from inner auth or
// rate-limit layers are traced too.
func Tracing(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := SanitizeRequestID(r.Header.Get(RequestIDHeader))
			if id == "" {
				id = NewRequestID()
			}
			w.Header().Set(RequestIDHeader, id)
			ctx := context.WithValue(r.Context(), requestIDCtxKey{}, id)

			if quietPath(r.URL.Path) {
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			rec := &traceStatusWriter{ResponseWriter: w, status: http.StatusOK}
			start := time.Now()
			next.ServeHTTP(rec, r.WithContext(ctx))
			logger.Info("http request",
				"request_id", id,
				"method", r.Method,
				"path", r.URL.Path,
				"status", rec.status,
				"duration_ms", float64(time.Since(start).Microseconds())/1000,
			)
		})
	}
}

// quietPath reports the timer-probed endpoints that skip the access log.
func quietPath(p string) bool {
	switch p {
	case "/healthz", "/readyz", "/metrics":
		return true
	}
	return false
}

// SanitizeRequestID accepts only a conservative charset (header values are
// free-form bytes; this id ends up in logs). Exported so the gRPC trace
// interceptor enforces the identical charset on x-request-id metadata.
func SanitizeRequestID(id string) string {
	if id == "" || len(id) > maxRequestIDLen {
		return ""
	}
	for _, c := range id {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case c == '-', c == '_', c == '.', c == ':':
		default:
			return ""
		}
	}
	return id
}

// NewRequestID mints a 128-bit random hex id.
func NewRequestID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand failing means the system entropy source is gone;
		// fall back to a time-based id so requests still get traced.
		return sprintfTimeID()
	}
	return hex.EncodeToString(b[:])
}

func sprintfTimeID() string {
	var b [16]byte
	now := time.Now().UnixNano()
	for i := range b {
		b[i] = byte(now >> (i % 8 * 8))
		now = now*6364136223846793005 + 1442695040888963407
	}
	return hex.EncodeToString(b[:])
}

// traceStatusWriter captures the status code for the access log.
type traceStatusWriter struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (w *traceStatusWriter) WriteHeader(code int) {
	if !w.wrote {
		w.status = code
		w.wrote = true
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *traceStatusWriter) Write(b []byte) (int, error) {
	if !w.wrote {
		w.status = http.StatusOK
		w.wrote = true
	}
	return w.ResponseWriter.Write(b)
}
