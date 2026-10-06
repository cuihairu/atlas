# TODO

> 每一项都是一个可独立提交的原子任务：完成后打勾并注明 commit，测试 / 门禁
> 全绿才提交推送（fetch + rebase origin/main，禁 tag / release / force push）。
>
> 当前批次：待队列下一批。文档-实现对账 sweep（6b9466c）与巡检派工五增量
> （storetest 契约补全 c5de2bf、gRPC 鉴权 905bc9b、gRPC 审计 a127bef、
> gRPC 限流 c07de18、gRPC TLS c7c9947）已完成（2026-10-05）——四层防护
> 双传输对齐；中断期积压已全部推送（至 60f03e5，含幻影项销账与口径修正）；
> 巡检点火三项（立档两项 / dead CORS / 决策面 N+1 收尾）全部清零；
> 「其余 SDK gRPC TLS」查实为幻影项已销（五语言 SDK 均 REST-only）；等下一批指令。
>
> 前批「覆盖率回补（巡检补令）」已完成，见「覆盖率回补（2026-10-04）」段。
>
> 版本策略：0.1.x 逐个推进；v1.0 需先冻结 API 契约（候选，见 roadmap）。

---

## 工程落地清单 · Discovery 读路径管线化（P1，打头）

> 立档位置：docs/performance.md §2「已知边界」；目标把 Redis 往返从 N 次
> （1,000 台 ≈ 300ms）降到常数 1–2 次，改动限定 `internal/store/redisstore`
> 与 discovery 读路径。完成后同步刷新 benchmarks.md / performance.md 数字。

- [x] 接口与实现：`RuntimeStore` 新增批量读 `GetRuntimes(ctx, ids)`；
      memory 单次锁内读全（缺失即缺席）、Redis pipeline 一次 Exec
      （单节点 1 RTT，Cluster 按 slot 分批）；`GetRuntime` 单点语义不变
      （commit 9f99745）
- [x] 调用方：`discovery.ListServers` 列表路径改批量合并；
      `GetServer` 详情路径维持单点读（commit 8201f57）
- [x] 契约测试：storetest `RunRuntime` 扩展批量语义
      （部分缺失 → 缺失键缺席、其余值正确；空 id 列表无副作用）
      + discovery 批量读契约与后端故障降级两条测试（commit 9f99745 / 8201f57）
- [x] 基准与文档：复测确认服务层分配仅 +5 allocs/千台（批量读的 ids
      切片与结果 map 容器，回归以分配数为准；文档精确化见 2026-10-05
      对账 sweep）、benchmarks.md §3 往返估算表改为 1–2 次 RTT 口径 /
      performance.md §2 更新 / roadmap 勾销（commit 见下一条）

## 工程落地清单 · 故障模式回归套件（P1）

> 立档位置：docs/performance.md §7 演练矩阵——把「正确性不靠人品」的七个
> 场景逐项变成 `go test` 可复现的故障用例，叠在现有健康巡检 / 契约套件上。
> 仅 PG 连接池自愈与 Atlas 副本宕机两行保留为部署级演练（ha.md）不自动化。

- [x] 游戏服网络分区：心跳停止 → suspect → offline 推进 + 客户端列表不抖动
      ——既有 health/monitor_test.go 巡检演练 + discovery 可见性测试已覆盖，
      矩阵表格指认落点
- [x] 游戏服重启 / 心跳延迟：重注册幂等恢复；3:1:6 节奏容忍抖动
      ——既有 storetest 重注册契约（suspect/offline 重置表）+ redisstore
      miniredis 契约
- [x] 重复注册 / 重复迁移：upsert 幂等；迁移幂等可重放
      ——既有 storetest 幂等 upsert + admin 迁移编排 / rollback 用例
