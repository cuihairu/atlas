# API 快速上手

本文档提供 curl 示例，帮助你在 5 分钟内跑通 Atlas 的全部 11 个 REST 端点。

假设 Atlas 运行在 `http://localhost:8080`。

---

## 完整 Happy Path

以下是一个端到端的典型流程：注册服务器 → 心跳 → 查询 → 写入角色 → 查询角色 → 下线。

### 1. 注册服务器

```bash
curl -X POST http://localhost:8080/v1/registry/servers/register \
  -H 'Content-Type: application/json' \
  -d '{
    "server_id": "game-1001",
    "name": "一区·青龙",
    "type": "game",
    "region": "cn-east",
    "realm_id": "realm-01",
    "shard_id": "shard-1001",
    "version": "1.8.2",
    "platform": "android",
    "endpoint": {
      "host": "10.0.1.21",
      "port": 30001
    },
    "capacity": 2000
  }'
```

```json
{
  "server_id": "game-1001",
  "status": "online",
  "registered_at": "2026-10-01T06:00:00Z"
}
```

### 2. 发送心跳

```bash
curl -X POST http://localhost:8080/v1/registry/servers/game-1001/heartbeat \
  -H 'Content-Type: application/json' \
  -d '{
    "players": 843,
    "load": 0.42,
    "status": "online"
  }'
```

```json
{
  "server_id": "game-1001",
  "status": "online",
  "next_heartbeat_in": 10
}
```

### 3. 查询服务器列表

```bash
# 列出所有在线服务器
curl http://localhost:8080/v1/discovery/servers

# 按区域筛选
curl 'http://localhost:8080/v1/discovery/servers?region=cn-east&status=online'
```

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
    }
  ]
}
```

### 4. 查询单个服务器详情

```bash
curl http://localhost:8080/v1/discovery/servers/game-1001
```

```json
{
  "id": "game-1001",
  "name": "一区·青龙",
  "type": "game",
  "region": "cn-east",
  "realm_id": "realm-01",
  "shard_id": "shard-1001",
  "version": "1.8.2",
  "platform": "android",
  "endpoint": {
    "host": "10.0.1.21",
    "port": 30001
  },
  "status": "online",
  "players": 843,
  "capacity": 2000,
  "load": 0.42
}
```

### 5. 创建角色索引

```bash
curl -X POST http://localhost:8080/v1/directory/characters \
  -H 'Content-Type: application/json' \
  -d '{
    "account_id": 10001,
    "server_id": "game-1001",
    "character_id": "823712",
    "name": "剑无尘",
    "level": 182,
    "class_id": 3
  }'
```

```json
{
  "account_id": "10001",
  "server_id": "game-1001",
  "character_id": "823712",
  "name": "剑无尘",
  "level": 182,
  "class_id": 3
}
```

### 6. 查询账号下所有角色

```bash
curl http://localhost:8080/v1/directory/accounts/10001/characters
```

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
    }
  ]
}
```

### 7. 查询单个角色

```bash
curl http://localhost:8080/v1/directory/characters/823712
```

```json
{
  "account_id": "10001",
  "server_id": "game-1001",
  "character_id": "823712",
  "name": "剑无尘",
  "level": 182,
  "class_id": 3,
  "last_login_at": "2026-09-30T12:30:00Z"
}
```

### 8. 更新角色索引

```bash
curl -X PATCH http://localhost:8080/v1/directory/characters/823712 \
  -H 'Content-Type: application/json' \
  -d '{
    "level": 183,
    "name": "剑无尘·改"
  }'
```

### 9. 查询服务器上的角色

```bash
curl http://localhost:8080/v1/directory/servers/game-1001/characters
```

```json
{
  "characters": [
    {
      "account_id": "10001",
      "character_id": "823712",
      "name": "剑无尘",
      "level": 183,
      "class_id": 3
    }
  ]
}
```

### 10. 删除角色索引

```bash
curl -X DELETE http://localhost:8080/v1/directory/characters/823712
```

### 11. 服务器下线

```bash
curl -X POST http://localhost:8080/v1/registry/servers/game-1001/unregister \
  -H 'Content-Type: application/json' \
  -d '{
    "reason": "shutdown"
  }'
```

```json
{
  "server_id": "game-1001",
  "status": "offline"
}
```

---

## 端点速查

### Registry

| Method | Path | 说明 |
| --- | --- | --- |
| `POST` | `/v1/registry/servers/register` | 注册服务器（幂等） |
| `POST` | `/v1/registry/servers/{id}/heartbeat` | 心跳上报 |
| `POST` | `/v1/registry/servers/{id}/unregister` | 主动下线 |

### Discovery

| Method | Path | 说明 |
| --- | --- | --- |
| `GET` | `/v1/discovery/servers` | 服务器列表（支持筛选） |
| `GET` | `/v1/discovery/servers/{id}` | 服务器详情 |

### Directory

| Method | Path | 说明 |
| --- | --- | --- |
| `GET` | `/v1/directory/accounts/{account_id}/characters` | 账号下所有角色 |
| `GET` | `/v1/directory/characters/{character_id}` | 单个角色索引 |
| `GET` | `/v1/directory/servers/{server_id}/characters` | 服务器上的角色 |
| `POST` | `/v1/directory/characters` | 创建角色索引（幂等） |
| `PATCH` | `/v1/directory/characters/{character_id}` | 更新角色索引 |
| `DELETE` | `/v1/directory/characters/{character_id}` | 删除角色索引 |

---

## 常用筛选参数

### 服务器列表

```bash
# 按区域
curl 'http://localhost:8080/v1/discovery/servers?region=cn-east'

# 按状态
curl 'http://localhost:8080/v1/discovery/servers?status=online'

# 按版本
curl 'http://localhost:8080/v1/discovery/servers?version=1.8.2'

# 组合筛选 + 分页
curl 'http://localhost:8080/v1/discovery/servers?region=cn-east&status=online&limit=50&cursor=game-1050'
```

支持的筛选维度：`region`、`realm`、`shard`、`version`、`platform`、`language`、`status`、`game_mode`。

### 服务器角色列表

```bash
# 按账号筛选
curl 'http://localhost:8080/v1/directory/servers/game-1001/characters?account_id=10001'

# 分页
curl 'http://localhost:8080/v1/directory/servers/game-1001/characters?limit=100&cursor=823712'
```

---

## 错误响应

所有错误遵循统一格式：

```json
{
  "error": {
    "code": "SERVER_NOT_FOUND",
    "message": "server game-9999 does not exist"
  }
}
```

| HTTP | code | 场景 |
| --- | --- | --- |
| 400 | `INVALID_ARGUMENT` | 参数缺失或格式错误 |
| 404 | `SERVER_NOT_FOUND` | 服务器不存在 |
| 404 | `CHARACTER_NOT_FOUND` | 角色索引不存在 |
| 409 | `ALREADY_REGISTERED` | 注册冲突 |
| 429 | `RATE_LIMITED` | 触发限流 |
| 503 | `STORAGE_UNAVAILABLE` | 存储不可用 |