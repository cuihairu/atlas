package audit

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

func TestJSONLSinkWritesOneLinePerEntry(t *testing.T) {
	f := t.TempDir() + "/audit.jsonl"
	s, err := NewJSONLSink(f)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	ctx := context.Background()
	when := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	if err := s.Write(ctx, Entry{
		Time: when, Actor: "operator:3f8a1c2b9d0e", Method: "POST",
		Path: "/v1/admin/servers/game-1001/drain", Query: "force=1", Status: 200,
		Body: `{"reason":"patch"}`,
	}); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := s.Write(ctx, Entry{
		Time: when.Add(time.Second), Actor: "viewer:aa", Method: "GET",
		Path: "/v1/admin/audit", Status: 403, Body: "",
	}); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := s.Write(ctx, Entry{
		Time: when.Add(2 * time.Second), Actor: "viewer:bb", Method: "POST",
		Path: "/v1/admin/broken", Status: 400, Body: `not json`,
	}); err != nil {
		t.Fatalf("write: %v", err)
	}

	file, err := os.Open(f)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer file.Close()
	sc := bufio.NewScanner(file)
	var lines []string
	for sc.Scan() {
		if l := sc.Text(); l != "" {
			lines = append(lines, l)
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(lines) != 3 {
		t.Fatalf("lines = %d, want 3 (one per entry)", len(lines))
	}

	type row struct {
		Time   time.Time `json:"time"`
		Actor  string    `json:"actor"`
		Method string    `json:"method"`
		Path   string    `json:"path"`
		Query  string    `json:"query"`
		Status int       `json:"status"`
		Body   any       `json:"body"`
	}
	var a, b, c row
	if err := json.Unmarshal([]byte(lines[0]), &a); err != nil {
		t.Fatalf("line 1: %v: %s", err, lines[0])
	}
	if err := json.Unmarshal([]byte(lines[1]), &b); err != nil {
		t.Fatalf("line 2: %v: %s", err, lines[1])
	}
	if err := json.Unmarshal([]byte(lines[2]), &c); err != nil {
		t.Fatalf("line 3: %v: %s", err, lines[2])
	}

	if a.Actor != "operator:3f8a1c2b9d0e" || a.Query != "force=1" || a.Status != 200 {
		t.Fatalf("row 1 = %+v", a)
	}
	if _, ok := a.Body.(map[string]any); !ok {
		t.Fatalf("row 1 body should embed the JSON value, got %T: %s", a.Body, lines[0])
	}
	if a.Time != when {
		t.Fatalf("row 1 time = %v, want %v", a.Time, when)
	}
	// A read with no payload carries no body key at all.
	if b.Body != nil || b.Query != "" {
		t.Fatalf("row 2 = %+v, want no body and no query", b)
	}
	// An invalid payload is kept as a string, mirroring the ring.
	if c.Body != "not json" {
		t.Fatalf("row 3 body = %v, want the raw string", c.Body)
	}
}

func TestJSONLSinkAppendsAcrossReopens(t *testing.T) {
	f := t.TempDir() + "/audit.jsonl"
	ctx := context.Background()
	s1, err := NewJSONLSink(f)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := s1.Write(ctx, Entry{Method: "POST", Path: "/a", Status: 200}); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := s1.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	s2, err := NewJSONLSink(f)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()
	if err := s2.Write(ctx, Entry{Method: "GET", Path: "/b", Status: 200}); err != nil {
		t.Fatalf("write: %v", err)
	}
	b, err := os.ReadFile(f)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if countLines(b) != 2 {
		t.Fatalf("lines = %d, want 2 (the first entry survived the reopen)", countLines(b))
	}
}

func TestJSONLSinkRejectsAnUnwritablePath(t *testing.T) {
	if _, err := NewJSONLSink("/no/such/dir/audit.jsonl"); err == nil {
		t.Fatal("opening under a missing directory succeeded")
	}
	if _, err := NewJSONLSink(t.TempDir()); err == nil {
		t.Fatal("opening a directory succeeded")
	}
}

func TestEntryMarshal(t *testing.T) {
	when := time.Date(2026, 10, 1, 12, 30, 0, 0, time.UTC).Add(2 * time.Hour)
	b, err := Entry{
		Time: when, Actor: "viewer:aa", Method: "GET", Path: "/v1/admin/servers",
		Query: "x=1", Status: 204, Body: `{"k":1}`,
	}.Marshal()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var row map[string]any
	if err := json.Unmarshal(b, &row); err != nil {
		t.Fatalf("marshal output is not JSON: %v: %s", err, b)
	}
	for _, key := range []string{"time", "actor", "method", "path", "query", "status", "body"} {
		if _, ok := row[key]; !ok {
			t.Errorf("row is missing %q: %s", key, b)
		}
	}
	// A time recorded in another zone is rendered in UTC.
	if got := row["time"].(string); !strings.HasPrefix(got, "2026-10-01T14:30:00Z") {
		t.Fatalf("time = %q, want the UTC rendering", got)
	}
	if row["body"].(map[string]any)["k"].(float64) != 1 {
		t.Fatalf("body = %v", row["body"])
	}
}

func countLines(b []byte) int {
	n := 0
	for i := 0; i < len(b); i++ {
		if b[i] == '\n' {
			n++
		}
	}
	return n
}
