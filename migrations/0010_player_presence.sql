-- Atlas: player presence (PP-1, docs/presence.md).
--
-- Character online state lives on the character_index row — presence_status
-- and last_logout_at travel with the projection so every read path (the
-- directory, the character search, the fleet stats) sees one answer. The
-- column defaults to 'unknown': a row written before this migration carries
-- no presence fact, and unknown must never be reported as offline.
--
-- player_sessions is the per-character login→logout history: one open row per
-- character at a time (login opens, logout closes) plus the closed rows that
-- make the session timeline. The single-open-session rule is enforced by the
-- store state machine — the login transaction checks the open row and treats a
-- replayed login of the same session as an idempotent no-op, a login in a
-- different session as a conflict, and a stray open row as a crash to heal —
-- not by a DB uniqueness constraint.

BEGIN;

ALTER TABLE character_index
    ADD COLUMN IF NOT EXISTS presence_status TEXT NOT NULL DEFAULT 'unknown',
    ADD COLUMN IF NOT EXISTS last_logout_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_char_presence ON character_index (server_id, presence_status);

CREATE TABLE IF NOT EXISTS player_sessions (
    session_id    TEXT        PRIMARY KEY,
    account_id    BIGINT      NOT NULL,
    character_id  BIGINT      NOT NULL,
    server_id     TEXT        NOT NULL,
    started_at    TIMESTAMPTZ NOT NULL,
    ended_at      TIMESTAMPTZ,
    end_reason    TEXT        NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_session_by_char    ON player_sessions (character_id);
CREATE INDEX IF NOT EXISTS idx_session_open     ON player_sessions (character_id, started_at DESC);

COMMIT;