- [x] Redis 数据丢失（flushall）：档案仍在 PG，心跳重填，无残留 stale 视图
      ——新增 redisstore `TestDataLossFlushAll_HeartbeatRefills`（miniredis
      FLUSHALL → 缺失即缺席 → 心跳重填，静默服务器不代劳）；顺带把
      redisstore 契约测试从「真实 Redis 才跑」升级为 miniredis 兜底常跑
- [x] PG 短暂不可用：服务层故障传播已有（discovery 后端降级测试 +
      registry 错误路径）；连接池自愈保留部署演练（ha.md）
- [x] 迁移中途失败：先复制后切换再清理，任意一步失败可回滚 / 重放
      ——既有 admin rollback 三分支用例
- [x] 巡检任务：跑绿并接进 CI（redisstore 契约此前默认 skip，本次起
      ci.yml 同包内自然纳入常跑）

---

## 工程落地清单 · 决策层读路径 N+1 收尾（P1，正确性×性能）

> 立档位置：上批 ListServers 对齐批的现场发现——`health sweep` /
> `routing.Recommend` / `routing.Diagnose` 仍在逐台 `GetRuntime`
> （整舰队 N 次往返），而 Discovery 管线化批已交付 `GetRuntimes`
> 批量读（Redis 单 pipeline、缺失缺席）。复用既有契约收尾。
> 与 discovery 展示层「降级容忍」不同，这三处是**决策层**：
> 推荐按 live 负载排序、巡检按 runtime 年龄判死——批量读失败
> 一律 fail closed 传播（宁可不决策，不拿未知状态决策），
> 与 maintenanceBlocklist 的 fail-closed 先例同哲学。

- [x] 三处调用点改 `GetRuntimes`：缺席 key = 从未心跳（sweep starting
      超龄分支语义保留）；整体 err 传播（sweep 本轮不翻转任何状态、
      Recommend/Diagnose 如实报错）（commit eb1021c）
- [x] 契约测试：runtime 故障 fail closed 三钉（sweep 不误判 + 两决策
      路径报错）+ starting 无心跳缺席语义钉子；既有 sweep 矩阵全绿
- [x] 文档同步：performance.md §2 / roadmap 管线化行补记决策面收尾
      （health 覆盖 83.9→87.2，race/vet 绿）

## 工程落地清单 · Routing 维护前引导（P1，正确性）

> 立档位置：docs/roadmap.md「v0.2+ 候选方向」Routing 策略扩展行。
> 维护窗口从创建到巡检翻转状态之间、以及窗口开始前的引导期，推荐流量
> 完全无感知：玩家可能被推荐进正在 / 即将维护的服务器，落点即被踢。
> 按「正确性优先于 QPS」择优——这是安全约束，不是偏好过滤。

- [x] 接口与实现：`routing.Service` 感知维护窗口——活动中（覆盖 now）或
      Lead 内开始的服务器从 strict/fallback 两阶段推荐中排除（缺省 Lead
      5m，`ATLAS_ROUTING_MAINTENANCE_LEAD` 可调）；全被排除如实 404
      （宁可无服可推，不引向即将维护的服务器）；结束未清扫的窗口不挡；
      窗口库故障 fail closed（commit e318936）
- [x] Diagnose 同步：逐台判定新增 `maintenance_window` 字段与拒绝原因，
      `Eligible` 计入窗口；推荐与诊断同一管线不漂移（commit e318936）
- [x] 契约测试：窗口排除（active / upcoming / Lead 外 / 已结束）、fallback
      也排除、nil 窗口库兼容、窗口库故障传播、同服多窗口取最紧迫、
      Diagnose 镜像、handler 层 JSON 面（commit e318936）
- [x] 文档同步：api.md §Routing 推荐前置过滤 + diagnose 字段、
      lifecycle.md §5 / operations.md §3 窗口段、roadmap.md 行更新
      （commit e318936）

## 工程落地清单 · Registry 写路径指标（P2，可观测性）

