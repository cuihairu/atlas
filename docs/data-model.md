# 数据模型

Atlas 采用 **PostgreSQL + Redis** 双存储，按**访问模式**分工：

| 存储 | 承载内容 | 访问模式 |
| --- | --- | --- |
| **Redis** | 心跳时间戳、`status`、`load`、`players` | 高频写、高频读、可丢失 |
| **PostgreSQL** | `servers`、`realms`、`shards`、`character_index`、`migrations` | 低频写、事务性、需持久 |

---

## 1. 为什么这样分

- **心跳是易失数据。** 服务器掉线后，最后一次心跳的精确数值没有历史价值。Redis TTL 天然契合这个语义。
- **服务器档案是事实。** 名称、区域、版本、拓扑归属需要持久化与审计。
- **角色索引需要事务与外键。** 合服/转服产生跨行变更，事务能力是刚需。
- **列表页是热点。** 拉服务器列表是最高频的读，Redis 承载可大幅降低 PG 压力。

### 一致性策略

**PostgreSQL 为准，Redis 为缓存。**

- 写路径先落 PG，再更新 Redis。
- Redis 中的运行时字段（`load` / `players` / `status`）允许短暂不一致，它们本身就是近似值。
- 心跳超时由定时任务扫描 Redis TTL，回写 PG 的 `status`。
- Redis 全量丢失时，可从 PG 重建运行时状态（服务器档案都在，只是心跳数据丢失）。

---

## 2. PostgreSQL 表结构

### servers

```sql
CREATE TABLE servers (
    id          TEXT PRIMARY KEY,
    name        TEXT        NOT NULL,
    type        TEXT        NOT NULL DEFAULT 'game',
    region      TEXT        NOT NULL,
    realm_id    TEXT        REFERENCES realms(id) ON DELETE SET NULL,
    shard_id    TEXT        REFERENCES shards(id) ON DELETE SET NULL,
    version     TEXT        NOT NULL,
    platform    TEXT        NOT NULL DEFAULT '',
    endpoint_host TEXT      NOT NULL,
    endpoint_port INTEGER   NOT NULL,
    capacity    INTEGER     NOT NULL DEFAULT 0,
    status      TEXT        NOT NULL DEFAULT 'starting',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_servers_region_status ON servers (region, status);
CREATE INDEX idx_servers_realm        ON servers (realm_id);
CREATE INDEX idx_servers_shard        ON servers (shard_id);
```

`status` 的取值域见 [lifecycle.md](lifecycle.md)：

```text
starting / online / draining / maintenance / suspect / offline / disabled
```

`players`、`load`、`last_heartbeat_at` **不在此表**，它们在 Redis 中。

---

### realms

```sql
CREATE TABLE realms (
    id        TEXT PRIMARY KEY,
    name      TEXT        NOT NULL,
    region    TEXT        NOT NULL,
    status    TEXT        NOT NULL DEFAULT 'active',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

---

### shards

```sql
CREATE TABLE shards (
    id        TEXT PRIMARY KEY,
    realm_id  TEXT        REFERENCES realms(id) ON DELETE SET NULL,
    name      TEXT        NOT NULL,
    status    TEXT        NOT NULL DEFAULT 'active',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_shards_realm ON shards (realm_id);
```

`realm_id` 可为 `NULL`，允许 Shard 直接挂在 Region 下。这与 [concepts.md](concepts.md) 中"层级可选"的原则一致。

---

### character_index

```sql
CREATE TABLE character_index (
    account_id    BIGINT      NOT NULL,
    server_id     TEXT        NOT NULL,
    character_id  BIGINT      NOT NULL,
    name          TEXT        NOT NULL,
    level         INTEGER     NOT NULL DEFAULT 1,
    class_id      INTEGER     NOT NULL DEFAULT 0,
    avatar        TEXT        NOT NULL DEFAULT '',
    last_login_at TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),

    PRIMARY KEY (account_id, server_id, character_id)
);

