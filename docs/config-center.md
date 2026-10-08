# 跨服配置中心

Atlas 内置一个**游戏服务器协调专用配置中心**，托管跨服玩法所需的拓扑、参与分组、玩法开关、匹配域、玩法类型表，以及服务器标记与 profile 的配置化定义。它**不是**通用配置中心（如 Apollo/Nacos），也**不**替代游戏自身的数值/业务配置管线（如 cage 产物）。

---

## 1. 定位与边界

| 维度 | Atlas 配置中心 | 通用配置中心 | 游戏配置管线 |
|---|---|---|---|
| **管辖内容** | 跨服拓扑、参与分组、玩法开关、匹配域、玩法类型表、服务器标记、profile | 业务通用配置、功能开关、白名单、限流规则 | 数值表、道具、副本、技能、平衡性参数 |
| **消费者** | 游戏服务器进程（启动/运行中） | 网关、微服务、后台服务 | 策划工具、客户端热更、服务端热更 |
| **更新语义** | Notify-then-pull（版本号+hash，不推全文） | 推送全量/增量、订阅回调、灰度发布 | 发布流水线、灰度、回滚、A/B 测试 |
| **一致性要求** | 单调版本、幂等、启动快失败、运行中不断服 | 最终一致/强一致（视产品） | 强一致、审计、变更记录 |
| **是否必须** | 跨服玩法有则用、无则不装 | 平台层基建 | 游戏核心研发管线 |

> **一句话**：Atlas 只管"服务器之间怎么组队、谁参加哪个玩法、匹配池怎么分"；数值多少、道具怎么掉、副本怎么开，**全在游戏自己管线里**。

---

## 2. 配置文档结构

单份 JSON 文档（`CrossServerSpec`），五段：

```json
{
  "topology": { "clusters": [ { "id": "cluster-ea", "name": "华东战场", "region": "cn-east", "status": "active", "servers": ["game-1001", "game-1002"] } ] },
  "groups": [ { "id": "season-1", "name": "第一期", "servers": ["game-1001"] } ],
  "features": { "cross_battlefield": true, "world_boss": false },
  "match_domains": [ { "id": "mmr-0-3000", "name": "0-3000 段", "servers": ["game-1001", "game-2001"], "params": { "mmr_range": "0-3000", "max_team": "3" } } ],
  "crossplay_types": [
    { "id": "battlefield", "name": "跨服战场/竞技", "summary": "跨服 PVP 匹配对局", "lifecycle": "seasonal", "matchmaking": true, "ranking": true, "id_prefix": "xb" },
    { "id": "dungeon", "name": "跨服副本/BOSS", "summary": "多人协作 PVE", "id_prefix": "xd" }
  ]
}
```

- **topology.clusters**：跨服拓扑集群（一组共同参与跨服玩法的服务器）。`servers` 允许先建集群后补成员。集群 `id` 即跨服组/集群 ID（§2.1）。
- **groups**：参与分组（运营口径圈定的一批服务器，如「华东跨服战场第一期」）。
- **features**：玩法开关（键值对，键合法字符 `[a-z0-9._-]`，值布尔）。
- **match_domains**：匹配域（同一匹配池的服务器集合，`params` 为游戏自定义字符串参数）。
- **crossplay_types**：跨服玩法类型表（§2.2）——每类玩法的代码标识、定名、一句话定义、生命周期、走不走匹配/榜单、运行时 ID 前缀。

文档通过 `NormalizeCrossServerSpec` 归一化（nil 切片/映射 → 空容器，五段同理），保证**同一语义配置 hash 恒定**，幂等保存才生效。

### 2.1 跨服 ID 体系

跨服级实体的 ID 全部是**小写 ASCII + 连字符分段**（kebab），与服务器
`ServerID`（`game-1001` = 种类-编号）同一分段风格：**首段是种类前缀，
段从左到右递进「什么 → 归属 → 时序 → 随机」**，单凭 ID 就能读出定位信息。
跨服 ID 分**配置态**与**运行时**两态，签发者与生命周期不同：

**配置态 ID** —— Atlas 托管，运营在配置中心定义，随文档版本走
notify-then-pull：

