-- Atlas: player presence (PP-1) — online state and play sessions.
--
-- Presence is a projection like the character index itself: game servers
-- report login/logout events, Atlas projects them into (a) two denormalized
-- columns on character_index so the hot "is this character online" read is a
-- single-row lookup, and (b) player_sessions, the append-style history one
-- row per play session.
--
-- presence_status defaults to 'unknown' so rows projected by game servers
-- that never report presence stay distinguishable from a reported 'offline'.

ALTER TABLE character_index
    ADD COLUMN IF NOT EXISTS presence_status TEXT NOT NULL DEFAULT 'unknown',
    ADD COLUMN IF NOT EXISTS last_logout_at  TIMESTAMPTZ;

-- The online list of a server is the hot read (per-server presence roster):
-- filter by server, then by status.
CREATE INDEX IF NOT EXISTS idx_char_by_server_presence
    ON character_index (server_id, presence_status);

-- Append-style session history. The closed-session history of one character
-- is read newest-first; the partial unique index enforces the invariant that
-- a character has at most one OPEN session (ended_at IS NULL) — the same
-- invariant the store checks under its write lock.
CREATE TABLE IF NOT EXISTS player_sessions (
    session_id    TEXT        PRIMARY KEY,
    account_id    BIGINT      NOT NULL,
    character_id  BIGINT      NOT NULL,
    server_id     TEXT        NOT NULL,
    started_at    TIMESTAMPTZ NOT NULL,
    ended_at      TIMESTAMPTZ,
    end_reason    TEXT        NOT NULL DEFAULT ''

    -- CHECK (end_reason IN ('', 'normal', 'timeout', 'kicked', 'crash'))
    -- is enforced in the application layer (model.SessionEndReason.Valid)
    -- so the migration stays portable across PG/MySQL syntax differences.
);

CREATE INDEX IF NOT EXISTS idx_session_by_character
    ON player_sessions (character_id, started_at DESC);

CREATE UNIQUE INDEX IF NOT EXISTS uq_session_open_per_character
    ON player_sessions (character_id) WHERE ended_at IS NULL;
