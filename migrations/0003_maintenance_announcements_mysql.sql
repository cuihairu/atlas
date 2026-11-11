-- Atlas v0.1.20 (MySQL): server metadata, maintenance windows, announcements
-- See docs/data-model.md §6 and docs/lifecycle.md §5.

-- Server process start time, reported at register (re-register updates it).
ALTER TABLE servers ADD COLUMN started_at DATETIME(6) NULL;

-- Scheduled maintenance intervals. Transient: the health monitor deletes a
-- window once it has restored the server's previous status.
CREATE TABLE IF NOT EXISTS maintenance_windows (
    id               VARCHAR(128) PRIMARY KEY,
    server_id        VARCHAR(128) NOT NULL,
    start_at         DATETIME(6)  NOT NULL,
    end_at           DATETIME(6)  NOT NULL,
    previous_status  VARCHAR(32)  NOT NULL DEFAULT '',
    announcement_id  VARCHAR(128) NULL,
    created_at       DATETIME(6)  NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    INDEX idx_maintenance_windows_server (server_id),
    INDEX idx_maintenance_windows_start (start_at)
);

-- Client-facing notices, global (server_id NULL) or server-scoped.
CREATE TABLE IF NOT EXISTS announcements (
    id         VARCHAR(128) PRIMARY KEY,
    server_id  VARCHAR(128) NULL,
    title      VARCHAR(255) NOT NULL,
    body       TEXT         NULL,
    level      VARCHAR(16)  NOT NULL DEFAULT 'info',
    starts_at  DATETIME(6)  NOT NULL,
    ends_at    DATETIME(6)  NOT NULL,
    created_at DATETIME(6)  NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    INDEX idx_announcements_server (server_id),
    INDEX idx_announcements_window (starts_at, ends_at)
);
