-- Atlas: server tag system (docs/concepts.md §服务器标记).
-- Operator-set markers on a server record (火热 / 爆满 / 禁止注册 / 维护中 /
-- 新服 / 推荐 + custom). Admin-owned configuration: the register upsert
-- never touches this column, so a game-server re-register cannot wipe tags.
-- Stored as a JSONB array of {code,label,tier,public}; empty array = no tags.

BEGIN;

ALTER TABLE servers ADD COLUMN IF NOT EXISTS tags JSONB NOT NULL DEFAULT '[]'::jsonb;

COMMIT;
