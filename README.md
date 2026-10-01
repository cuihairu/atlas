# Atlas

> **Atlas — Service Discovery and Character Directory for Online Games**

> **Atlas：面向在线游戏的服务器注册、发现与角色目录基础设施。**

Atlas is a lightweight control plane for online games, providing game server registration, discovery, health tracking, and account-to-character directory services.

---

## 定位

Atlas 的核心目标不是"返回一份服务器列表"，而是建立游戏后端的 **Game Infrastructure Directory**。

它围绕三个问题构建：

| 模块 | 回答的问题 | 说明 |
| --- | --- | --- |
| **Registry** | "我是谁？" | 游戏服务器上线时注册自身身份、拓扑位置与接入端点 |
| **Discovery** | "谁在线？" | 客户端与工具查询可用服务器，含健康状态与负载 |
| **Directory** | "我的角色在哪？" | 账号 → 角色的跨服索引，让玩家看到自己的全部角色 |
| **Routing** | "我应该去哪个服务器？" | 基于区域、版本、容量、负载推荐接入目标 |

> **APISIX 是入口，不是 Atlas 的核心。**
>
> Atlas 可以独立运行。APISIX 作为集成层存在，负责 TLS、认证、限流、WAF 与可观测性，但不承载 Atlas 的业务语义。

---

## 架构

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

数据层采用 **PostgreSQL + Redis** 双存储：Redis 承载高频运行时状态（心跳、负载、在线数），PostgreSQL 承载持久事实（服务器档案、拓扑、角色索引、迁移记录）。

---

## 核心能力

```text
Atlas
│
├── Registry    服务器注册 / 心跳 / 注销
├── Discovery   服务器发现 / 查询 / 推荐
├── Directory   账号-角色目录（跨服索引）
└── Routing     接入推荐
```

- **[Registry](docs/api.md#registry)** — 服务器启动注册、周期心跳、主动注销
- **[Discovery](docs/api.md#discovery)** — 按区域 / 版本 / 状态 / 平台筛选服务器
- **[Directory](docs/api.md#directory)** — 账号下所有角色的跨服索引
- **[Routing](docs/api.md#routing)** — 基于负载与容量推荐接入目标

---

## 关键设计原则

**1. Character Directory 是 Projection，不是 Source of Truth。**

Atlas 保存角色的索引信息（`account_id` / `server_id` / `character_id` / `name` / `level` / `class_id` / `last_login_at`），用于跨服检索与展示。真正的角色数据仍然由各游戏服务器的角色数据库负责。

**2. Region / Realm / Shard 是可选 metadata，不是硬编码层级。**

不同品类（MMORPG / MOBA / SLG）的拓扑差异巨大，Atlas 不强制任何一种层级结构。见 [概念模型](docs/concepts.md)。

**3. Discovery 与 Routing 在 API 层分离。**

`GET /v1/discovery/servers` 与 `GET /v1/routing/recommended` 是两件事，不合并成一个接口。

**4. Atlas Core 独立于任何网关。**

```text
Atlas Core
    │
    ├── HTTP API
    ├── gRPC API
    ├── SDK
    └── APISIX Integration
```

---

## 文档

| 文档 | 内容 |
| --- | --- |
| [docs/architecture.md](docs/architecture.md) | 整体架构、组件职责、与 APISIX 的边界 |
| [docs/concepts.md](docs/concepts.md) | Region / Realm / Shard / Server / Character 概念模型 |
| [docs/api.md](docs/api.md) | 四组 REST API 的完整定义 |
| [docs/data-model.md](docs/data-model.md) | PostgreSQL 表结构与 Redis 键设计 |
| [docs/lifecycle.md](docs/lifecycle.md) | 服务器生命周期状态机 |
| [docs/migration.md](docs/migration.md) | 合服 / 转服 / 迁服 |
| [docs/sync.md](docs/sync.md) | 游戏服务器到 Atlas 的数据同步 |
| [docs/roadmap.md](docs/roadmap.md) | MVP 范围与演进路线 |

---

## 路线图

**v0.1（MVP）**

```text
Atlas v0.1
│
├── Server Registry      Register / Heartbeat / Unregister
├── Server Discovery     List / Get
├── Character Directory  Create / Update / Delete / Account → Characters
├── Health               Automatic Offline
└── REST API
```

**明确不在 v0.1 范围内**

```text
❌ Kafka / NATS / RabbitMQ
❌ Kubernetes Operator
❌ Service Mesh
❌ 复杂调度算法
❌ 强绑定 APISIX
```

完整规划见 [docs/roadmap.md](docs/roadmap.md)。

---

## License

见 [LICENSE](LICENSE)。
