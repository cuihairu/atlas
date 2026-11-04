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

- [ ] API 稳定化与 v1.0：生产验证充分后冻结 REST/gRPC 契约发 v1.0（BLOCKED：冻结决策与发版须用户拍板 + 缺生产环境部署验证实证；契约 pin 守卫已在仓；2026-11-04 复核：仍缺外部条件，保留 BLOCKED 不重试）
- [ ] 生产形状验收实跑：100/1k/5k 全规模 + ≥24h 浸泡 + 部署级演练折入（BLOCKED：待用户指定机器与排期（外部条件）；docker100 BatchMode 拒 key、hermes 仅 win-worker 承接不了 Linux HA 栈（内部基础设施约束）；2026-11-04 复核：外部条件与内部基础设施约束均未解，保留 BLOCKED 不重试）
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
- [x] 审核发现：TestOpenDefaultsToLocalhostWhenURLOmitted 重构后调用的是包内未导出 `open`（adapter_test.go:430）而非导出 `Open`——生产入口 `Open → open(opts, nats.Connect)` 的接线与 DefaultURL 回退在真实入口处已无任何测试覆盖：若 Open 被改为绕过 open、换用其它 connect 或在其复制路径丢弃回退，本测试仍 PASS（TestOpenConnectFailureAndAccessors 仅覆盖显式关闭端口 URL），测试名已名不副实（低危）— 修复：Open 经包级 `defaultConnect` 注入缝（adapter.go:83-89），测试改走导出 `Open` 并断言回退 URL；删除回退后 empty 子测试 FAIL（connect URL = "", want "nats://localhost:4222"）；恢复后 PASS — internal/event/nats/adapter_test.go:417-457
- [x] 审核发现：注入的 mock connect 丢弃全部选项（`_ ...nats.Option`，adapter_test.go:430），`open` 传入的 `nats.Name("atlas")`/`nats.Timeout(5s)`（adapter.go:92）未被任何测试断言——本次新增注入缝本可轻易断言选项却未用（既有缺口，非本 diff 引入，低危）— 修复：注入缝改为收集 `nats.Option` 并套用到 `nats.Options` 断言 `Name=="atlas"`、`Timeout==5s`；`go test -race ./internal/event/nats/: ok`；`make lint: 0 issues`；`go test ./...: exit 0` — internal/event/nats/adapter.go:92 adapter_test.go:417-457

## 审核发现（12fbd699..HEAD）

