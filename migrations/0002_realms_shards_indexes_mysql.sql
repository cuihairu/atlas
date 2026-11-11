-- Realm / shard management (TODO v0.1.14) support indexes — MySQL variant.
-- (shards.realm_id already has idx_shards_realm from 0001_init_mysql.sql.)

CREATE INDEX idx_realms_created ON realms (created_at);
CREATE INDEX idx_shards_created ON shards (created_at);

CREATE INDEX idx_realms_status  ON realms (status);
CREATE INDEX idx_shards_status  ON shards (status);
