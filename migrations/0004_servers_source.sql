-- Atlas: config-declared servers (docs/server-config.md).
-- Marks who owns a server's profile fields: '' = API-registered (default),
-- 'config' = declared in the servers config file (ATLAS_SERVERS_CONFIG).
-- The register API rejects updates for config-owned IDs; heartbeats and
-- lifecycle operations still apply.

BEGIN;

ALTER TABLE servers ADD COLUMN IF NOT EXISTS source TEXT NOT NULL DEFAULT '';

COMMIT;
