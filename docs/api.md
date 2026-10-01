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
  "capacity": 2000
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

**Request**

```json
{
  "region": "cn-east",
  "platform": "android",
  "version": "1.8.2",
  "mode": "pvp"
}
```

**Response** `200 OK`

```json
{
  "server": {
    "id": "game-1001",
    "endpoint": "game-1001.example.com:30001"
  },
  "reason": "lowest_load"
}
```

**决策依据**

```text
Region
Version
Capacity
Load
Maintenance
Player Character
```

`reason` 字段取值：`lowest_load` / `highest_capacity` / `nearest_region` / `has_character` / `fallback`，用于排查推荐异常。

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

---

## 通用约定

### 认证

| 调用方 | 认证方式 |
| --- | --- |
| 游戏服务器（Registry / Directory 写） | 服务间 mTLS 或 Service Token |
| 客户端（Discovery / Directory 读 / Routing） | 玩家 AccessToken，经 APISIX 校验 |
| 运维工具（Admin） | 独立的运维凭证 + RBAC |

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