> 立档位置：docs/roadmap.md「v0.2+ 候选方向」可观测性深化行「更多写路径
> 指标」。注册 / 心跳 / 注销是全系统最热写路径：写延迟劣化会先于玩家可
> 感知症状出现（心跳落盘变慢 → 假 suspect/offline 雪崩）——指标是把这类
> 正确性隐患在生产显形的手段。复刻目录写路径指标（首期）范式。

- [x] 指标与埋点：`atlas_registry_write_duration_seconds{op}`（op =
      register / heartbeat / unregister）直方图 + `WithMetrics` 可选注入，
      服务层三写路径 defer 观测（commit fef3567）
- [x] 测试：metrics 标签 / 桶 / nil 接收器 + registry 三写路径观测断言
      （commit fef3567）
- [x] 文档同步：api.md / architecture.md / sync.md 指标表补行，
      roadmap.md 可观测性行更新（commit fef3567）

---

## 归档（全部已完成，逐段压缩；细节与 commit 见 git log）

### 巡检派工（2026-10-05）✅

> 自立项五增量，功能优先：storetest 未钉契约补全 + gRPC 传输鉴权缺口 +
> 同族审计收尾 + 同族限流收尾 + 同族 TLS 收官。门禁全仓绿 + race + vet；
> 本地真库 pg+mysql 全跑通过、实机双模式 e2e 验证。

- [x] storetest 最后两个未钉契约：ListRealms（created_at 降序断言按
      非增写、规避 SQL µs 平局；limit>0 截断 / ≤0 无上限）与 ListRuntimes
      （全量快照视图：在录必现 / 未心跳缺席 / 删除即出列 / 无键不误伤）
      ——commit c5de2bf
- [x] gRPC :9090 鉴权缺口（AdminService 10 个 RPC 此前零鉴权，REST 等价
      操作需 Key+RBAC+白名单+审计）：UnaryAuth 拦截器按三安全域校验，
      与 REST 共用同一批环境变量、未配置=开发开放；域前缀从 pb
      ServiceDesc 派生——初版硬编码 /admin.AdminService/ 与 proto 包
      atlas.v1 不符导致放行、单测同错假绿，实机 e2e 抓出后改为派生 +
      bufconn 端到端测试钉真实全方法名——commit 905bc9b（含 api.md
      认证表脚注截断修复、RBAC 行补全、security.md gRPC 同规与
      「:9090 明文」如实标注）
- [x] 同族收尾——gRPC Admin 操作审计缺口（鉴权补上后审计仍仅 REST）：
      UnaryAudit 拦截器接在 auth 之后（审计在认证之内同 REST），读记
      GET / 写记 POST 带 protojson diff（同 4KB 上限与 JSON 合法性规则），
      gRPC code 映射等价 HTTP 状态，actor 经 httpapi.WithActor 从 auth
      传递（role:sha256 前 12 位，与 REST 同词表）；AuditLog.Record 导出
      为双传输单点落环+结构化日志；实机验证 gRPC 写经 GET /v1/admin/audit
      可见、被拒不进环、公共域不记；api.md / security.md 撤销「审计仅
      REST」旧表述——commit a127bef
- [x] 同族第四层——gRPC 限流缺口（REST 有共享令牌桶、gRPC 零限流）：
      UnaryRateLimit 拦截器限 Registry/Admin 两域（挂载域与 REST 一致，
      公网口不设限），按全方法名前缀匹配规则、未命中落 default 桶，
      拒绝返回 ResourceExhausted + RATE_LIMITED；链序镜像 REST（限流在
      认证之外）；RateLimiter.Allow 导出，桶与拒统计双传输共享——实机
      验证二次 GetStats 被拒且计入 GET /v1/admin/rate-limits 视图；
      api.md 错误映射补 401/403/429 行（鉴权两行为 905bc9b 漏加）、
      security.md §4/§1 补双传输与 TLS 范围——commit c07de18
