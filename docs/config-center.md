# 配置中心（跨服协调配置）

Atlas 内置一个**面向游戏服务器协调的专用配置中心**：托管跨服玩法需要的协调配置（跨服拓扑、参与分组、玩法开关、匹配域）， game server 启动时拉取、运行中热更新，全程不需要重启进程。

它不管游戏数值、不管业务配置——只管"哪些服一起玩、按什么规则组局、开关在哪"这类**服与服之间的协调事实**。数值与业务配置归游戏自己的配置管线（见 §8 边界对照表）。

---

## 1. 托管内容

一份配置文档（Spec）由四段组成，管理台可配、持久化存储：

| 段 | 结构 | 说明 |
| --- | --- | --- |
| `topology`（跨服拓扑） | `clusters[]`：`{id, name, region, status, servers[]}` | 一组共同参与跨服玩法的服务器；`status` 为 `active`（缺省）/`disabled`；允许先建集群、后补成员 |
| `groups`（参与分组） | `{id, name, servers[]}` | 按运营口径圈定的服务器集合（如"华东跨服战场第一期"），玩法开关与匹配域可引用 |
| `features`（玩法开关） | `{key: bool}` | 跨服玩法总闸；key 形如 `[a-z0-9._-]{1,64}`，必须以字母或数字开头 |
| `match_domains`（匹配域） | `{id, name, servers[], params}` | 同一匹配池的服务器集合；`params` 为游戏自定义字符串参数（如 `mmr_range`、`max_team`） |

校验规则（`model.ValidateCrossServerSpec`）：各类 `id` 必填且唯一；集群内 `servers` 不允许空串与重复；玩法开关 key 非法直接 400。**不校验引用的服务器是否存在**——拓扑可以先于服务器注册存在（规划期先建组、开服后补成员）。

