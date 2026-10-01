# TODO

> 每一项都是一个可独立提交的原子任务。完成后打勾并注明 commit。
>
> 版本策略：0.1.x 逐个推进，产品验证后再考虑 0.2.0。

---

## v0.1.0 — MVP ✅

- [x] Server Registry — register / heartbeat / unregister
- [x] Server Discovery — list / get
- [x] Character Directory — create / update / delete / account → characters
- [x] Health Monitor — automatic offline (online → suspect → offline)
- [x] REST API — 11 endpoints, Go 1.22+ pattern routing
- [x] Memory Store — in-memory implementation for tests/dev
- [x] PostgreSQL Store — pgx + pgxpool
- [x] Redis Store — runtime state (heartbeat/load/players)
- [x] Docker — multi-stage Dockerfile + docker-compose (atlas + PG + Redis)
- [x] CI — GitHub Actions for VitePress docs deployment
- [x] VitePress docs site — config + custom CSS + landing page + api-quickstart
- [x] README — logo + quick start + API reference + project structure

## v0.1.1 — Admin + Dashboard + Security ✅

- [x] Admin API — server lifecycle (maintenance / drain / enable / disable)
- [x] Admin API — global stats (servers by status/region/version, player counts)
- [x] Admin API — character search (by name/server/class/level range)
- [x] Admin API — migration CRUD (create / get / list / rollback)
- [x] Dashboard — React 19 + Vite 6 + Ant Design 5, dark theme
- [x] Dashboard — Overview / Servers / ServerDetail / Characters / Migrations
- [x] MySQL Store — database/sql + go-sql-driver/mysql
- [x] Metadata — Server.Metadata + Character.Metadata (JSON KV)
- [x] Auth — AdminAuth (API key + IP whitelist) + RegistryAuth (service token)
- [x] Security — three-port isolation (:8080 public / :8081 registry / :8082 admin)
- [x] Migration SQL — PostgreSQL + MySQL schema with metadata columns
- [x] Docs — architecture selection rationale

---

## v0.1.2 — Event Adapter ✅

- [x] Define `EventAdapter` interface in `internal/event/adapter.go` (518f76d)
- [x] Implement `internal/event/http/` — HTTP sync adapter (current behavior) (518f76d)
- [x] Implement `internal/event/redis/` — Redis Streams adapter (XADD / XREADGROUP) (d53d9c9)
- [x] Add `ATLAS_EVENT_ADAPTER` config (http / redis) (518f76d)
- [x] Wire adapter into Directory service, replace direct HTTP call (518f76d)
- [x] Tests for both adapters (518f76d, d53d9c9)
- [x] Update docs/sync.md migration path

## v0.1.3 — Prometheus Metrics ✅

- [x] Add `internal/metrics/` package with Prometheus registry (d6742b1)
- [x] Expose `GET /metrics` on admin port (:8082) (d6742b1)
- [x] `atlas_registry_servers_total{status}` — gauge (d6742b1)
- [x] `atlas_registry_heartbeat_lag_seconds` — histogram (d6742b1)
- [x] `atlas_directory_characters_total` — gauge (d6742b1)
- [x] `atlas_discovery_requests_total{filter}` — counter (d6742b1)
- [x] `atlas_admin_requests_total{endpoint,status}` — counter (d6742b1)
- [x] `atlas_health_transitions_total{from,to}` — counter (d6742b1)
- [x] Grafana dashboard JSON in `deployments/grafana/`

## v0.1.4 — Server Routing ✅

- [x] Add `internal/routing/service.go` — recommendation logic
- [x] Filter: region, version, platform, status=online
- [x] Score: load (lower better), capacity remaining (higher better)
- [x] Tiebreak: has existing character (account_id optional)
- [x] `GET /v1/routing/recommended` on public port (:8080)
- [x] Response: `{server, reason}` (lowest_load / highest_capacity / has_character / fallback)
- [x] Tests

## v0.1.5 — gRPC API ✅

- [x] Define `api/proto/atlas.proto` — all service definitions
- [x] Generate Go code (protoc / buf)
- [x] Implement gRPC server in `internal/grpc/`
- [x] Start on `:9090` (configurable `ATLAS_GRPC_ADDR`; empty disables)
- [x] Feature parity: Registry, Discovery, Directory, Routing, Admin
- [x] bufconn end-to-end tests (register → heartbeat → list → character → recommend → admin)

## v0.1.6 — Go SDK ✅