- [x] 同族收官——gRPC TLS/mTLS 缺口（security.md 四层之首仅覆盖
      :8081）：新增 ATLAS_GRPC_TLS_CERT/_KEY/_CLIENT_CA 复用 tlsutil
      （配 CA 即 mTLS，材料无效 fail fast，与 Registry 口独立，未配置
      =明文历史行为）；Go SDK 新增 GRPCTLS/GRPCTLSCACert（grpcDialCreds
      单测四路径）；实机四象限（明文vs TLS 拒 / TLS 通 / mTLS 无证书拒 /
      mTLS 带证书通）——commit c7c9947。四层防护至此双传输对齐；
      「其余五个 SDK 的 gRPC TLS」查实为幻影项——五语言 SDK 均为
      REST-only（roadmap:46 与各 sdk-*.md 一致），无 gRPC 传输可谈 TLS，
      顺带修正 security.md/api.md 两处因此误写的口径

### 文档-实现对账 sweep（2026-10-05）✅

> 自立项：README / performance / roadmap / benchmarks / api 逐条对账当前实现。
> 门禁全仓绿 + race + vet；修正 5 处（commit 6b9466c）。

- [x] 端点数 45（47 注册 − 探针）、gRPC 5 服务 22 RPC、错误码表全集合对、
      roadmap 8 个 commit hash、链接全检、Makefile 目标、覆盖数字无具体 %
      声明——均一致
- [x] 命令示例实机验证：起服跑通注册→心跳→发现→目录投影→跨服查询→
      推荐（粘滞 has_character / 兜底 lowest_load）与响应字段
- [x] 修正：benchmarks.md §2 B/op+allocs 列刷新为复测钉值（ns/QPS 标注
      2026-10-03 空载基线）；两处「分配数不变」→ +5 allocs（ids 切片与
      结果 map）；performance.md 心跳 350 万 QPS 出处从并发扇入改为
      `BenchmarkHeartbeat`；api.md RATE_LIMITED 出处钉内置令牌桶
      （APISIX 为可选叠加）；grpc 钉值 v1.86.0 → v1.86.0-dev（对齐 go.mod）

### 条件触发定档（2026-10-04）✅

> 两项从「待命」转「定档」：概念与契约安全的代码前置全部就位，
> 重命名 / 拆分本体**未做**，触发条件钉在 roadmap 对应行——触发即执行。

- [x] 状态三态显式化——概念定稿（lifecycle §0 / architecture §4.4 /
      data-model 存储批注），代码三态批注落位（`ServerStatus` 类型 +
      `Server.Status` 字段，commit 4a2ff75）；字段 / 接口重命名定档
      v1.0 API 冻结窗口执行
- [x] Migration Controller 独立模块——概念边界定稿（migration §9），
      代码分流核验通过（`internal/admin` 编排 / `internal/directory`
      投影零交叉）；拆分定档「独立扩缩容真实需求」触发

### v0.1 系列（0.1.0 ~ 0.1.20）✅

| 里程碑 | 内容 |
| --- | --- |
| v0.1.0 | MVP：Registry / Discovery / Directory / 健康监控 / REST API / 双存储 / Docker / CI / 文档站 |
| v0.1.1 | Admin + Dashboard（蓝图 5 页面）+ MySQL store + 鉴权 + 三端口隔离 |
| v0.1.2 | EventAdapter 抽象 + HTTP / Redis Streams 适配器 |
| v0.1.3 | Prometheus 指标 + Grafana 看板 |
| v0.1.4 | Routing 接入推荐（load / capacity / 角色粘滞） |
| v0.1.5 | gRPC 双传输（5 服务 22 RPC，:9090） |
| v0.1.6 ~ 0.1.11 | 六语言 SDK（Go / C++ / Python / JS / Java / C#，自动心跳 + 重试） |
| v0.1.12 | Kafka / NATS(JetStream) / RabbitMQ 总线适配器 |
| v0.1.13 | APISIX 插件（atlas-auth 玩家身份注入 + atlas-ratelimit） |
| v0.1.14 | Realm / Shard 管理（CRUD + 三存储） |
| v0.1.15 | 健康告警（suspect / offline 占比阈值 + webhook 锁存） |
| v0.1.16 | 角色索引 account-hash 分片 + 跨分片查询 + reshard 工具 |
| v0.1.17 | 安全加固（Registry mTLS / Admin RBAC + 审计 / 令牌桶限流） |
| v0.1.18 | Dashboard 增强（服务器地图 / 趋势 / 迁移时间线 / 主题 / i18n） |
| v0.1.19 | 高可用（Redis 哨兵集群 / PG 主从 / HAProxy 扇入 / 连接池调优） |
| v0.1.20 | 注册元数据（players / started_at）+ 计划维护窗口 + 公告系统 + topology 文档 |