- [x] 审核发现：Adapter.Close() 在 `a.ch.Close()` 返回错误时提前 return，跳过 `a.conn.Close()`——本 diff 为“修复 ExchangeDeclare 失败路径连接泄漏”而新增的 conn 字段，在其自身的 ch.Close 错误分支仍会泄漏 broker 连接（ch.Close 已失败时 conn 大概率也处于故障态，实际影响偏小，但属资源管理缺陷）— `go test ./...: ok`；`make lint: 0 issues`；commit adcebe27379c258f455ff35eda261bc5c6fba18f internal/event/rabbitmq/adapter.go:258-266 internal/event/rabbitmq/adapter_test.go:604,617
- [x] 审核发现：`amqpDial` 包变量注释明确其用途是“让 ExchangeDeclare 错误路径（连接必须被关闭）可测试”，但该范围内无任何测试改写 amqpDial 或驱动 Open() 走 ExchangeDeclare 失败分支；新增的 TestCloseReleasesConnection 仅直接对手工构造的 adapter 调 Close()，未覆盖这个变量本要暴露的回归路径（测试缺口）— `go test ./...: ok`；`make lint: 0 issues`；commit 0a49dca80027241df25ec70aebdb7210e2957de7 internal/event/rabbitmq/adapter.go:54-75 internal/event/rabbitmq/adapter_test.go:591,614,652
- [x] 审核发现：readyzHandler 忽略 json.Marshal 错误（`body, _ :=`），当前 map[string]string 载荷不会失败，但若载荷类型变更，会在 503 下写出空体而非合法 JSON，违背本改动“确保输出恒为合法 JSON”的初衷（防御性缺陷，低危）— `go test ./...: ok`；`make lint: 0 issues`；commit 4a455207a70ef6fd3de4fc4040fbbcbb9ee56d23 cmd/atlas/main.go:866-871
- [x] 审核发现：TestRoomLoopDegradesOnDeleteFailure 断言 `s.register >= 2` 依赖 runRoom 内 30ms 的单次 settle 窗口（recycle=0 时循环重注册），负载下时序敏感、存在偶发 flake 风险；建议以轮询直至 register>=2 或超时判定替代固定 30ms — `go test ./cmd/atlas-demoagents/... -count=100: ok`；`go test ./...: ok`；`make lint: 0 issues`；commit af5fc68e813efc7f34e6609239721939f9da5027 cmd/atlas-demoagents/topology_test.go:352-375
- [x] 审核发现：本轮 diff 12fbd699..HEAD（5 commits：45fd98d 修复、d38b4a5/b0b16c9/ea27a9b/3a93697 文档）覆盖 22 文件——busState 互斥重构（bus/busRdb 竞态修复）、deleteJSON 的 url.PathEscape（路径注入修复）、响应体 drain、rabbitmq conn 关闭（ExchangeDeclare 泄漏修复）、/readyz JSON 转义均实现正确；docs/Makefile 无安全问题。以上 4 条为本次新增发现，其余未再见问题 — `go test ./...: ok`；`make lint: 0 issues`；commit b9f1e5e7e9bcb777bc11286cbb07a184f3b16bc4 todo.md

## 审核发现（3a93697f..HEAD）

- [x] 审核发现：fakeChannel 新增的 `errClose error` 与 `errExchangeDeclare error` 字段破坏了 struct 字段对齐（gofmt 期望整个字段块右移对齐到最长字段名），`gofmt -l internal/event/rabbitmq/adapter_test.go` 标记该文件；`.golangci.yml` 仅启用 errcheck/govet、未启用 gofmt linter，`make lint` 拦截不到——需按 gofmt 重新对齐该字段块 — `gofmt -l: clean`；`go test ./...: ok`；`make lint: 0 issues`；commit 60f5b39c95d105b174683783446e9e35572e8447 internal/event/rabbitmq/adapter_test.go:47-49


## 审核发现（dd8d62d4..HEAD）

- [x] 审核发现：gofmt 对齐修复（60f5b39）仅修正了 fakeChannel 字段块本身，`.golangci.yml` 仍只启用 errcheck/govet、未启用 gofmt linter，且 Makefile 与 .github/workflows 均无 gofmt 检查目标——同类 struct 字段对齐回归仍无法被 `make lint`/CI 自动拦截（测试缺口，低危）— `make lint: 0 issues`；`go test ./...: ok`；`go build ./...: ok`；commit fa2cc410d678c7beba20a0bb53e004c51bc948b5 Makefile:65-73 .github/workflows/ci.yml:91-99


## 审核发现（964074ab..fc9d5646）

- [x] 审核发现：gofmt 门禁在 Makefile 与 CI 两处硬编码同一目录列表 `./cmd ./internal ./sdk ./api ./examples ./deploy`（重复且无单一来源），而 `go build/vet/test ./...` 均用 `./...`；若日后在列表外新增 Go 包（含仓库根）gofmt 门禁将静默漏检，与 build/vet/test 覆盖不对称——当前所有 .go 文件均在列表内（`git ls-files '*.go'` 越界 0 个），属潜在覆盖盲区/维护陷阱（低危）— `make lint: 0 issues`；`go test ./...: ok`；`go build ./...: ok`；commit d6aa60f524760d427998cea9b2e393087d4b7fe0 Makefile:64-66 .github/workflows/ci.yml:91-96
- [x] 审核发现：`.golangci.yml` 仍未启用 gofmt linter（仅 errcheck/govet），gofmt 检查仅存在于 make lint/CI 的自定义步骤；直接运行 `golangci-lint run ./...` 或 IDE 内 lint 不会返回格式反馈，与 make lint/CI 形成双轨机制、存在漂移风险（低危；本轮已在 make lint/CI 补齐拦截，非阻断）— `make lint: 0 issues`；`go test ./...: ok`；`go build ./...: ok`；commit d6aa60f524760d427998cea9b2e393087d4b7fe0 .golangci.yml:3-5 Makefile:64-66