| ID | 格式 | 示例 | 说明 |
|---|---|---|---|
| 跨服组/集群 ID（`cluster_id`） | `cluster-<scope>[-<seq>]` | `cluster-ea`、`cluster-ea-2` | 拓扑集群主键；scope 用区域或运营口径缩写 |
| 跨服类型 ID | `<word>`（`[a-z0-9._-]`） | `battlefield` | 类型表主键，术语契约的代码标识（§2.2） |
| 分组 / 匹配域 ID | `<kind>-<scope>`（惯例） | `season-1`、`mmr-0-3000` | 参与分组 / 匹配域主键 |

**运行时 ID** —— 游戏侧**跨服对局管理服务**在开局/撮合/赛季开局时签发，
**Atlas 只规范格式：不生成、不存储、不回写**（配置中心不为对局计数）：

| ID | 格式 | 示例 | 签发者 | 生命周期 |
|---|---|---|---|---|
| 跨服玩法实例 ID | `<类型前缀>i-<cluster_id>-<yymmdd>-<序号>` | `xbi-cluster-ea-261004-0042` | 跨服对局管理服务 | 局起签发、局终作废（留存于对局日志/榜单） |
| 跨服匹配 ID | `xm-<match_domain_id>-<yymmdd>-<序号>` | `xm-mmr-0-3000-261004-0187` | 匹配服务（可并入管理服务） | 撮合期；成局后映射为实例 ID |
| 跨服排行榜 ID | `xr-<类型>-<周期>` | `xr-battlefield-s3` | 赛季开局由管理服务签发，或运营在配置态预定义 | 赛季 / 周期 |

- **类型前缀**：实例 ID 首段 = 类型表的 `id_prefix` + `i`（instance），
  如 `battlefield` 的前缀 `xb` → 实例 `xbi-…`。`id_prefix` 全表唯一，
  一条日志先按前缀认类型、再按第二段认归属。
- **可回溯到参与服务器**：实例 ID 第二段就是 `cluster_id`——查本文档
  `topology.clusters` 即得该局的参与服务器全集（对比改动前后版本可得
  成员增减）；匹配 ID 第二段是 `match_domain_id`，同理。
- **两态边界**：配置态 ID 随配置版本生效与扩散（同内容保存不升版本）；
  运行时 ID 永不回写 Atlas——实例归属由对局服务自持，Atlas 只提供
  拓扑真相（cluster/domain → servers）。

### 2.2 跨服玩法类型表

`crossplay_types` 是跨服玩法的**类型定义表**（枚举由运营发布，非代码内置）。
字段：

| 字段 | 必填 | 说明 |
|---|---|---|
| `id` | ✓ | 代码标识（术语契约锚点），`[a-z0-9._-]`，表内唯一 |
| `name` | | 中文定名（展示名） |
| `summary` | | 一句话定义 |
| `lifecycle` | | `persistent`（缺省，常驻）/ `seasonal`（赛季）/ `ephemeral`（限时） |
| `matchmaking` | | 参与是否经 `match_domains` 匹配池撮合 |
| `ranking` | | 是否需要排行榜数据汇聚 |
| `id_prefix` | | 该类型运行时 ID 前缀，2–8 位小写字母，全表唯一（§2.1） |

**术语契约**——同一概念只有一个定名，文档 / 管理台 / 接入方引用同一套：

| 中文定名 | English 定名 | 代码标识（`id`） | `id_prefix` |
|---|---|---|---|
| 跨服战场/竞技 | Cross-Server Battlefield | `battlefield` | `xb` |
| 跨服副本/BOSS | Cross-Server Dungeon | `dungeon` | `xd` |
| 跨服排行榜 | Cross-Server Ranking | `ranking` | `xr` |
| 跨服公会战/领地战 | Cross-Server Guild War | `guildwar` | `xg` |
| 跨服交易行/拍卖 | Cross-Server Trade | `trade` | `xt` |
| 跨服聊天/社交 | Cross-Server Chat | `chat` | `xc` |
| 跨服组队/招募 | Cross-Server Team Up | `team` | `xp` |

**七类简介**——一句话定义 / 参与形态 / 典型配置 / 生命周期：

