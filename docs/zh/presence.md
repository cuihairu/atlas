# 玩家在线状态（Player Presence）

角色的在线状态是 lobby / 好友列表 / 公会名册最需要、且只有游戏服自己知道的少数事实之一。
Atlas 把它记下来，任何组件——不只是看到登录的那台服——都能回答「这个角色在不在线、在哪台服」。

在线投影是一个基于两行的小状态机：

- `character_index.presence_status`（`unknown` / `online` / `offline`）以及 `last_login_at` / `last_logout_at`。
- 每次登录对应 `player_sessions` 一行，登出时关闭。

一个角色**在线 ⟺ 恰好有一条打开会话**。下面的每条流转都按构造保持该不变量：
登录在同一个临界区 / 事务里打开会话并翻转在线，登出在同一个里关闭会话并翻转离线。

## 1. 数据模型

| 字段 | 类型 | 含义 |
| --- | --- | --- |
| `PresenceStatus` | `unknown` \| `online` \| `offline` | 投影在线状态；首次登录前为 `unknown` |
| `PlayerSession.SessionID` | string | 游戏服生成的会话 id（每次登录唯一） |
| `PlayerSession.AccountID` | int64 | 所属账号 |
| `PlayerSession.CharacterID` | int64 | 角色 |
| `PlayerSession.ServerID` | string | 登录的那台服 |
| `PlayerSession.StartedAt` | time | 登录时间 |
| `PlayerSession.EndedAt` | *time | 登出时间；会话打开时为 nil |
| `PlayerSession.EndReason` | `normal` \| `timeout` \| `kicked` \| `crash` | 会话结束原因 |

`EndReason` 集合是固定的：玩家退出（`normal`）、服侧超时（`timeout`）、运营踢人（`kicked`）、
或服崩溃 / 丢失会话（`crash`）。Atlas 在治愈游离打开会话时按 `crash` 关闭——例如迁服或删除
留下的那条会话。

## 2. 上报通道

游戏服通过**注册信任区**上报在线状态，与 register / heartbeat / unregister 同一条通道。
它继承 `RegistryAuth`：服务 token（`Authorization: Bearer <token>` 或 `X-Atlas-Token`）
以及可选 IP 白名单。

`POST /v1/registry/servers/{id}/presence`

路径 `{id}` 是**上报方**——游戏服只能上报自己那台服，所以请求体里刻意不收 `server_id`。
该服必须已注册（否则 404）。

**请求**

```json
{
  "action": "login",
  "account_id": 10001,
  "character_id": 823712,
  "session_id": "sess-9f2c",
  "timestamp": "2026-11-22T12:30:00Z"
}
```

登出再加原因：

```json
{
  "action": "logout",
  "account_id": 10001,
  "character_id": 823712,
  "session_id": "sess-9f2c",
  "end_reason": "normal",
  "timestamp": "2026-11-22T12:40:00Z"
}
```

- `action` — `login` 或 `logout`（必填）。
- `account_id` / `character_id` — 上报对（必填，> 0）。
- `session_id` — `login` 必填；`logout` 指明要关闭的会话（空 = 关掉当前打开的那个）。
- `end_reason` — `logout` 可选，四种之一；`login` 忽略。
- `timestamp` — 产生方发生时间；缺省为「现在」。

**响应** `200 OK`（同步适配器）/ `202 Accepted`（异步）

```json
{
  "server_id": "game-1001",
  "character_id": 823712,
  "presence_status": "online",
  "presence": {
    "presence_status": "online",
    "active_session": {
      "session_id": "sess-9f2c",
      "started_at": "2026-11-22T12:30:00Z"
    }
  }
}
```

