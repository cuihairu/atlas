# 路线图

## 最终定位

> **Atlas is a lightweight control plane for online games, providing game server registration, discovery, health tracking, and account-to-character directory services.**

```text
                     Atlas
                       │
       ┌───────────────┼────────────────┐
       ▼               ▼                ▼
 Server Registry   Server Discovery   Character Directory
       │               │                │
       ▼               ▼                ▼
   "我是谁？"       "谁在线？"       "我的角色在哪？"
```

这个定位比单纯的 Game Server Discovery 更完整，而且以后做游戏服务器框架、账号系统时都能复用。

---

## v0.1 — MVP ✅ 已完成

**目标：把核心模型跑通。** ✅

```text
Atlas v0.1
│
├── ✅ Server Registry
│   ├── ✅ Register
│   ├── ✅ Heartbeat
│   └── ✅ Unregister
│
├── ✅ Server Discovery
│   ├── ✅ List
│   └── ✅ Get
│
├── ✅ Character Directory
│   ├── ✅ Create
│   ├── ✅ Update
│   ├── ✅ Delete
│   └── ✅ Account → Characters
│
├── ✅ Health
│   └── ✅ Automatic Offline
│
└── ✅ REST API
```

### 技术栈

| 组件 | 选型 |
| --- | --- |
| 语言 | Go |
| 存储 | PostgreSQL + Redis（含内存实现用于开发/测试） |
| API | REST (HTTP/1.1 + JSON) |
| 部署 | Docker Compose |
| 测试 | Go `testing` + testcontainers |

### 明确不在 v0.1 范围内

```text
❌ Kafka / NATS / RabbitMQ     v0.1 用 HTTP 同步写入
❌ Kubernetes Operator
❌ Service Mesh
❌ 复杂调度算法
❌ 强绑定 APISIX
❌ gRPC API
❌ 多语言 SDK
❌ 合服 / 转服
❌ Server Lifecycle 完整状态机
```

先把核心模型跑通，再谈工程化。

---

## v0.2 — 事件驱动 + 生命周期

```text
Atlas v0.2
│
├── Event Adapter 接口
│   ├── Redis Streams 实现
│   └── HTTP 同步实现（保留）
│
├── Server Lifecycle 完整状态机
│   ├── STARTING / ONLINE / DRAINING
│   ├── MAINTENANCE / SUSPECT / OFFLINE / DISABLED
│   └── 自动掉线两段式判定
│
├── Admin API
│   ├── maintenance
│   ├── drain
│   ├── enable
│   └── disable
│
└── Observability
    ├── Prometheus metrics
    └── 健康状态告警
```

---

## v0.3 — Routing + 合服

```text
Atlas v0.3
│
├── Server Routing
│   └── GET /v1/routing/recommended
│
├── Server Migration
│   ├── 合服 (merge)
│   ├── 转服 (transfer)
│   ├── 迁服 (relocate)
│   └── 幂等 / 可重放 / 可回滚
│
├── gRPC API
│   └── 与 REST 等价的高性能接口
│
└── Message Bus 适配器
    ├── Kafka
    └── NATS
```

---

## v0.4 — SDK 与集成

```text
Atlas v0.4
│
├── Server SDK
│   ├── atlas-sdk-go
│   └── atlas-sdk-cpp
│
├── Character SDK
│   └── character_created / updated / deleted
│
├── APISIX Integration
│   ├── plugins/apisix
│   └── 认证 / 限流 / 路由规则
│
└── Realm / Shard 管理 API
```

SDK 优先级：**C++ > Go**，因为游戏服务器主语言是 C++，Go 更适合工具链与内部服务。

---

## v1.0 — 生产就绪

```text
Atlas v1.0
│
├── 高可用
│   ├── Atlas 多副本无状态部署
│   ├── Redis 哨兵 / 集群
│   └── PostgreSQL 主从 + 流复制
│
├── character_index 分片
│   └── 按 account_id hash
│
├── 多语言 SDK
│   ├── atlas-sdk-java
│   └── atlas-sdk-csharp
│
├── 管理控制台
│   └── 服务器 / 角色 / 迁移 可视化
│
└── 安全
    ├── mTLS 服务间认证
    ├── RBAC
    └── 审计日志
```

---

## 演进原则

### 1. 先跑通模型，再谈工程化

v0.1 故意不做 Message Bus、不做 K8s Operator、不做复杂调度。这些是**放大器**——核心模型不对，放大只会放大错误。

### 2. Atlas Core 独立于任何网关

APISIX 从第一天起就是**可选集成层**，不是依赖。这个原则贯穿所有版本。

### 3. Character Directory 永远是 Projection

任何版本都不要让 Atlas 持有角色权威数据。这是职责边界，不是性能取舍。

### 4. 写入接口从第一天就幂等

无论是 HTTP 还是 Message Bus，重试都不应产生副作用。这个决定成本极低，但后悔成本极高。

### 5. 层级结构保持可选

Region / Realm / Shard 的可选性不会因为功能增加而收紧。MMORPG、MOBA、SLG 的拓扑差异是永久的。

---

## 项目结构

```text
atlas/
├── cmd/atlas/              主服务入口
├── internal/
│   ├── model/              领域模型（Server, Character, Status）
│   ├── store/              存储接口
│   │   ├── memory/         内存实现（测试 + 开发）
│   │   ├── postgres/       PostgreSQL 实现
│   │   └── redisstore/     Redis 运行时状态
│   ├── registry/           注册 / 心跳 / 注销
│   ├── discovery/          服务器发现
│   ├── directory/          角色目录
│   ├── health/             健康监控（自动掉线）
│   ├── httpapi/            REST API handlers
│   ├── config/             环境变量配置
│   └── version/            版本信息
├── migrations/             SQL migration
├── deployments/docker/     Dockerfile + docker-compose
├── docs/                   设计文档
├── Makefile
├── .env.example
└── go.mod
```

**当前状态**：v0.1 MVP 已完成。下一步是 v0.2 事件驱动 + 生命周期。