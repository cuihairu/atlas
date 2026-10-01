<p align="center">
  <img src="../static/logo.svg" alt="Atlas" width="64" height="64" />
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

## 3. 与网关的边界

**核心原则：Atlas 是独立服务，网关是可选的接入层。**

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

这些是**通用网关能力**，与游戏业务语义无关，任何后端服务都需要，不应由 Atlas 重复实现。APISIX、Kong、Envoy、Nginx 都能提供这些能力。

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

### 为什么不做网关插件

把 Atlas 做成 APISIX / Kong / Envoy 的插件会带来三个问题：

1. **生命周期耦合** — 插件运行在网关 worker 内，无法独立扩缩容、独立灰度、独立回滚。
2. **能力受限** — 插件难以持有长连接、后台定时任务（如健康扫描）、独立存储连接池。
3. **可移植性丧失** — 换成 Nginx / Envoy / Traefik / 直连，Atlas 就不可用了。

正确的形态是：

```text
Atlas Core
    │
    ├── HTTP API              独立服务，标准 REST
    ├── gRPC API              高性能内部调用
    ├── SDK                   C++ / Go / Java / C#
    └── Gateway Integration   APISIX / Kong / Envoy / Nginx，可选
```

这样 Atlas 可以脱离任何网关独立存在，网关只是众多部署形态中的一种。

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

---

## 8. 架构选型理由

### 8.1 网关选型：APISIX 是推荐，不是绑定

Atlas 是标准 HTTP REST 服务，**任何能做反向代理的网关都可以放在前面**：

| 网关 | 限流 | 动态路由 | 可观测性 | 备注 |
| --- | --- | --- | --- | --- |
| **APISIX** | 插件内置，毫秒级热更新 | etcd watch，原生动态 | Prometheus/SkyWalking | 推荐，理由见下 |
| **Kong** | 插件内置 | 依赖 DB 或声明式 | 插件支持 | 成熟，但中文社区弱 |
| **Envoy** | 本地限流或外部限流服务 | xDS 协议 | 原生最强 | 需要控制面，运维重 |
| **Nginx** | `limit_req` 模块 | reload 或 OpenResty | 需额外配置 | 最轻量，但动态能力弱 |
| **Caddy** | 插件 | 自动 HTTPS | 基础 | 适合小规模 |
| **无网关** | Atlas 自身中间件 | — | — | 开发/测试，不推荐生产 |

**Atlas 自身也可以做基础限流**——在 `internal/httpapi` 加一个令牌桶中间件就够了。但生产环境建议把限流放在网关层，原因是：
- 网关限流在 Atlas 进程之外，Atlas 过载时网关还能挡
- 网关可以对不同路由设不同限流策略（注册接口严格，查询接口宽松）
- 网关限流不需要改 Atlas 代码

**选 APISIX 的具体理由：**

1. **动态路由是刚需。** 游戏服务器频繁上下线，网关必须能毫秒级更新路由表，不需要 reload。APISIX 的 etcd watch 机制天然契合。
2. **限流插件开箱即用。** `limit-count`、`limit-req`、`limit-conn` 三个插件覆盖了游戏场景的所有限流需求，不需要自己写。
3. **国内社区活跃。** 中文文档完善，遇到问题能找到人。Kong 和 Envoy 在国内的社区弱一些。
4. **插件可以前置通用逻辑。** 认证、WAF、日志在网关层做，Atlas 就不需要重复实现。

**但如果你已经有 Kong / Envoy / Nginx 的运维经验，直接用就好。** Atlas 不关心前面是什么网关，它只暴露标准的 REST 端点。

### 8.2 为什么 Atlas 是独立服务而不是网关插件

这个决定经过反复权衡。插件模式看起来更简单，但在游戏场景下有三个致命问题：

**1. 生命周期耦合**

```text
插件模式：
  网关进程 crash → Atlas 一起挂 → 所有游戏服务器注册状态丢失

独立服务：
  网关 crash → Atlas 不受影响 → 游戏服务器心跳继续 → 网关恢复后自动回连
```

游戏服务器的注册和心跳不应该因为网关重启而中断。

**2. 后台任务受限**

Atlas 需要运行后台任务：
- 心跳超时扫描（每 10 秒）
- 合服迁移编排（可能持续数分钟）
- 健康状态机推进

插件运行在请求-响应模型中，没有原生的后台任务能力。要实现就得 hack，不优雅也不可靠。

**3. 独立扩缩容**

```text
插件模式：
  网关扩 10 个副本 → Atlas 逻辑也跟着扩 10 份 → 资源浪费

独立服务：
  网关扩 10 个副本（处理流量）
  Atlas 扩 2 个副本（处理注册和查询）
  各自按需扩缩
```

网关的流量峰值和 Atlas 的计算负载是不同的曲线，绑定在一起无法独立优化。

### 8.3 为什么 PostgreSQL + Redis 而不是单一数据库

| 数据特征 | 放在哪 | 理由 |
| --- | --- | --- |
| 心跳、负载、在线数 | Redis | 高频写（每服务器每 10 秒），TTL 自动过期，丢失可恢复 |
| 服务器档案 | PostgreSQL | 低频写，需要持久化和审计 |
| 角色索引 | PostgreSQL | 合服需要跨行事务，Redis 事务能力不够 |
| 列表页缓存 | Redis | 最高频的读路径，ZSET 排序天然支持 |

**为什么不用 PostgreSQL 单独完成？**

技术上可以——心跳也写 PG，用定时任务扫描 `last_heartbeat_at` 字段。但：
- 1000 台服务器 × 0.1 QPS = 100 QPS 的心跳写入，PG 能扛但不优雅
- TTL 过期是 Redis 的原生语义，用 PG 模拟需要额外的扫描逻辑
- 列表页的 `ZRANGEBYSCORE` 查询在 Redis 是 O(log N)，在 PG 是全表扫描 + 排序

**为什么不用 Redis 单独完成？**

Redis 不适合：
- 事务性写入（合服时角色索引跨行变更）
- 持久化审计（服务器历史、迁移记录）
- 复杂查询（按名称搜索角色）

双存储的代价是运维复杂度多一个组件，但对于游戏基础设施这个量级，这是值得的。

### 8.4 为什么用 Go 而不是 C++ / Rust

| 维度 | Go | C++ | Rust |
| --- | --- | --- | --- |
| **开发效率** | 高，标准库完善 | 低，构建和依赖管理复杂 | 中，学习曲线陡 |
| **并发模型** | goroutine，天然适合心跳/扫描 | 需手动管理线程 | async/await，但心智负担重 |
| **部署** | 单二进制，无依赖 | 需要运行时库 | 单二进制，无依赖 |
| **生态** | pgx、go-redis、net/http 都是高质量 | 需要选型，版本碎片化 | 生态好但游戏领域库少 |
| **团队匹配** | Go 是游戏后端主流语言 | 游戏服务器主语言，但基础设施不一定用 | 非主流 |

Atlas 是控制面，不是数据面。控制面优先考虑开发效率和运维简单性，Go 的单二进制 + goroutine 模型是最合适的选择。

游戏服务器本身用 C++ 是合理的（性能敏感），但 Atlas 不在热路径上——它不处理游戏帧，不转发战斗包，只回答"服务器在哪、角色在哪"。这类控制面用 Go 完全够用。
