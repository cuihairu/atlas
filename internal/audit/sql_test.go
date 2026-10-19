package audit

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// openTestPool provisions the audit_log table against the CI PostgreSQL the
// store contract uses. Without the variable the tests skip, so `go test
// ./...` stays dependency-free locally.
func openTestPool(t *testing.T) (*pgxpool.Pool, context.Context) {
	t.Helper()
	url := os.Getenv("ATLAS_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("ATLAS_TEST_DATABASE_URL not set; skipping the SQL sink contract")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatalf("connect test database: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool, ctx
}

func TestSQLSinkWritesAndReadsBack(t *testing.T) {
	pool, ctx := openTestPool(t)
	s, err := NewSQLSink(ctx, pool)
	if err != nil {
		t.Fatalf("new sink: %v", err)
	}
	_, _ = pool.Exec(ctx, `DELETE FROM audit_log`)

	when := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	if err := s.Write(ctx, Entry{
		Time: when, Actor: "operator:3f8a1c2b9d0e", Method: "POST",
		Path: "/v1/admin/servers/game-1001/drain", Query: "force=1", Status: 200,
		Body: `{"reason":"patch"}`,
	}); err != nil {
		t.Fatalf("write: %v", err)
	}

	var actor, method, path, rawQuery, body string
	var status int
	var recorded time.Time
	err = pool.QueryRow(ctx, `SELECT recorded_at, actor, method, path, raw_query, status, body FROM audit_log WHERE path = $1`,
		"/v1/admin/servers/game-1001/drain").
		Scan(&recorded, &actor, &method, &path, &rawQuery, &status, &body)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if actor != "operator:3f8a1c2b9d0e" || method != "POST" || rawQuery != "force=1" || status != 200 || body != `{"reason":"patch"}` {
		t.Fatalf("row = %q %q %q %q %d %q", actor, method, path, rawQuery, status, body)
	}
	if !recorded.Equal(when) {
		t.Fatalf("recorded_at = %v, want %v", recorded, when)
	}
}

func TestSQLSinkCreatesTheTable(t *testing.T) {
	pool, ctx := openTestPool(t)
	if _, err := pool.Exec(ctx, `DROP TABLE IF EXISTS audit_log`); err != nil {
		t.Fatalf("drop: %v", err)
	}
	// The DDL is re-issued on open, so a deployment that has not run
	// migrations/0009_audit_log.sql still gets durability.
	if _, err := NewSQLSink(ctx, pool); err != nil {
		t.Fatalf("new sink on a fresh table: %v", err)
	}
	var name string
	if err := pool.QueryRow(ctx, `SELECT tablename FROM pg_tables WHERE tablename = 'audit_log'`).Scan(&name); err != nil {
		t.Fatalf("audit_log was not created: %v", err)
	}
	// The index is what keeps the retention sweep off the write path's table.
	var idx string
	if err := pool.QueryRow(ctx, `SELECT indexname FROM pg_indexes WHERE tablename = 'audit_log' AND indexname = 'audit_log_recorded_at_idx'`).Scan(&idx); err != nil {
		t.Fatalf("retention index missing: %v", err)
	}
	// Re-opening is idempotent (the service reopens after a restart).
	if _, err := NewSQLSink(ctx, pool); err != nil {
		t.Fatalf("re-open: %v", err)
	}
}

func TestSQLSinkRetainDeletesOnlyOldRows(t *testing.T) {
	pool, ctx := openTestPool(t)
	s, err := NewSQLSink(ctx, pool)
	if err != nil {
		t.Fatalf("new sink: %v", err)
	}
	_, _ = pool.Exec(ctx, `DELETE FROM audit_log`)

	old := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	fresh := old.Add(2 * time.Hour)
	for _, when := range []time.Time{old, old.Add(time.Hour), fresh, fresh.Add(time.Hour)} {
		if err := s.Write(ctx, Entry{Time: when, Method: "POST", Path: "/v1/admin/x", Status: 200}); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	// The cutoff sits between the two old rows and the two fresh ones, so the
	// sweep must delete exactly the two old rows.
	cutoff := old.Add(90 * time.Minute)
	n, err := s.Retain(ctx, cutoff)
	if err != nil {
		t.Fatalf("retain: %v", err)
	}
	if n != 2 {
		t.Fatalf("retained = %d, want 2", n)
	}
	var left int64
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM audit_log`).Scan(&left); err != nil {
		t.Fatalf("count: %v", err)
	}
	if left != 2 {
		t.Fatalf("rows left = %d, want 2 (the fresh ones)", left)
	}
	// Retaining again is a no-op, not an error.
	if n, err := s.Retain(ctx, cutoff); err != nil || n != 0 {
		t.Fatalf("second retain = %d %v, want 0/nil", n, err)
	}
}

func TestSQLSinkWriteFailureSurfaces(t *testing.T) {
	pool, ctx := openTestPool(t)
	s, err := NewSQLSink(ctx, pool)
	if err != nil {
		t.Fatalf("new sink: %v", err)
	}
	// The pool disappears mid-flight: the entry is lost, and the dispatcher
	// has to count it as dropped — which only works if the error surfaces.
	s.closeAfter = 1
	if err := s.Write(ctx, Entry{Method: "POST", Path: "/v1/admin/x", Status: 200}); err == nil {
		t.Fatal("writing through a closed pool succeeded")
	}
}

func TestSQLSinkCloseLeavesThePoolToItsOwner(t *testing.T) {
	pool, ctx := openTestPool(t)
	s, err := NewSQLSink(ctx, pool)
	if err != nil {
		t.Fatalf("new sink: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	// The pool is owned by the caller: the sink must not have closed it.
	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("pool unusable after sink close: %v", err)
	}
	if err := s.Write(ctx, Entry{Method: "GET", Path: "/v1/admin/y", Status: 200}); err != nil {
		t.Fatalf("write after close: %v", err)
	}
}
