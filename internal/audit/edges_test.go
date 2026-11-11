package audit

import (
	"bytes"
	"context"
	"encoding/json"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// dbURL returns the test database DSN, skipping the test when the variable
// is unset (same contract as openTestPool).
func dbURL(t *testing.T) string {
	t.Helper()
	u := os.Getenv("ATLAS_TEST_DATABASE_URL")
	if u == "" {
		t.Skip("ATLAS_TEST_DATABASE_URL not set; skipping the SQL sink error contract")
	}
	return u
}

// dsnAs rewrites the credentials of a DSN so a test can connect as a
// different (restricted) role against the same host and database.
func dsnAs(base, user, pass string) string {
	u, err := url.Parse(base)
	if err != nil {
		return base
	}
	u.User = url.UserPassword(user, pass)
	return u.String()
}

// TestJSONLSinkWriteAfterClose pins the append-error branch: writing to a
// closed file surfaces as a wrapped error instead of a silent success that
// would lose the entry.
func TestJSONLSinkWriteAfterClose(t *testing.T) {
	s, err := NewJSONLSink(t.TempDir() + "/audit.jsonl")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if err := s.Write(context.Background(), Entry{Path: "/v1/admin/a"}); err == nil {
		t.Fatal("write after close: expected an error, got nil")
	}
}

// TestMaskBodyScalarAndNullValues pins the scalar passthrough cases of the
// value switch: a bare boolean and a bare null are returned verbatim (the
// masker only rewrites objects), and the same holds for a bare string and
// number (the other two scalar arms).
func TestMaskBodyScalarAndNullValues(t *testing.T) {
	m := NewMasker([]string{"secret"})
	for _, body := range []string{`"just a string"`, `42`, `true`, `null`} {
		if got := string(m.MaskBody([]byte(body))); got != body {
			t.Errorf("MaskBody(%s) = %s, want unchanged", body, got)
		}
	}
}

// TestMaskBodyDeniedKeyObjectValue pins the raw-copy path for a denied key
// whose value is itself an object: the whole value is redacted, and the
// nil-receiver re-encode (the `(*Masker)(nil)` copy used to measure the
// value) walks the object without a denylist.
func TestMaskBodyDeniedKeyObjectValue(t *testing.T) {
	m := NewMasker([]string{"token"})
	got := string(m.MaskBody([]byte(`{"token":{"nested":1,"keep":2},"keep":3}`)))
	if strings.Contains(got, `"nested"`) || strings.Contains(got, `"keep":2`) {
		t.Fatalf("denied object value not fully redacted: %s", got)
	}
	if !strings.Contains(got, `"keep":3`) {
		t.Fatalf("non-denied sibling lost: %s", got)
	}
	if !strings.Contains(got, `"***:`) {
		t.Fatalf("denied key not redacted: %s", got)
	}
}

// TestMaskQueryInvalidPercentEncoding pins the parse-failure branch: a query
// with a bad percent escape is returned byte-identical rather than partially
// redacted or dropped.
func TestMaskQueryInvalidPercentEncoding(t *testing.T) {
	m := NewMasker([]string{"token"})
	q := "token=%zz&page=2"
	if got := m.MaskQuery(q); got != q {
		t.Fatalf("MaskQuery(%q) = %q, want unchanged", q, got)
	}
}

// TestMaskCopyErrorBranches drives the masker's internal decoder directly
// with malformed inputs. The public MaskBody guards json.Valid up front, so
// these error returns are only reachable through the re-encoder's own
// contract (a denied value or nested structure that does not parse). Each
// case must surface an error, never a panic.
func TestMaskCopyErrorBranches(t *testing.T) {
	m := NewMasker([]string{"token"})
	cases := map[string]string{
		"empty input":          ``,
		"truncated object key": `{"`,
		"mismatched delimiter": `{"a":1]`,
		"denied key no value":  `{"token":`,
		"plain key no value":   `{"a":`,
		"object missing close": `{"a":1`,
		"array missing value":  `[`,
		"array missing close":  `[1`,
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			var out bytes.Buffer
			dec := json.NewDecoder(strings.NewReader(in))
			dec.UseNumber()
			if err := m.copy(&out, dec); err == nil {
				t.Errorf("copy(%q) = nil, want an error", in)
			}
		})
	}
}

