# TODO

> 每一项都是一个可独立提交的原子任务：完成后打勾并注明 commit，测试 / 门禁
> 全绿才提交推送（fetch + rebase origin/main，禁 tag / release / force push）。
>
> 当前批次：**文档清味批（用户令）**——按规范文档清机器味 + 禁吹牛扫描
> （README 与 docs 全量，一次提交）；随后接续其它队列批次。
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
- [x] 基准与文档：复测确认服务层分配无变化（回归以分配数为准）、
      benchmarks.md §3 往返估算表改为 1–2 次 RTT 口径 / performance.md §2
      更新 / roadmap 勾销（commit 见下一条）

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
- 巡检新发现（如实记档，未处置——修复涉及契约/语义，留独立批次）：
  ① 三库 `ListServers` 均 cap 200（storetest 钉住的契约），而
  `crossserver.listServersAll` 按每页 500 判终页、`fleet.Index.Reconcile`
  以 Limit 500 循环——**舰队 >200 台时信号寻址与索引重建漏读**；
  `routing.Diagnose` Limit 200 单页截断同理；② `model.CrossServerSpec.Clone()`
  方法与 `CloneCrossServerSpec` 函数重复实现，生产只用函数（方法本批已补测）

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
- 遗留（如实记录，未处置）：`ATLAS_CORS_ORIGINS` 读取无消费、
  `CORSMiddleware` 生产路径未挂载——dead config，文档有意不收录，
  待后续批次挂载或删除

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