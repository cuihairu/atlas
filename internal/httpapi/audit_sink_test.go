package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cuihairu/atlas/internal/audit"
)

// captureSink collects the entries the dispatcher handed it.
type captureSink struct {
	mu     sync.Mutex
	writes []audit.Entry
}

func (s *captureSink) Write(_ context.Context, e audit.Entry) error {
	s.mu.Lock()
	s.writes = append(s.writes, e)
	s.mu.Unlock()
	return nil
}

func (s *captureSink) Close() error { return nil }

func (s *captureSink) snapshot() []audit.Entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]audit.Entry(nil), s.writes...)
}

func sinkLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func waitForSink(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestAudit_MaskedEntriesReachTheSink(t *testing.T) {
	sink := &captureSink{}
	disp := audit.NewDispatcher(sink, 8, 0, nil, sinkLogger())
	defer disp.Drain(time.Second)

	al := NewAuditLog(10, 4096, sinkLogger()).
		WithMasker(audit.NewMasker([]string{"token"})).
		WithSink(disp)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/admin/x", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	req := httptest.NewRequest("POST", "/v1/admin/x?token=abc123&page=2", bytes.NewBufferString(`{"token":"abc123","keep":"k"}`))
	al.Middleware(mux).ServeHTTP(httptest.NewRecorder(), req)

	waitForSink(t, "one sink write", func() bool { return len(sink.snapshot()) == 1 })

	// The sink entry (and the ring it mirrors) must both carry the masked
	// payload: the secret value appears nowhere outside the hash stub.
	for _, got := range []string{sink.snapshot()[0].Body, string(al.Recent(1)[0].Body)} {
		if !strings.Contains(got, `"keep":"k"`) {
			t.Errorf("masked body lost an unmasked key: %s", got)
		}
		if strings.Contains(got, "abc123") {
			t.Errorf("masked body leaked the secret: %s", got)
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(got), &m); err != nil {
			t.Fatalf("masked body not valid JSON: %v: %s", err, got)
		}
		stub, _ := m["token"].(string)
		if !strings.HasPrefix(stub, "***:") || len(stub) != len("***:")+12 {
			t.Errorf("token = %q, want a ***:<12 hex> stub", stub)
		}
	}
	if q := sink.snapshot()[0].Query; strings.Contains(q, "abc123") || !strings.Contains(q, "page=2") {
		t.Errorf("masked query = %q, want the stub plus the kept parameter", q)
	}
}

func TestAudit_RecordsWithoutMaskerOrSink(t *testing.T) {
	// Default wiring (masker and sink unset) records exactly as before: a nil
	// masker must be a no-op, and Record must not panic without a sink.
	al := NewAuditLog(10, 4096, sinkLogger())
	al.Record(AuditEntry{
		Time: time.Now().UTC(), Actor: "operator:x", Method: "POST",
		Path: "/v1/admin/y", Status: 200, Body: []byte(`{"token":"abc123"}`),
	})
	entries := al.Recent(1)
	if len(entries) != 1 || !bytes.Contains(entries[0].Body, []byte("abc123")) {
		t.Fatalf("entry = %+v, want the body recorded as-is", entries)
	}
}

func TestAudit_DrainWithoutSinkIsNoop(t *testing.T) {
	al := NewAuditLog(4, 64, sinkLogger())
	if n := al.Drain(time.Second); n != 0 {
		t.Fatalf("drain without a sink = %d, want 0", n)
	}
}
