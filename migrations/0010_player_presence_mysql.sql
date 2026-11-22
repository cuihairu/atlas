-- Atlas (MySQL): player presence (PP-1, docs/presence.md).
-- Schema twin of 0010_player_presence.sql. DATETIME is used for the MySQL
-- column types throughout (the store writes UTC); MySQL ALTER TABLE has no
-- IF EXISTS for columns, so this file is applied once per deployment.

ALTER TABLE character_index
    ADD COLUMN presence_status VARCHAR(16) NOT NULL DEFAULT 'unknown',
    ADD COLUMN last_logout_at DATETIME(6) NULL,
    ADD KEY idx_char_presence (server_id, presence_status);

CREATE TABLE IF NOT EXISTS player_sessions (
    session_id    VARCHAR(128)  PRIMARY KEY,
    account_id    BIGINT        NOT NULL,
    character_id  BIGINT        NOT NULL,
    server_id     VARCHAR(128)  NOT NULL,
    started_at    DATETIME(6)   NOT NULL,
    ended_at      DATETIME(6)   NULL,
    end_reason    VARCHAR(16)   NOT NULL DEFAULT '',
    KEY idx_session_by_char (character_id),
    KEY idx_session_open (character_id, started_at)
);