-- Atlas v0.1.1 — MySQL schema
-- Equivalent to the PostgreSQL schema in migrations/0001_init.sql

CREATE TABLE IF NOT EXISTS realms (
    id        VARCHAR(128) PRIMARY KEY,
    name      VARCHAR(255) NOT NULL,
    region    VARCHAR(128) NOT NULL,
    status    VARCHAR(32)  NOT NULL DEFAULT 'active',
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS shards (
    id        VARCHAR(128) PRIMARY KEY,
    realm_id  VARCHAR(128) DEFAULT NULL,
    name      VARCHAR(255) NOT NULL,
    status    VARCHAR(32)  NOT NULL DEFAULT 'active',
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    INDEX idx_shards_realm (realm_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS servers (
    id            VARCHAR(128) PRIMARY KEY,
    name          VARCHAR(255) NOT NULL,
    type          VARCHAR(64)  NOT NULL DEFAULT 'game',
    region        VARCHAR(128) NOT NULL,
    realm_id      VARCHAR(128) DEFAULT NULL,
    shard_id      VARCHAR(128) DEFAULT NULL,
    version       VARCHAR(64)  NOT NULL,
    platform      VARCHAR(64)  NOT NULL DEFAULT '',
    endpoint_host VARCHAR(255) NOT NULL,
    endpoint_port INT          NOT NULL,
    capacity      INT          NOT NULL DEFAULT 0,
    -- Metadata is optional; inserts never write it (the store does not map
    -- it yet). MySQL cannot put a DEFAULT on JSON columns, so allow NULL —
    -- postgres uses DEFAULT '{}'::jsonb there.
    metadata      JSON         DEFAULT NULL,
    status        VARCHAR(32)  NOT NULL DEFAULT 'starting',
    created_at    DATETIME(6)  NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at    DATETIME(6)  NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    INDEX idx_servers_region_status (region, status),
    INDEX idx_servers_realm (realm_id),
    INDEX idx_servers_shard (shard_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS character_index (
    account_id    BIGINT       NOT NULL,
    server_id     VARCHAR(128) NOT NULL,
    character_id  BIGINT       NOT NULL,
    name          VARCHAR(255) NOT NULL,
    level         INT          NOT NULL DEFAULT 1,
    class_id      INT          NOT NULL DEFAULT 0,
    avatar        VARCHAR(255) NOT NULL DEFAULT '',
    metadata      JSON         DEFAULT NULL,
    last_login_at DATETIME(6)  DEFAULT NULL,
    created_at    DATETIME(6)  NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at    DATETIME(6)  NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (account_id, server_id, character_id),
    INDEX idx_char_by_char_id (character_id),
    INDEX idx_char_by_server (server_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS server_migrations (
    id             VARCHAR(128) PRIMARY KEY,
    source_servers JSON         NOT NULL,
    target_server  VARCHAR(128) NOT NULL,
    status         VARCHAR(32)  NOT NULL DEFAULT 'pending',
    started_at     DATETIME(6)  NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    completed_at   DATETIME(6)  DEFAULT NULL,
    INDEX idx_migration_status (status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;