### 工程化 Ongoing ✅

- [x] Dependabot 周更 + auto-merge（Go / dashboard / docs 三门禁，patch 自动合并）
- [x] 周期安全审计（govulncheck + npm audit；检出 GO-2026-6443 已升级修复）
- [x] 性能基准（bench_test.go ×2 + benchmarks.md 基线表 + 回归警戒线）
- [x] 覆盖率门禁（service/handler >80%，ci.yml 每包进 Actions Summary）
- [x] 文档与实现同步双门禁（docs.yml + ci.yml）

### 巡检修复（2026-10-02）✅

- [x] memory CreateMaintenanceWindow / CreateAnnouncement 不回填 CreatedAt → 三库对齐
- [x] 心跳响应硬编码 `online` → REST/gRPC 回显生效状态（offline 如实上报，重注册恢复可用）
- [x] 重注册状态语义三库分裂（postgres/mysql offline 无法复活、memory 覆盖运维态）→ 统一契约：仅 suspect/offline 重置 starting
- [x] realm/shard 创建响应 created_at 零值 → 按窗口范式三库对齐，全量清点无第六处
- [x] NATS 消费者测试断言竞态 → 等待 handler 调用 + 毒丸探活，`-count=3` 稳定

### 覆盖率回补 · 巡检补令（2026-10-04）✅

> CI 口径 total 70.4% → **87.0%**（10-03 基线 85.8%）；本地无 DB 口径
> 70.4% → 71.3%（DB 契约测试由 CI service containers 承载，本地无 DSN 时
> skip，故两个口径并存）。只补测试不改语义（commit f750263）：

- [x] 点名短板全部补齐（复测值）：crossserver Update 0%→100%、
      diffDomains 57.9%→100%、receivers 78.3%→95.7%、listServersAll
      75%→91.7%；model Clone 0%→100%；httpapi sprintfTimeID 0%→100%；
      fleet Matches 76.2%→100%、track 四包装 75–80%→100%
- [x] 顺手快赢：envOr、Endpoint.String、MigrationStatus.Valid、redis
      适配器身份、pbMetadata PATCH 语义、readyz 存储故障、admin crossserver
      GET 基线、RegistryRoutes 挂载面
- [x] 门禁：改动包 `-race` 绿、`go vet ./...` 绿、service/handler 层
      全部 >80%（httpapi 86.7 / crossserver 92.3 / admin 92.7 / fleet 94.6 /
      registry 88.0 / routing 91.4）
- 巡检新发现（如实记档）：① 三库 `ListServers` 均 cap 200 而调用方按
  500/50 判页——**舰队 >200 台漏读**，已处置（commit 7e36e88，见
  「巡检点火 · 立档问题处置」段）；② `model.CrossServerSpec.Clone()` 方法
  与函数重复实现——已收敛为函数一处（commit f493154）

### 巡检点火 · CORS 挂载（2026-10-05）✅

