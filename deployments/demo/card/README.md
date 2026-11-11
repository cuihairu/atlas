# 卡牌/放置 demo —— 最小接入形态

弱实时品类的接入下限：三个进程、无 realm/shard、无标签、无跨服配置、
默认 poll 模式——**先把"注册 + 心跳 + 角色目录"跑通**，其余能力按品类需要
再加。这个 demo 就是给"我的游戏很小，Atlas 怎么接"这个问题的最短答案。

```bash
docker compose up -d --build   # 管理台 http://localhost:28240
docker compose down -v         # 收尾（清库重来）
```

## 品类机制 → Atlas 概念映射

| 卡牌/放置机制 | Atlas 概念 | 本 demo 的体现 |
|---|---|---|
| 账号服（登录/充值回调） | server type = `account` | `card-account-1`，容量 20000 |
| 游戏逻辑服（卡牌养成） | server type = `game` + 角色目录 | `card-game-1`，两个演示账号各一角色 |
| 战斗计算（无状态、可横向扩） | server type = `battle` | `card-battle-1`：无角色，按需多开同型即可分摊计算 |
| 无大区/无分线 | 不建 realm/shard | fleet 不声明，注册时不挂拓扑——Atlas 不强制 |
| 无实时扩缩容压力 | 心跳放宽 15s | `-interval=15s`：弱实时服心跳可以更稀 |

## 为什么这就是"最小接入"

一个卡牌服接 Atlas 只需要三件事，demo 各用一行配置示范：

1. **注册**：`POST /v1/registry/servers/register`（SDK `Register`）——
   进服务器列表、可被发现。
2. **心跳**：`Heartbeat` 带人数/负载——管理台的容量条与趋势图有数据。
3. **角色目录**：`CreateCharacter`——账号跨服换绑/客服查询有据可查。

没有 realm/shard 的世界照样完整工作；等品类长大（开海外服、出跨服玩法），
再往 fleet 里加 `realm_id`/`shard_id`/`notify_mode` 字段即可，注册接口不变。

## 管理台里看什么

- **服务器列表**：3 台，region 都是 cn-east——小体量就是一张干净的表。
- **角色目录**：搜账号 `900001` —— 欧皇本皇一行，这就是客服侧的查询视图。

## 与其他 demo 的对照

其他四个 demo 展示的是"品类机制长在 Atlas 的哪些概念上"；这个 demo 展示
的是**不使用那些概念时的基线**——Atlas 的必选面只有注册/心跳/目录三项。
