-- Atlas v0.2.0: cross-server coordination config (config center).
-- See docs/config-center.md.

BEGIN;

-- Config-update notify declaration on servers (subscribe / callback / poll).
-- Declared at register time; callback mode requires an absolute http(s) URL.
ALTER TABLE servers ADD COLUMN IF NOT EXISTS notify_mode         TEXT NOT NULL DEFAULT '';
ALTER TABLE servers ADD COLUMN IF NOT EXISTS notify_callback_url TEXT NOT NULL DEFAULT '';

-- Exactly one row (id = 'default'). Every save bumps version atomically in
-- the upsert itself (version = crossserver_config.version + 1), so the
-- version is monotonic under concurrent writers.
CREATE TABLE IF NOT EXISTS crossserver_config (
    id         TEXT        PRIMARY KEY DEFAULT 'default',
    version    BIGINT      NOT NULL DEFAULT 0,
    hash       TEXT        NOT NULL DEFAULT '',
    spec       JSONB       NOT NULL DEFAULT '{}'::jsonb,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMIT;
