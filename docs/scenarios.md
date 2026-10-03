# 场景导览

按游戏运营里最常见的一天，把 Atlas 用一遍。每节 = 场景描述 + 真实界面截图 + 具体操作路径。截图全部来自真实运行的演示集群；想先看整体印象，回[首页走马灯](/)。

## 1. 运营每天先看这一页 {#scenario-overview}

**场景**：早上开服前扫一眼全局——多少服在线、多少玩家、容量水位、状态分布。数字不对劲（在线数掉了、某区域全是 suspect）就是今天的第一件事。

![总览页 — 在线服务器/玩家/容量卡片与状态、区域分布图](/screenshots/overview.png)

**操作路径**

- 管理台：`Overview`（总览）——「在线服务器 / 总容量 / 在线玩家」卡片 + 状态分布、区域分布、近 24 小时玩家趋势。
- 接口：`GET /v1/admin/stats`（管理口 :8082）——页面上的每个数字都来自这一条真实接口：

```bash
curl -s localhost:8082/v1/admin/stats | jq
# "online_servers": 8, "total_players": 4628 ← 玩家数由 Redis 心跳视图实时合并
```

**延伸**：[架构设计](/architecture) · [性能基准](/benchmarks)

## 2. 给服务器打标记 {#scenario-tags}

**场景**：开新服要突出「新服」、大促的服挂「火热」、满了的标「爆满」、要停导入的打「禁止注册」。有些标记给玩家看（角标），有些只给运营看（如内部备注）。**「禁止注册」不只是显示——注册/创角接口会真的拒绝并返回明确错误。**

![服务器列表 — 标记列渲染角标：火热（火爆红）/ 新服（新服绿）/ 爆满（警示黄）](/screenshots/servers.png)

![服务器详情 — 标记管理面板：预设下拉 + 自定义（代码/文案/样式档位/对外开关），标记区分 对外/内部](/screenshots/server-detail-tags.png)

**操作路径**（管理台：`Servers` → 点开某台服务器 → 「标记管理」卡片）

1. 选预设（自带文案与样式档位），或填自定义：标记代码 + 展示文案 + 样式档位（火爆红/新服绿/警示黄/信息蓝/中性）+ 是否对外。
2. 对外标记立即出现在玩家可见的服务器列表/详情（角标）；内部标记只存在于管理台。
3. 「禁止注册」生效于 `POST /v1/directory/characters` 与 gRPC 创角；「维护中」标记与服务端维护状态同权，可按 `ATLAS_MAINTENANCE_ENFORCE=block|warn` 配置为拒绝或放行加告警头。

等价的接口操作与一次真实走查（打「火热+禁止注册」→ 玩家侧出角标 → 创角被拒 → 摘除恢复）：

```bash
# 打标记（管理口 :8082）
curl -X POST localhost:8082/v1/admin/servers/game-1001/tags \
     -H 'Content-Type: application/json' -d '{"code":"hot"}'
curl -X POST localhost:8082/v1/admin/servers/game-1001/tags \
     -H 'Content-Type: application/json' -d '{"code":"no_register"}'

# 玩家侧（公网 :8080）只见对外标记 → 角标数据源
curl -s localhost:8080/v1/discovery/servers/game-1001 | jq .tags
# [{"code":"hot","label":"火热","tier":"hot","public":true},
#  {"code":"no_register","label":"禁止注册","tier":"warning","public":true}]

# 创角被拒，错误明确
curl -X POST localhost:8080/v1/directory/characters -H 'Content-Type: application/json' \
     -d '{"account_id":424242,"server_id":"game-1001","character_id":90001,"name":"demo"}'
# HTTP 403 {"error":{"code":"REGISTRATION_FORBIDDEN",
#        "message":"server game-1001 does not accept new characters (禁止注册)"}}

# 摘除 → 恢复注册
curl -X DELETE localhost:8082/v1/admin/servers/game-1001/tags/no_register
# 之后同一笔创角 → HTTP 201
```