服务器标记（火热/爆满/禁止注册等）与 profile 是同一管理口径下的并列配置能力，不在本文档内：标记见[概念模型 §服务器标记](concepts.md#服务器标记)，静态舰队声明见[服务器信息配置化](server-config.md)。

---

## 2. 版本与持久化

每次保存是一次**全文替换**（`PUT /v1/admin/crossserver/config`，管理口 :8082），存储层原子自增版本号：

- `version`：从 1 开始**单调递增**，由存储层拥有——调用方给不出版本号，旧写盖新写在结构上不可能。
- `hash`：Spec 规范化 JSON 的 sha256 前 16 十六进制字符（64 bit 指纹）；nil 与空容器归一化后再算，保证同一份内容永远是同一个 hash。
- **幂等保存**：内容与已存一致时不升版本、不发信号，响应里 `notify.idempotent: true` 直接可见。
- 三库一致：PostgreSQL / MySQL（`migrations/0006_crossserver_config*.sql`）与内存实现走同一套 `storetest` 契约（保存→自增→幂等→快照一致）。

---

## 3. 启动拉取（快失败）

服务器启动时调注册域的拉取端点（内网 :8081）：

```bash
curl http://127.0.0.1:8081/v1/crossserver/config
# 未发布过：404 {"error":{"code":"CONFIG_NOT_FOUND", ...}}
# 已发布：  200 {"version":3,"hash":"9f2c…","spec":{…},"updated_at":"…"} + ETag 头
```

语义是**严格拉取**：没发布过就 404 `CONFIG_NOT_FOUND`，调用方必须当启动失败处理（ loud exit），不能把"没拉到"误当成"没配置"照常开服。Go SDK 的 `FetchCrossServerConfig` 把 404 转成 `ErrConfigNotPublished`，`ConfigWatcher.Run` 在启动窗口内重试（默认 3 次、退避）仍失败则返回错误——由游戏服决定是否带病启动。

注册响应里顺带返回当前生效版本（`crossserver_config: {version, hash}`），刚注册的服务器就知道自己该从哪个版本收敛，版本一致时可跳过一次无意义的拉取。

---

## 4. 通知-拉取（不推全文）

配置变更后，Atlas 只广播**变更信号**，不推配置全文。信号里只有版本号、hash、变更目标（`targets`：变的集群/分组/匹配域 id，或 `*`）与受影响服务器（`receivers`，见下）。服务器收到信号后**主动调拉取端点取全文**，再热生效。

```text
管理台 PUT 新文档 ──▶ version+1 ──▶ 信号(config.updated, version/hash/targets)
                                           ├──▶ Message Bus 主题 atlas.config（订阅模式）
                                           └──▶ POST 服务器回调 URL（回调模式）
信号到达 ──▶ 服务器 GET /v1/crossserver/config ──▶ 版本更高才生效（旧/同版本丢弃）
```

总线事件类型为 `config.updated`，主题为 `atlas.config`。回调请求体同样只有信号字段（`type/version/hash/targets/crossserver_url/updated_at`，`crossserver_url` 由 `ATLAS_PUBLIC_URL` 拼出，告诉接收方去哪拉）——两种通道都不载正文，这是硬约束（单测显式断言信号体无 `spec` 字段）。

**定向投递**：`receivers` 是变更前后两份文档中受影响集群/分组/匹配域的成员并集，再过滤掉已下线/已禁用的注册。刚被移出分组的服仍会被点名——它要把玩法拆掉；刚加入的同样被点名——它要把玩法接上。玩法开关变化是全局变更，`receivers` 为 `["*"]`。回调投递不按 `receivers` 过滤——每个声明了 `callback` 且在线的服务器都会被调。

---

## 5. 三档通知模式

注册时用 `notify_mode` 声明（`subscribe` / `callback` / `poll`，可逗号并存如 `subscribe,callback`；重注册直接替换声明，切到 `poll` 会清空旧回调 URL）。`callback` 必须同时上报绝对 URL（`notify_callback_url`，http/https）；三种之外的取值、或有 URL 无模式，注册直接 400。

| 模式 | 信号怎么到 | 适用形态 | 兜底 |
| --- | --- | --- | --- |
| `subscribe`（订阅，长连接） | 服务器订阅 `atlas.config` 主题（Message Bus/Redis Streams 消费组），收到信号→拉取 | 常驻机房、有总线可用的舰队（推荐） | 订阅不可用时退回轮询 |
| `callback`（回调） | Atlas `POST` 注册时上报的 URL（只带版本/信号），服务器收到后回拉 | 有公网可达 HTTP 端点、想被动接收的小规模服 | 回调连续失败→降级告警，切订阅或轮询 |
| `poll`（轮询） | 服务器按可配间隔自查 `GET /v1/crossserver/config?version=N` 或 `If-None-Match` | 无长连接、函数式/边缘服；也是前两者的最终兜底 | ——（它本身就是兜底） |

各模式的可靠性语义：

- **断线重连补拉**：订阅重建后立即补拉一次（`atlas-crossagent` 的 `/ctl/bus-reset` 即该语义），断线窗口内落地的更新不会丢。心跳恢复成功后同样触发一次补拉——心跳失败期间服务器会被标 offline 并停止接收信号（**下线即退订**：`offline`/`disabled` 不再出现在 `receivers` 里，也收不到回调），恢复注册即补齐。
- **回调失败重试+退避**：单次变更对每个回调目标最多 3 次（可配）、指数退避；全部失败不回滚已持久化的配置，只在响应 `notify.callbacks` 里报告 `targets/delivered/failed/errors` 并记降级日志，运维据此切通道。
- **拉取失败不换配置**：运行中拉取失败（网络断、中心不可用）只记失败计数、退避重试，游戏继续跑在旧配置上并告警；**不断服**。`ConfigWatcher.Pull` 的三条版本守卫：首次拉取直接采用；同版本幂等忽略；旧版本拒绝（防乱序响应回滚）。
- **订阅生命周期与注册绑定**：注销（unregister）即退订；下次启动重新走"注册→启动拉取→订阅"。

参考实现：`sdk/go/atlas/config.go`（`FetchCrossServerConfig` + `ConfigWatcher`：启动快失败、信号合并、轮询、退避、版本守卫）与 `cmd/atlas-crossagent`（三种模式的完整接收端，含 `/ctl/failpull`、`/ctl/bus-off`、`/ctl/bus-reset`、`/ctl/status` 故障注入端点，走查即用它跑）。

轮询是便宜的：条件拉取（`?version=N&hash=H` 或 `If-None-Match: "<hash>"`）在无变化时回 304 空体；且条件拉取永不 404——没发布时回 200 的 version-0 空快照，运行中的服务器不用为"还没配过"写特殊分支。

---

## 6. 管理台操作

管理台 `跨服配置` 页（`/crossserver`）：集群/分组/匹配域三张表增删改、玩法开关拨动新增，右上"发布"即全文保存。新版本号、hash 与上次通知结果（总线、变更目标、回调送达 `delivered/targets`、失败则告警样式）直接显示在页头；内容未变时发布会明确提示"无变化、版本未动"（幂等）。

等价的接口操作：

```bash
# 查看当前（含版本）
curl -s http://localhost:8082/v1/admin/crossserver/config | jq '{version, hash}'

# 发布全文（PUT，替换）
curl -X PUT http://localhost:8082/v1/admin/crossserver/config \
  -H 'Content-Type: application/json' -d '{
    "topology": {"clusters": [{"id":"cluster-ea","name":"华东战场","region":"cn-east",
      "servers":["game-1001","game-1002"]}]},
    "groups": [{"id":"season-1","name":"第一期","servers":["game-1001"]}],
    "features": {"cross_battlefield": true},
    "match_domains": []
  }' | jq '{version: .config.version, idempotent: .notify.idempotent,
             targets: .notify.targets, callbacks: .notify.callbacks}'
```

---

## 7. 真实走查

以下为本地演示栈实录（Atlas compose + `atlas-crossagent` 三台，分别跑 `poll` / `callback` / `subscribe` 模式）。管理台改配置 → 启动拉取 → 运行中改配置 → 热更新 → 拉取失败旧配置续跑 → 断连补拉 → 回调到达，全链路输出见 [场景导览 §8](scenarios.md#scenario-crossserver)。

---

## 8. 边界对照表

| 归属 | 管什么 | 举例 | 不管什么 |
| --- | --- | --- | --- |
| **Atlas 配置中心**（本页） | 游戏服务器**之间**的协调事实：跨服拓扑、参与分组、玩法开关、匹配域；以及同口径的服务器标记、profile | 哪些服打跨服战场、匹配池划分、跨服开关总闸 | 数值、掉落、任务、文案等一切游戏业务配置 |
| **通用配置中心**（Apollo / Nacos 等） | 应用通用配置：开关、阈值、连接串、限流配额 | 功能灰度、第三方 endpoint、日志级别 | 游戏服拓扑与跨服协调语义（它不知道 server_id/集群/匹配域是什么） |
| **游戏配置管线**（cage 等产物管线） | 游戏业务配置的构建与分发：数值表、关卡、活动排期 | 装备属性、副本掉落、赛季奖励 | 服务器注册发现、在线状态、跨服分组 |

一句话：Atlas 只回答"**谁和谁一起玩**"；"**玩什么、掉什么**"归游戏管线；"**应用怎么配**"归通用配置中心。三者不重叠、不互相替代。

---

## 9. 端点速查

| 方法 | 路径 | 域 | 说明 |
| --- | --- | --- | --- |
| `GET` | `/v1/crossserver/config` | 内网 :8081 | 启动拉取（严格：未发布 404 `CONFIG_NOT_FOUND`）；`?version=N&hash=H` / `If-None-Match` 条件拉取（304，无变化） |
| `GET` | `/v1/admin/crossserver/config` | 管理 :8082 | 管理侧读取（未发布回 version-0 空快照；管理台用它） |
| `PUT` | `/v1/admin/crossserver/config` | 管理 :8082 | 发布全文；响应含 `config{version,hash}` + `notify{bus,targets,receivers,idempotent,callbacks}` |
| `POST` | `/v1/registry/servers/register` | 内网 :8081 | 注册字段 `notify_mode` / `notify_callback_url`；响应带 `crossserver_config{version,hash}` |

环境变量：`ATLAS_PUBLIC_URL`（回调信号里 `crossserver_url` 的基址，缺省为空）；`ATLAS_EVENT_ADAPTER`（订阅模式的总线选型，见[数据同步](sync.md)）。错误码完整定义见 [API 参考](api.md)。

---

## 10. 相关文档

- [场景导览 §8](scenarios.md#scenario-crossserver) — 跨服配置走查实录
- [数据同步](sync.md) — Message Bus 五种传输与选型
- [概念模型 §服务器标记](concepts.md#服务器标记) — 标记体系（并列配置能力）
- [服务器信息配置化](server-config.md) — profile 静态舰队声明
- [API 参考](api.md) — 端点与错误码完整定义
