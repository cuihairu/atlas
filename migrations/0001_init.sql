-- Atlas v0.1.1 initial schema
-- See docs/data-model.md for design rationale.

BEGIN;

-- Realms (logical game worlds / regions)
CREATE TABLE IF NOT EXISTS realms (
    id         TEXT PRIMARY KEY,
    name       TEXT        NOT NULL,
    region     TEXT        NOT NULL,
    status     TEXT        NOT NULL DEFAULT 'active',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Shards (partitions within a realm)
CREATE TABLE IF NOT EXISTS shards (
    id         TEXT PRIMARY KEY,
    realm_id   TEXT        REFERENCES realms(id) ON DELETE SET NULL,
    name       TEXT        NOT NULL,
    status     TEXT        NOT NULL DEFAULT 'active',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_shards_realm ON shards (realm_id);

-- Servers (the core registration unit)
CREATE TABLE IF NOT EXISTS servers (
    id            TEXT PRIMARY KEY,
    name          TEXT        NOT NULL,
    type          TEXT        NOT NULL DEFAULT 'game',
    region        TEXT        NOT NULL,
    realm_id      TEXT        REFERENCES realms(id) ON DELETE SET NULL,
    shard_id      TEXT        REFERENCES shards(id) ON DELETE SET NULL,
    version       TEXT        NOT NULL,
    platform      TEXT        NOT NULL DEFAULT '',
    endpoint_host TEXT        NOT NULL,
    endpoint_port INTEGER     NOT NULL,
    capacity      INTEGER     NOT NULL DEFAULT 0,
    metadata      JSONB       NOT NULL DEFAULT '{}',
    status        TEXT        NOT NULL DEFAULT 'starting',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_servers_region_status ON servers (region, status);
CREATE INDEX IF NOT EXISTS idx_servers_realm         ON servers (realm_id);
CREATE INDEX IF NOT EXISTS idx_servers_shard         ON servers (shard_id);

-- Character index (projection, not source of truth)
CREATE TABLE IF NOT EXISTS character_index (
    account_id    BIGINT      NOT NULL,
    server_id     TEXT        NOT NULL,
    character_id  BIGINT      NOT NULL,
    name          TEXT        NOT NULL,
    level         INTEGER     NOT NULL DEFAULT 1,
    class_id      INTEGER     NOT NULL DEFAULT 0,
    avatar        TEXT        NOT NULL DEFAULT '',
    metadata      JSONB       NOT NULL DEFAULT '{}',
    last_login_at TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),

    PRIMARY KEY (account_id, server_id, character_id)
);

CREATE INDEX IF NOT EXISTS idx_char_by_account ON character_index (account_id);
CREATE INDEX IF NOT EXISTS idx_char_by_char_id ON character_index (character_id);
CREATE INDEX IF NOT EXISTS idx_char_by_server  ON character_index (server_id);

-- Server migrations (merge / transfer tracking)
CREATE TABLE IF NOT EXISTS server_migrations (
    id             TEXT PRIMARY KEY,
    source_servers JSONB       NOT NULL DEFAULT '[]',
    target_server  TEXT        NOT NULL,
    status         TEXT        NOT NULL DEFAULT 'pending',
    started_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at   TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_migration_status ON server_migrations (status);

COMMIT;