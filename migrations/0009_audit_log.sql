-- Atlas: durable Admin audit log (docs/security.md §3).
-- One row per recorded Admin operation, across both transports (REST
-- middleware and gRPC UnaryAudit funnel through AuditLog.Record). The sink
-- issues this DDL itself on open (CREATE TABLE IF NOT EXISTS), so the table
-- also appears on deployments that never run this file; the migration makes
-- the schema explicit and reviewable.
-- Retention: the sink's hourly sweep deletes rows with recorded_at older
-- than ATLAS_AUDIT_RETENTION (default 90 days). The recorded_at index keeps
-- that DELETE off the insert path's table scan.

CREATE TABLE IF NOT EXISTS audit_log (
    id BIGSERIAL PRIMARY KEY,
    recorded_at TIMESTAMPTZ NOT NULL,
    actor TEXT NOT NULL,
    method TEXT NOT NULL,
    path TEXT NOT NULL,
    raw_query TEXT NOT NULL DEFAULT '',
    status INTEGER NOT NULL,
    body TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS audit_log_recorded_at_idx ON audit_log (recorded_at);