## 审核发现（964074ab..HEAD）

- [x] 审核发现：`.github/workflows/ci.yml:93` Gofmt 步骤误用 Makefile 的 `$$` 转义——该 run 块是原始 bash 环境，`$$` 被解释为进程 PID 而非 `$` 转义，`$$(gofmt -l $$(go list ...))` 不构成命令替换、嵌套 `$$(` 直接触发 bash 语法错误（实测 `syntax error near unexpected token '('`），步骤每次运行都 exit 1 并打印 `gofmt needed for:` + PID 垃圾，无论 gofmt 是否干净都使 CI 门禁恒定失败；同一逻辑在 Makefile:65 正确（make 会把 `$$` 展开为 `$`），CI 此处应改用单 `$` — `bash 复现原脚本: syntax error + exit 1；修复后: exit 0；make lint: 0 issues`；commit 0ed102d2ac73e3e8ace3444e67d3bd2e4a5f495d .github/workflows/ci.yml:91
- [x] 审核发现：`Makefile:65` 的 `gofmt -l $(go list -f '{{.Dir}}' ./...)` 在 `go list` 无输出（包加载/编译失败）时 `gofmt -l` 收不到任何参数而改读 stdin——非交互（CI/`/dev/null`）下静默返回 exit 0（实测无输出、放行），掩盖本应拦截的格式回归；交互式 `make lint` 下则挂起等待 stdin（低危，健壮性） — `注入 go list 失败: 修复后 exit 1；make test: exit 0`；commit 0ed102d2ac73e3e8ace3444e67d3bd2e4a5f495d Makefile:64
- [x] 审核发现：TestOpenDefaultsToLocalhostWhenURLOmitted 的 errEmpty.Error()==errDefault.Error() 恒等断言无法检测包级 DefaultURL 回退被删除——空 URL 会落入 nats.go 自身默认 nats://127.0.0.1:4222，与包默认 nats://localhost:4222 均为环回主机、无监听时错误串完全相同（实测均 "nats: no servers available for connection"）；且本环境有服务监听 4222 时测试在 427-430 行提前 return、根本不走该比较，未真正验证回退契约（低危）— 重构为注入 connect 并断言实际解析 URL：删除回退后 empty 子测试 FAIL（connect URL = "", want "nats://localhost:4222"）；回退存在时 PASS — internal/event/nats/adapter_test.go:427-439
- [x] 审核发现：测试成功路径（默认端口有 JetStream 服务）丢弃两个 *Adapter 并直接 return 未调用 Close()，泄漏两条 NATS 连接及适配器 goroutine 至进程结束；本环境 4222 有服务故每次运行均泄漏（低危）— 重构后测试不再打开真实连接（mock connect 返回 err），无泄漏；go test ./... exit 0 — internal/event/nats/adapter_test.go:427-439

## 审核发现（e24eec495238..2cde877）

- [x] 审核发现：第二条 BLOCKED 项的「2026-11-04 复核：仍缺外部条件」把继续阻滞归纳为「外部条件」，但该行阻滞清单除「待用户指定机器与排期」（外部）外，还含「docker100 BatchMode 拒 key、hermes 仅 win-worker 承接不了 Linux HA 栈」等内部基础设施约束——归纳失准，复核说明未完整反映实际阻滞来源（低危，文档准确性）— 已修正 todo.md:23 复核说明为「外部条件与内部基础设施约束均未解」；`go test ./...`: exit 0；`make lint`: 0 issues
