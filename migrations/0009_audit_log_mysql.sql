-- Atlas (MySQL): durable Admin audit log (docs/security.md §3).
-- Schema twin of 0009_audit_log.sql. The runtime sink reads/writes through
-- pgx (ATLAS_STORE=postgres); this file keeps the migration set in parity
-- for MySQL deployments managing the table with their own tooling.

CREATE TABLE IF NOT EXISTS audit_log (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    recorded_at TIMESTAMP(6) NOT NULL,
    actor TEXT NOT NULL,
    method VARCHAR(32) NOT NULL,
    path TEXT NOT NULL,
    raw_query TEXT NOT NULL,
    status INT NOT NULL,
    body MEDIUMTEXT NOT NULL,
    KEY audit_log_recorded_at_idx (recorded_at)
);