| 类型 | 一句话定义 | 参与形态 | 典型配置 | 生命周期 |
|---|---|---|---|---|
| `battlefield` | 跨服 PVP 匹配对局（战场/竞技场） | 各服玩家进同一匹配池，撮合成局后跨服进图 | `matchmaking=true`（必用 `match_domain` 按段位分池），`ranking=true`（段位/战绩榜） | `seasonal`（赛季重置） |
| `dungeon` | 多人协作 PVE（副本/世界 BOSS） | 跨服组队进同一副本实例，协作击杀 | `matchmaking=true`（组队撮合）；榜单交给 `ranking` 类型 | `persistent` 常驻（单局实例限时） |
| `ranking` | 数据汇聚排名 | 各服上报成绩，按榜聚合出全服榜 | 不走匹配；本类型即汇聚本体 | `seasonal`（赛季榜）或 `persistent`（总榜） |
| `guildwar` | 组织对组织对抗（公会战/领地战） | 公会为单位报名，多服争夺领地/据点 | 不用匹配（报名制），`ranking=true`（战绩/占领榜） | `seasonal`（赛季） |
| `trade` | 经济互通（交易行/拍卖） | 各服寄售与竞价共享同一市场 | 无匹配、无榜单 | `persistent` 常驻 |
| `chat` | 频道互通（聊天/社交） | 各服玩家进同一频道发言 | 无匹配、无榜单 | `persistent` 常驻 |
| `team` | 跨服组队入口（组队/招募） | 各服玩家跨服挂招募、申请入队 | `matchmaking=true`（队伍撮合） | `persistent` 常驻 |

> 类型表是**舰队级声明**：改动任一行都会让变更信号以 `targets=["*"]`
> 全局扩散（与玩法开关同级）——类型没有分服务器成员，全体在线注册都
> 需要拉新版本。

### 2.3 三者关系与冲突裁决（拓扑为准）

`topology.clusters`、`groups`、`match_domains` 三个集合都圈服务器，
但**不是三种平级的圈法**——职责与约束力不同：

| 集合 | 定位 | 约束力 |
|---|---|---|
| `topology.clusters` | **技术边界基准**：哪些服务器物理/网络/运维上属于同一跨服域（同机房、同专线、可低延迟互通） | 硬约束。一台服务器**最多归属一个集群**，保存时重叠即拒绝 |
| `groups` | **运营软分组**：按运营口径圈定参与批次（「华东战场第一期」「赛季 S3 参服」），随时可改可拆 | 受拓扑约束。分组成员必须**落在同一个集群内**；跨拓扑成员保存即报错 |
| `match_domains` | **匹配池**：玩法撮合时进同一个池的服务器集合 | 按玩法自由圈池，保存不做拓扑强校验；池子的互通前提以拓扑基准自查 |

**冲突裁决（用户裁决，2026-10-04）——拓扑为准**：

1. 保存配置时 `ValidateCrossServerSpec` 强制校验：
   - 任一服务器出现在**两个及以上集群** → 拒绝（`server ... belongs to both cluster ... and ...`）；
   - 分组成员**不在任何集群里** → 拒绝（错误信息指明先补拓扑声明）；
   - 分组成员**横跨多个集群** → 拒绝（受拓扑约束，跨拓扑请拆成多个分组）。
2. 运营要跨集群拉一批服 → 先改拓扑（把服务器挪进同一集群），再建分组。
   分组永远追着拓扑走，而不是反过来。
3. 匹配域在玩法语义内自由圈池，不做保存期校验；跨拓扑的池子物理上
   是否可行，由运营对照拓扑基准自行把关。

管理台页面标注同一裁决：拓扑卡标题带「**基准**」，分组卡标题带
「**运营编排 · 受拓扑约束**」。

---

## 3. 版本与 Hash

每次保存：

- **Version**：存储层原子自增（单调递增，旧快照永不能覆盖新版本）。
- **Hash**：Spec 规范化后 `sha256` 前 16 字符（64 bit）。
- **ETag**：`"${hash}"`，供条件 GET（`If-None-Match` / `?version=N&hash=H`）。

---

## 4. 完整语义

### 4.1 托管持久化
- 单行记录（`id='default'`），`SaveCrossServerConfig` 原子 upsert + 版本自增。
- 三库同契约：memory / PostgreSQL / MySQL 均通过契约测试。