- [x] `sdk/go/` — Go client library
- [x] `Register` / `Heartbeat` / `Unregister`
- [x] `ListServers` / `GetServer`
- [x] `UpsertCharacter` / `ListByAccount` / `SearchCharacters`（Directory + Admin 全量方法）
- [x] Auto-heartbeat goroutine with configurable interval（`StartHeartbeat`，启动即报、`Set` 并发更新负载）
- [x] Retry with exponential backoff（全抖动；网络错误 + 5xx / gRPC Unavailable，4xx 不重试）
- [x] Both REST and gRPC transport（同一 `Client` API，`Options.Transport` 切换）
- [x] Example: `examples/go/`（注册 → 心跳 → 推荐 → 角色 → 注销）
- [x] Docs: `docs/sdk-go.md` + VitePress 侧边栏

## v0.1.7 — C++ SDK ✅

- [x] `sdk/cpp/` — C++ client library
- [x] HTTP client (cpp-httplib v0.58.0, vendored single header)
- [x] JSON serialization (nlohmann/json v3.12.0, vendored single header)
- [x] Same API surface as Go SDK（五组方法全量 + AutoHeartbeat 线程 + 重试）
- [x] CMake build system（`atlas_sdk` 目标 + ctest；`ATLAS_SDK_BUILD_TESTS`）
- [x] Example: `examples/cpp/`（注册 → 心跳 → 推荐 → 角色 → 注销，对真实 Atlas 冒烟通过）
- [x] `registry_base_url` 覆盖 Registry 独立端口（Go SDK 同步补 `RegistryAddr`）
- [x] 扁平/嵌套目录写回复归一化（Go `CharacterWriteResult.UnmarshalJSON` + C++ `ParseCharacterWrite`）

## v0.1.8 — Python SDK ✅

- [x] `sdk/python/` — Python client library
- [x] `atlas_client.py` — sync client using httpx
- [x] `AtlasAsyncClient` — async client using httpx + asyncio
- [x] `Register` / `Heartbeat` / `Unregister`
- [x] `ListServers` / `GetServer`
- [x] `UpsertCharacter` / `ListByAccount` / `SearchCharacters`
- [x] Auto-heartbeat background task（同步线程 + asyncio 任务，`on_error` 回调）
- [x] PyPI package config (`pyproject.toml`)
- [x] Tests — 真实 socket 假 Atlas（路径/认证/查询/错误映射/重试/心跳，同步+异步 13 例）
- [x] Example: `examples/python/`（注册 → 心跳 → 推荐 → 角色 → 注销，对真实 Atlas 冒烟通过）
- [x] Docs: `docs/sdk-python.md` + VitePress 侧边栏

## v0.1.9 — JavaScript/TypeScript SDK ✅

- [x] `sdk/js/` — TypeScript client library
- [x] `AtlasClient` — fetch based client（Node 18+ 与浏览器通用，零运行时依赖）
- [x] Same API surface as Go SDK（五组方法全量，传输 snake_case / SDK camelCase）
- [x] npm package config (`package.json`，`@cuihairu/atlas-client`，ESM + CJS 双产物 + 类型)
- [x] Works in Node.js and browser（fetch / AbortSignal.timeout / URL 全平台内置）
- [x] Tests — node:test 真实 socket 假 Atlas（12 例，Node 类型剥离直接跑 TS 源码）
- [x] Example: `examples/js/`（注册 → 心跳 → 推荐 → 角色 → 注销，对真实 Atlas 冒烟通过）
- [x] Docs: `docs/sdk-js.md` + VitePress 侧边栏
- [x] 示例竞态修复（Go/Python/JS 同步）：首发心跳改同步，在线后才推荐

## v0.1.10 — Java SDK ✅

- [x] `sdk/java/` — Java client library（Java 17+，`io.github.cuihairu:atlas-client`）
- [x] `AtlasClient` — OkHttp + Gson（snake_case/camelCase 双向映射，Instant 时间戳）
- [x] Same API surface as Go SDK（五组方法全量 + AutoHeartbeat 守护线程 + 重试）
- [x] Maven + Gradle config（pom.xml + build.gradle，两者构建/测试均验证通过）
- [x] Tests — JUnit 5 + JDK httpserver 假 Atlas（13 例，含显式 null 集合兜底）
- [x] Example: `examples/java/`（注册 → 心跳 → 推荐 → 角色 → 注销，对真实 Atlas 冒烟通过）

## v0.1.11 — C# SDK ✅