> 对账批唯一遗留（dead config）按配置钉法自主定夺：选「挂载」不选
> 「删除」——dashboard 本地开发跨域是真实场景，删了读配置功能就没了。
> commit 7653798（5 文件 +133/-28）：

- [x] `CORSMiddleware(allowedOrigins)` 重写为显式白名单：来源只认
      `ATLAS_CORS_ORIGINS`（逗号分隔），空配置**零 CORS 头不放开**；
      匹配来源回显 origin + `Vary: Origin`（不发 blanket `*`）；
      `*` 显式配置才全放开（开发用）；白名单外与无 Origin 请求原样透传，
      preflight 不放行
- [x] 挂载点：公网 :8080 与管理 :8082，均放最外层（preflight 在
      Tracing/限流/鉴权之前短路——管理口 preflight 不带 Admin Key，
      必须在鉴权外应答）；注册口 :8081 机器流量不挂（注释钉理由）
- [x] 测试矩阵重写（未配置/白名单内/白名单外/无 Origin/`*` 五态，
      GET 与 preflight 双路径）；httpapi 复测 87.0%，race 绿，vet 绿
- [x] 文档同步：api.md「监听地址与探针」补 CORS 段、security.md
      配置速查补 `ATLAS_CORS_ORIGINS` 行

### 巡检点火 · 立档问题处置（2026-10-05）✅

> 覆盖率批立档的两项真问题清零，顺藤摸出同根三处一并处置。
> 受影响包复测：crossserver 91.6 / fleet 95.0 / routing 93.6 /
> health 83.9 / httpapi 86.8 / serversconfig 96.9，全过 80% 门禁；
> 全仓测试 + 改动包 `-race` + `go vet ./...` 绿。

- [x] **ListServers 上限对齐**（commit 7e36e88）：`store.ListServersMaxLimit=200`
      单一事实源（接口注释钉住 cap 200 + 默认页 50 两契约）；三库 ListServers
      改用常量（字符/迁移 cap 不动）；`crossserver.listServersAll` pageSize
      500→cap、`fleet.Reconcile` Limit 500→cap、`serversconfig` 200→常量；
      `routing.Diagnose` 单页 200 → 游标分页。**同根新发现三处一并修**：
      Recommend `listMatching`（空 Limit→默认 50 截断候选，>50 台舰队后段
      永不被推荐）、health `sweep`（同 50 截断，后段永不 suspect/offline）、
      admin fallback 单页 200 对齐常量（该端点本就 client 翻页）
- [x] 回归 4 条 + 契约钉子：跨服寻址 250 台、舰队索引重建 250 台、
      Diagnose 250 台全量判决、listMatching 60 台全候选、sweep 60 台全
      suspect；storetest 补「默认页 = 50 行」钉子；api.md 分页节补
      「单页上限 200」
- [x] **Clone 收敛**（commit f493154）：删除无生产调用方的
      `CrossServerSpec.Clone()` 方法（白名单字段式拷贝对未来新增字段
      不安全），保留生产在用的 `CloneCrossServerSpec`（`out := spec`
      整体拷贝再替换容器）；双向写穿断言并入既有契约测试

### 文档清味 + 禁吹牛（2026-10-05）✅

> 按《文档写作风格规范》+《产品文案用词规范》清 README 与 docs 全量，
> 一次提交（commit 0b5b54d，7 文件 9 处），docs build（死链门禁）过。
> 禁词表（自研 / 遥遥领先 / 首创 / 完美 / 极致 / 赋能 / 生态 / 闭环 /
> 颠覆 / 行业第一）全仓 grep 零命中，README 已补技术底座来源行。

- [x] 假深度句式（不是…而是…）4 处：README 定位、index tagline、
      security-audit 开篇、performance §7——改为直接陈述
