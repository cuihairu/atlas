# API 参考

Atlas 的 API 分为五组，职责清晰互不重叠。

```text
/v1/registry/*     服务器注册
/v1/discovery/*    服务器发现
/v1/directory/*    角色目录
/v1/routing/*      接入推荐
/v1/admin/*        运维管理
```

**设计原则：Discovery 与 Routing 在 API 层分离。**

`GET /v1/discovery/servers` 回答"有哪些服务器"，`GET /v1/routing/recommended` 回答"我该去哪个"。两者语义不同，不合并成一个接口。

除 REST 外，同一套能力也以 gRPC 暴露（见下方 [gRPC API](#grpc-api)），proto 定义在 [`api/proto/atlas.proto`](https://github.com/cuihairu/atlas/blob/main/api/proto/atlas.proto)。

---

## Registry

服务器注册、心跳、注销。由**游戏服务器**调用。

### POST /v1/registry/servers/register

服务器启动时注册。

**Request**

```json
{
  "server_id": "game-1001",
  "type": "game",
  "region": "cn-east",
  "realm": "realm-01",
  "shard": "shard-1001",
  "version": "1.8.2",
  "endpoint": {
    "host": "10.0.1.21",
    "port": 30001
  },
  "capacity": 2000,
  "players": 512,
  "started_at": "2026-10-01T05:58:00Z"
}
```

**Response** `201 Created`

```json
{
  "server_id": "game-1001",
  "status": "online",
  "registered_at": "2026-10-01T06:00:00Z"
}
```

Atlas 保存的字段：

```text
server_id
type
region
realm
shard
version
endpoint
capacity
status
players     # 初始在线人数，直接写入运行时数据（重注册恢复现场）
started_at  # 进程启动时间，缺省取注册时刻；重注册刷新（可算 uptime）
```

**幂等性**：重复注册同一 `server_id` 视为更新，返回 `200 OK`。

---

### POST /v1/registry/servers/{id}/heartbeat

周期上报，间隔由游戏部署配置决定（建议 5–15 秒）。

**Request**

```json
{
  "players": 843,
  "load": 0.42,
  "status": "online"
}
```

**Response** `200 OK`

```json
{
  "server_id": "game-1001",
  "status": "online",
  "next_heartbeat_in": 10
}
```

Atlas 依据心跳自动推进健康状态：

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

---

### POST /v1/registry/servers/{id}/unregister

服务器主动下线。

**Request**

```json
{
  "reason": "shutdown"
}
```

**Response** `200 OK`

```json
{
  "server_id": "game-1001",
  "status": "offline"
}
```

与心跳超时的区别：`unregister` 是**主动**下线，Atlas 会立即把状态置为 `offline` 并停止向客户端推荐；心跳超时是**被动**推断，需经过 `suspect` 阶段。

---

## Discovery

服务器发现。由**客户端与运维工具**调用。

### GET /v1/discovery/servers

列出服务器。支持按下列维度筛选：

```text
region
realm
shard
version
platform
language
status
game_mode
```

**Example**

```http
GET /v1/discovery/servers?region=cn-east&status=online
```

**Response** `200 OK`

```json
{
  "servers": [
    {
      "id": "game-1001",
      "name": "一区·青龙",
      "region": "cn-east",
      "status": "online",
      "players": 843,
      "capacity": 2000
    },
    {
      "id": "game-1002",
      "name": "二区·白虎",
      "region": "cn-east",
      "status": "online",
      "players": 1260,
      "capacity": 2000
    }
  ]
}
```

**分页**：`?limit=50&cursor=...`，游标基于 `server_id`。

**默认行为**：不带 `status` 参数时，默认过滤掉 `offline` 与 `disabled` 状态的服务器，客户端不应看到已死的服务器。

---

### GET /v1/discovery/servers/{id}

获取单个服务器详情。

**Response** `200 OK`

```json
{
  "id": "game-1001",
  "name": "一区·青龙",
  "type": "game",
  "region": "cn-east",
  "realm": "realm-01",
  "shard": "shard-1001",
  "version": "1.8.2",
  "platform": "android",
  "endpoint": {
    "host": "game-1001.example.com",
    "port": 30001
  },
  "status": "online",
  "players": 843,
  "capacity": 2000,
  "load": 0.42
}
```

**Response** `404 Not Found` — 服务器不存在。

---

## Directory

角色目录。这是 Atlas 与普通 Service Discovery 最大的区别。

### GET /v1/directory/accounts/{account_id}/characters

查询账号下的所有角色（跨服）。

**Response** `200 OK`

```json
{
  "characters": [
    {
      "server_id": "game-1001",
      "character_id": "823712",
      "name": "剑无尘",
      "level": 182,
      "class_id": 3,
      "last_login_at": "2026-09-30T12:30:00Z"
    },
    {
      "server_id": "game-1008",
      "character_id": "923812",
      "name": "无尘",
      "level": 97,
      "class_id": 7,
      "last_login_at": "2026-09-25T08:20:00Z"
    }
  ]
}
```

客户端可以直接渲染：

```text
我的角色

一区 · 青龙
剑无尘             Lv.182
[进入游戏]

八区 · 白虎
无尘               Lv.97
[进入游戏]
```

**注意**：返回的 `name`、`level` 是投影值，允许秒级到分钟级滞后，仅用于列表展示。

---

### GET /v1/directory/characters/{character_id}

查询单个角色的索引信息。

**Response** `200 OK`

```json
{
  "account_id": "10001",
  "server_id": "game-1001",
  "character_id": "823712",
  "name": "剑无尘",
  "level": 182,
  "class_id": 3,
  "avatar": "avatar_03",
  "last_login_at": "2026-09-30T12:30:00Z"
}
```

---

### GET /v1/directory/servers/{server_id}/characters

查询某服务器上的角色索引，主要用于运维与合服校验。

**Query**：`?account_id=`、`?limit=`、`?cursor=`

---

### POST /v1/directory/characters

写入角色索引。由**游戏服务器**调用。

**Request**

```json
{
  "account_id": 10001,
  "server_id": 1001,
  "character_id": 823712,
  "name": "剑无尘",
  "level": 1,
  "class_id": 3
}
```

**Response** `201 Created`

**幂等性**：以 `(account_id, server_id, character_id)` 为唯一键，重复写入视为更新。

---

### PATCH /v1/directory/characters/{character_id}

更新角色索引（如升级、改名）。

```json
{
  "level": 183,
  "name": "剑无尘·改"
}
```

---

### DELETE /v1/directory/characters/{character_id}

删除角色索引。**只删除索引，不删除角色数据**——删角色由游戏服务器负责。

---

## Routing

接入推荐。

### GET /v1/routing/recommended

**Query 参数**（均可选）

| 参数 | 说明 |
| --- | --- |
| `region` | 目标大区，精确匹配 |
| `version` | 客户端版本，精确匹配 |
| `platform` | 平台，精确匹配 |
| `account_id` | 玩家账号；提供时优先推荐已有角色的服务器 |

未指定 `status` 时只推荐 `online` 的服务器。

```text
GET /v1/routing/recommended?region=cn-east&platform=android&account_id=10001
```

**Response** `200 OK`

```json
{
  "server": { "id": "game-1001", "endpoint": {"host": "10.0.0.1", "port": 30001} },
  "reason": "lowest_load"
}
```

`server` 为完整 Server 对象（见 Discovery）。无可用服务器时返回 `404 NO_SERVER_AVAILABLE`。

**决策依据**（依次比较）

```text
1. Player Character   account_id 已有角色的服务器优先
2. Load               负载低者优先
3. Capacity           负载相同时，剩余容量高者优先
4. Server ID          确定性兜底
```

`reason` 字段取值：`lowest_load` / `highest_capacity` / `has_character` / `fallback`（严格过滤无命中、放宽为仅按状态推荐时），用于排查推荐异常。

**与 Discovery 的区别**：Discovery 返回**候选集合**，Routing 返回**单一决策**。客户端选服界面走 Discovery，"开始游戏"按钮走 Routing。

---

## Admin

运维管理。由**运维工具**调用，不暴露给客户端。

### POST /v1/admin/servers/{id}/maintenance

进入维护状态。新玩家不可进入，老玩家可继续游戏。

### POST /v1/admin/servers/{id}/drain

进入排水状态。停止接收新连接，等待存量玩家自然离开。

### POST /v1/admin/servers/{id}/enable

重新启用服务器。

### POST /v1/admin/servers/{id}/disable

禁用服务器。客户端完全不可见。

状态机语义详见 [lifecycle.md](lifecycle.md)。

### POST /v1/admin/realms

创建大区（Realm）。ID 冲突返回 `409 REALM_EXISTS`，缺少 `id` / `name` 返回 `400`。

```json
{
  "id": "realm-01",
  "name": "华东大区",
  "region": "cn-east",
  "status": "active"
}
```

`status` 省略时默认 `active`。返回 `201` 与完整 Realm 对象。

### GET /v1/admin/realms

按 `created_at` 倒序列出大区。`?limit=` 限制条数（默认 50）。

```json
{ "realms": [ ... ] }
```

### POST /v1/admin/shards

在大区下创建分片（Shard）。`realm_id` 必须指向已存在的大区，否则返回 `404 REALM_NOT_FOUND`；ID 冲突返回 `409 SHARD_EXISTS`。

```json
{
  "id": "shard-0101",
  "realm_id": "realm-01",
  "name": "一区",
  "status": "active"
}
```

### GET /v1/admin/shards

按 `created_at` 倒序列出分片。`?realm_id=` 过滤指定大区，`?limit=` 限制条数。

```json
{ "shards": [ ... ] }
```

服务器注册时可携带 `realm_id` / `shard_id` 关联所属大区与分片（见 Registry），发现过滤支持 `realm` / `shard`（见 Discovery）。

### POST /v1/admin/servers/{id}/maintenance-window

计划维护窗口（v0.1.20）：`start_at` 到达时健康监控自动把服务器置入 `maintenance`，`end_at` 后恢复原状态（详见 lifecycle.md §5）。`start_at` 缺省取当前时刻；`announce` 缺省 `true`，自动创建一条覆盖同时段的服务器级 warning 公告并与窗口关联。

**Request**

```json
{
  "start_at": "2026-10-02T02:00:00Z",
  "end_at": "2026-10-02T04:00:00Z",
  "announce": true
}
```

**Response** `201 Created`

```json
{
  "id": "mwin-1759376400000000000",
  "server_id": "game-1001",
  "start_at": "2026-10-02T02:00:00Z",
  "end_at": "2026-10-02T04:00:00Z",
  "previous_status": "",
  "announcement_id": "ann-1759376400000000000",
  "created_at": "2026-10-01T12:00:00Z"
}
```

错误：`404 SERVER_NOT_FOUND`（服务器不存在）、`400 INVALID_ARGUMENT`（`end_at` 不晚于 `start_at`）。

### GET /v1/admin/maintenance-windows

列出维护窗口（新→旧）。`?server_id=` 过滤指定服务器，`?limit=` 限制条数。

```json
{ "maintenance_windows": [ ... ] }
```

### DELETE /v1/admin/maintenance-windows/{window_id}

取消窗口（`204 No Content`）。已进入维护的服务器不会被自动拉出，需走常规 `enable` / `maintenance` 转移；已自动创建的公告不会被撤回。

### POST /v1/admin/announcements

创建公告。`server_id` 缺省为全局公告；`level` ∈ `info` / `warning` / `critical`（缺省 `info`）。

**Request**

```json
{
  "server_id": "game-1001",
  "title": "双倍掉落周末",
  "body": "10 月 5 日 00:00 - 10 月 7 日 24:00",
  "level": "info",
  "starts_at": "2026-10-04T16:00:00Z",
  "ends_at": "2026-10-07T16:00:00Z"
}
```

**Response** `201 Created`（完整公告对象）。

错误：`404 SERVER_NOT_FOUND`、`400 INVALID_ARGUMENT`（缺 `title`、未知 `level`、`ends_at` 不晚于 `starts_at`）。

### GET /v1/admin/announcements

列出公告（新→旧）。`?server_id=` = 全局 + 指定服务器；`?active=true` 只返回当前生效的；`?limit=` 限制条数。

```json
{ "announcements": [ ... ] }
```

### DELETE /v1/admin/announcements/{announcement_id}

删除公告（`204 No Content`）。

### GET /v1/discovery/announcements

玩家客户端拉取**当前生效**的公告：全局 +（可选）`?server_id=` 指定服务器的。只读，无需认证。

**Request**

```text
GET /v1/discovery/announcements?server_id=game-1001&limit=20
```

**Response** `200 OK`

```json
{
  "announcements": [
    {
      "id": "ann-1759376400000000000",
      "server_id": "game-1001",
      "title": "维护公告",
      "body": "Server game-1001 will be under maintenance from ...",
      "level": "warning",
      "starts_at": "2026-10-02T02:00:00Z",
      "ends_at": "2026-10-02T04:00:00Z",
      "created_at": "2026-10-01T12:00:00Z"
    }
  ]
}
```

### GET /metrics

Prometheus 抓取端点（管理端口 :8082，受 Admin 认证保护）。暴露：

| 指标 | 类型 | 说明 |
| --- | --- | --- |
| `atlas_registry_servers_total{status}` | gauge | 各生命周期状态的服务器数 |
| `atlas_directory_characters_total` | gauge | 角色索引条目总数 |
| `atlas_registry_heartbeat_lag_seconds` | histogram | 健康巡检时观测到的心跳延迟 |
| `atlas_discovery_requests_total{filter}` | counter | 发现服务列表请求（按过滤条件） |
| `atlas_admin_requests_total{endpoint,status}` | counter | 管理 API 请求（按路由与状态码） |
| `atlas_health_transitions_total{from,to}` | counter | 服务器生命周期状态迁移 |

---

## gRPC API

REST 之外的第二种传输方式，与 REST **完全同源**：五个服务一一对应五组端点，共用同一批内部 service，因此两条路径的行为（过滤、排序、事件发布、错误语义）保持一致。

- **监听地址**：`:9090`（`ATLAS_GRPC_ADDR` 可改；设为空字符串可关闭 gRPC）
- **proto 定义**：[`api/proto/atlas.proto`](https://github.com/cuihairu/atlas/blob/main/api/proto/atlas.proto)，Go 包 `github.com/cuihairu/atlas/api/pb`
- **与 REST 的关系**：gRPC 不是替代品——SDK（v0.1.6+）双传输都可选，游戏服侧高频心跳走 gRPC 更省开销，运维工具走 REST 更顺手

```protobuf
package atlas.v1;

service RegistryService {    // 对应 /v1/registry/*
  rpc Register(RegisterRequest) returns (RegisterResponse);
  rpc Heartbeat(HeartbeatRequest) returns (HeartbeatResponse);
  rpc Unregister(UnregisterRequest) returns (OkResponse);
}

service DiscoveryService {   // 对应 /v1/discovery/*
  rpc ListServers(ListServersRequest) returns (ListServersResponse);
  rpc GetServer(GetServerRequest) returns (GetServerResponse);
}

service DirectoryService {   // 对应 /v1/directory/*
  rpc CreateCharacter(CreateCharacterRequest) returns (CreateCharacterResponse);
  rpc GetCharacter(GetCharacterRequest) returns (GetCharacterResponse);
  rpc ListCharactersByAccount(ListCharactersByAccountRequest) returns (ListCharactersByAccountResponse);
  rpc ListCharactersByServer(ListCharactersByServerRequest) returns (ListCharactersByServerResponse);
  rpc UpdateCharacter(UpdateCharacterRequest) returns (UpdateCharacterResponse);
  rpc DeleteCharacter(DeleteCharacterRequest) returns (DeleteCharacterResponse);
}

service RoutingService {     // 对应 /v1/routing/recommended
  rpc Recommend(RecommendRequest) returns (RecommendResponse);
}

service AdminService {       // 对应 /v1/admin/*
  rpc SetMaintenance(ServerIdRequest) returns (OkResponse);
  rpc SetDrain(ServerIdRequest) returns (OkResponse);
  rpc Enable(ServerIdRequest) returns (OkResponse);
  rpc Disable(ServerIdRequest) returns (OkResponse);
  rpc GetStats(GetStatsRequest) returns (GetStatsResponse);
  rpc SearchCharacters(SearchCharactersRequest) returns (SearchCharactersResponse);
  rpc CreateMigration(CreateMigrationRequest) returns (CreateMigrationResponse);
  rpc GetMigration(GetMigrationRequest) returns (GetMigrationResponse);
  rpc ListMigrations(ListMigrationsRequest) returns (ListMigrationsResponse);
  rpc RollbackMigration(RollbackMigrationRequest) returns (RollbackMigrationResponse);
}
```

### 错误映射

| REST | gRPC status | 场景 |
| --- | --- | --- |
| 400 `INVALID_ARGUMENT` | `InvalidArgument` | 参数缺失或格式错误 |
| 404 `SERVER_NOT_FOUND` / `CHARACTER_NOT_FOUND` | `NotFound` | 服务器 / 角色 / 迁移不存在 |
| 409 `ALREADY_REGISTERED` | `AlreadyExists` | 注册冲突 |
| 500 / 503 | `Internal` | 存储层错误 |

### 字段约定

- 时间戳为 RFC3339 字符串，与 REST 的 JSON 表示一致
- 可选更新字段使用 proto3 `optional`（`UpdateCharacterRequest` 的 `name` / `level` / `class_id` / `avatar`），未设置的字段不会被修改——语义与 `PATCH` 一致
- Directory 写 RPC 与 REST 写端点一样经过事件适配器：同步适配器返回 `{"character": ...}` + `status: "created|updated|deleted"`，异步适配器（如 Redis Streams）返回 `status: "queued"`

## 通用约定

### 认证

Atlas 将 API 划分为三个安全域，各自独立配置：

```text
┌──────────────────────────────────────────────────────────────┐
│                        Atlas API                              │
│                                                              │
│  ┌──────────────────┐  ┌──────────────────┐  ┌────────────┐ │
│  │  Registry        │  │  Discovery /     │  │  Admin     │ │
│  │  /v1/registry/*  │  │  Directory 读    │  │  /v1/admin │ │
│  │                  │  │  /v1/discovery/* │  │  /*        │ │
│  │  Service Token   │  │  /v1/directory/* │  │  API Key   │ │
│  │  + IP 白名单     │  │                  │  │  + IP 白名 │ │
│  │                  │  │  网关层处理认证  │  │  单        │ │
│  │  内部网络        │  │  公网            │  │  运维工具  │ │
│  └──────────────────┘  └──────────────────┘  └────────────┘ │
└──────────────────────────────────────────────────────────────┘
```

| 安全域 | 端点 | 调用方 | 认证方式 | 配置 |
| --- | --- | --- | --- | --- |
| **Registry** | `/v1/registry/*` | 游戏服务器（内部网络） | Service Token（`Authorization: Bearer <token>` 或 `X-Atlas-Token`）+ IP 白名单 | `ATLAS_REGISTRY_TOKENS` + `ATLAS_REGISTRY_IP_WHITELIST` |
| **Public** | `/v1/discovery/*` `/v1/directory/*` | 客户端（公网） | 网关层处理（玩家 AccessToken），Atlas 不校验 | 网关配置 |
| **Admin** | `/v1/admin/*` | GM 工具 / 运维 | API Key（`Authorization: Bearer <key>`）+ IP 白名单 | `ATLAS_ADMIN_API_KEYS` + `ATLAS_ADMIN_IP_WHITELIST` |

**为什么 Public 端点不在 Atlas 内做认证？**

客户端请求经过网关（APISIX / Kong / Nginx），网关已经完成了 TLS 终结和玩家身份校验。Atlas 只处理业务逻辑，不重复校验 token。这样做的好处：

1. 网关可以统一管理所有后端服务的认证，不只是 Atlas
2. 认证策略（token 过期、刷新、黑名单）在网关层集中管理
3. Atlas 减少一次 JWT 解析的开销

**开发模式**

所有认证配置为空时，对应端点完全开放。这是为了本地开发方便，**生产环境必须配置**。

### 错误响应

```json
{
  "error": {
    "code": "SERVER_NOT_FOUND",
    "message": "server game-1001 does not exist"
  }
}
```

| HTTP | code | 场景 |
| --- | --- | --- |
| 400 | `INVALID_ARGUMENT` | 参数缺失或格式错误 |
| 404 | `SERVER_NOT_FOUND` | 服务器不存在 |
| 404 | `CHARACTER_NOT_FOUND` | 角色索引不存在 |
| 409 | `ALREADY_REGISTERED` | 注册冲突（保留给未来的强校验场景） |
| 429 | `RATE_LIMITED` | 触发 APISIX 限流 |
| 503 | `STORAGE_UNAVAILABLE` | Redis / PostgreSQL 不可用 |

### 版本

所有路径以 `/v1` 开头。破坏性变更递增主版本号并保留旧版本至少一个发布周期。
