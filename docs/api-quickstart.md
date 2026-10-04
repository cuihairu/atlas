# 快速上手

跑通 Atlas 的完整链路：**起服上线 → 玩家选服 → 角色目录 → 运维操作 → 优雅下线**。
每一步都是可直接粘贴的命令和真实响应，全部步骤约 5 分钟。

## 0. 一条命令跑起来

**方式 A：源码 + 内存存储（零依赖，最适合第一次体验）**

```bash
go run ./cmd/atlas
# …level=INFO msg="public API listening" addr=:8080
# …level=INFO msg="registry API listening" addr=:8081 scheme=http
# …level=INFO msg="admin API listening" addr=:8082
```

看到三行 listening 就是结果——Atlas 已经在跑了，内存存储，无需 PostgreSQL / Redis。

**方式 B：Docker Compose（PostgreSQL + Redis 持久化）**

```bash
docker compose -f deployments/docker/docker-compose.yml up -d --build
```

自动完成：建容器、跑 `migrations/` 全部迁移、起 Atlas。数据落在卷里，重启不丢。

> 开发联调用方式 A；预发 / 压测用方式 B。方式 A 重启即清空，这只是体验限制。

## 三个端口，三种角色

Atlas 是一台服务、三个端口，先分清再敲命令——**注册走 :8081，玩家查询走 :8080，
运维走 :8082**：

| 端口 | 服务域 | 谁在调 | 本页用到的端点 |
| --- | --- | --- | --- |
| `:8080` 公网 | Discovery / Directory / Routing | 玩家客户端、网关 | 列表 / 推荐 / 角色 / 公告 |
| `:8081` 内网 | Registry | 游戏服务器进程 | 注册 / 心跳 / 注销 |
| `:8082` 管理端 | Admin | 运维平台、后台任务 | 大区 / 公告 / 维护 / 统计 |

反向代理合并部署（APISIX 前置）时三端口可收敛为一个域名，见[部署拓扑](/topology)。

---

## 1. 把游戏服务器接进来（:8081）

游戏服务器进程启动时注册自己的身份与端点，然后周期心跳保持在线：

```bash
curl -X POST http://localhost:8081/v1/registry/servers/register \
  -H 'Content-Type: application/json' -d '{
    "server_id": "game-1001",
    "name": "一区·青龙",
    "type": "game",
    "region": "cn-east",
    "realm_id": "realm-01",
    "shard_id": "shard-1001",
    "version": "1.8.2",
    "platform": "android",
    "endpoint": { "host": "10.0.1.21", "port": 30001 },
    "capacity": 2000
  }'
```

```json
{"server_id":"game-1001","status":"starting"}
```

`starting` = 已登记、未经心跳确认。**首个有效心跳自动提升 `online`**：

```bash
curl -X POST http://localhost:8081/v1/registry/servers/game-1001/heartbeat \
  -H 'Content-Type: application/json' -d '{"players": 843, "load": 0.42}'
```

```json
{"next_heartbeat_in":10,"server_id":"game-1001","status":"online"}
```

再起一台 `game-1002`（同区域、更空），后面推荐环节会用它对比：

```bash
curl -s -X POST http://localhost:8081/v1/registry/servers/register \
  -H 'Content-Type: application/json' \
  -d '{"server_id":"game-1002","name":"一区·白虎","region":"cn-east","endpoint":{"host":"10.0.1.22","port":30002},"capacity":2000}' > /dev/null
curl -s -X POST http://localhost:8081/v1/registry/servers/game-1002/heartbeat \
  -H 'Content-Type: application/json' -d '{"players":120,"load":0.11}' > /dev/null
```

> 静态舰队可以不写注册代码，用配置文件声明直接起服——见[服务器信息配置化](/server-config)。

## 2. 玩家侧：选服与角色（:8080）

**列表与筛选**——客户端拉可玩服务器，支持 `region / realm / shard / version / platform / status`
筛选与游标分页：

```bash
curl 'http://localhost:8080/v1/discovery/servers?status=online'
```

```json
{
  "servers": [
    {
      "id": "game-1001", "name": "一区·青龙", "region": "cn-east",
      "endpoint": { "host": "10.0.1.21", "port": 30001 },
      "status": "online", "players": 843, "load": 0.42, "capacity": 2000
    },
    {
      "id": "game-1002", "name": "一区·白虎", "region": "cn-east",
      "endpoint": { "host": "10.0.1.22", "port": 30002 },
      "status": "online", "players": 120, "load": 0.11, "capacity": 2000
    }
  ]
}
```

**接入推荐**——不自己做负载均衡，问 Atlas 要一个"该进哪服"的答案。没有历史角色时按
负载推荐（`lowest_load`）：