- [x] 禁吹牛口径 3 处：api-quickstart「从零到跑通」→「跑通」；
      architecture 网关表「原生最强」无据比较级 →「原生支持」；
      README 首屏补底座来源（Go 1.27 / net/http + gRPC / pgx / go-redis /
      go-sql-driver / Prometheus / Kafka-NATS-RabbitMQ 客户端，Apache-2.0）
- [x] 空洞程度词 2 处：data-model / architecture「可大幅降低 PG 压力」
      → 机制事实「这份流量走 Redis，不进 PG」
- 判断保留（非违规，如实记档）：✅/❌/🔒 为能力矩阵状态标记非装饰；
      粗体冒号列表全为字段/配置项列举型（56 处）；「永不 / 永远」为
      技术不变式陈述非产品承诺；破折号多为单个插入说明未构成滥用

### 全量对账 · 首轮（2026-10-04）✅

> 按「文档与实现对账」全局令做全量清点：env 双向 diff、42 条 HTTP 路由 vs
> api.md、8 指标 vs 指标表、22 RPC vs proto/实现、目录树 vs 实际、SDK 示例
> 变量。偏差逐条修（commit 5c31776）：

- [x] gRPC「与 REST 完全同源 / 同一 API 面」过强表述 → 精确为「核心五服务
      同源共用内部 service，管理面扩展端点 REST-only」（api.md / README /
      roadmap 三处）
- [x] README「28 个 REST 端点」计数失真 → 45 个业务端点、六组速查
      （补跨服配置组；Admin 速查补 maintenance）
- [x] api.md：Admin 节补扩展端点索引（tags / crossserver / audit → 专题
      文档）；认证表 Public 域补 `/v1/routing/*` 与 crossserver 双口说明；
      新增「监听地址与探针」小节（四个 ATLAS_*_ADDR 变量 +
      healthz 公网/注册口、readyz 管理口按口区分）
- [x] sdk-go.md：示例自造 `ATLAS_ADMIN_API_KEY` 名改为「值须为服务端
      `ATLAS_ADMIN_API_KEYS` 之一」；双传输地址行补全三监听变量
- [x] README / roadmap 目录树对齐实际：cmd 四工具（atlas / crossagent /
      demoagents / reshard）、internal 补 crossserver / fleet /
      serversconfig / telemetry、补 dashboard/
- 无偏差核实：指标名双向一致；22 RPC = proto = 实现；`ATLAS_ALERT_*` /
  `ATLAS_PG_POOL_*` 为通配写法或已载于 performance.md；PORT / IMAGE / TAG /
  TUNNEL_PORT / MIGRATIONS_DIR / PG_PASSWORD 为 compose 部署层变量；
  `ATLAS_TEST_*` / `ATLAS_TRANSPORT`（examples 自用）豁免
- 遗留（如实记录）：`ATLAS_CORS_ORIGINS` 读取无消费、
  `CORSMiddleware` 生产路径未挂载——已处置（commit 7653798，见
  「巡检点火 · CORS 挂载（2026-10-05）」段）

### 巡检修复（2026-10-04）✅

- [x] telemetry `TestSamplerLockstepSeries` 假钟数据竞争——`tick++` 在采样
      goroutine（sample 内）与测试 goroutine（Series 内）并发改写无同步，
      race 下稳定失败、非 race 偶发：改 `atomic.Int64` 计数 + 深度断言
      轮询等待（commit 50ea126）

### 可观测性深化 · 首期（2026-10-03）✅

- [x] X-Request-ID 贯通三监听口（生成/沿用/回显/访问日志，健康路径免打扰）
- [x] `atlas_directory_write_duration_seconds{op}` 目录写路径延迟指标

### 跨服 ID 体系与玩法类型表（2026-10-04）✅

- [x] config-center §2.1 跨服 ID 两态规范（配置态 Atlas 托管 / 运行时实例 ID 只规范格式不生成不存储）
- [x] §2.2 七类玩法类型枚举 + 类型表进 Spec 第五段（Normalize / Validate / 契约夹具 / dashboard 卡片）