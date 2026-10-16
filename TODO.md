# TODO

> 需求清单：完成项一行证据（commit + 测试数），未完成项一句话需求。
> 全仓 510 tests / 39 packages 绿。提交前 fetch + rebase origin/main，门禁全绿，禁 tag / release / force push。

## v0.2 进行中

- [x] 六类内存倒排索引：servers 按 region/realm/shard/version/platform/status + public tags 建倒排，characters 按 byAccount/byServer/byAccountServer/byCharID 建倒排，ListServers 取最小候选集逐条验全过滤器，SQL 侧补 tags 过滤 — commit d53a150
- [x] 指令化写队列：写路径封装指令结构，按实体 FNV-1a 哈希入 4 控制道 + 2 热道，可合并性按指令类型声明，合并窗随队列供给，批量上限 256，队列满退化内联同步写 — commit d53a150
- [x] 原子性拍板 + 版本水位：data+index 同锁同临界区，每批提交递增 monotonic 水位作为可见性契约 — commit d53a150
- [x] dash 队列可观测：atlas_store_queue_* 指标族 + GET /v1/admin/indexqueue/status + 管理台存储队列卡 — commit 46caf2d
- [x] performance.md §8 性能设计章 + bench 对照收编 — commit ba79d6f

### 候补

- [ ] API 稳定化与 v1.0：生产验证充分后冻结 REST/gRPC 契约发 v1.0 — 复核 2026-10-16：仍无生产环境验证证据（deploy/ 仅演示材料，docs「生产」仅指引非实证），触发条件未成立，维持挂起
- [x] Routing 策略扩展余项（权重 / 灰度白名单）— 权重已交付（2026-10-14）：`weight` 字段 + 三库存储契约 + `POST /v1/admin/servers/{id}/weight` + 推荐排序按有效负载 `load × 100 / weight`（诊断附 `weight`/`effective_load`）；灰度白名单 = 私有 tag 圈组 + 权重放量（已写进 api.md），routing/admin/httpapi/storetest 25+ tests 绿；REST 五 SDK（python 同步异步 / js / java / csharp / cpp）`set_weight` 铺齐（Go SDK 有意排除：端点 REST-only，grpcBackend 无 REST 回退）— commit f46b7cf；`Server` 模型同带 `weight` 读回（五 SDK，缺省 0）— commit 80ba2e7
- [x] 公告与窗口批量编排 — 批量创建 + 舰队级模板（stagger / dry_run / 部分成功）**已在管理台可视化**（2026-10-14 补）：运维页「批量窗口」入口，一份时段贴服务器列表铺多台，错峰间隔 + 仅预览开关，逐台成功/失败结果表（部分成功不静默）；逻辑抽 operationsBatch.tsx 可单测（19 vitest 绿）+ **迁移-窗口联动**已交付（2026-10-14）：`CreateMigration` 自动在源服务器铺 24h 维护窗口（复用批量窗口 API，fail-closed：窗口铺不上则迁移记录不写；窗口不自动取消，运维可手动 DELETE）；`migration.Service` 持可选 `WindowCreator` 接口（时序归 migration 包，窗口创建由 admin 注入，不反向依赖）；migration/admin/httpapi 测试绿
- [x] Kubernetes 部署样例 — 已交付（2026-10-14）：`deploy/k8s/` 目录含 namespace + configmap + postgres StatefulSet + redis Deployment + 迁移 Job + atlas Deployment/Service + README（边界：无 Helm chart、无 Operator，仅基础 Deployment/Service/Job manifest，符合 roadmap「明确不做」清单）
- [ ] Migration Controller 独立模块拆分：多舰队独立迁移集群需求触发 — 🚧 代码模块拆分已交付（internal/migration 独立包 + admin 薄委托，行为零变更，migration/admin 20 tests 绿，见 commit）；拆成独立服务仍需扩缩容需求触发；复核 2026-10-16：无多舰队独立迁移集群/独立扩缩容新需求（migration.md §9 口径未变），触发条件未成立，维持挂起
- [x] docs 双语扩展（用户令 2026-10-16）— 已交付（2026-10-16）：结构按 VitePress locales 定案，**根 locale = English、中文原文整体迁入 `docs/zh/`**（30 篇），18 篇 `.en.md` 孪生译版提升为根页并去掉 `.en` 后缀（②号「二选一」取「提升为根页」）；①11 篇英文对照本轮补齐：benchmarks / config-center / ha / migration / operations / performance / security / security-audit / topology + audit-source-review-round2，api.md（1019 行）与 audit-doc-consistency（377 行）按硬约束保留「暂无译版」标注桩不硬翻；③docs/ 子目录仅 public/ 静态资源、无 md，无需处理；④nav/sidebar 两 locale 各配中英文案，config.ts 抽 `localize()` 给 zh 侧统一补 `/zh` 前缀（站外绝对地址原样保留），避免逐条手改漏加；每页顶部 `[English] | [简体中文]` 切换器指向对应 locale 同名页；门禁 `pnpm build` 通过（产物 31 根页 + 30 zh 页，lang 属性 en-US / zh-CN 正确，无 `.en.md` 死链、无中文锚点残留）

