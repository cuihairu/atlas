<p align="center">
  <img src="../static/logo.svg" alt="Atlas" width="100" height="100" />
</p>

# 架构设计

## 1. 总览

Atlas 是一个面向在线游戏的**控制面（Control Plane）**。它不处理游戏逻辑，不存储角色权威数据，只负责回答"服务器在哪、是否可用、玩家的角色在哪"这一类目录性问题。

```text
                                      ┌──────────────────┐
                                      │      Client      │
                                      └────────┬─────────┘
                                               │
                                               ▼
                                      ┌──────────────────┐
                                      │      APISIX      │
                                      │   API Gateway    │
                                      └────────┬─────────┘
                                               │
                         ┌─────────────────────┼─────────────────────┐
                         │                     │                     │
                         ▼                     ▼                     ▼
                 ┌──────────────┐     ┌──────────────┐     ┌──────────────┐
                 │    Server    │     │  Character   │     │    Account   │
                 │   Discovery  │     │   Directory  │     │    Routing   │
                 └──────┬───────┘     └──────┬───────┘     └──────────────┘
                        │                    │
                        ▼                    ▼
                 ┌──────────────┐     ┌──────────────┐
                 │    Server    │     │   Character  │
                 │   Registry   │     │    Index     │
                 └──────┬───────┘     └──────┬───────┘
                        │                    │
             ┌──────────┼──────────┐         │
             ▼          ▼          ▼         ▼
          Game-01    Game-02    Game-03   Character DB
```

---

## 2. 分层职责

| 层 | 组件 | 职责 |
| --- | --- | --- |
| 接入层 | APISIX | TLS 终结、认证鉴权、限流、路由、负载均衡、WAF、可观测性 |
| 服务层 | Atlas Core | Registry / Discovery / Directory / Routing 四大模块 |
| 数据层 | Redis | 运行时状态：心跳、状态、负载、在线数 |
| 数据层 | PostgreSQL | 持久事实：服务器档案、拓扑、角色索引、迁移记录 |
| 来源层 | Game Server | 角色数据的 Source of Truth，向 Atlas 投递索引事件 |

---

## 3. 与 APISIX 的边界

**核心原则：APISIX 是入口，不是 Atlas 的核心。**

### APISIX 负责

```text
TLS
Authentication
Rate Limit
Routing
Load Balancing
Observability
WAF
```

这些是**通用网关能力**，与游戏业务语义无关，任何后端服务都需要，不应由 Atlas 重复实现。

### Atlas 负责

```text
Server Registry
Server Discovery
Character Directory
Server Health
Server Lifecycle
Realm / Shard
Server Migration
Routing Metadata
```

这些是**游戏领域语义**，只有 Atlas 理解"什么是 Realm""什么是合服"。

### 为什么不做 APISIX Plugin

把 Atlas 做成 APISIX 插件会带来三个问题：

1. **生命周期耦合** — 插件运行在网关 worker 内，无法独立扩缩容、独立灰度、独立回滚。
2. **能力受限** — 插件难以持有长连接、后台定时任务（如健康扫描）、独立存储连接池。
3. **可移植性丧失** — 换成 Nginx / Envoy / Traefik / 直连，Atlas 就不可用了。

正确的形态是：

```text
Atlas Core
    │
    ├── HTTP API          独立服务，标准 REST
    ├── gRPC API          高性能内部调用
    ├── SDK               C++ / Go / Java / C#
    └── APISIX Integration   作为可选集成层
```

这样 Atlas 可以脱离 APISIX 独立存在，APISIX 只是众多部署形态中的一种。

---

## 4. 数据流

### 4.1 服务器注册与心跳

```text
Game Server
     │
     │ register            POST /v1/registry/servers/register
     ▼
   Atlas
     ▲
     │
     │ heartbeat           POST /v1/registry/servers/{id}/heartbeat
     │
     └──────────────── every N seconds
```

Atlas 依据心跳时间戳自动推进健康状态：

```text
online
   ↓
heartbeat timeout
   ↓
suspect
   ↓
offline
```

详见 [lifecycle.md](lifecycle.md)。

### 4.2 客户端查询

```text
Client → APISIX → Atlas Discovery → Redis (运行时状态) + PostgreSQL (服务器档案)
Client → APISIX → Atlas Directory  → PostgreSQL (角色索引)
```

Discovery 是高频读路径，Redis 承载热点；Directory 是按 `account_id` 的点查，PostgreSQL 索引足以支撑。

### 4.3 角色索引同步

```text
Game Server
     │
     │ CharacterCreated / CharacterUpdated / CharacterDeleted
     ▼
  Message Bus  （v0.1 直接 HTTP）
     │
     ▼
   Atlas
     │
     └──▶ PostgreSQL character_index
```

Atlas 只保存投影，不保存权威数据。详见 [sync.md](sync.md)。

---

## 5. 存储分层

采用 **PostgreSQL + Redis** 双存储，按访问模式分工而非按数据类型分工：

| 存储 | 承载内容 | 访问模式 |
| --- | --- | --- |
| **Redis** | 心跳时间戳、`status`、`load`、`players` | 高频写（每服务器每 N 秒一次）、高频读（列表页） |
| **PostgreSQL** | `servers`、`realms`、`shards`、`character_index`、`migrations` | 低频写、事务性、需要持久与关联查询 |

### 为什么这样分

- **心跳是易失数据。** 服务器掉线后，最后一次心跳的精确数值没有历史价值，放 Redis 用 TTL 自动过期，天然契合语义。
- **服务器档案是事实。** 名称、区域、版本、拓扑归属需要持久化与审计，放 PostgreSQL。
- **角色索引需要事务与外键。** 合服/转服会产生跨行变更，PostgreSQL 的事务能力是刚需。
- **列表页是热点。** 客户端拉服务器列表是最高频的读，Redis 承载可大幅降低 PG 压力。

### 双写一致性

v0.1 采用 **PostgreSQL 为准、Redis 为缓存** 的策略：

- 写路径先落 PG，再更新 Redis。
- Redis 中的运行时字段（`load` / `players` / `status`）允许短暂不一致，因为它们本身就是近似值。
- 心跳超时由定时任务扫描 Redis TTL 并回写 PG 的 `status`。

详细表结构与键设计见 [data-model.md](data-model.md)。

---

## 6. 部署形态

### 最小部署

```text
┌─────────┐   ┌─────────┐   ┌─────────┐
│  Atlas  │   │ Postgres │   │  Redis  │
└─────────┘   └─────────┘   └─────────┘
     3 个容器，docker-compose 一键拉起
```

### 生产部署

```text
Internet
    │
    ▼
  APISIX  ──┬── Game API
            ├── Atlas API  ──▶  Atlas (多副本, 无状态)
            └── Account API      │
                           ┌─────┴─────┐
                           ▼           ▼
                        Redis       PostgreSQL
                      (哨兵/集群)    (主从/流复制)
```

Atlas Core 无状态，可水平扩展；所有状态都在 Redis 与 PostgreSQL 中。

---

## 7. 可观测性

Atlas 对外暴露的运维指标建议覆盖：

```text
atlas_registry_servers_total{status}          当前各状态服务器数
atlas_registry_heartbeat_lag_seconds          心跳延迟分布
atlas_directory_characters_total              角色索引总量
atlas_discovery_requests_total{filter}        发现查询量与筛选维度
atlas_routing_decisions_total{reason}         推荐决策的原因分布
```

这些指标同时服务于容量规划与告警（例如 `suspect` 状态服务器数突增）。
