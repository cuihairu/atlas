-- Atlas: server tag system (docs/concepts.md §Server Tags).
-- Operator-set markers on a server record (hot / full / registration closed /
-- maintenance / new server / recommended + custom). Admin-owned configuration:
-- the register upsert
-- never touches this column, so a game-server re-register cannot wipe tags.
-- Stored as a JSONB array of {code,label,tier,public}; empty array = no tags.

BEGIN;

ALTER TABLE servers ADD COLUMN IF NOT EXISTS tags JSONB NOT NULL DEFAULT '[]'::jsonb;

COMMIT;
