# TODO

需求清单（2026-11-02 整理）：每条需求一行，已交付项保留一条证据（commit）；叙事、符号、情绪措辞已删。审计/走查类记录见 docs/review/ 与 docs/审计-文档一致性.md。

## 已交付

- [x] 覆盖率 ≥98% 门禁（Makefile test-coverage 交叉覆盖口径 + codecov target 98%，storetest 剔除，终局 98.09%）— commit e2e112c
- [x] 注册生命周期：permanent/temporary 标记 + TTL 回收，显式删除入口 — commit 4ed7dae
- [x] 永久实例离线告警（permanent_offline，webhook + Warn 日志，temporary/维护豁免）— commit 4ed7dae
- [x] 2026-10-18 独立审核整改（维护窗口失败不遗留 maintenance、巡检旧快照 CAS 防覆盖、写失败不计转移、角色事件防回退投影）— commit 991bb32
- [x] Admin 监听口 TLS/mTLS（ATLAS_ADMIN_TLS_CERT/KEY/CLIENT_CA，Registry/Admin 同套 newListener 收敛）— commit 470f40d
- [x] 文档时效性清理（env/HTTP 路由/路径引用/release 口径四维比对）— commit 96e449d
- [x] 事件投递上限与死信（ATLAS_EVENT_MAX_DELIVERY，四适配器 .dl 镜像，至少一次语义+幂等验证）— commit 379b45d
- [x] 死信重放 CLI（cmd/dlq，四适配器 ReplayDeadLetters 最旧优先+发布成功才移除）— commit d2a99d9
- [x] Admin 审计持久化（internal/audit：mask 脱敏 + 有界队列 + postgres/jsonl sink + 90 天保留）— commit 6c65e63
- [x] 生产形状压测与长稳观测：设计定稿 + 工具交付（run.sh/soak.sh/loadgen/soakcheck/drills.sh，拓扑 deploy/docker-compose.ha.yaml）— commit bacac09
- [x] DEMO-001 五品类 demo 集（mmorpg/moba/slg/card/fps，复用 cmd/atlas-demoagents，deployments/demo/）— commit 51217cd
- [x] 契约 pin 守卫（REST 路由金清单 + gRPC 描述符比对 + /metrics 端点 pin，加/删契约即测试红）— commit 0ff5321

## 待办

- [ ] API 稳定化与 v1.0：生产验证充分后冻结 REST/gRPC 契约发 v1.0（BLOCKED：冻结决策与发版须用户拍板 + 缺生产环境部署验证实证；契约 pin 守卫已在仓）
- [ ] 生产形状验收实跑：100/1k/5k 全规模 + ≥24h 浸泡 + 部署级演练折入（BLOCKED：待用户指定机器与排期；docker100 BatchMode 拒 key、hermes 仅 win-worker 承接不了 Linux HA 栈）
- [x] DOC-GIF（P2）：配置开关效果动态图，每开关 off→on 对比 + GIF — commit ea27a9b（6 张 demo 舰队玩法开关 off→on 实录，接入 docs/config-center.md §7.1 + zh 镜像，docs build 绿）
- [x] CODE-LINT（P3）：接入 golangci-lint/errcheck 并纳入 `make lint`，防未检错误回归 — `make lint: 0 issues`；`go build ./...: ok`；`go test ./...: exit 0`

## 审核发现

- [x] 审核发现：临时房间注销仅将记录置为 offline，未删除服务器行和标签，与“注销删除行/标签”的实现描述不一致 — `go test ./cmd/atlas-demoagents/...: ok`
- [x] 审核发现：临时房间注销后仍保留 offline 服务器记录，demo 无法呈现声称的列表增减效果 — `go test ./cmd/atlas-demoagents/...: ok`
- [x] 审核发现：start_delay 延迟的是首个 close 时刻而非首个 open 时刻，README 错峰开局表述与实际房间注册时序不符 — `go test ./cmd/atlas-demoagents/...: ok`
- [x] 审核发现：roomLoop 的“注销成功后显式删除行”编排无集成测试，新测试仅覆盖 deleteJSON HTTP 辅助函数，注销失败跳过删除、删除失败降级路径均未验证 — commit 45fd98d25fecdd2b0a287fed93012462f1a566b4 cmd/atlas-demoagents/topology_test.go:378,398,415
- [x] 审核发现：deleteJSON 以字符串拼接构造 admin URL 且未对 ServerID 做 url.PathEscape（Server.Validate 仅校验非空），含 `/`、`?`、`#`、`..` 的 server_id 会错路由到其它 admin 端点；延续既有 postJSON 模式，配置可控、非攻击者可控 — commit 45fd98d25fecdd2b0a287fed93012462f1a566b4 cmd/atlas-demoagents/main.go:738 cmd/atlas-demoagents/topology_test.go:198
- [x] 审核发现：deleteJSON 关闭响应体前未 drain，与 crossserver/service.go:427 的“drain 以便连接复用”约定不一致，demo adminClient 的 keep-alive 连接无法复用 — commit 45fd98d25fecdd2b0a287fed93012462f1a566b4 cmd/atlas-demoagents/main.go:947,969 cmd/atlas-demoagents/topology_test.go:224
- [x] 审核发现：make lint 改为硬依赖本机 golangci-lint 二进制且未 pin 版本，本机缺失时 command not found、与 CI 的 v2.15.0 版本漂移时行为不一致 — commit 45fd98d25fecdd2b0a287fed93012462f1a566b4 Makefile:65-66
- [x] 审核发现：rabbitmq.Open 的 ExchangeDeclare 失败路径 `_ = a.Close()` 仅关闭 channel，`owns` 字段只写不读、amqp.Connection 从不关闭，错误路径连接泄漏（既有缺陷，本 diff 触及该行） — commit 45fd98d25fecdd2b0a287fed93012462f1a566b4 internal/event/rabbitmq/adapter.go:96,133,252 internal/event/rabbitmq/adapter_test.go:597
- [x] 审核发现：atlas-crossagent 的 bus/busRdb 由 /ctl/bus-reset（busReset）与 /ctl/bus-off（busDrop）两个并发 HTTP handler 读写且无锁保护，并发请求存在数据竞态（既有缺陷，本 diff 触及该闭包） — commit 45fd98d25fecdd2b0a287fed93012462f1a566b4 cmd/atlas-crossagent/main.go:398 cmd/atlas-crossagent/main_test.go:30
- [x] 审核发现：/readyz 将 err.Error() 直接字符串拼接进 JSON 而未转义，Ping 错误信息含引号/控制字符时产出畸形 JSON（既有缺陷，本 diff 仅改写 w.Write 丢弃语义） — commit 45fd98d25fecdd2b0a287fed93012462f1a566b4 cmd/atlas/main.go:864,869 cmd/atlas/main_test.go:408
- [x] 审核发现：本轮覆盖 0e19d5a64715..HEAD 全部 22 文件（2 commits：9a6df01 demo 房间注销修复、12fbd69 golangci-lint errcheck 接入）——安全注入/反序列化/越界未见问题，rows.Err() 均已检查，lint 门禁实测 0 issues — commit 45fd98d25fecdd2b0a287fed93012462f1a566b4 todo.md