## Discovery 读路径管线化

- [x] RuntimeStore 批量读 GetRuntimes：memory 单次锁内读全，Redis pipeline 一次 Exec — commit 9f99745
- [x] discovery.ListServers 列表路径改批量合并，GetServer 详情路径维持单点读 — commit 8201f57
- [x] storetest RunRuntime 批量语义契约 + discovery 批量读契约与后端故障降级测试 — commit 9f99745 / 8201f57
- [x] benchmarks.md §3 往返估算改 1–2 次 RTT 口径，performance.md §2 更新 — commit 8201f57

## 故障模式回归套件

- [x] 游戏服网络分区：心跳停止后 suspect 到 offline 推进，客户端列表不抖动（既有 health/monitor_test.go 覆盖）
- [x] 游戏服重启 / 心跳延迟：重注册幂等恢复（既有 storetest 重注册契约覆盖）
- [x] 重复注册 / 重复迁移：upsert 幂等，迁移幂等可重放（既有 storetest + admin 用例覆盖）
- [x] Redis 数据丢失 flushall：档案仍在 PG，心跳重填，无残留 stale 视图（新增 redisstore TestDataLossFlushAll_HeartbeatRefills）
- [x] PG 短暂不可用：服务层故障传播（既有 discovery 后端降级测试覆盖）
- [x] 迁移中途失败：先复制后切换再清理，任意一步失败可回滚 / 重放（既有 admin rollback 用例覆盖）
- [x] 巡检任务跑绿并接进 CI（ci.yml 同包内自然纳入常跑）

## 决策层读路径 N+1 收尾

- [x] health sweep / routing.Recommend / routing.Diagnose 三处改 GetRuntimes，缺席 key 语义保留，整体 err 传播 — commit eb1021c
- [x] 契约测试：runtime 故障 fail closed 三钉 + starting 无心跳缺席语义钉子 — commit eb1021c
- [x] 文档同步：performance.md §2 / roadmap 管线化行补记 — commit eb1021c

## Routing 维护前引导

- [x] routing.Service 感知维护窗口：活动中或 Lead 内开始的服务器从 strict/fallback 两阶段推荐中排除，全排除如实 404，窗口库故障 fail closed — commit e318936
- [x] Diagnose 同步：逐台判定新增 maintenance_window 字段与拒绝原因 — commit e318936
- [x] 契约测试：窗口排除五态 + fallback 排除 + nil 窗口库 + 故障传播 + 同服多窗口 + Diagnose 镜像 + handler JSON 面 — commit e318936
- [x] 文档同步：api.md §Routing + lifecycle.md §5 + operations.md §3 + roadmap — commit e318936

## Registry 写路径指标

- [x] atlas_registry_write_duration_seconds{op} 直方图 + WithMetrics 可选注入，服务层三写路径 defer 观测 — commit fef3567
- [x] 测试：metrics 标签 / 桶 / nil 接收器 + registry 三写路径观测断言 — commit fef3567
- [x] 文档同步：api.md / architecture.md / sync.md 指标表补行 + roadmap — commit fef3567

## 可观测性续跑（2026-10-07）

- [x] atlas_admin_requests_total 接入 gRPC Admin RPC：UnaryMetrics 拦截器挂最外层，endpoint=全方法名，status=httpStatusOf 映射 HTTP 码 — commit aa52b80
- [x] X-Request-ID 贯通 gRPC 口：UnaryTrace 拦截器，metadata 沿用/生成/回显，grpc request 访问日志 — commit aa52b80
- [x] Go SDK Options.DefaultHeaders 关联头：REST 头 / gRPC metadata 双传输同语义 — commit aa52b80
- [x] 关联头通道铺齐五语言 SDK：Python a5eec20 / JS da0458f / Java 7b313cd / C# 866d382 / C++ 5ef329f — commit a5eec20 等

## 巡检修复 · 心跳关停排空（2026-10-07）

