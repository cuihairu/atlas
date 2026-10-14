-- Atlas: registration lifecycle class (docs/lifecycle.md §3 注册生命周期).
-- Two classes of registration:
--   permanent — never auto-expired; only an explicit admin instruction can
--               remove the record. The default for every pre-existing row.
--   temporary — reclaimed automatically once reg_ttl_seconds have passed
--               since the record's creation time (absolute TTL).
-- The marker is explicit, not inferred: the expiry path acts only on
-- reg_kind = 'temporary', so an absent or empty value can never opt a
-- record into auto-deletion. reg_ttl_seconds = 0 means no TTL.

ALTER TABLE servers ADD COLUMN IF NOT EXISTS reg_kind TEXT NOT NULL DEFAULT 'permanent';
ALTER TABLE servers ADD COLUMN IF NOT EXISTS reg_ttl_seconds INTEGER NOT NULL DEFAULT 0;