- [x] `sdk/csharp/` — C# client library（`Atlas.Client`，net8.0 + net10.0 multi-target，零第三方依赖）
- [x] `AtlasClient` — HttpClient + System.Text.Json（SnakeCaseLower 双向映射，DateTimeOffset 时间戳）
- [x] Same API surface as Go SDK（五组方法全量 + StartHeartbeat/AutoHeartbeat + 重试）
- [x] NuGet package config（csproj 打包配置，dotnet pack 产出 nupkg 验证通过）
- [x] Tests — xUnit + TcpListener 假 Atlas（23 例，含重试耗尽/端口拆分/心跳循环/OnError）
- [x] Example: `examples/csharp/`（注册 → 心跳 → 推荐 → 角色 → 注销，对真实 Atlas 冒烟通过；PosixSignalRegistration 处理 Ctrl+C，stdin 重定向时可用 ATLAS_RUN_SECONDS 退出）

## v0.1.12 — Message Bus Adapters ✅

- [x] Implement `internal/event/kafka/`（segmentio/kafka-go，纯 Go；消费组手动提交 offset，at-least-once）
- [x] Implement `internal/event/nats/`（nats.go JetStream：流 `ATLAS` + durable pull consumer 显式 Ack）
- [x] Implement `internal/event/rabbitmq/`（amqp091-go：durable topic exchange + persistent 消息 + Nack requeue）
- [x] Config: `ATLAS_EVENT_ADAPTER=kafka|nats|rabbitmq`（+ `ATLAS_KAFKA_BROKERS` / `ATLAS_NATS_URL` / `ATLAS_RABBITMQ_URL`）
- [x] Adapter selection guide in docs/sync.md（五种适配器连接配置 + 重投语义对照 + 运维选型参照）
- [x] Tests — kafka/rabbitmq 接口注入 fake（发布编解码/拓扑声明/投递 ack/失败重投/poison 丢弃/Close），nats 用嵌入式 nats-server 端到端（JetStream 真实发布-消费-确认）

## v0.1.13 — APISIX Plugin ✅

- [x] `plugins/apisix/` — Lua plugin（`atlas-auth.lua` + `atlas-ratelimit.lua`，标准 APISIX schema/access 结构）
- [x] Route Atlas requests to Atlas backend（`/v1/*` → atlas:8080，config-example.yaml 一条路由覆盖全 API 面）
- [x] Inject player token into Atlas headers（token 头/Bearer 校验 → 注入 `X-Atlas-Player-ID`，可剥离原 token，匿名模式可选）
- [x] Rate limit config per endpoint group（discovery/directory/routing/registry 四组最长前缀匹配 + 固定窗口 + fail open）
- [x] Installation and config guide（README：安装/启用/共享字典/路由配置/验证 curl；mock 测试 13 例全过，luac 语法检查通过）

## v0.1.14 — Realm / Shard Management

- [x] `POST /v1/admin/realms` / `GET /v1/admin/realms`
- [x] `POST /v1/admin/shards` / `GET /v1/admin/shards`（`?realm_id=` 过滤）
- [x] Wire into store interfaces and all implementations（memory / postgres / mysql；`0002_realms_shards_indexes` 迁移补 created_at / status 索引）
- [x] Tests（admin service 9 例 + httpapi 端到端 1 例）

## v0.1.15 — Health Alerts

- [x] Alert thresholds in config (`ATLAS_ALERT_SUSPECT_RATIO`，默认 0.3；`ATLAS_ALERT_OFFLINE_RATIO`，默认 0.2；0 关闭)
- [x] Structured log alert when suspect/offline exceeds threshold（锁存语义：越限 firing 一次，回落 recovered 一次）
- [x] Optional: webhook notification (`ATLAS_ALERT_WEBHOOK_URL`，5s 超时，投递失败不影响巡检)

## v0.1.16 — Character Index Sharding

- [x] Sharding strategy interface in `internal/store/`（`CharacterShardStrategy`）
- [x] Hash-based sharding by `account_id`（FNV-1a，`HashShardStrategy` + `store/sharded` 组合：键路由一跳，扇出查询归并；`ATLAS_CHAR_SHARDS` 接线）
- [x] Cross-shard query for admin search（SearchCharacters 全分片扇出 + 全局游标序归并，分页跨分片正确）
- [x] Migration tool for resharding（`sharded.Reshard` + `cmd/atlas-reshard` CLI：单库→N 库 / N→M 重切 / 逻辑分片验证，幂等续传）
- [x] 顺带修复 memory 存储搜索游标的词法比较 bug（跨位数翻页丢行，postgres 语义为类型化元组比较）

## v0.1.17 — Security Hardening