`202` 意为**已入队**，并非状态机已接受：依赖当前状态的校验（冲突 login、无打开会话的
logout）只在消费时执行，表现为消费失败——按适配器的投递语义
（[sync.md §4](sync.md#_4-message-bus-选型)）重投与死信，而非 `409`。

**错误**

| 码 | HTTP | 场景 |
| --- | --- | --- |
| `INVALID_ARGUMENT` | 400 | action 非法、login 缺 `session_id`、`end_reason` 非法 |
| `SERVER_NOT_FOUND` | 404 | 路径服未注册 |
| `CHARACTER_NOT_FOUND` | 404 | 角色不在索引 |
| `PRESENCE_CONFLICT` | 409 | 非法流转（在线时另一会话登录、登出非打开会话） |
| `MISSING_TOKEN` / `INVALID_TOKEN` | 401 | 服务 token 缺失 / 非法 |
| `IP_NOT_ALLOWED` | 403 | 客户端 IP 不在白名单 |

## 3. 状态机

登录和登出是**唯一**移动在线状态的操作。普通的 `character.updated` 补丁、
`character.created` upsert、`character.moved` 迁服都**不碰** `presence_status`。

| 当前 | 上报 | 结果 |
| --- | --- | --- |
| `unknown` | login | 打开会话，翻 `online`，盖 `last_login_at` |
| `unknown` | logout | **`INVALID_ARGUMENT`**——没有可关的打开会话 |
| `online` | login，同 `session_id` | **幂等 no-op**（at-least-once 传输会重投） |
| `online` | login，异 `session_id` | **`PRESENCE_CONFLICT`**——先关旧会话（`logout` + `crash`） |
| `online` | logout，打开会话 | 关会话，翻 `offline`，盖 `last_logout_at` |
| `online` | logout，非打开 `session_id` | **`PRESENCE_CONFLICT`** |
| `offline` | login | 开新会话，翻 `online` |
| `offline` | logout | **幂等 no-op**（重复投递） |

**崩溃治愈。** 如果投影被留下「有打开会话但离线」——迁服或删除是常见元凶——下一次 `login`
先按 `crash` 关闭游离会话再开新会话，恢复不变量并保持历史自洽。`moved` 事件把目标行重置为
`unknown`；在线状态从不随角色迁移。

## 4. 查询 API

读走目录面。

### GET /v1/directory/characters/{character_id}/presence

角色在线查询——角色的在线状态，以及在线时的打开会话。

**响应** `200 OK`

```json
{
  "presence_status": "online",
  "active_session": {
    "session_id": "sess-9f2c",
    "account_id": 10001,
    "character_id": 823712,
    "server_id": "game-1001",
    "started_at": "2026-11-22T12:30:00Z"
  },
  "character": {
    "account_id": 10001,
    "server_id": "game-1001",
    "character_id": 823712,
    "name": "Jian Wuchen",
    "presence_status": "online"
  }
}
```

离线时 `active_session` 省略。角色不在索引时 `404`。

### GET /v1/directory/servers/{server_id}/presence

某服在线名单与计数——某服的在线名单、实时计数与游标分页。

**Query**：`?limit=`（默认 50，最大 200）、`?cursor=`（续传 token）。

**响应** `200 OK`

```json
{
  "server_id": "game-1001",
  "online_count": 2,
  "characters": [
    { "character_id": 823712, "name": "Jian Wuchen", "presence_status": "online" },
    { "character_id": 823713, "name": "Wuchen", "presence_status": "online" }
  ]
}
```

`next_cursor` 是最后一个返回的 `character_id`——续传 token，最后一页也会带上。
越过它读返回空名单。

### 会话历史

`GET /v1/admin/...` 不暴露会话；会话经 store 契约与 directory 服务可达，供运营与后续
管理面使用。

## 5. 存储说明

- **迁移** `0010`（PostgreSQL）与 `0010_player_presence_mysql`（MySQL）给
  `character_index` 加 `presence_status` / `last_logout_at` 并建 `player_sessions`，
  带 `(server_id, presence_status)` 索引支撑名单查询。
- store 契约（`storetest`）把状态机钉在 memory / PostgreSQL / MySQL 三套实现上——
  幂等与非法流转规则三者完全一致。
- 内存 store 里 login/logout 是**不合并**的控制 lane 指令（状态机不是 last-writer-wins），
  与其它写一样走 capture ring，`ReplayInto` 能复现会话历史。