```bash
curl 'http://localhost:8080/v1/routing/recommended?account_id=10001&region=cn-east'
```

```json
{"reason":"lowest_load","server":{"id":"game-1002","name":"一区·白虎","region":"cn-east","status":"online","players":120,"load":0.11,"capacity":2000,"endpoint":{"host":"10.0.1.22","port":30002},"type":"","version":"","platform":""}}
```

**写角色索引**——玩家在 `game-1001` 建了角色，游戏服务器把投影报给 Atlas（这是索引，
不是权威数据，角色库始终归游戏服务器所有）：

```bash
curl -X POST http://localhost:8080/v1/directory/characters \
  -H 'Content-Type: application/json' -d '{
    "account_id": 10001,
    "server_id": "game-1001",
    "character_id": 823712,
    "name": "剑无尘",
    "level": 182,
    "class_id": 3
  }'
```

```json
{"account_id":10001,"server_id":"game-1001","character_id":823712,"name":"剑无尘","level":182,"class_id":3,"last_login_at":"…","created_at":"…","updated_at":"…"}
```

**角色粘滞**——同一个账号再问推荐，Atlas 让他回到已有角色的服务器（`has_character`），
这就是"我的角色在哪"的直接答案：

```bash
curl 'http://localhost:8080/v1/routing/recommended?account_id=10001&region=cn-east'
```

```json
{"reason":"has_character","server":{"id":"game-1001","…":"…"}}
```

**账号跨服角色**——登录后一次拉全：

```bash
curl http://localhost:8080/v1/directory/accounts/10001/characters
```

```json
{"characters":[{"account_id":10001,"server_id":"game-1001","character_id":823712,"name":"剑无尘","level":182,"class_id":3,"last_login_at":"…","created_at":"…","updated_at":"…"}]}
```

> 大规模写路径不该逐条落地——同一个写端点加一个环境变量切到 Message Bus
> 异步缓冲（Atlas 入队、内建消费循环回放），游戏服务器仍只调 REST，
> 见[数据同步](/sync)。

## 3. 运维侧：大区、公告、维护、统计（:8082）

**Realm / Shard**——大区与分服的运营档案，服务器的 `realm_id` / `shard_id` 与之对应，
之后可按维度筛选发现列表：

```bash
curl -X POST http://localhost:8082/v1/admin/realms \
  -H 'Content-Type: application/json' -d '{"id":"realm-01","name":"华东大区","region":"cn-east"}'
```

```json
{"id":"realm-01","name":"华东大区","region":"cn-east","status":"active","created_at":"…"}
```

```bash
curl -X POST http://localhost:8082/v1/admin/shards \
  -H 'Content-Type: application/json' -d '{"id":"shard-1002","name":"白虎服","realm_id":"realm-01"}'
```

```json
{"id":"shard-1002","realm_id":"realm-01","name":"白虎服","status":"active","created_at":"…"}
```

```bash
curl 'http://localhost:8080/v1/discovery/servers?shard=shard-1001'
# {"servers":[ …只有 game-1001… ]}
```

完整用法见[Realm 与 Shard](/realms-shards)。

**公告**——一条 API 发布全局公告，客户端登录即拉到：

```bash
curl -X POST http://localhost:8082/v1/admin/announcements \
  -H 'Content-Type: application/json' -d '{
    "title": "周末双倍经验",
    "body": "10月4日 10:00-22:00 全服双倍经验",
    "level": "info",
    "starts_at": "2026-10-04T02:00:00Z",
    "ends_at": "2026-10-04T14:00:00Z"
  }'
```

```json
{"id":"ann-1790909864389093095","title":"周末双倍经验","body":"10月4日 10:00-22:00 全服双倍经验","level":"info","starts_at":"2026-10-04T02:00:00Z","ends_at":"2026-10-04T14:00:00Z","created_at":"…"}
```

玩家侧（:8080）只返回**生效区间内**的公告——上面这条 `starts_at` 在未来，现在拉是空的，
到点自动出现：

```bash
curl http://localhost:8080/v1/discovery/announcements
# {"announcements":[]}   ← 未到生效时间
```

**计划维护**——声明一个维护窗口，到点自动进维护、结束自动恢复，还自动挂出维护公告
（`announcement_id` 就是它建的）：

```bash
curl -X POST http://localhost:8082/v1/admin/servers/game-1002/maintenance-window \
  -H 'Content-Type: application/json' -d '{
    "start_at": "2026-10-02T03:00:00Z",
    "end_at": "2026-10-02T04:00:00Z"
  }'
```

```json
{"id":"mwin-1790909851023996409","server_id":"game-1002","start_at":"2026-10-02T03:00:00Z","end_at":"2026-10-02T04:00:00Z","announcement_id":"ann-1790909851023996409","created_at":"…"}
```