// TestDispatcherSizeFallback pins the queue-size clamp: a non-positive size
// falls back to the 1000-entry default instead of building an unbuffered
// channel that would turn every Submit into a drop.
func TestDispatcherSizeFallback(t *testing.T) {
	for _, size := range []int{0, -5} {
		d := NewDispatcher(newTestSink(t, nil), size, 0, nil, testLogger(t))
		d.Submit(Entry{Path: "/v1/admin/size"})
		waitFor(t, "one sink write", func() bool {
			w, _ := d.Stats()
			return w == 1
		})
		d.Drain(time.Second)
	}
}

// TestDispatcherNilLoggerFallsBack pins the logger clamp: a nil logger is
// replaced by a discarding one so the drop/write paths never panic on a
// missing logger (a caller that wires the dispatcher without telemetry).
func TestDispatcherNilLoggerFallsBack(t *testing.T) {
	d := NewDispatcher(newTestSink(t, nil), 4, 0, nil, nil)
	d.Submit(Entry{Path: "/v1/admin/nil-logger"})
	waitFor(t, "one sink write", func() bool {
		w, _ := d.Stats()
		return w == 1
	})
	d.Drain(time.Second)
}

// TestDispatcherDrainStopsRetentionSweep pins the sweepLoop shutdown: with a
// retention window the sweep goroutine is running, and a Drain must close
// done so the loop exits instead of leaking a timer goroutine.
func TestDispatcherDrainStopsRetentionSweep(t *testing.T) {
	d := NewDispatcher(newTestSink(t, nil), 8, 90*24*time.Hour, nil, testLogger(t))
	if dropped := d.Drain(time.Second); dropped != 0 {
		t.Fatalf("drain dropped = %d, want 0", dropped)
	}
}

// TestSQLSinkPingFailure pins NewSQLSink against a dead pool: the ping error
// is wrapped and returned, never a nil sink.
func TestSQLSinkPingFailure(t *testing.T) {
	url := dbURL(t)
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	pool.Close()
	if _, err := NewSQLSink(ctx, pool); err == nil {
		t.Fatal("NewSQLSink on a closed pool: expected an error, got nil")
	}
}

// TestSQLSinkExecFailure pins the DDL branch: a connected pool that cannot
// create the audit table surfaces the exec error instead of pretending the
// sink is ready.
func TestSQLSinkExecFailure(t *testing.T) {
	ctx := context.Background()
	admin, _ := openTestPool(t)

	// A non-superuser role has USAGE on public but not CREATE (PG15 default),
	// so the sink's CREATE TABLE must fail.
	if _, err := admin.Exec(ctx, `DO $$ BEGIN
		IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'atlas_nopriv') THEN
			CREATE ROLE atlas_nopriv LOGIN PASSWORD 'atlas_nopriv';
		END IF;
	END $$`); err != nil {
		t.Fatalf("create restricted role: %v", err)
	}
	var db string
	if err := admin.QueryRow(ctx, `SELECT current_database()`).Scan(&db); err != nil {
		t.Fatalf("current database: %v", err)
	}
	if _, err := admin.Exec(ctx, `GRANT CONNECT ON DATABASE `+db+` TO atlas_nopriv`); err != nil {
		t.Fatalf("grant connect: %v", err)
	}

	nopriv, err := pgxpool.New(ctx, dsnAs(dbURL(t), "atlas_nopriv", "atlas_nopriv"))
	if err != nil {
		t.Fatalf("connect as restricted role: %v", err)
	}
	t.Cleanup(nopriv.Close)
	if _, err := NewSQLSink(ctx, nopriv); err == nil {
		t.Fatal("NewSQLSink without CREATE privilege: expected an error, got nil")
	}
}

// TestSQLSinkRetainFailure pins the retention DELETE error branch: a closed
// pool surfaces a wrapped error rather than a silent zero.
func TestSQLSinkRetainFailure(t *testing.T) {
	url := dbURL(t)
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	sink, err := NewSQLSink(ctx, pool)
	if err != nil {
		t.Fatalf("new sink: %v", err)
	}
	pool.Close()
	if _, err := sink.Retain(ctx, time.Now().Add(-time.Hour)); err == nil {
		t.Fatal("Retain on a closed pool: expected an error, got nil")
	}
}