- [x] mTLS for service-to-service (Registry API)（`ATLAS_REGISTRY_TLS_CERT/_KEY`，配 `ATLAS_REGISTRY_CLIENT_CA` 升级双向 TLS；握手层拒绝无证客户端）
- [x] RBAC for Admin API（admin/operator/viewer 三角色，`ATLAS_ADMIN_ROLES`，未列出的 key 默认 admin 向后兼容；viewer 写操作 403 ROLE_NOT_ALLOWED）
- [x] Audit log — actor（role:key 指纹）+ RFC3339 时间 + method/path/status + 变更请求体（diff），结构化日志 + 内存环 + `GET /v1/admin/audit`
- [x] Rate limiting middleware（令牌桶，路径前缀最长匹配 × 客户端 IP 分桶，`ATLAS_RATE_LIMITS`/`ATLAS_RATE_LIMIT_DEFAULT`，429 + Retry-After，桶表上限防 XFF 伪造）
- [x] docs/security.md 四层防护说明 + 测试 18 例（含真实 mTLS 握手）

## v0.1.18 — Dashboard Enhancements

- [x] Real-time server map (geographic distribution)（ServerMap 组件：按区域卡片聚合状态/玩家/负载，10s 轮询 + 更新时间徽标）
- [x] Player trend charts (daily/weekly active)（PlayerTrend：后端暂无历史库，仪表盘 localStorage 滚动采样（7 天/5000 点），24h/7d 折线切换）
- [x] Migration progress with live updates（迁移 pending/running 时静默 5s 轮询 + 行展开 Steps 时间线（已创建→迁移中→完成，失败置 error））
- [x] Dark/light theme toggle（theme.ts 单例 + localStorage，antd darkAlgorithm/defaultAlgorithm 切换，侧栏/头部联动）
- [x] i18n (English + Chinese)（i18n.ts 模块单例 + useLang 订阅，非 React 的 api client 也可翻译；全部页面/组件硬编码中文迁入词典，头部语言/主题切换按钮）

## v0.1.19 — High Availability

- [x] Atlas multi-replica deployment guide (stateless, horizontal scaling)（docs/ha.md：状态归属表 + 每副本健康巡检/审计环的告警去重与 admin 粘性说明）
- [x] Internal LB (HAProxy TCP mode / APISIX) in front of the Registry port for heartbeat fan-in（deploy/haproxy/haproxy.cfg：registry 扇入 round-robin + 主动 TCP 健康检查，admin source 粘性，stats 面板）
- [x] Redis Sentinel / Cluster config（ATLAS_REDIS_CLUSTER > ATLAS_REDIS_SENTINELS + ATLAS_REDIS_MASTER_NAME > 单节点；URL 密码/DB 全模式生效；runtime store 与 Redis Streams 事件适配器共用拓扑，go-redis UniversalClient）
- [x] PostgreSQL primary-replica with streaming replication（docs/ha.md §4：pg_hba + pg_basebackup -R + hot_standby，故障晋升 runbook；deploy/postgres/init-replica.sh）
- [x] Connection pool tuning documentation（ATLAS_PG_POOL_MAX_CONNS/MIN_CONNS/LIFETIME/IDLE_TIME/HEALTH_CHECK_PERIOD 叠加 pgxpool.ParseConfig（MinConns>MaxConns 钳制），ATLAS_REDIS_POOL_SIZE；docs/ha.md §5 容量经验公式）
- [x] Lab stack：deploy/docker-compose.ha.yaml（2× Atlas + HAProxy + PG 主从 + Redis 主从 + 3 Sentinel 一键起）

## v0.1.20 — Server Metadata, Maintenance Windows & Announcements ✅

- [x] Register metadata extension: initial `players`, `started_at` (server uptime metadata)（注册请求新增 players 种子初始心跳、started_at 缺省注册时刻并随重注册刷新，Discovery 可展示 uptime；迁移 0003 三库同步）
- [x] Heartbeat cadence guidance: report interval vs suspect/offline thresholds (3:1, e.g. 10s / 30s / 60s)（lifecycle.md §4.2 心跳节奏 3:1:6 法则与按比例缩放指引）
- [x] Scheduled maintenance windows — `POST /v1/admin/servers/{id}/maintenance-window {start_at, end_at}`; health monitor auto-enters `maintenance` at start, restores previous status at end（健康巡检 start_at 自动置 maintenance 并记 previous_status，end_at 仅恢复窗口放入的状态——运维手动转移不被回滚；operator 状态/offline 不动、窗口标记已应用不重试，到期删除窗口记录）
- [x] Announcements resource — server-scoped or global, active time range, `GET /v1/discovery/announcements` for clients（全局/服务器范围公告，半开 [starts_at, ends_at) 生效区间，info/warning/critical 级别校验；Discovery 只读接口仅返回生效公告，服务器的全局公告始终可见）
- [x] Maintenance window ↔ announcement linkage (optionally auto-create a maintenance announcement)（announce 缺省 true 自动创建同时段 warning 级公告并与窗口双向关联；删除窗口不撤回公告）
- [x] `docs/topology.md` — deployment topology: client → APISIX (public LB + auth) → game servers / Atlas public; game servers → (HAProxy) → Atlas registry; Atlas → Redis / PostgreSQL; Prometheus → Grafana（分层职责表 + 端口矩阵 + 单机/标准生产/大规模三档部署形态；VitePress 导航新增）