标记由管理台独占维护：游戏服重新注册/心跳**永远不会**覆盖或清掉它们。详见[概念模型 §服务器标记](/concepts#服务器标记)。

## 3. 玩家找角色 / 客服查号 {#scenario-characters}

**场景**：玩家问「我在 3 区的号叫什么来着」；客服接到申诉要先核实账号下的角色。跨服角色索引一次拉全，按名字/服务器/等级过滤。

![角色搜索 — 按角色名/服务器/等级组合过滤，跨服结果](/screenshots/characters.png)

**操作路径**

- 管理台：`Characters`——角色名 / 服务器 / 等级区间组合过滤。
- 接口：`GET /v1/admin/characters/search?q=名字&server_id=…&min_level=…`（管理口）；玩家侧自己的列表走 `GET /v1/directory/accounts/{id}/characters`（公网）。
- 角色写入的唯一入口是游戏服的写端点（可走消息总线异步缓冲、at-least-once 幂等落地），见[数据同步](/sync)。

## 4. 合服 / 转服 {#scenario-migration}

**场景**：两台服人少了要合服。迁移编排「先复制、后切换、再清理」，角色索引在事务内原子切换，可回滚。

![迁移管理 — 创建迁移、状态流转、失败回滚](/screenshots/migrations.png)

**操作路径**

- 管理台：`Migrations`——填源/目标服务器创建迁移，页面实时刷新状态，失败可一键回滚。
- 接口：`POST /v1/admin/migrations`（body 指定 `source_server_id` / `target_server_id`），`POST /v1/admin/migrations/{id}/rollback`。
- 详见[合服 / 转服 / 迁服](/migration)。

## 5. 开服与维护公告 {#scenario-ops}

**场景**：今晚 2 点停服维护——提前建维护窗口，到点自动进维护、结束自动恢复，维护公告同时段自动挂出；紧急故障则一条 API 立刻发全服公告。玩家客户端登录即见。

**操作路径**

```bash
# 计划维护窗口（到点自动进维护/恢复）
curl -X POST localhost:8082/v1/admin/maintenance-windows -H 'Content-Type: application/json' \
     -d '{"server_id":"game-1001","start_at":"2026-10-03T02:00:00+08:00","end_at":"2026-10-03T04:00:00+08:00"}'

# 全服公告
curl -X POST localhost:8082/v1/admin/announcements -H 'Content-Type: application/json' \
     -d '{"title":"今晚例行维护","body":"02:00-04:00","level":"info"}'
```

服务器侧的紧急维护/排水/禁用在 `Servers` 页每行操作按钮里（进维护、排水、禁用、启用）——[场景 2](#scenario-tags) 截图里的操作列。详见[公告与计划维护](/operations)。

## 6. 半夜掉线谁告诉我 {#scenario-alerts}

**场景**：某台服心跳断了。健康监控按 `suspect → offline` 两段式判死并触发告警，不会把一台卡顿的服务器立刻踢下线，也不会让一台真死的继续吃玩家。

**操作路径**：无需人工轮询——配置告警通道后（webhook），事件自动推送；事后在 `GET /v1/admin/audit` 审计流里回放谁在什么时候动了哪台服。详见[服务器生命周期 §健康警报](/lifecycle#_4-1-健康警报-v0-1-15-交付)。

## 7. 多区扩展 {#scenario-realms}

**场景**：华北、华南各一个大区（Realm），大区内再分片（Shard）横向扩。服务器归属在注册时声明，玩家侧按大区筛选。

**操作路径**：`Realms`/`Shards` 管理在管理口（`POST /v1/admin/realms`、`POST /v1/admin/shards`）；玩家侧 `GET /v1/discovery/servers?realm=realm-cn`。详见[Realm 与 Shard](/realms-shards)。

## 8. 跨服配置热更新 {#scenario-crossserver}

**场景**：跨服玩法要开新赛季——运营改一把配置，三种形态的服务器（轮询 / 回调 / 订阅）各自收到信号、拉取、热生效，全程不重启。有人断网、有人拉取失败，旧配置继续跑，恢复后自动补齐。

**操作路径**：管理台 `跨服配置` 页改完点"发布"，或等价接口（`atlas-crossagent` 是三种模式的完整接收端，`go run ./cmd/atlas-crossagent -h` 看参数）：

```bash
# 发布全文（PUT，替换）→ version 8 → 9
curl -X PUT localhost:8082/v1/admin/crossserver/config \
  -H 'Content-Type: application/json' -d @spec.json
# {"config":{"version":9,"hash":"3768c1708e690107"},
#  "notify":{"targets":["*","season-1"],"receivers":["*"],
#   "bus":"redis","callbacks":{"targets":1,"delivered":1,"failed":0}}}
```

真实走查实录（演示栈，`ATLAS_EVENT_ADAPTER=redis`）：

1. **启动拉取**——三台 agent 注册（`poll` / `callback` / `subscribe`），注册响应带 `config_version: 8`，启动拉取直接采用：
   `CONFIG APPLIED (hot) version=8 hash=b0c7e5cc595bf888 clusters=3 groups=2 features_on=1`
2. **运行中改配置**——发布 v9（`cross_arena` 开启 + 分组加成员），三台各自热生效：
   poll（5s 轮询）`config_version: 9`；callback 收到 `POST /notify` 即拉取；subscribe 收到总线 `config.updated` 即拉取。
3. **拉取失败旧配置续跑**——给 poll 实例开故障注入（`POST /ctl/failpull {"on":true}`）再发布 v10：
   `{"config_version":9,"pull_failing":true,"pull_failures":3}`——版本不动、进程不退；关注入后下一次轮询自动追上 v10。
4. **断连补拉**——给 subscribe 实例 `POST /ctl/bus-off`（订阅丢弃），发布 v11（只改匹配域参数，`receivers` 定向为 `["game-1001","game-9001"]`）期间它停在 v10；`POST /ctl/bus-reset` 重建订阅即补拉：`config_version: 11`。
5. **回调到达**——发布 v12（新增走查分组），Atlas 主动 `POST` 回调 URL（只带版本/信号），agent 日志：
   `callback signal received type=config.updated version=12` →
   `CONFIG APPLIED (hot) version=12 … groups=3`；发布响应 `callbacks:{targets:2, delivered:2}`。

语义与边界见[配置中心](/config-center)。

---

以上界面来自 [dashboard/](https://github.com/cuihairu/atlas/tree/main/dashboard)（React + antd）。本地跑起来：`docker compose up -d` 起 Atlas 三件套，`cd dashboard && npm run dev` 起管理台（dev 代理自动分流公网/管理口）。
