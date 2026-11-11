-- Atlas (MySQL): registration lifecycle class (docs/lifecycle.md §3 注册生命周期).
-- permanent = never auto-expired (default for pre-existing rows);
-- temporary = reclaimed once reg_ttl_seconds have passed since creation.
-- Explicit marker: the expiry path acts only on reg_kind = 'temporary'.

ALTER TABLE servers ADD COLUMN reg_kind VARCHAR(16) NOT NULL DEFAULT 'permanent';
ALTER TABLE servers ADD COLUMN reg_ttl_seconds INT NOT NULL DEFAULT 0;