- [x] C# AutoHeartbeat.Stop 排空在途心跳：_idle MRES + _inflight 计数，锁内复查 _stopped 拒迟到回调 — commit 4398506
- [x] Java AutoHeartbeat.stop 排空在途心跳：inflight 计数 + 锁内复查 stopped，可重复调用 — commit 61c87d5
- [x] JS AutoHeartbeat.stop 改 async：追踪当前 beat promise 并 await 其落地 — commit eb95b57
- [x] 复核 Go / Python / C++ 均已排空在途 beat，无需改

## 巡检修复 · C# 错误面对齐（2026-10-07）

- [x] 网络层错误码 NETWORK_ERROR 改 NETWORK（五语言统一词表）— commit e5545f0
- [x] 错误信封解析改嵌套读取 + 非信封回退 HTTP_<status>，夹具改真实信封 — commit 4fd0f76

## 归档

### 巡检派工（2026-10-05）

- [x] storetest 补 ListRealms 与 ListRuntimes 契约 — commit c5de2bf
- [x] gRPC :9090 鉴权缺口：UnaryAuth 拦截器按三安全域校验 — commit 905bc9b
- [x] gRPC Admin 操作审计缺口：UnaryAudit 拦截器，读记 GET / 写记 POST 带 protojson diff — commit a127bef
- [x] gRPC 限流缺口：UnaryRateLimit 拦截器限 Registry/Admin 两域 — commit c07de18
- [x] gRPC TLS/mTLS 缺口：ATLAS_GRPC_TLS_CERT/_KEY/_CLIENT_CA 复用 tlsutil — commit c7c9947

### 文档-实现对账 sweep（2026-10-05）

- [x] 端点数 / 错误码表 / roadmap commit / 链接 / Makefile / 覆盖数字全量对账，修正 5 处 — commit 6b9466c

### 条件触发定档（2026-10-04）

- [x] 状态三态显式化概念定稿 + 代码三态批注落位 — commit 4a2ff75
- [x] Migration Controller 独立模块概念边界定稿，拆分定档触发 — commit 4a2ff75

### v0.1 系列（0.1.0 ~ 0.1.20）

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

### 工程化 Ongoing

- [x] Dependabot 周更 + auto-merge（Go / dashboard / docs 三门禁，patch 自动合并）
- [x] 周期安全审计（govulncheck + npm audit；检出 GO-2026-6443 已升级修复）
- [x] 性能基准（bench_test.go ×2 + benchmarks.md 基线表 + 回归警戒线）
- [x] 覆盖率门禁（service/handler >80%，ci.yml 每包进 Actions Summary）
- [x] 文档与实现同步双门禁（docs.yml + ci.yml）

### 巡检修复（2026-10-02）

- [x] memory CreateMaintenanceWindow / CreateAnnouncement 不回填 CreatedAt，三库对齐
- [x] 心跳响应硬编码 online，改 REST/gRPC 回显生效状态
- [x] 重注册状态语义三库分裂，统一契约：仅 suspect/offline 重置 starting
- [x] realm/shard 创建响应 created_at 零值，按窗口范式三库对齐
- [x] NATS 消费者测试断言竞态，改等待 handler 调用 + 毒丸探活，-count=3 稳定

### 覆盖率回补 · 巡检补令（2026-10-04）

- [x] 点名短板全部补齐：crossserver Update / diffDomains / receivers / listServersAll / model Clone / httpapi sprintfTimeID / fleet Matches / track 四包装 — commit f750263
- [x] 顺手快赢：envOr / Endpoint.String / MigrationStatus.Valid / redis 适配器身份 / pbMetadata PATCH 语义 / readyz 存储故障 / admin crossserver GET 基线 / RegistryRoutes 挂载面 — commit f750263
- [x] 门禁：改动包 -race 绿、go vet 绿、service/handler 层全部 >80% — commit f750263

### 巡检点火 · CORS 挂载（2026-10-05）

- [x] CORSMiddleware 重写为显式白名单：ATLAS_CORS_ORIGINS 逗号分隔，空配置零 CORS 头不放开 — commit 7653798
- [x] 挂载点：公网 :8080 与管理 :8082 均放最外层，注册口 :8081 不挂 — commit 7653798
- [x] 测试矩阵重写：五态 × GET/preflight 双路径 — commit 7653798
- [x] 文档同步：api.md CORS 段 + security.md 配置速查 — commit 7653798

### 巡检点火 · 立档问题处置（2026-10-05）

- [x] ListServers 上限对齐：store.ListServersMaxLimit=200 单一事实源，三库 + 四处调用方改用常量 — commit 7e36e88
- [x] 回归 4 条 + 契约钉子：跨服寻址 250 台 / 舰队索引重建 250 台 / Diagnose 250 台 / listMatching 60 台 / sweep 60 台 — commit 7e36e88
- [x] Clone 收敛：删除 CrossServerSpec.Clone() 方法，保留生产在用的 CloneCrossServerSpec — commit f493154

