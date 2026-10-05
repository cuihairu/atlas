// Audit logging for Admin API operations (TODO v0.1.17): every request is
// recorded with actor, timestamp, method, path, response status and — for
// mutating methods — the request payload as the change record. Entries go to
// the structured log (slog) and to a bounded in-memory ring exposed via
// GET /v1/admin/audit for ops tooling.
package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

// AuditEntry is one recorded Admin API operation.
type AuditEntry struct {
	Time   time.Time       `json:"time"`
	Actor  string          `json:"actor"` // "role:fingerprint" (see Actor.String)
	Method string          `json:"method"`
	Path   string          `json:"path"`
	Status int             `json:"status"`
	Body   json.RawMessage `json:"body,omitempty"` // mutating methods only, truncated
	Query  string          `json:"query,omitempty"`
}

// AuditLog records Admin API operations.
type AuditLog struct {
	logger *slog.Logger
	max    int // ring capacity

	mu      sync.Mutex
	ring    []AuditEntry
	maxBody int
}

// NewAuditLog creates an audit log holding the most recent max entries
// (<= 0 means 1000). maxBody bounds the captured request payload per entry
// (<= 0 means 4096 bytes).
func NewAuditLog(max, maxBody int, logger *slog.Logger) *AuditLog {
	if max <= 0 {
		max = 1000
	}
	if maxBody <= 0 {
		maxBody = 4096
	}
	return &AuditLog{logger: logger, max: max, maxBody: maxBody}
}

// auditStatusWriter captures the response status for the audit entry.
type auditStatusWriter struct {
	http.ResponseWriter
	status int
}

func (w *auditStatusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (a *AuditLog) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Capture the payload of mutating requests (the change record);
		// reads have no diff to record.
		var body []byte
		if isMutating(r.Method) && r.Body != nil {
			buf, err := io.ReadAll(io.LimitReader(r.Body, int64(a.maxBody)+1))
			if err == nil {
				if len(buf) > a.maxBody {
					buf = buf[:a.maxBody]
				}
				body = buf
				r.Body = io.NopCloser(bytes.NewReader(buf))
			}
		}

		sw := &auditStatusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r)

		actor := ActorFrom(r.Context())
		entry := AuditEntry{
			Time:   time.Now().UTC(),
			Actor:  actor.String(),
			Method: r.Method,
			Path:   r.URL.Path,
			Status: sw.status,
			Query:  r.URL.RawQuery,
		}
		if len(body) > 0 {
			// Keep valid JSON as-is; anything else (broken payloads) is
			// wrapped as a string so the audit entry itself always marshals.
			if json.Valid(body) {
				entry.Body = json.RawMessage(body)
			} else {
				if raw, err := json.Marshal(string(body)); err == nil {
					entry.Body = raw
				}
			}
		}

		a.Record(entry)
	})
}

// Record appends an entry to the ring and mirrors it to the structured log.
// Transport-neutral: the REST middleware and the gRPC UnaryAudit
// interceptor both funnel through here, so ops tooling sees one ring and
// one log vocabulary regardless of how the operation arrived.
func (a *AuditLog) Record(e AuditEntry) {
	a.mu.Lock()
	a.ring = append(a.ring, e)
	if len(a.ring) > a.max {
		a.ring = a.ring[len(a.ring)-a.max:]
	}
	a.mu.Unlock()

	a.logger.Info("admin audit",
		"actor", e.Actor,
		"method", e.Method,
		"path", e.Path,
		"query", e.Query,
		"status", e.Status,
		"body", string(e.Body),
		"time", e.Time.Format(time.RFC3339Nano),
	)
}

// MaxBody reports the per-entry payload bound the log enforces.
func (a *AuditLog) MaxBody() int { return a.maxBody }

// Recent returns up to n most recent entries, oldest first.
func (a *AuditLog) Recent(n int) []AuditEntry {
	a.mu.Lock()
	defer a.mu.Unlock()
	if n <= 0 || n > len(a.ring) {
		n = len(a.ring)
	}
	out := make([]AuditEntry, n)
	copy(out, a.ring[len(a.ring)-n:])
	return out
}

func isMutating(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	}
	return false
}