---

## Ongoing

- [x] Dependency updates — Dependabot auto-merge for patch versions（dependabot.yml：gomod + dashboard/docs npm 三生态周更，开发依赖分组；auto-merge workflow：仅 dependabot[bot] PR，先跑 Go/dashboard/docs 三门禁，同 major.minor 的 patch 自动 approve+squash 合并，minor/major/digest 人工评审）
- [x] Security audits — periodic review of auth and storage layers（security-audit workflow：每周 govulncheck + 双 npm audit；docs/security-audit.md 八层检查清单 + v0.1.20 审计记录——检出并修复 GO-2026-6443 gRPC panic（升级修复版本）、SQL 全参数化确认、Server.ID 字符集低危建议）
- [x] Performance benchmarks — Registry and Discovery QPS（bench_test.go ×2：注册/重注册/心跳/并发心跳扇入/列表 100–5000 台/region 过滤/详情；docs/benchmarks.md 基线表 + O(fleet) 列表的 Redis 往返估算 + 回归警戒线，挂导航）
- [x] Test coverage — aim for >80% on service and handler layers（discovery 0→90.9%、httpapi 66.3→80.7%、health 72.1→83.8%、model 36.2→91.4%、memory 31.1→88.4%；admin 87.5 / directory 82.4 / registry 81.8 / routing 87.8 全部达标；新增 ci.yml：build/vet/test + 每包覆盖率进 Actions Summary）
- [x] Documentation — keep docs/ in sync with implementation（本节交付均已同步：api.md 端点、lifecycle 窗口语义、data-model §7、新拓扑/基准/安全审计三篇 + 导航；docs.yml + ci.yml 双门禁防漂移）
---

## 巡检修复（2026-10-02）

- [x] memory store `CreateMaintenanceWindow` / `CreateAnnouncement` 不回填 `CreatedAt`——postgres/mysql 在调用方对象上戳时间,memory 只戳内部副本,导致 API 响应 `created_at` 恒为零值（实测 `mwin-*` 返回 `0001-01-01T00:00:00Z`）；已对齐三库语义（在调用方对象上戳时间再存副本）。走查方式：`ATLAS_STORE=memory` 起真实进程,覆盖注册→心跳提升→窗口自动维护→联动公告→CRUD→异常路径→审计全链路
- [x] 心跳响应谎报 `status:"online"`——REST handler 与 gRPC server 均硬编码 `online`,服务器被监控判 `suspect`/`offline` 后心跳照常 200 且自称 online,调用方（SDK/运维脚本）无从感知已出局、也就不会触发文档承诺的重注册恢复（实测 offline 后心跳返回 `{"status":"online"}` 而 discovery 列 offline、路由拒派）；已改为 `Service.Heartbeat` 返回生效状态,REST/gRPC 两面如实回显（实测 offline 心跳响应 `"status":"offline"`,重注册→starting→心跳→online 全链路复活）
- [x] 重注册状态语义三库分裂——postgres/mysql upsert 的 `CASE WHEN EXCLUDED.status='starting' THEN servers.status` 使 offline 服务器重注册后仍 offline（心跳只提升 starting→online,恢复路径在 SQL store 上永久断裂）;memory 则无条件重置,连 `disabled`/`maintenance` 都会被重注册打回 starting（违反 lifecycle.md「禁用不被自动状态机覆盖」）；已统一契约：仅旧状态为 suspect/offline 时重置为 starting,其余（online 及运维态）保留（实测 disable→重注册→仍 disabled;SQL 语句已对齐,三库行为一致）
- [x] realm/shard 创建响应 `created_at` 恒为零值——fa5b874 修窗口/公告时同款缺陷漏查了这两处:memory 只戳内部副本,postgres/mysql 把 `time.Now()` 内联进 INSERT 不回填调用方对象（实测 `POST /v1/admin/realms` 返回 `0001-01-01T00:00:00Z`）；已按窗口范式对齐三库（先回填调用方再落库）,实测返回真实时间戳；并全量清点三库 5 类 Create 确认无第六处（Migration 模型无 created_at,Character/Server 本就有回填）