### 文档清味 + 禁吹牛（2026-10-05）

- [x] 假深度句式 4 处改直接陈述 — commit 0b5b54d
- [x] 禁吹牛口径 3 处修正 — commit 0b5b54d
- [x] 空洞程度词 2 处改机制事实 — commit 0b5b54d

### 全量对账 · 首轮（2026-10-04）

- [x] gRPC「与 REST 完全同源」过强表述精确化 — commit 5c31776
- [x] README「28 个 REST 端点」计数失真修正为 45 个 — commit 5c31776
- [x] api.md Admin 节补扩展端点索引 + 认证表 Public 域补 /v1/routing/* + 新增「监听地址与探针」小节 — commit 5c31776
- [x] sdk-go.md 示例 ATLAS_ADMIN_API_KEY 名修正 + 双传输地址行补全 — commit 5c31776
- [x] README / roadmap 目录树对齐实际 — commit 5c31776

### 巡检修复（2026-10-04）

- [x] telemetry TestSamplerLockstepSeries 假钟数据竞争，改 atomic.Int64 计数 — commit 50ea126

### 可观测性深化 · 首期（2026-10-03）

- [x] X-Request-ID 贯通三监听口（生成/沿用/回显/访问日志，健康路径免打扰）
- [x] atlas_directory_write_duration_seconds{op} 目录写路径延迟指标

### 跨服 ID 体系与玩法类型表（2026-10-04）

- [x] config-center §2.1 跨服 ID 两态规范
- [x] §2.2 七类玩法类型枚举 + 类型表进 Spec 第五段

### OpenTelemetry span 树（2026-10-08）

- [x] 追踪地基：internal/tracing OTLP/HTTP 导出管线 + httpapi.Spans / grpc.UnarySpans 每请求开 root span — commit 0fe965d
- [x] 服务层成树：atlastracing.Start 14 个服务方法子 span + tracingtest.Install 测试助手 — commit f5359e2
- [x] 文档同步：api.md「分布式追踪」新节 + architecture.md 交叉引用 + roadmap 五期收官 — commit 0fe965d / f5359e2

### 覆盖率回补（2026-10-07）

- [x] 真基线口径重测 + 逐包行为测试补齐，total 88.2% 升到 89.4% — commit f40a7f4 / 5e653db / 6c48c30 / 3c59244 / c3fe8dc / b99e698 / f0e2866

### 服务器热力图（2026-10-12）

- [x] telemetry 热力图聚合：BuildServerHeatmap / BucketizePair / ParseHeatmapWindow / BucketSpanLabel — commit 3232f6c
- [x] REST GET /v1/admin/load-heatmap + gRPC AdminService.GetLoadHeatmap — commit e1d176b
- [x] dashboard /heatmap 页：Okabe-Ito 暖色阶 + 图例 + tooltip — commit a26f25e
- [x] 留存 10h 升到 24h15m + 文档同步（api / security / roadmap / README）— commit c573846

### 巡检修复（2026-10-13）

- [x] go.mod toolchain 钉 1.27.2：清掉 11 条 Go 标准库漏洞告警（govulncheck 0）+ Dockerfile golang:1.27.2-alpine
- [x] 文档-码一致性 41 条修复（合服表述、gRPC 访问日志字段、PG 池缺省、2026-10-03 基线数字全量复测替换等）— 详见 docs/审计-文档一致性.md 2026-10-13 轮
- [x] redis 事件适配器 sweep 测试改确定性驱动（全量 -race 并行下消费循环被饿死 >30s 超时）
- [x] BUG-1 收尾：ServerDetail getServer 失败渲染错误面板（重试/返回）、角色列表请求失败降级空列表，补 vitest 3 例（此前 dashboard 无任何测试设施）

### 管理台补面（2026-10-14）

- [x] 舰队批量维护窗口可视化：运维页「批量窗口」入口（服务器列表贴入 + 错峰间隔 + 仅预览开关 + 逐台成功/失败结果表），逻辑抽 operationsBatch.tsx 可脱离 antd 表单单测 — 26 vitest 绿
- [x] Admin 操作审计页（/audit）：`GET /v1/admin/audit` 自 v0.1.17 交付起就没进管理台，本轮补齐——写操作展示变更体、状态码 2xx/4xx/5xx 分色、limit 可调、失败重试；页脚如实标注内存环重启即丢；api.md 补该端点正式小节（此前只在索引表一行）；修 docs/api.md 重复标题（POST /v1/admin/announcements 出现两次）；AuditLog.test 7 例（dashboard 全量 26 绿）
