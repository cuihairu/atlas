-- Atlas (MySQL): player presence (PP-1) — online state and play sessions.
-- Schema twin of 0010_player_presence.sql.

ALTER TABLE character_index
    ADD COLUMN presence_status VARCHAR(16) NOT NULL DEFAULT 'unknown',
    ADD COLUMN last_logout_at  DATETIME(6) NULL;

CREATE INDEX idx_char_by_server_presence
    ON character_index (server_id, presence_status);

CREATE TABLE IF NOT EXISTS player_sessions (
    session_id    VARCHAR(64)  PRIMARY KEY,
    account_id    BIGINT       NOT NULL,
    character_id  BIGINT       NOT NULL,
    server_id     VARCHAR(255) NOT NULL,
    started_at    DATETIME(6)  NOT NULL,
    ended_at      DATETIME(6)  NULL,
    end_reason    VARCHAR(16)  NOT NULL DEFAULT '',
    -- open_flag emulates the PG partial unique index: NULL (a unique value
    -- in MySQL) for every closed session, 1 for the single open one — so
    -- (character_id, open_flag) is unique exactly when at most one session
    -- per character is open.
    open_flag     TINYINT      GENERATED ALWAYS AS (IF(ended_at IS NULL, 1, NULL)) STORED,
    KEY idx_session_by_character (character_id, started_at DESC),
    UNIQUE KEY uq_session_open_per_character (character_id, open_flag)
);
