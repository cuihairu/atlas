// Package audit is the persistent audit sink behind the Admin operation log.
//
// The in-memory ring (internal/httpapi) is an ops view that dies with the
// process; a sink makes the record durable and queryable. The choke point
// both transports already funnel through is AuditLog.Record, so a sink only
// needs to be attached there — the REST middleware and the gRPC UnaryAudit
// interceptor never learn that a database is involved.
//
// Writes are asynchronous (Dispatcher), which is an explicit constraint: the
// sink must never add latency to an Admin request. Durability is therefore
// best effort and honestly counted — overflow and flush timeouts increment a
// drop counter rather than applying backpressure.
package audit

import (
	"encoding/json"
	"time"
)

// Entry is one recorded Admin operation in transport-neutral form.
type Entry struct {
	Time   time.Time
	Actor  string // "role:key-fingerprint" (httpapi.Actor.String)
	Method string // HTTP method, or the gRPC-mapped equivalent (GET / POST)
	Path   string // URL path, or the gRPC full method name
	Query  string
	Status int
	Body   string // request payload of mutating methods (masked, size-capped)
}

// Marshal renders one entry as a compact JSON object (the JSONL sink's line
// format). A valid-JSON body is embedded as a JSON value; anything else stays
// a string, mirroring how the ring wraps invalid payloads.
func (e Entry) Marshal() ([]byte, error) {
	var body any // nil means "no body", so a read drops the key entirely
	if e.Body != "" {
		body = e.Body
		var parsed any
		if err := json.Unmarshal([]byte(e.Body), &parsed); err == nil {
			body = parsed
		}
	}
	type row struct {
		Time   time.Time `json:"time"`
		Actor  string    `json:"actor"`
		Method string    `json:"method"`
		Path   string    `json:"path"`
		Query  string    `json:"query,omitempty"`
		Status int       `json:"status"`
		Body   any       `json:"body,omitempty"`
	}
	return json.Marshal(row{
		Time: e.Time.UTC(), Actor: e.Actor, Method: e.Method, Path: e.Path,
		Query: e.Query, Status: e.Status, Body: body,
	})
}
