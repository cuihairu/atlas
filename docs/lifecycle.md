# 服务器生命周期

服务器不是简单的 `online / offline`，而应有完整生命周期。这对大型游戏尤为重要。

---

## 1. 状态机

```text
                         ┌──────────┐
                         │ STARTING │
                         └────┬─────┘
                              ▼
                         ┌──────────┐
                    ┌────│ ONLINE   │────┐
                    │    └──────────┘    │
                    │                    │
                    ▼                    ▼
              ┌──────────┐         ┌────────────┐
              │ DRAINING │         │ MAINTENANCE │
              └────┬─────┘         └────────────┘
                   │
                   ▼
              ┌──────────┐
              │ OFFLINE  │
              └──────────┘
```

完整状态集：

| 状态 | 含义 | 客户端可见 | 可接受新连接 | 存量玩家 |
| --- | --- | --- | --- | --- |
| `starting` | 进程启动中，尚未就绪 | 否 | 否 | — |
| `online` | 正常服务 | 是 | 是 | 正常 |
| `draining` | 排水中 | 否 | 否 | 正常 |
| `maintenance` | 维护中 | 是（标记维护） | 否 | 正常 |
| `suspect` | 疑似失联 | 是（标记异常） | 是 | 正常 |
| `offline` | 已下线 | 否 | 否 | — |
| `disabled` | 运维禁用 | 否 | 否 | 强制断开 |

---

## 2. 状态详解

### STARTING

服务器进程已启动，正在加载资源、连接依赖、预热缓存。

- 由 `register` 触发进入
- 就绪后由游戏服务器显式上报 `status: online`，或 Atlas 依据首次有效心跳自动提升
- **不对外可见**，避免客户端连上一个还没准备好的服务器

### ONLINE

正常服务状态。唯一同时满足"客户端可见 + 接受新连接"的状态。

### DRAINING

排水状态，用于**优雅下线**。

```text
ONLINE
  ↓
DRAINING
  ↓
OFFLINE
```

- 停止接受新连接
- **存量玩家不受影响**，继续游戏直到自己退出
- 等待玩家自然离开或到达超时阈值后转 `OFFLINE`
- 通常用于版本更新前的缩容、实例替换

### MAINTENANCE

维护状态。

```text
ONLINE
  ↓
MAINTENANCE
```

**与 DRAINING 的关键区别**：`MAINTENANCE` 时**老玩家可以继续游戏**，只是新玩家不能进入。

- 客户端选服界面会展示"维护中"标记，而不是隐藏该服务器
- 适用于：临时故障排查、数据库变更、热修复
- 维护完成后转回 `ONLINE`

### SUSPECT

疑似失联。

```text
online
   ↓
heartbeat timeout
   ↓
suspect
   ↓
offline
```

- 心跳超时但尚未确认死亡
- **仍然对外可见**，因为可能是网络抖动而非服务器崩溃
- 客户端会看到"连接不稳定"之类的提示
- 超过第二段阈值后转 `OFFLINE`

两段式设计的意义：避免因一次网络抖动就把服务器从列表中摘掉，导致客户端频繁刷新列表。

### OFFLINE

已下线。不可见、不接受连接。

### DISABLED

运维禁用。与 `OFFLINE` 的区别是这是**显式人工操作**，需要 `enable` 才能恢复，不会被自动状态机覆盖。

---

## 3. 状态转移触发

| 转移 | 触发方 | 说明 |
| --- | --- | --- |
| — → `starting` | 游戏服务器 | `register` |
| `starting` → `online` | 游戏服务器 / Atlas | 首次有效心跳 |
| `online` → `draining` | 运维 | `POST /admin/servers/{id}/drain` |
| `online` → `maintenance` | 运维 | `POST /admin/servers/{id}/maintenance` |
| `online` → `suspect` | Atlas | 心跳超时阈值 1 |
| `suspect` → `online` | Atlas | 收到心跳（恢复） |
| `suspect` → `offline` | Atlas | 心跳超时阈值 2 |
| `draining` → `offline` | Atlas / 游戏服务器 | 存量清零或超时 |
| `maintenance` → `online` | 运维 | `POST /admin/servers/{id}/enable` |
| 任意 → `disabled` | 运维 | `POST /admin/servers/{id}/disable` |
| 任意 → `offline` | 游戏服务器 | `unregister` |

---

## 4. 自动掉线判定

Atlas 用 Redis TTL 做第一层判定，定时任务做第二层状态推进：

```text
心跳写入
  │
  └─▶ PEXPIRE atlas:server:{id}:hb   TTL = 30s (3 个心跳周期)
              │
              ▼
        键过期 = 心跳超时
              │
              ▼
定时任务 (每 10s) 扫描
  │
  ├─▶ 超时 < 30s   → 保持 online
  ├─▶ 超时 30–60s  → suspect
  └─▶ 超时 > 60s   → offline
```

阈值建议：

| 参数 | 建议值 | 说明 |
| --- | --- | --- |
| 心跳间隔 | 10s | 游戏服务器上报频率 |
| TTL | 30s | 3 个心跳周期，容忍单次丢包 |
| suspect 阈值 | 30s | TTL 过期即进入 |
| offline 阈值 | 60s | 再给一个周期确认 |

阈值应可配置，不同部署环境（同机房 vs 跨地域）差异较大。

---

## 5. 与 API 的对应

| 状态操作 | API |
| --- | --- |
| 注册 | `POST /v1/registry/servers/register` |
| 心跳 | `POST /v1/registry/servers/{id}/heartbeat` |
| 主动下线 | `POST /v1/registry/servers/{id}/unregister` |
| 进入维护 | `POST /v1/admin/servers/{id}/maintenance` |
| 排水 | `POST /v1/admin/servers/{id}/drain` |
| 启用 | `POST /v1/admin/servers/{id}/enable` |
| 禁用 | `POST /v1/admin/servers/{id}/disable` |

完整定义见 [api.md](api.md)。

---

## 6. 客户端可见性规则

Discovery 接口的默认行为：

| 状态 | 默认返回 | 理由 |
| --- | --- | --- |
| `online` | ✅ | 正常 |
| `maintenance` | ✅（带标记） | 玩家需要知道服务器在维护，而不是"消失" |
| `suspect` | ✅（带标记） | 可能是网络抖动，不应摘除 |
| `starting` | ❌ | 未就绪 |
| `draining` | ❌ | 正在下线，不应再进入 |
| `offline` | ❌ | 已死 |
| `disabled` | ❌ | 运维禁用 |

**核心原则：客户端不应看到已经死掉的服务器，但也不应看到"突然消失"的服务器。** `maintenance` 与 `suspect` 保持可见并打标记，是避免客户端列表抖动的关键。