CREATE INDEX idx_char_by_account ON character_index (account_id);
CREATE INDEX idx_char_by_char_id ON character_index (character_id);
CREATE INDEX idx_char_by_server  ON character_index (server_id);
```

**这是投影表，不是权威数据表。** 完整角色数据在游戏服务器的角色数据库中。见 [concepts.md §7](concepts.md#7-character)。

主键选 `(account_id, server_id, character_id)` 三元组，因为：
- 同一账号可在多个服务器有角色
- `character_id` 全局唯一性由游戏服务器保证，但 Atlas 不依赖它作为主键，以容忍不同服务器 ID 空间冲突

`idx_char_by_char_id` 用于 `GET /v1/directory/characters/{id}` 反查。

---

### server_migrations

```sql
CREATE TABLE server_migrations (
    id            TEXT PRIMARY KEY,
    source_server TEXT        NOT NULL,
    target_server TEXT        NOT NULL,
    status        TEXT        NOT NULL DEFAULT 'pending',
    started_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at  TIMESTAMPTZ
);

CREATE INDEX idx_migration_status ON server_migrations (status);
```

详见 [migration.md](migration.md)。

---

## 3. Redis 键设计

### 服务器运行时状态

```text
atlas:server:{server_id}:state       HASH    服务器实时状态
atlas:server:{server_id}:hb          STRING  最后心跳时间戳 (Unix ms)，带 TTL
atlas:servers:region:{region}        ZSET    按 score 排序的服务器索引
atlas:servers:all                    ZSET    全量服务器索引
```

**`atlas:server:{id}:state`** 字段：

```text
status       online / suspect / ...
players      843
load         "0.42"
capacity     2000
region       cn-east
realm        realm-01
shard        shard-1001
version      1.8.2
endpoint     game-1001.example.com:30001
```

**`atlas:server:{id}:hb`** — 单值，TTL 设为心跳超时阈值（如 30 秒，即 3 个心跳周期）。键过期即代表心跳超时。

### 为什么用 ZSET 索引

```text
atlas:servers:region:{cn-east}
    score = load * 1000
    member = game-1001
```

Discovery 的列表查询、Routing 的"最低负载推荐"都变成一次 `ZRANGEBYSCORE`，无需访问 PG。

### 心跳写路径

```text
收到心跳
  │
  ├─▶ PEXPIRE atlas:server:{id}:hb       刷新 TTL
  ├─▶ HSET    atlas:server:{id}:state    players / load / status
  └─▶ ZADD    atlas:servers:region:{r}   更新 score
```

单次心跳涉及 3 个 Redis 命令，可用 pipeline 一次往返完成。

### 掉线检测

```text
定时任务 (每 10s)
  │
  ├─▶ 扫描 atlas:server:*:hb
  │     键已过期 ──▶ 状态机 online → suspect → offline
  │
  └─▶ 回写 PG servers.status
```

---

## 4. 数据流

### 写路径

```text
Game Server
    │ register / heartbeat
    ▼
  Atlas
    │
    ├──▶ Redis      运行时状态（立即）
    └──▶ PostgreSQL 服务器档案（注册时）
```

### 读路径

```text
GET /v1/discovery/servers
    │
    ▼
  Redis ZSET  ──命中──▶ 返回
    │
    └──回源──▶ PostgreSQL（缓存重建）
```

### 角色索引路径

```text
Game Server
    │ POST /v1/directory/characters
    ▼
  Atlas
    │
    └──▶ PostgreSQL character_index（事务性）
```

角色索引不进 Redis，因为它是按 `account_id` 的点查，PG 索引足以支撑，且需要事务保证合服时的跨行一致性。

---

## 5. 数据量估算

以中等规模 MMORPG 为例：

| 数据 | 规模 | 存储 |
| --- | --- | --- |
| 服务器 | 1,000 台 | PG + Redis，可忽略 |
| 心跳写入 | 1,000 × 0.1 QPS = 100 QPS | Redis |
| 列表读取 | 5,000 QPS | Redis ZSET |
| 角色索引 | 5,000 万行 | PG |
| 角色点查 | 2,000 QPS | PG（走 `idx_char_by_account`） |

角色索引是唯一有规模压力的表，分片策略（按 `account_id` hash）预留为 v2.0 能力，见 [roadmap.md](roadmap.md)。

---

## 6. 迁移与备份

- **PG**：常规逻辑备份 + WAL 归档。`character_index` 可从游戏服务器的角色数据库**全量重建**，这是它作为投影表的最大运维优势。
- **Redis**：不需要备份。全量丢失时从 PG 的 `servers` 表重建运行时状态，代价是所有服务器需重新上报一次心跳（通常 15 秒内恢复）。

```text
Redis 数据丢失
    │
    ▼
  从 PG servers 表重建 state HASH 与 ZSET
    │
    ▼
  各服务器下次心跳自动补全 players / load
```
