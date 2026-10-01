-- Atlas v0.1.20: server metadata, maintenance windows, announcements
-- See docs/data-model.md §6 and docs/lifecycle.md §5.

BEGIN;

-- Server process start time, reported at register (re-register updates it).
ALTER TABLE servers ADD COLUMN IF NOT EXISTS started_at TIMESTAMPTZ;

-- Scheduled maintenance intervals. Transient: the health monitor deletes a
-- window once it has restored the server's previous status.
CREATE TABLE IF NOT EXISTS maintenance_windows (
    id               TEXT        PRIMARY KEY,
    server_id        TEXT        NOT NULL,
    start_at         TIMESTAMPTZ NOT NULL,
    end_at           TIMESTAMPTZ NOT NULL,
    previous_status  TEXT        NOT NULL DEFAULT '',
    announcement_id  TEXT,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_maintenance_windows_server ON maintenance_windows (server_id);
CREATE INDEX IF NOT EXISTS idx_maintenance_windows_start ON maintenance_windows (start_at);

-- Client-facing notices, global (server_id NULL) or server-scoped.
CREATE TABLE IF NOT EXISTS announcements (
    id         TEXT        PRIMARY KEY,
    server_id  TEXT,
    title      TEXT        NOT NULL,
    body       TEXT        NOT NULL DEFAULT '',
    level      TEXT        NOT NULL DEFAULT 'info',
    starts_at  TIMESTAMPTZ NOT NULL,
    ends_at    TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_announcements_server ON announcements (server_id);
CREATE INDEX IF NOT EXISTS idx_announcements_window ON announcements (starts_at, ends_at);

COMMIT;
