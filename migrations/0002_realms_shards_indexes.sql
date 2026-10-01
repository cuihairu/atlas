-- Realm / shard management (TODO v0.1.14) support indexes.
--
-- The realms and shards tables themselves ship with 0001_init.sql (servers
-- already hold realm_id / shard_id foreign keys with idx_servers_realm and
-- idx_servers_shard). This migration adds the ordering columns used by the
-- admin list endpoints.
BEGIN;

-- ListRealms / ListShards order by created_at descending.
CREATE INDEX IF NOT EXISTS idx_realms_created ON realms (created_at DESC);
CREATE INDEX IF NOT EXISTS idx_shards_created ON shards (created_at DESC);

-- Realm status is expected to filter the lists once lifecycle actions land.
CREATE INDEX IF NOT EXISTS idx_realms_status  ON realms (status);
CREATE INDEX IF NOT EXISTS idx_shards_status  ON shards (status);

COMMIT;
