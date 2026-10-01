# 合服 / 转服 / 迁服

Atlas 的角色目录设计让它天然适合承载服务器之间的角色迁移。这是 Atlas 相对普通 Service Discovery 的第二大差异点。

---

## 1. 场景

```text
合服      多个服务器合并为一个
转服      玩家把角色从一个服务器移到另一个
迁服      服务器整体搬迁（换机房 / 换集群）
跨区      跨 Region 迁移
```

四者在 Atlas 中抽象为同一件事：**角色索引的 `server_id` 从源迁移到目标**。

---

## 2. 合服

```text
Server 1001
Server 1002
     │
     │ merge
     ▼
Server 2001
```

### 角色目录的变化

```text
character_id 123
old_server     = 1001
current_server = 2001
```

### 过程

```text
1. 运维创建 migration 记录        status = pending
2. 目标服务器 2001 进入 starting  加载合并后的角色数据
3. 源服务器 1001 / 1002 进入 draining
4. 存量玩家自然离开或到达超时
5. 源服务器角色数据导出 → 导入目标
6. 更新 character_index 的 server_id    status = migrating
7. 校验角色数量一致
8. 源服务器 offline，目标 online         status = completed
```

### 校验

合服最容易出问题的是**角色丢失**。Atlas 在第 7 步提供校验能力：

```text
GET /v1/directory/servers/{source_server}/characters?migration_id={id}
```

比对迁移前后的角色数与 `character_id` 集合，确保：

```text
source 角色数  ==  target 中来自 source 的角色数
无 character_id 遗漏
无 character_id 重复
```

---

## 3. 转服

玩家主动转移单个角色。

```text
character_id 123
    source_server = 1001
    target_server = 2001
```

与合服的区别：转服是**单角色粒度**，合服是**服务器粒度**。但对 Atlas 而言都是 `character_index` 中 `server_id` 字段的变更。

```text
1. 游戏服务器完成角色数据转移
2. 调用 PATCH /v1/directory/characters/{id}  { "server_id": "2001" }
3. Atlas 事务性更新索引
```

Atlas 需要保证的是**索引更新的原子性**，不是数据转移本身——数据转移由游戏服务器负责。

---

## 4. 迁服

服务器整体搬迁，`server_id` 不变，`endpoint` 变化。

```text
game-1001
    before: 10.0.1.21:30001
    after:  10.0.5.88:30001
```

对 Atlas 只是 `servers.endpoint` 的更新，角色索引无需变更。

```text
1. 目标实例以相同 server_id 注册，新 endpoint
2. Atlas 更新 endpoint，状态置为 starting
3. 旧实例 draining
4. 新实例 online
```

---

## 5. 数据模型

### server_migrations

```sql
CREATE TABLE server_migrations (
    id            TEXT PRIMARY KEY,
    source_server TEXT        NOT NULL,
    target_server TEXT        NOT NULL,
    status        TEXT        NOT NULL DEFAULT 'pending',
    started_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at  TIMESTAMPTZ
);
```

`status` 取值：

```text
pending      已创建，未开始
migrating    进行中
verifying    校验中
completed    已完成
failed       失败，需要人工介入
rolled_back  已回滚
```

### character_index 的变更

合服不产生新行，只更新 `server_id`：

```sql
-- 合服：1001 与 1002 并入 2001
UPDATE character_index
   SET server_id = '2001', updated_at = now()
 WHERE server_id IN ('1001', '1002');
```

因为主键是 `(account_id, server_id, character_id)`，`server_id` 变更会改变主键，所以需要：

1. 插入新键行
2. 删除旧键行
3. **在同一个事务中完成**

```sql
BEGIN;

INSERT INTO character_index (account_id, server_id, character_id, name, level, class_id, last_login_at)
SELECT account_id, '2001', character_id, name, level, class_id, last_login_at
  FROM character_index
 WHERE server_id = '1001';

DELETE FROM character_index WHERE server_id = '1001';

INSERT INTO server_migrations (id, source_server, target_server, status, completed_at)
VALUES ('mig-2026-001', '1001', '2001', 'completed', now());

COMMIT;
```

**这正是角色索引必须放在 PostgreSQL 而不是 Redis 的原因**：跨行事务是刚需。

---

## 6. 迁移 API

### POST /v1/admin/migrations

```json
{
  "type": "merge",
  "source_servers": ["game-1001", "game-1002"],
  "target_server": "game-2001"
}
```

### GET /v1/admin/migrations/{id}

```json
{
  "id": "mig-2026-001",
  "type": "merge",
  "source_servers": ["game-1001", "game-1002"],
  "target_server": "game-2001",
  "status": "verifying",
  "started_at": "2026-10-01T02:00:00Z",
  "completed_at": null,
  "progress": {
    "characters_total": 18420,
    "characters_migrated": 18420,
    "verify_passed": null
  }
}
```

### POST /v1/admin/migrations/{id}/rollback

失败时回滚。前提是迁移过程中的每一步都可逆，因此**迁移必须是幂等且可重放的**。

---

## 7. 幂等与重放

迁移过程中途失败是常态（网络抖动、目标服务器崩溃、磁盘满）。Atlas 的迁移必须满足：

| 要求 | 实现 |
| --- | --- |
| **幂等** | 以 `character_id` 为粒度，重复迁移同一角色不产生副作用 |
| **可重放** | 记录迁移进度游标，失败后从中断点继续 |
| **可回滚** | 保留源服务器的角色索引直到校验通过 |
| **可校验** | 提供迁移前后角色集合比对接口 |

核心设计：**先复制，后切换，再清理**，而不是原地移动。

```text
复制   source 的索引行复制到 target 新键   （source 行保留）
校验   比对 source 与 target 的角色集合
切换   标记 migration = completed
清理   删除 source 行
```

任何一步失败，源数据都还在，可以重来。

---

## 8. 为什么归 Atlas 管

一个自然的问题是：**合服不就是游戏服务器自己的事吗？**

不是。原因有三：

1. **跨服务器** — 合服涉及两个以上服务器，任何单个服务器都无法拥有全局视图。
2. **角色索引是 Atlas 的数据** — `character_index` 的归属在 Atlas，`server_id` 的变更只能由 Atlas 原子完成。
3. **可观测性** — 迁移进度、失败原因、回滚记录需要统一的地方呈现给运维。

但要注意边界：

| 归 Atlas | 归游戏服务器 |
| --- | --- |
| 角色索引的 `server_id` 变更 | 角色权威数据的搬移 |
| 迁移记录与状态机 | 迁移过程中玩家的在线体验 |
| 校验索引完整性 | 校验角色数据完整性 |
| 迁移编排 | 具体的数据导入导出 |

Atlas 是**编排者与索引持有者**，不是数据搬运工。