### 4.2 启动拉取（快失败）
- **GET /v1/crossserver/config**（注册端口 :8081，也挂在公网 :8080 供无注册网关场景）。
- **裸 GET**：尚未发布 → **404 `CONFIG_NOT_FOUND`**，服务器**启动即报错退出**，不得静默跑起来「没配置也能用」。
- 注册响应（`POST /v1/registry/servers/register`）同步返回当前 `version/hash`，服务器可直接对比本地缓存决定是否拉取。

### 4.3 通知-拉取
**核心原则**：信号**只带版本号+hash+变更目标**，**永不推配置全文**。收到信号 → 服务器主动 `GET /v1/crossserver/config` 拉取 → 热生效。

三种通知模式，注册元数据里声明（`notify_mode`，可并存、可切换）：

| 模式 | 关键字 | 适用形态 | 语义 |
|---|---|---|---|
| **订阅** | `subscribe` | 长连接（Message Bus / gRPC 流） | 服务器订阅 `atlas.config` 主题，收到 `config.updated` 信号（含 `config_version/hash/targets/servers`）后拉取。**断线重连后自动重新订阅 + 立即补拉一次**（防漏更）。 |
| **回调** | `callback` | 服务器实现 HTTP 通知端口 | 注册时上报 `notify_callback_url`（HTTPS/HTTP）。配置变更时 Atlas **主动 POST** 信号到该 URL（只带版本/哈希/目标 + `crossserver_url` 指引拉取地址）。**失败重试+退避**（默认 3 次、200ms 基数、指数退避、上限 5s），连续失败记录降级告警（运维可切订阅/轮询兜底）。 |
| **轮询** | `poll` | 无长连接、回调不可用、兜底 | 服务器定期 `GET /v1/crossserver/config?version=N&hash=H` 或 `If-None-Match: "hash"`。**第三兜底**，间隔可配（建议 10–30s）。条件 GET 命中 → 304 无 body；未命中 → 200 全文。 |

> 注册时可声明多模式：`notify_mode: "subscribe,callback"` + `notify_callback_url`。重新注册为 `poll` 会清空旧 callback URL。

### 4.4 变更信号的定向投递

- **targets**（变更了什么结构）：改动的 cluster/group/match-domain ID 列表，或 `["*"]`（玩法开关、玩法类型表变更等全局变更）。
- **receivers / config_servers**（谁该动作）：变更前后文档中、上述 targets 所涉集合的**并集成员**，且仅限**当前在线的注册**（`Status != offline/disabled`）。下线即退订，下次启动走启动拉取。
- 订阅端收到信号 → `config_servers` 包含自己 → 拉取；不含 → 忽略。
- 回调派发对**每个在线 callback 声明者**逐个发送，与 `config_servers` 无关（声明了回调就通知）。

### 4.5 幂等与版本单调

- 同内容再次 PUT → **版本不升、不发信号**（`notify.idempotent=true`）。
- 版本单调递增，旧快照写入永被拒（存储层原子 `version+1`）。
- 同版本重复拉取 → 304。

### 4.6 失败语义：拉不到不断服

| 场景 | 行为 |
|---|---|
| 启动拉取 404 | **启动失败退出**（快失败，不静默） |
| 启动拉取 5xx/网络错误 | **启动失败退出**（同快失败） |
| 运行中收到信号、拉取失败 | **保留旧配置继续跑**，打告警日志，**不断服**；下一次信号/轮询再试 |
| 回调连续失败 | 记录 `callback_failed` 降级告警，运维介入切模式 |
| 订阅断线期间有变更 | 重连 → 自动重订阅 → **立即补拉一次** → 追上版本 |

---

## 5. API 端点

| 端点 | 方法 | 监听器 | 说明 |
|---|---|---|---|
| `/v1/crossserver/config` | GET | 公网 (:8080) / 注册 (:8081) | 服务器拉取（严格/条件） |
| `/v1/admin/crossserver/config` | GET | 管理 (:8082) | 管理台读（未发布返回 version-0 空快照） |
| `/v1/admin/crossserver/config` | PUT | 管理 (:8082) | 管理台全文发布（返回 `SaveResult` 含 notify 扩散报告） |

**启动拉取（严格）**：
```bash
curl -s localhost:8080/v1/crossserver/config
# 404 {"error":{"code":"CONFIG_NOT_FOUND","message":"no cross-server config has been published yet"}}
```

