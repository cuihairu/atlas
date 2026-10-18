# TODO

- [x] 注册生命周期：永久 / 暂时 两类标记（来源：用户 2026-10-14）— 已交付（2026-10-14）：`reg_kind`（permanent / temporary）+ `ttl_seconds` 显式标记落 server 记录与三库存储（迁移 0008），注册 API（REST）与校验接入；过期走健康监控独立路径，只对 `temporary` 且 `created_at + ttl_seconds` 超时回收（绝对时长，不读运行时），永久与遗留空值行任何情况不自动删；显式删除入口 `DELETE /v1/admin/servers/{id}`（config 托管 409）；model/registry/health/admin/httpapi/storetest 新增 14 tests 绿 — commit 4ed7dae
- [x] 永久实例离线告警（来源：用户 2026-10-14）— 已交付（2026-10-14）：健康监控 offline 转移时经既有 Alerter 渠道（Warn 日志 + `ATLAS_ALERT_WEBHOOK_URL`）发单服务器告警 `permanent_offline`，载荷含 `server_id` / `reg_kind` / `offline_for` / `maintenance`（lifecycle.md §4.4）；豁免两条路径齐备：maintenance 非 auto-managed 不进转移（结构性豁免），temporary 消失属预期不告警；阈值沿用 `ATLAS_SUSPECT_AFTER` / `ATLAS_OFFLINE_AFTER`（默认 30s / 60s）；health 新增 6 tests 绿（含告警触发与维修豁免） — commit 4ed7dae
- [x] 审核整改（2026-10-18 独立审核，见 docs/review/luna-review-2026-10-18.md）：
      表一「缺陷」（按风险排序；代码核对/修复证据）：
      - [x] 高｜维护窗口失败可能遗留 maintenance：改为先持久化原状态、再 CAS 改状态；失败可重试，窗口结束可恢复。证据：`internal/health/monitor.go:346-381`；失败与重试回归：`internal/health/guarded_transition_test.go:102-201`。
      - [x] 高｜巡检旧快照覆盖重注册：状态写入使用预期旧状态 CAS，冲突时不覆盖新写入。证据：`internal/health/monitor.go:262-285`；并发回归：`internal/health/guarded_transition_test.go:59-97`。
      - [x] 中｜写失败仍计入转移：仅在存储接受 CAS 后改内存状态、记日志和指标；新增失败写回归。证据：`internal/health/monitor.go:272-284`；`internal/health/guarded_transition_test.go:260-283`。
      - [x] 中｜旧角色事件回退投影：加入带来源时间戳的 created/login 防回退，覆盖 `last_login_at` 单调性；用例：`internal/directory/events.go:85-103,183-208`；`internal/directory/reorder_test.go:11-115`。边界已明确：无时间戳事件和一般字段更新仍无源端版本/序列校验，保留最终一致取舍（`docs/sync.md:257-272`、`docs/zh/sync.md:232-245`）；不虚称为全事件版本化。
      表二「文档与实现不一致」（以代码为准，只改文档）：
      - [x] 迁移并不搬迁/切换角色索引，也不自动下线源服：`docs/zh/realms-shards.md:91-93`、`docs/realms-shards.md:98-102`、`docs/zh/scenarios.md:77-88`、`docs/zh/index.md:84`、`docs/index.md:97`；代码证据：`internal/migration/service.go:68-113,125-140`。
      - [x] Realm/Shard 是创建与列表，不是 Admin CRUD/索引迁移：`docs/roadmap.md:52`、`docs/zh/roadmap.md:52`；路由证据：`internal/httpapi/handlers.go:192-195`，存储接口：`internal/store/store.go:175-199`。
      - [x] runtime TTL 只清理运行时键，健康巡检负责判活：`docs/zh/performance.md:29`、`docs/performance.md:36-39`；实现证据：`internal/health/monitor.go:151-181,184-191`。
      - [x] Quick Start 语言链接移出 bash 代码块：`README.md:118-128`、`README.zh.md:118-128`；实际命令：`Makefile:12-13`。
      表三「值得补的能力」只完成评估与分期建议，**没有实现**：
      - [x] Admin 审计持久化/可靠投递（高）：当前 1,000 条进程内环并另写 slog，重启后环丢失（`internal/httpapi/audit.go:39-49,107-127`；`docs/zh/security.md:55,72-74`）。建议 P1：先定保留/脱敏/失败策略，再评估可确认写入的持久后端；不承诺同步落库阻塞 Admin 请求。
      - [x] Kafka/NATS 重试上限与 DLQ（高）：处理失败分别不提交 offset / 不 ACK（`internal/event/kafka/adapter.go:217-223`；`internal/event/nats/adapter.go:224-232`）。建议 P1：设计最大投递次数、退避、死信保留/重放和监控，先验证至少一次语义及幂等性。
      - [x] Admin 监听口 TLS（高）：Admin HTTP 使用 `ListenAndServe`（`cmd/atlas/main.go:589-595,684-690`），现有 TLS 章节覆盖 Registry/gRPC（`docs/zh/security.md:6-8`）。建议 P0（暴露非可信网络前）：统一证书配置/轮换、TLS/mTLS 选择与启动失败测试；现阶段依赖网络隔离，不视为已有 TLS。
      - [x] 生产形状压测/长稳观测（高）：现有基准排除网络、序列化和存储 I/O（`docs/zh/benchmarks.md:4-8,72-79`）。建议 P2：先定代表性部署与负载模型，再测端到端 p95/p99、GC、故障恢复和长稳资源趋势。
      - [x] 验证：`go test ./...` 通过；Dashboard `npm test` 26/26、`npm run build` 通过；Docs `npm run build` 完成（有已知 SSR `getComputedStyle` 日志，不影响退出状态）。
      全部测试通过后 commit + push；英文简短 commit；不 force push、不 bump 版本号。
- [x] Admin 监听口 TLS（来源：2026-10-18 审核表三 P0，暴露非可信网络前）— 已交付（2026-10-19）：`ATLAS_ADMIN_TLS_CERT` / `ATLAS_ADMIN_TLS_KEY` / `ATLAS_ADMIN_TLS_CLIENT_CA`（config 三字段，默认空 = 明文，既有网络隔离部署零变化）；Admin HTTP 监听镜像 Registry 口同一套 tlsutil 材料校验与 fail-fast，配客户端 CA 升级 mTLS（dashboard / 运维工具须出示客户端证书）；Registry/Admin 两口的 TLS 包裹收敛为同一 `newListener`，杜绝两口漂移；测试：config env 映射 pin + `newListener` 明文 / TLS 握手 / mTLS 拒绝未持证客户端回归（覆盖 TLS 1.3 客户端握手先返回、读写时才见拒绝的已知行为）；docs/security.md §1 中英改为三监听口。
- [ ] 文档时效性清理（2026-10-18 用户令：很多文档已过时）：
      在本轮审核整改完成之后执行，避免与整改冲突。
      1) 通读 README / docs，找出与当前代码不符的描述（能力、配置项、命令、路径、版本）
      2) 以代码为准修正文档；**禁止为对齐文档去改业务代码**
      3) 已废弃但仍留在文档里的功能，明确标注「已移除」或删除，不要留成"看起来还能用"
      4) 命令行示例逐条跑一遍，跑不通的改掉或删掉
      5) 构建通过后 commit+push，回报改动文件清单
