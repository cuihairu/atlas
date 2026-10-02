<p align="center">
  <img src="../static/logo.svg" alt="Atlas" width="64" height="64" />
</p>

# 架构设计

## 1. 总览

Atlas 是一个面向在线游戏的**控制面（Control Plane）**。它不处理游戏逻辑，不存储角色权威数据，只负责回答"服务器在哪、是否可用、玩家的角色在哪"这一类目录性问题。

```mermaid
flowchart TB
    Player["玩家客户端"]
    GW["APISIX<br/>API 网关（可选）"]

    subgraph atlas["Atlas 控制面（无状态，可多副本）"]
        direction TB
        REG["Registry<br/>注册 / 心跳 / 注销"]
        DISC["Discovery<br/>服务器发现"]
        DIR["Directory<br/>角色目录"]
        ROUT["Routing<br/>接入推荐"]
        ADMIN["Admin<br/>Realm / Shard / 迁移 / 审计"]
    end

    subgraph stores["数据层"]
        direction LR
        REDIS[("Redis<br/>运行时状态")]
        PG[("PostgreSQL<br/>持久事实")]
    end

    GS["Game Server 集群"]
    BUS["Message Bus<br/>Kafka / NATS / RabbitMQ / Redis Streams"]

    Player -->|HTTPS| GW
    GW --> DISC & DIR & ROUT
    GS -->|"register / heartbeat<br/>:8081（内网）"| REG
    GS -->|角色索引事件| BUS --> DIR
    REG --> REDIS & PG
    DISC -->|热点读| REDIS
    DIR & ROUT & ADMIN --> PG
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

TLS 终结、认证鉴权、限流、路由、负载均衡、可观测性、WAF。

这些是**通用网关能力**，与游戏业务语义无关，任何后端服务都需要，不应由 Atlas 重复实现。APISIX、Kong、Envoy、Nginx 都能提供这些能力。

### Atlas 负责

服务器注册与发现、角色目录、健康监控与生命周期、Realm / Shard、合服迁移、接入推荐元数据。

这些是**游戏领域语义**，只有 Atlas 理解"什么是 Realm""什么是合服"。

### 为什么不做网关插件

把 Atlas 做成 APISIX / Kong / Envoy 的插件会带来三个问题：

1. **生命周期耦合** — 插件运行在网关 worker 内，无法独立扩缩容、独立灰度、独立回滚。
2. **能力受限** — 插件难以持有长连接、后台定时任务（如健康扫描）、独立存储连接池。
3. **可移植性丧失** — 换成 Nginx / Envoy / Traefik / 直连，Atlas 就不可用了。

正确的形态是：

```mermaid
flowchart LR
    CORE["Atlas Core"]
    CORE --> HTTP["HTTP API<br/>标准 REST"]
    CORE --> GRPC["gRPC API<br/>高性能内部调用"]
    CORE --> SDK["六语言 SDK<br/>Go / C++ / Python / JS / Java / C#"]
    CORE -.->|可选集成| GWI["网关集成<br/>APISIX / Kong / Envoy / Nginx"]
```

这样 Atlas 可以脱离任何网关独立存在，网关只是众多部署形态中的一种。

---

## 4. 数据流

### 4.1 服务器注册与心跳

```mermaid
sequenceDiagram
    autonumber
    participant GS as Game Server
    participant A as Atlas Registry（:8081）

    GS->>A: POST /v1/registry/servers/register
    A-->>GS: 201 Created（status=starting）
    loop 每 N 秒（建议 5~15s）
        GS->>A: POST /v1/registry/servers/{id}/heartbeat
        A-->>GS: 200 OK（status=当前生效状态）
    end
    GS->>A: POST /v1/registry/servers/{id}/unregister（优雅下线）
```

Atlas 依据心跳时间戳自动推进健康状态：

```mermaid
stateDiagram-v2
    direction LR
    [*] --> starting: register
    starting --> online: 首个有效心跳
    online --> suspect: 心跳超时 30s
    suspect --> online: 心跳恢复
    suspect --> offline: 心跳超时 60s
    offline --> starting: 重新注册
```

详见 [lifecycle.md](lifecycle.md)。

### 4.2 客户端查询

```mermaid
flowchart LR
    C["Client"] --> GW["APISIX"] --> D["Discovery"] --> R[("Redis<br/>运行时状态")]
    D --> P1[("PostgreSQL<br/>服务器档案")]
    C2["Client"] --> GW2["APISIX"] --> DIR["Directory"] --> P2[("PostgreSQL<br/>角色索引")]
