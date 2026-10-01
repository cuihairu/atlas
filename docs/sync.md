# 数据同步

游戏角色数据如何进入 Atlas 的角色索引。

---

## 1. 核心原则

> **不要让 Game Server 每次修改角色都直接强耦合调用 Atlas API。**

正确的形态是**事件驱动**：

```text
Game Server
     │
     │ CharacterCreated
     │ CharacterUpdated
     │ CharacterDeleted
     ▼
Message Bus
     │
     ▼
   Atlas
```

---

## 2. 事件定义

```json
{
  "event": "character.created",
  "account_id": 10001,
  "server_id": 1001,
  "character_id": 823712,
  "name": "剑无尘",
  "level": 1
}
```

| 事件 | 触发时机 | 携带字段 |
| --- | --- | --- |
| `character.created` | 创建角色 | 全量索引字段 |
| `character.updated` | 升级 / 改名 / 转职 | 变更字段 |
| `character.deleted` | 删除角色 | 主键三元组 |
| `character.moved` | 转服 | 主键 + `target_server` |
| `character.login` | 登录 | 主键，用于刷新 `last_login_at` |

---

## 3. 为什么用事件而不是同步调用

| 维度 | 同步调用 | 事件驱动 |
| --- | --- | --- |
| **耦合** | 游戏服务器强依赖 Atlas 可用性 | Atlas 不可用时游戏照常运行 |
| **性能** | 角色创建路径增加一次网络往返 | 异步，不阻塞玩家操作 |
| **重试** | 每个游戏服务器自己实现 | 由 Message Bus 保证投递 |
| **扩展** | 加订阅者要改游戏服务器 | 加订阅者不改生产方 |
| **削峰** | 无 | 开服、合服时的写入洪峰被缓冲 |

最关键的是**故障隔离**：Atlas 是控制面，它的可用性不应影响游戏数据面。玩家创建角色时如果 Atlas 正在重启，不应该失败。

---

## 4. Message Bus 选型

第一版支持多种，通过 **Event Adapter** 抽象：

```text
Kafka
NATS
Redis Streams
RabbitMQ
```

### 推荐

| 场景 | 选型 | 理由 |
| --- | --- | --- |
| 中小规模 | **Redis Streams** | 已有 Redis 依赖，零额外组件 |
| 大规模 | **Kafka** | 持久化、回放、多消费者组 |
| 低延迟 | **NATS** | 轻量，适合内网高频事件 |
| 传统架构 | **RabbitMQ** | 成熟的路由与确认机制 |

### Event Adapter 接口

```text
EventAdapter
    │
    ├── Subscribe(topic, handler)
    ├── Publish(event)
    └── Ack(event)
```

Atlas Core 只依赖这个接口，不依赖具体实现。换 Message Bus 不改业务代码。

---

## 5. v0.1 的简化做法

**第一版可以直接 HTTP 同步写入，预留 Event Adapter 接口。**

```text
Game Server
     │
     │ POST /v1/directory/characters      (HTTP)
     ▼
   Atlas
```

理由：

1. **MVP 范围控制** — 引入 Message Bus 会显著增加部署复杂度与调试成本
2. **接口已就位** — 一旦 Event Adapter 抽象好了，后续切到 Kafka/NATS 不改业务
3. **量级可承受** — 角色创建是低频操作（相对登录、战斗），HTTP QPS 通常在百级

### 迁移路径

```text
v0.1   HTTP 同步写入
         │
         ▼
v0.2   Event Adapter 接口 + Redis Streams 实现
         │
         ▼
v0.3   Kafka / NATS 适配器
```

关键是**从第一天就把写入接口设计成幂等的**，这样无论底层是 HTTP 还是 MQ，重试都不会产生副作用。

---

## 6. 幂等性

事件可能重复投递（MQ 的 at-least-once 语义），Atlas 必须幂等。

```text
幂等键 = (account_id, server_id, character_id)
```

```sql
INSERT INTO character_index (account_id, server_id, character_id, name, level, class_id)
VALUES (10001, 1001, 823712, '剑无尘', 1, 3)
ON CONFLICT (account_id, server_id, character_id)
DO UPDATE SET
    name      = EXCLUDED.name,
    level     = EXCLUDED.level,
    class_id  = EXCLUDED.class_id,
    updated_at = now();
```

`ON CONFLICT DO UPDATE` 让重复的 `character.created` 变成一次无害的 upsert。

### 乱序处理

事件可能乱序到达（`updated` 先于 `created`）。两种处理策略：

| 策略 | 做法 | 适用 |
| --- | --- | --- |
| **版本号** | 事件携带单调递增版本，低版本丢弃 | 强一致要求 |
| **最终一致** | 不处理乱序，靠下一次事件覆盖 | MVP 推荐 |

v0.1 采用**最终一致**，因为索引字段只用于列表展示，短暂的 `level` 滞后可以接受。

---

## 7. 一致性边界

Atlas 的角色索引保证的是：

| 保证 | 不保证 |
| --- | --- |
| 索引完整性（账号下角色都能查到） | 字段实时准确 |
| `character_id` 不丢失 | `level` / `name` 秒级一致 |
| 合服/转服的原子切换 | 角色数据本身的一致性 |

**索引的完整性 > 字段的实时性。** 这是投影表的设计取舍。

---

## 8. 反向同步

Atlas 也需要在特定场景下**反向通知**游戏服务器：

```text
Atlas
  │
  │ server.draining
  │ server.maintenance
  │ migration.started
  ▼
Game Server
```

用途：

| 事件 | 游戏服务器响应 |
| --- | --- |
| `server.draining` | 停止接受新连接，提示玩家服务器将关闭 |
| `server.maintenance` | 拒绝新登录，存量玩家继续 |
| `migration.started` | 角色写入加锁，配合迁移 |

同样走 Message Adapter，方向相反。

---

## 9. 监控

```text
atlas_events_received_total{type}       事件接收量
atlas_events_failed_total{type}         处理失败量
atlas_events_lag_seconds                处理延迟
atlas_index_drift_estimate              索引滞后估算
```

`atlas_events_failed_total` 是最重要的告警项——它上升意味着角色索引开始与实际脱节。