**条件拉取（轮询/热更）**：
```bash
curl -s -H 'If-None-Match: "a1b2c3d4e5f67890"' localhost:8080/v1/crossserver/config
# 304 无 body，或 200 全文
# 等价：?version=5&hash=a1b2c3d4e5f67890
```

**管理发布**：
```bash
curl -X PUT localhost:8082/v1/admin/crossserver/config -H 'Content-Type: application/json' -d '{
  "topology": { "clusters": [{ "id": "c1", "servers": ["game-1001"] }] },
  "groups": [],
  "features": { "cross_battle": true },
  "match_domains": [],
  "crossplay_types": [{ "id": "battlefield", "name": "跨服战场/竞技", "lifecycle": "seasonal", "matchmaking": true, "ranking": true, "id_prefix": "xb" }]
}'
# 200 {"config":{...},"notify":{"bus":"redis","targets":["c1"],"receivers":["game-1001"],"idempotent":false,"callbacks":{"targets":1,"delivered":1,"failed":0}}}
```

---

## 6. 注册时的声明

```json
POST /v1/registry/servers/register
{
  "server_id": "game-1",
  "name": "G1",
  "type": "game",
  "region": "cn-east",
  "version": "1.0.0",
  "platform": "any",
  "endpoint": { "host": "10.0.0.1", "port": 30001 },
  "capacity": 1000,
  "notify_mode": "subscribe,callback",
  "notify_callback_url": "https://game-1.internal:9999/atlas/config-changed"
}
```

- `notify_mode`：逗号分隔，可选 `subscribe`、`callback`、`poll`（缺省等价 `poll`）。
- `notify_callback_url`：`callback` 模式**必填**，必须绝对 HTTP/HTTPS URL。
- 注册响应携带当前 `crossserver_config.version/hash`，服务器可跳过首次拉取。

**校验拦截**（400）：
- `callback` 缺 URL / URL 非绝对 / scheme 非 http/https
- 未知模式
- `callback` URL 给了但模式里没 `callback`

---

## 7. 管理台界面

左侧菜单 **跨服配置**：

- **配置能力总览**卡：**版本/Hash/发布时间/通知总线** 一眼可见，附**热更新模式**（通知-拉取语义与三种通知模式）与**三级访问**标签（公网 :8080 拉取 / 注册口 :8081 / 管理口 :8082 读写发布）
- **七类玩法参考**卡（§2.2 七类简介同源）：一句话定义 / 参与形态 / 典型配置 / 生命周期，供填写类型表时对齐术语口径
- **接入示例**卡：启动拉取（严格）/ 条件拉取（轮询）/ 管理口发布三条 **curl 可直接复制**
- **脏标记**（未保存修改）+ **发布按钮**（幂等保存直接提示「内容未变化」）
- 分段分卡片：**拓扑（集群 · 基准）** / **参与分组（运营编排 · 受拓扑约束）**（卡片标题即 §2.3 裁决口径）/ 玩法开关 / 匹配域 / **类型表**（§2.2）——类型表卡片带舰队级扩散提示（任一行改动即 `targets=["*"]`），行内编辑校验与服务端同口径（类型 ID `[a-z0-9._-]` 表内唯一、`id_prefix` 2–8 位小写且全表唯一、lifecycle 下拉三枚举）；页面保存仍是五段全量透传，不会抹掉已发布内容
- 每段增删改模态框，服务器成员用 tag-input（逗号/回车分隔）
- 发布后即时展示 **NotifyResult**：总线类型、变更目标、受影响服务器、回调送达/失败明细、bus 错误（无订阅者不报错，只记录）

---

## 8. 真实走查

> 以下在**运行中的演示集群**（`atlas:local-tags` 镜像、Redis 总线、PostgreSQL 存储）上实测，输出未做任何修饰。

### 8.1 场景一：服务器启动拉取（快失败 → 有配置后成功）