```

Discovery 是高频读路径，Redis 承载热点；Directory 是按 `account_id` 的点查，PostgreSQL 索引足以支撑。

### 4.3 角色索引同步

```mermaid
flowchart LR
    GS["Game Server<br/>（角色数据 Source of Truth）"]
    GS -->|CharacterCreated / Updated / Deleted<br/>Login / Moved| BUS["Message Bus<br/>http 同步 / Redis Streams / Kafka / NATS / RabbitMQ"]
    BUS --> A["Atlas Directory<br/>幂等投影（ApplyEvent）"]
    A --> PG[("PostgreSQL<br/>character_index")]
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
- 心跳超时由健康监控巡检（默认每 10s）计算 `last_seen_at` 年龄并回写 PG 的 `status`；Redis 键上的 TTL（120s）只为键自清理，不参与判活。

详细表结构与键设计见 [data-model.md](data-model.md)。

---

## 6. 部署形态

### 最小部署

```mermaid
flowchart LR
    A["Atlas"] --- P[("PostgreSQL")] --- R[("Redis")]
```

3 个容器，docker-compose 一键拉起。

### 生产部署

```mermaid
flowchart TB
    IN["Internet"] --> APISIX["APISIX"]
    APISIX --> GAME["Game API"]
    APISIX --> ATLAS["Atlas API"]
    APISIX --> ACCT["Account API"]
    ATLAS --> A1["Atlas 副本 ×N<br/>（无状态）"]
    A1 --> R[("Redis<br/>哨兵 / 集群")]
    A1 --> P[("PostgreSQL<br/>主从 / 流复制")]
```

Atlas Core 无状态，可水平扩展；所有状态都在 Redis 与 PostgreSQL 中。

---

## 7. 可观测性

Atlas 对外暴露的运维指标建议覆盖：

| 指标 | 含义 |
| --- | --- |
| `atlas_registry_servers_total{status}` | 当前各状态服务器数 |
| `atlas_registry_heartbeat_lag_seconds` | 心跳年龄分布 |
| `atlas_directory_characters_total` | 角色索引总量 |
| `atlas_discovery_requests_total{filter}` | 发现查询量与筛选维度 |
| `atlas_admin_requests_total{endpoint,status}` | Admin 请求量与响应码 |
| `atlas_health_transitions_total{from,to}` | 生命周期状态迁移量 |

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

| 形态 | 网关进程 crash 时 | 后果 |
| --- | --- | --- |
| 插件模式 | Atlas 一起挂 | 所有游戏服务器注册状态丢失 |
| 独立服务 | Atlas 不受影响 | 心跳继续，网关恢复后自动回连 |

游戏服务器的注册和心跳不应该因为网关重启而中断。

**2. 后台任务受限**

Atlas 需要运行后台任务：
- 心跳超时扫描（每 10 秒）
- 合服迁移编排（可能持续数分钟）
- 健康状态机推进

插件运行在请求-响应模型中，没有原生的后台任务能力。要实现就得 hack，不优雅也不可靠。

**3. 独立扩缩容**

| 形态 | 扩缩行为 | 结果 |
| --- | --- | --- |
| 插件模式 | 网关扩 10 副本 → Atlas 逻辑跟着扩 10 份 | 资源浪费 |
| 独立服务 | 网关扩 10 副本（流量），Atlas 扩 2 副本（注册与查询） | 各自按需扩缩 |

网关的流量峰值和 Atlas 的计算负载是不同的曲线，绑定在一起无法独立优化。

### 8.3 为什么 PostgreSQL + Redis 而不是单一数据库

| 数据特征 | 放在哪 | 理由 |
| --- | --- | --- |
| 心跳、负载、在线数 | Redis | 高频写（每服务器每 10 秒），TTL 自动过期，丢失可恢复 |
| 服务器档案 | PostgreSQL | 低频写，需要持久化和审计 |
| 角色索引 | PostgreSQL | 合服需要跨行事务，Redis 事务能力不够 |
| 运行时合并读 | Redis | 列表/详情读路径把 PG 档案与 Redis 运行时（`players` / `load` / `last_seen_at`）合并返回 |

**为什么不用 PostgreSQL 单独完成？**

技术上可以——心跳也写 PG，用定时任务扫描 `last_seen_at` 字段（Atlas 的巡检本来就要回写 PG 状态，这个方案是自洽的）。但：
- 1000 台服务器 × 0.1 QPS = 100 QPS 的心跳写入，PG 能扛但不优雅
- 运行时数值（`players` / `load`）每次心跳都变，放 PG 意味着最高频的写落在最贵的一层
- Redis 键 TTL 过期是原生的自清理语义，PG 需要额外的清理任务

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
