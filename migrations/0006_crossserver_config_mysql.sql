-- Atlas v0.2.0 (MySQL): cross-server coordination config (config center).
-- See docs/config-center.md.

-- Config-update notify declaration on servers (subscribe / callback / poll).
ALTER TABLE servers ADD COLUMN notify_mode         VARCHAR(16)  NOT NULL DEFAULT '';
ALTER TABLE servers ADD COLUMN notify_callback_url VARCHAR(512) NOT NULL DEFAULT '';

-- Exactly one row (id = 'default'). Version is bumped atomically inside the
-- upsert (version = version + 1 on duplicate key), keeping it monotonic.
CREATE TABLE IF NOT EXISTS crossserver_config (
    id         VARCHAR(32)  PRIMARY KEY,
    version    BIGINT       NOT NULL DEFAULT 0,
    hash       VARCHAR(64)  NOT NULL DEFAULT '',
    spec       JSON         NOT NULL,
    updated_at DATETIME(6)  NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
);
