package audit

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	createTableSQL = `CREATE TABLE IF NOT EXISTS audit_log (
	id          BIGSERIAL PRIMARY KEY,
	recorded_at TIMESTAMPTZ NOT NULL,
	actor       TEXT NOT NULL,
	method      TEXT NOT NULL,
	path        TEXT NOT NULL,
	raw_query   TEXT NOT NULL DEFAULT '',
	status      INTEGER NOT NULL,
	body        TEXT NOT NULL DEFAULT ''
)`

	createIndexSQL = `CREATE INDEX IF NOT EXISTS audit_log_recorded_at_idx ON audit_log (recorded_at)`
)

// SQLSink writes entries into an audit_log table in the same PostgreSQL the
// service already runs, which is why it is the recommended backend: the write
// is confirmed, the rows are queryable, retention is a sweep, and prod
// deployments already own the database. The DDL is re-issued on open, so a
// deployment that has not run migrations/0009_audit_log.sql still gets
// durability.
//
// The pool is owned by the caller: Close is a no-op, so the service's normal
// pool teardown applies.
type SQLSink struct {
	pool *pgxpool.Pool

	// closeAfter closes the pool after N calls (tests only): it is how the
	// dispatcher's store-gone retry path is exercised without real downtime.
	closeAfter int
	calls      int
}

func NewSQLSink(ctx context.Context, pool *pgxpool.Pool) (*SQLSink, error) {
	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("audit sink: ping: %w", err)
	}
	for _, ddl := range []string{createTableSQL, createIndexSQL} {
		if _, err := pool.Exec(ctx, ddl); err != nil {
			return nil, fmt.Errorf("audit sink: create audit_log: %w", err)
		}
	}
	return &SQLSink{pool: pool}, nil
}

// Write appends one row. RETURNING id double-checks the row landed rather
// than returning nil after a round trip that merely did not error out.
func (s *SQLSink) Write(ctx context.Context, e Entry) error {
	s.calls++
	if s.closeAfter > 0 && s.calls == s.closeAfter {
		s.pool.Close()
	}
	var id int64
	err := s.pool.QueryRow(ctx, `INSERT INTO audit_log (recorded_at, actor, method, path, raw_query, status, body)
VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id`,
		e.Time.UTC(), e.Actor, e.Method, e.Path, e.Query, e.Status, e.Body,
	).Scan(&id)
	if err != nil {
		return fmt.Errorf("audit sink: insert: %w", err)
	}
	return nil
}

// Retain deletes entries older than before and returns how many went.
func (s *SQLSink) Retain(ctx context.Context, before time.Time) (int64, error) {
	n, err := s.pool.Exec(ctx, `DELETE FROM audit_log WHERE recorded_at < $1 RETURNING id`, before.UTC())
	if err != nil {
		return 0, fmt.Errorf("audit sink: retain: %w", err)
	}
	return n.RowsAffected(), nil
}

func (s *SQLSink) Close() error { return nil }