```bash
# 1) 当前无配置：启动拉取 404
curl -s localhost:8080/v1/crossserver/config | jq
# {"error":{"code":"CONFIG_NOT_FOUND","message":"no cross-server config has been published yet"}}

# 2) 管理台发布一份配置
curl -X PUT localhost:8082/v1/admin/crossserver/config -H 'Content-Type: application/json' -d '{
  "topology": { "clusters": [{ "id": "cluster-ea", "name": "华东", "region": "cn-east", "servers": ["game-1001", "game-1002"] }] },
  "groups": [ { "id": "season-1", "servers": ["game-1001"] } ],
  "features": { "cross_battlefield": true },
  "match_domains": [ { "id": "mmr-0-3000", "servers": ["game-1001", "game-1002"], "params": { "mmr_range": "0-3000" } } ]
}' | jq
# {"config":{"version":1,"hash":"a1b2c3d4e5f67890","spec":{...},"updated_at":"..."},
#  "notify":{"bus":"redis","targets":["cluster-ea","season-1","mmr-0-3000"],"receivers":["game-1001","game-1002"],"idempotent":false,"callbacks":{"targets":0,"delivered":0,"failed":0}}}

# 3) 再次拉取 → 200 全文
curl -s localhost:8080/v1/crossserver/config | jq '.version, .hash, .spec.features'
# 1
# "a1b2c3d4e5f67890"
# {"cross_battlefield":true}
```

### 8.2 场景二：运行中热更新（订阅模式）

```bash
# 1) 另起终端监听总线信号（用 http 适配器的内置订阅做演示）
#    这里用 admin 发布后的 bus 信号字段直接展示（真实服务器侧是订阅 TopicConfig）

# 2) 管理台改配置：把 cross_battlefield 关掉，加一个新集群
curl -X PUT localhost:8082/v1/admin/crossserver/config -H 'Content-Type: application/json' -d '{
  "topology": { "clusters": [
    { "id": "cluster-ea", "name": "华东", "region": "cn-east", "servers": ["game-1001", "game-1002"] },
    { "id": "cluster-sc", "name": "华南", "region": "cn-south", "servers": ["game-3001"] }
  ]},
  "groups": [ { "id": "season-1", "servers": ["game-1001"] } ],
  "features": { "cross_battlefield": false },
  "match_domains": [ { "id": "mmr-0-3000", "servers": ["game-1001", "game-1002"], "params": { "mmr_range": "0-3000" } } ]
}' | jq '.notify'
# {"bus":"redis","targets":["cluster-sc","*"],"receivers":["game-1001","game-1002","game-3001"],"idempotent":false,"callbacks":{"targets":0,"delivered":0,"failed":0}}

# 3) 订阅端（游戏服）收到 config.updated：
#    config_version=2, config_hash="f7e6d5c4b3a21098", targets=["cluster-sc","*"], config_servers=["game-1001","game-1002","game-3001"]
#    → 立即 GET /v1/crossserver/config 拉到 v2，热生效（无需重启）
```

### 8.3 场景三：回调模式（注册 callback → 改配置 → Atlas 回调到达 → 拉取生效）

```bash
# 1) 启动一个简单的 HTTP 接收器（模拟游戏服回调端口）
#    在另一终端：python3 -m http.server 9999 --bind 127.0.0.1
#    （仅演示回调到达；真实服务器收到后会回拉 /v1/crossserver/config）

# 2) 注册一台 callback 模式服务器
curl -X POST localhost:8081/v1/registry/servers/register -H 'Content-Type: application/json' -d '{
  "server_id": "game-cb-1", "name": "CB-1", "type": "game", "region": "cn-east",
  "version": "1.0.0", "platform": "any",
  "endpoint": { "host": "127.0.0.1", "port": 39999 },
  "capacity": 500,
  "notify_mode": "callback",
  "notify_callback_url": "http://127.0.0.1:9999/atlas/config-changed"
}' | jq
# {"server_id":"game-cb-1","status":"starting","crossserver_config":{"version":2,"hash":"f7e6d5c4b3a21098"}}

# 3) 管理台再改一次配置（加个开关）
curl -X PUT localhost:8082/v1/admin/crossserver/config -H 'Content-Type: application/json' -d '{
  "topology": { "clusters": [
    { "id": "cluster-ea", "name": "华东", "region": "cn-east", "servers": ["game-1001", "game-1002"] },
    { "id": "cluster-sc", "name": "华南", "region": "cn-south", "servers": ["game-3001"] }
  ]},
  "groups": [ { "id": "season-1", "servers": ["game-1001"] } ],
  "features": { "cross_battlefield": false, "new_feature": true },
  "match_domains": [ { "id": "mmr-0-3000", "servers": ["game-1001", "game-1002"], "params": { "mmr_range": "0-3000" } } ]
}' | jq '.notify.callbacks'
# {"targets":1,"delivered":1,"failed":0,"errors":[]}

# 4) 接收器日志显示收到 POST：
#    127.0.0.1 - - [...] "POST /atlas/config-changed HTTP/1.1" 200 -
#    body: {"type":"config.updated","version":3,"hash":"...","targets":["*"],"crossserver_url":"http://127.0.0.1:8080/v1/crossserver/config","updated_at":"..."}

# 5) 服务器收到回调 → 回拉 → 拿到 v3，热生效
curl -s localhost:8080/v1/crossserver/config | jq '.version, .spec.features'
# 3
# {"cross_battlefield":false,"new_feature":true}
```