场景化细节见[公告与计划维护](/operations)。

**舰队统计**——运营大盘的一手数字：

```bash
curl http://localhost:8082/v1/admin/stats
```

```json
{"total_servers":2,"servers_by_status":{"online":2},"servers_by_region":{"cn-east":2},"servers_by_version":{"":1,"1.8.2":1},"total_players":963,"total_capacity":4000,"total_characters":1}
```

## 4. 优雅下线（:8082 + :8081）

先排空（停止接新玩家），存量自然走完后注销：

```bash
curl -X POST http://localhost:8082/v1/admin/servers/game-1002/drain
curl -X POST http://localhost:8081/v1/registry/servers/game-1002/unregister -H 'Content-Type: application/json' -d '{}'
```

```json
{"server_id":"game-1002","status":"offline"}
```

之后 `?status=online` 的列表里不再有它。完整生命周期见[服务器生命周期](/lifecycle)。

---

## 端点速查

| 域 | Method | Path | 端口 | 说明 |
| --- | --- | --- | --- | --- |
| Registry | `POST` | `/v1/registry/servers/register` | :8081 | 注册（幂等） |
| Registry | `POST` | `/v1/registry/servers/{id}/heartbeat` | :8081 | 心跳 |
| Registry | `POST` | `/v1/registry/servers/{id}/unregister` | :8081 | 主动下线 |
| Discovery | `GET` | `/v1/discovery/servers` | :8080 | 列表（筛选 + 分页） |
| Discovery | `GET` | `/v1/discovery/servers/{id}` | :8080 | 详情 |
| Discovery | `GET` | `/v1/discovery/announcements` | :8080 | 生效中的公告 |
| Directory | `POST` | `/v1/directory/characters` | :8080 | 写角色索引（幂等） |
| Directory | `GET` | `/v1/directory/accounts/{id}/characters` | :8080 | 账号跨服角色 |
| Directory | `GET` | `/v1/directory/characters/{id}` | :8080 | 单角色 |
| Directory | `GET` | `/v1/directory/servers/{id}/characters` | :8080 | 服务器角色列表 |
| Directory | `PATCH` / `DELETE` | `/v1/directory/characters/{id}` | :8080 | 改 / 删 |
| Routing | `GET` | `/v1/routing/recommended` | :8080 | 接入推荐 |
| Admin | `POST` | `/v1/admin/realms` / `/v1/admin/shards` | :8082 | 大区 / 分服档案 |
| Admin | `POST` | `/v1/admin/servers/{id}/maintenance` / `drain` / `enable` / `disable` | :8082 | 生命周期操作 |
| Admin | `POST` | `/v1/admin/servers/{id}/maintenance-window` | :8082 | 计划维护窗口 |
| Admin | `POST` / `GET` / `DELETE` | `/v1/admin/announcements[/{id}]` | :8082 | 公告管理 |
| Admin | `POST` / `GET` | `/v1/admin/migrations` | :8082 | 合服 / 转服编排 |
| Admin | `GET` | `/v1/admin/stats` | :8082 | 舰队统计 |
| Admin | `GET` | `/v1/admin/audit` · `/metrics` | :8082 | 审计 · Prometheus |

## 错误响应

统一格式：

```json
{"error":{"code":"SERVER_NOT_FOUND","message":"server game-9999 does not exist"}}
```

| HTTP | code | 场景 |
| --- | --- | --- |
| 400 | `INVALID_ARGUMENT` | 参数缺失或格式错误 |
| 404 | `SERVER_NOT_FOUND` / `CHARACTER_NOT_FOUND` | 服务器 / 角色不存在 |
| 409 | `SERVER_MANAGED_BY_CONFIG` | 注册对象由服务器配置文件托管（见[配置化](/server-config)） |
| 429 | `RATE_LIMITED` | 触发限流 |
| 503 | `STORAGE_UNAVAILABLE` | 存储不可用 |

## 下一步

| 想做什么 | 去哪 |
| --- | --- |
| 不写注册代码，配置文件声明服务器 | [服务器信息配置化](/server-config) |
| 游戏服务器接 SDK（Go / C++ / Python / JS / Java / C#） | 侧栏「SDK」任一篇，均有同款走查 |
| 角色写入走 Message Bus 而不是逐条 REST | [数据同步](/sync) |
| 掉线判定 / 状态机 / 运维操作语义 | [服务器生命周期](/lifecycle) |
| 大区 / 分服运营 | [Realm 与 Shard](/realms-shards) |
| 公告 / 计划维护的场景与边界 | [公告与计划维护](/operations) |
| 全部端点与参数 | [API 参考](/api) |
