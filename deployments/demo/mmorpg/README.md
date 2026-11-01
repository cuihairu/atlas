# MMORPG 大世界 demo —— 分线制 + 跨服战场

一键起一套"活的" MMORPG 服务器拓扑：网关接入、分线场景服按大区/分线组织、
跨服战场节点实时收跨服配置，角色目录按大区/分线落库。

```bash
docker compose up -d --build   # 管理台 http://localhost:28210
docker compose down -v         # 收尾（清库重来）
```

## 品类机制 → Atlas 概念映射

| MMORPG 机制 | Atlas 概念 | 本 demo 的体现 |
|---|---|---|
| 大区（华东天域 / 华北星海） | realm | `realm-tianyu` / `realm-xinghai`，demoagents 启动时经管理 API 自动建 |
| 分线（一线/二线…） | shard（realm 下的分片） | 每条线一个 `shard-tianyu-N`，场景服注册时挂 shard |
| 网关接入层 | server type = `gateway` | 2 台网关，只做流量不入目录角色 |
| 场景服（承载玩家） | server type = `scene` + 心跳 players/load | 6 台，人数在基线上正弦漂移，容量条随时间动 |
| 跨服战场（王者之巅等） | server type = `cross` + 跨服配置 | 2 台：`subscribe` 模式走 Redis 信号，`callback` 模式走回调 |
| 停机维护的分线 | seed_only + maintenance 标记 | 天域四线：注册后即停心跳、打维护标记，状态不回 online |
| 角色目录（大区/分线组织） | directory（account → 每 server 的角色） | 账号 700001 在一线/跨服战场都有角色，管理台角色页可按账号跨服查看 |

## 拓扑

```
realm-tianyu (华东·天域)                realm-xinghai (华北·星海)
├─ mmorpg-gw-east-1        网关          ├─ mmorpg-gw-north-1       网关
├─ shard-tianyu-1 一线·青龙 [hot][full]  ├─ shard-xinghai-1 一线·天枢
├─ shard-tianyu-2 二线·白虎 [recommended]├─ shard-xinghai-2 二线·天璇
├─ shard-tianyu-3 三线·朱雀 [new]        └─ (跨服) mmorpg-xbattle-2  callback 模式
├─ shard-tianyu-4 四线·玄武 [maintenance·seed_only]
└─ (跨服) mmorpg-xbattle-1  subscribe 模式
```

## 管理台里看什么

- **服务器列表**：10 台按 region/realm 过滤；四线停在 `maintenance` 状态。
- **角色目录**：搜账号 `700001` —— 青岚在一线、跨服战场各有一行（跨服角色视图）。
- **跨服配置**：发布 demo 附带的 spec（改任何字段重发一次即热更），
  `docker compose logs demoagents` 里立刻出现
  `cross-server config applied (hot, no restart)` —— subscribe 模式的热更新链路：

  ```bash
  curl -X PUT -H 'Content-Type: application/json' \
    --data-binary @crossserver-config.json \
    http://localhost:28210/v1/admin/crossserver/config
  ```

  spec 有两条拓扑约束（PUT 会被 400 拒绝并说明原因）：分组里的服务器必须
  先出现在某个 cluster 里；一个分组必须整个落在单一 cluster 内，跨集群就拆成
  多个分组——「拓扑为准，分组受拓扑约束」。

## 与源码的关系

fleet 是纯数据（`fleet.json`），由 `cmd/atlas-demoagents` 解释执行；改服务器
数量/名字/人数后 `docker compose restart demoagents` 即生效（先把数据卷清掉：
`docker compose down -v`）。