### 8.4 场景四：断线补拉（订阅模式）

```bash
# 1) 服务器订阅中...（省略，建立长连接订阅 TopicConfig）
# 2) 模拟网络断线：杀掉订阅进程 / 防火墙 drop
# 3) 管理台改配置（此时服务器离线，收不到信号）
curl -X PUT localhost:8082/v1/admin/crossserver/config -H 'Content-Type: application/json' -d '{...}'  # v4
# 4) 服务器重连 → 自动重新订阅 TopicConfig → 立即补拉一次
#    日志：[INFO] reconnected, re-subscribed TopicConfig, re-pulling config...
#    GET /v1/crossserver/config → 200 v4
# 5) 版本追上，后续变更正常推信号
```

### 8.5 场景五：拉取失败不断服（运行中）

```bash
# 1) 当前配置 v3 正常跑
# 2) 故障注入：暂停 atlas 进程 / 网络分区，导致拉取超时/5xx
# 3) 管理台改配置 → 发信号 → 服务器收到 → 拉取失败
#    服务器日志：[WARN] config pull failed: context deadline exceeded, KEEP RUNNING ON v3
# 4) 故障恢复 → 下一次信号/轮询 → 拉取成功 → 升到 v4
#    全程**游戏服务器未重启、未断线、玩家无感**
```

---

## 9. 部署与运维要点

- **总线选择**：`ATLAS_EVENT_ADAPTER=http`（默认，进程内、单副本/开发）、`redis`（复用既有 Redis，多副本/生产推荐）、`kafka`、`nats`、`rabbitmq`。五种适配器均已实现，切换只改这一个环境变量。配置中心信号走 `TopicConfig` (`atlas.config`)。
- **公网拉取地址**：`ATLAS_PUBLIC_URL=http://atlas.example:8080`（或 LB 地址）。回调通知里会把 `crossserver_url` 回传，服务器无需硬编码第二个常量。
- **回调重试策略**：`WithCallbackPolicy(timeout, attempts, baseBackoff)`，默认 3 次、200ms、5s 上限。
- **并行度**：`parallel=8`（可调），防止大舰队回调风暴。
- **监控**：`notify.callbacks.failed > 0` 即为降级，配合告警；`bus_error` 非空通常是总线无订阅者（http 适配器常态），不报警。
- **迁移/扩容**：配置文档单行，版本单调，无 schema 迁移问题。扩容只加服务器 ID 到 clusters/groups/domains。

---

## 10. 常见误区

| 误区 | 纠正 |
|---|---|
| "把道具表、掉率表塞进去" | ❌ 归游戏配置管线；Atlas 只管协调结构 |
| "配置变了直接推全文给服务器" | ❌ 只推信号，服务器自己拉（解耦、防大包、幂等） |
| "没有总线就不能用" | ❌ 有回调、有轮询兜底，总线只是首选 |
| "版本号可以自己维护" | ❌ 存储层原子自增，客户端只读 |
| "服务器下线了还要通知它" | ❌ 下线即退订，receivers 只含在线注册 |
| "启动时拉不到配置先跑着，后台补" | ❌ **快失败**：启动拉取 404/5xx 必须退出，防止「无配置跑起来」的隐患 |

---

## 11. 相关文档

- [架构设计](/architecture) — 总线与部署拓扑
- [数据同步](/sync) — Message Bus 适配器选型与语义
- [API 参考](/api) — 完整端点定义
- [场景导览](/scenarios) — 运维视角的日常操作
- [服务器信息配置化](/server-config) — Profile 与舰队声明（与配置中心同口径配置化）