# TODO

需求清单（2026-11-05 整理）：每条需求一行；已交付项保留一条证据（commit hash + 测试结果）；叙事、符号、括注补充已删，历史明细见 git 历史与 docs/review/、docs/审计-文档一致性.md。

## 已交付

- [x] 覆盖率 ≥98% 门禁 — commit e2e112c；终局覆盖率 98.09%
- [x] 注册生命周期 permanent/temporary 标记 + TTL 回收 + 显式删除入口 — commit 4ed7dae
- [x] 永久实例离线告警 permanent_offline（webhook + Warn 日志）— commit 4ed7dae
- [x] 维护窗口失败不遗留 maintenance、巡检旧快照 CAS 防覆盖、写失败不计转移、角色事件防回退投影 — commit 991bb32
- [x] Admin 监听口 TLS/mTLS — commit 470f40d
- [x] 文档时效性清理（env/HTTP 路由/路径引用/release 口径比对）— commit 96e449d
- [x] 事件投递上限与死信 ATLAS_EVENT_MAX_DELIVERY（四适配器 .dl 镜像）— commit 379b45d
- [x] 死信重放 CLI cmd/dlq（最旧优先 + 成功才移除）— commit d2a99d9
- [x] Admin 审计持久化 internal/audit（mask 脱敏 + postgres/jsonl sink + 90 天保留）— commit 6c65e63
- [x] 生产形状压测与长稳观测工具（run/soak/loadgen/soakcheck/drills）— commit bacac09
- [x] 五品类 demo 集（mmorpg/moba/slg/card/fps）— commit 51217cd
- [x] 契约 pin 守卫（REST 金清单 + gRPC 描述符 + /metrics pin）— commit 0ff5321
- [x] 配置开关效果动态图（六张 off→on 实录接入 config-center.md）— commit ea27a9b；docs build 绿
- [x] golangci-lint/errcheck 接入 make lint — make lint: 0 issues；go test ./...: exit 0

- [x] 原生安装包四类进 rolling nightly（linux deb/rpm × amd64+arm64、windows exe zip + msi）— nightly run 43629997782 completed/success（head 40ece0e，6 job 全绿，含 gate 的 postgres/mysql/redis store 契约套件）；release `nightly` 资产 7 件：atlas-nightly-linux-amd64.deb 11234764B / arm64.deb 10033956B / x86_64.rpm 11601936B / aarch64.rpm 10365409B / windows-amd64.zip 11600817B / windows-amd64.msi 11358208B / SHA256SUMS 583B（prerelease，draft=false，不进 latest），六类必选资产全在。冒烟：amd64 二进制 serve + /healthz rc=0；deb/rpm 解包断言 /usr/bin/atlas 与 /etc/atlas/atlas.env；arch 头逐个断言（rpm Arch=x86_64/aarch64、deb Arch=amd64/arm64）；arm64 为 CGO_ENABLED=0 交叉编译但未执行冒烟，已在 release notes 标注。msi 腿本轮**通过**（非 BLOCKED 非 FAILED）：run 43627241820 的 LGHT0103 因 .wixobj 落在 build 而 payload 在 stage，已按 commit 0907e49 把 wixobj 改落 stage 与 payload 同目录（两种解析口径皆成立），重跑 43629997782 后 0 次 LGHT0103；本地 msiextract 得 Atlas/atlas.exe 33662464B + LICENSE + README.md + .env.example，且与 zip 内 atlas.exe 逐字节相同（sha256 bae30425…）——**不是空壳**（dark/wixl 退出 0 对空壳同样成立，结构解析无判别力，故 commit 40ece0e 增解包字节断言）；msi 仅结构验证，未安装。release 六值本地重算与 SHA256SUMS 全部一致；asset 清单见 nightly release notes。已修：f38059d 改为 hash-first 规范格式 `${sum}  ${f}` 并在 CI 内自校验，run 43661096382（head 8f2a5e3）6 job 全绿且 `sha256sum -c SHA256SUMS` 逐条 OK（aarch64.rpm / amd64.deb / arm64.deb / x86_64.rpm / windows-amd64.zip），SHA256SUMS 583B 现可机器校验。commit 链 39110bd..f38059d
## 待办

- [ ] API 稳定化与 v1.0：生产验证充分后冻结 REST/gRPC 契约发 v1.0（BLOCKED：待用户拍板与生产部署实证；2026-11-06 复核：所缺外部条件（用户拍板 + 生产部署实证）未解，保留 BLOCKED）
- [ ] 生产形状验收实跑：100/1k/5k 全规模 + ≥24h 浸泡 + 部署级演练（BLOCKED：待用户指定机器与排期，内部基础设施约束未解；2026-11-06 复核：外部条件（用户指定机器与排期）与内部基础设施约束（docker100 BatchMode 拒 key、hermes 仅 win-worker 承接不了 Linux HA 栈）均未解，保留 BLOCKED）

## 审核发现（已修复）

- [x] P0 applyMoved 重投丢行：重放 moved 事件解析到已迁入目标服的新行，upsert 空转后 DeleteCharacter 删掉唯一一行，目录字符永久丢失 — commit 3eda7c3（src.ServerID==target 短路，events.go:241；回归测试 TestApplyEventMovedRedeliveryKeepsRow + TestApplyEventMovedSameSourceAndTarget，旧码红新码绿；go build/vet ok、make lint 0 issues、go test ./internal/... ./sdk/... ./cmd/... 37 包 ok）
- [x] P1 extractIP 无条件信 X-Forwarded-For，Admin/Registry 白名单与限流桶键可伪造 — commit 62d887d（extractIP 加 trust 开关 + ATLAS_TRUST_FORWARDED_FOR opt-in（=1），三面接入：AdminAuth/RegistryAuth/RateLimiter；回归测试 auth_test/edges_test/handlers_gap_test 三处，build/vet/lint/37 包绿）
- [x] P1 ParseIPWhitelist 非法裸 IP 静默跳过 + main.go 两处吞错，白名单可缩成空后监听口全开 — commit b431312（非法条目返回 error + main 启动即退；回归测试 TestParseIPWhitelistVariants 断言错误；build/vet/lint/37 包绿）
- [x] P1 memory queue Close 与 submit 无同步，关闭后提交 send-on-closed panic — commit 4721ae3（closeMu 序列化 enqueue vs close + closed 标记，post-Close 走 inline 路径；TestCloseConcurrentWithSubmit 旧码 panic 新码绿 + TestSubmitAfterCloseInline；-race 37 包绿）
- [x] P1 registry 心跳促活无条件 UpdateServerStatus，可覆盖 health monitor 刚落地的 offline 判定 — commit cf145f7（两处促活改 UpdateServerStatusIf CAS，!applied 回读行上真值；TestHeartbeatReentryLostRace + faultServers 补 updateStatusIfFn；-race 37 包绿）
- [x] 配套：docs 双语（security.md env 表 + api.md 白名单语义）与前置 d312829（main 恢复全码树，保 619739d 分析文档 + 保留未提交依赖漂移） — docs commit 505099f，docs build 绿 81.05s
- [x] demo 临时房间注销后离线记录未删除，与实现描述不一致 — go test ./cmd/atlas-demoagents/...: ok
- [x] start_delay 延迟首个 close 而非首个 open，README 时序表述不符 — go test ./cmd/atlas-demoagents/...: ok
- [x] roomLoop 注销后删除行的编排缺集成测试 — commit 45fd98d
- [x] deleteJSON 构造 admin URL 未做 url.PathEscape — commit 45fd98d
- [x] deleteJSON 关闭响应体前未 drain — commit 45fd98d
- [x] make lint 硬依赖本机 golangci-lint 且未 pin 版本 — commit 45fd98d
- [x] rabbitmq ExchangeDeclare 失败路径连接泄漏 — commit 45fd98d
- [x] atlas-crossagent bus/busRdb 并发读写无锁 — commit 45fd98d
- [x] /readyz 错误信息直拼 JSON 未转义 — commit 45fd98d
- [x] rabbitmq Close 在 ch.Close 失败时跳过 conn.Close 泄漏连接 — commit adcebe2
- [x] amqpDial 注入缝缺 ExchangeDeclare 失败路径回归覆盖 — commit 0a49dca
- [x] readyzHandler 忽略 json.Marshal 错误 — commit 4a45520
- [x] TestRoomLoopDegradesOnDeleteFailure 固定 sleep 时序敏感 — commit af5fc68
- [x] gofmt 对齐回归无门禁拦截（Makefile/CI 补 gofmt 步骤）— commit fa2cc41
- [x] gofmt 门禁目录列表与 build/vet/test 覆盖不对称 — commit d6aa60f
- [x] .golangci.yml 与 make lint 双轨漂移 — commit d6aa60f
- [x] CI Gofmt 步骤误用 Makefile 的 $$ 转义致门禁恒红 — commit 0ed102d
- [x] Makefile gofmt -l 在 go list 失败时静默放行 — commit 0ed102d
- [x] NATS 默认 URL 回退测试未走导出 Open — commit e24eec4；go test -race ./internal/event/nats/: ok
- [x] mock connect 未断言连接选项 — commit e24eec4
- [x] NATS 测试成功路径泄漏连接 — commit 103ecac
- [x] 复核归纳失准：BLOCKED 项漏列内部基础设施约束 — go test ./...: exit 0
- [x] Docker Image dashboard 腿红灯：8f2a5e3（wip: session checkpoint）把 git stash 冲突标记连同 dashboard/package.json 与 package-lock.json 一起提交进 main（package.json 2 组、package-lock.json 23 组，共 48 处 `<<<<<<<`/`=======`/`>>>>>>>`）→ 锁文件不是合法 JSON，`npm ci` 报 EUSAGE "can only install with an existing package-lock.json ... with lockfileVersion >= 1"，dashboard/Dockerfile:9 失败（run 43660236959 failure）；根因不在 node 版本（node 24 与 node 22 同走 npm 10.x，与绿基线无关），是文件损坏。已按冲突前基线 e8b4e14（docker run 43633110510 success、lock 可解析 341 包）还原两文件，commit 68d49de → Docker Image 43663245589 与 ci 43663245671 均 completed/success（head 68d49de）；本地验收 `npm ci && npm run build && npm test` exit 0；全库 4 个 package-lock.json 均可解析、0 残留标记、`go mod verify` all modules verified。附带核到 docs 同时存在 package-lock.json 与 pnpm-lock.yaml，但 docs.yml:39 用 `npm ci`，pnpm-lock.yaml 无任何 workflow 引用，不在红灯路径，未动 — commit 68d49de

## 审核发现（进行中）

- [x] 本轮 diff（7a4f62f2..HEAD）复核未见问题 — 核实属实：范围 7a4f62f2..HEAD（HEAD=becd00b）仅 docs 变更、无 .go 改动，复核无新问题；go test ./...: exit 0；commit becd00b；todo.md:54

- [x] 审核发现：本轮 diff 未见问题（08bf1b7f..HEAD） — 核实属实：范围 08bf1b7f..HEAD（HEAD=becd00b）仅 docs 变更、无 .go 改动；范围内长代码行不换行问题（下一条发现）已由同范围 becd00b 修复，终态无问题；go test ./...: exit 0；docs build 绿；commit becd00b
- [x] 审核发现：代码块换行样式只设置在 pre 上，VitePress 默认给子元素 code 显式设置 white-space: pre 和 word-break: normal，导致长代码行仍不换行（docs/.vitepress/theme/custom.css:260） — 核实属实：VitePress 默认 vp-doc.css:312-314 给 code 设 white-space:pre/word-break:normal，原仅覆盖 pre 不生效；becd00b 增加 pre code 选择器（specificity 更高）已修复；go test ./...: exit 0；docs build 绿；commit becd00b；custom.css:260
- [x] 审核发现：本轮 diff 未见问题（3ec5e77c..HEAD） — 核实属实：范围 3ec5e77c..HEAD（HEAD=becd00b）仅 custom.css 一处（pre code 换行修复 + !important），无 .go 改动，复核无新问题；go test ./...: exit 0；docs build 绿；commit becd00b
- [x] 审核发现：本轮 diff 未见问题（becd00b..2ac4a9e） — 核实属实：范围 becd00b..2ac4a9e 仅 todo.md 变更（5 增 1 删）、无 .go 改动，复核无新问题；go test ./...: exit 0；commit 2ac4a9e；todo.md:59
- [x] 审核发现：本轮 diff 未见问题（2ac4a9e2..HEAD） — 核实属实：范围 2ac4a9e2..HEAD（HEAD=dde65b6）仅 todo.md 变更（1 增）、无 .go 改动，复核无新问题；go test ./...: exit 0；commit dde65b6；todo.md:60
- [x] 审核发现：本轮 diff 未见问题（dde65b64..HEAD） — 核实属实：范围 dde65b64..HEAD（HEAD=04148f7）仅 todo.md 变更（1 增）、无 .go 改动，复核无新问题；go test ./...: exit 0；commit 04148f7；todo.md:61
- [x] 审核发现：本轮 diff 未见问题（04148f7c..HEAD） — 核实属实：范围 04148f7c..HEAD（HEAD=957dafd）仅 todo.md 变更（1 增）、无 .go 改动，复核无新问题；go test ./...: exit 0；commit 957dafd；todo.md:62
- [x] 审核发现：本轮 diff 未见问题（957dafdb..HEAD） — 核实属实：范围 957dafd..HEAD（HEAD=7aa4e21）仅 todo.md 变更（1 增）、无 .go 改动，复核无新问题；go test ./...: exit 0；docs build 绿；commit 7aa4e21；todo.md:63
- [x] 审核发现：本轮 diff 未见问题（7aa4e216..HEAD） — 核实属实：范围 7aa4e216..HEAD（HEAD=1f44f9d）仅 todo.md 变更（1 增，即上一轮结论行）、无任何代码改动，复核无新问题；证据：git diff --stat 7aa4e216..HEAD → "todo.md | 1 +，1 file changed, 1 insertion(+)"；HEAD=1f44f9d962646a5c3e794629959f387e1c96b691；commit 1f44f9d；todo.md:64
- [x] 审核发现：本轮 diff 未见问题（1f44f9d..HEAD） — 核实属实：范围 1f44f9d..HEAD（HEAD=29960f0）仅 todo.md 变更（1 增，即上一轮结论行）、无任何代码改动，复核无新问题；证据：git diff --stat 1f44f9d..HEAD → "todo.md | 1 +，1 file changed, 1 insertion(+)"；HEAD=29960f025bfea68ac9f35433174f459d3fa0c993；commit 29960f0；todo.md:65

## Codex 深审发现（2026-11-12，luna profile 近5 commit 审核，0C/3M/2m，待修）

- [x] [M] Load() 无效数值/时长/比率配置改显式启动失败（internal/config/config.go:379）——已修：Load() 改回 (Config, error)，设了但解析失败或越域的键聚合为单条 `invalid configuration: ...` 错误，atlas 启动记日志 exit 1（cmd/atlas/main.go:71）、dlq 返回 2（cmd/dlq/main.go:75）；新增边界值测试（0s 时长、PG_MIN_CONNS=0、ratio "1"、sink "off"、告警比率>1 仍合法）与聚合错误测试（一次重启报出全部错键）；go test ./... 39 包全绿；commit 0708a67
- [ ] [M] 定义并测试 Store.Close() 后提交与既有队列写的关系（internal/store/memory/queue.go:502）——关闭后 inline 写入不保证同目标顺序，CAS 可能先执行并失败
- [x] [M] 修正转发头仅作用于 REST 监听口的文档表述（docs/api.md:963、docs/security.md:214、docs/zh/security.md:124）——已修共 8 行：所列三处 + docs/zh/api.md:996/998 镜像 + docs/api.md 与 docs/zh/api.md 采样段，统一为「仅 REST 生效；gRPC 一律取传输层对端地址、从不读 XFF/X-Real-IP，代理后 gRPC 白名单看到的是代理 IP」；采样比率 (0,1] 外同步改为「启动即报错」（与 0708a67 行为一致）；commit 62730ba
- [x] [m] 提交或还原 go.mod/go.sum 与 npm/pnpm 依赖漂移（go.mod:33）——已解：兄弟会话 checkpoint 8f2a5e3 收编全部依赖漂移并修复 UU 索引；本轮回检 git status 仅含本会话 8 个改动文件，正常 commit/push 恢复。**但 8f2a5e3 引入了新缺陷**：dashboard/package.json 与 package-lock.json 带 git stash 冲突标记提交进 main（48 处），Docker Image dashboard 腿红灯（run 43660236959 failure），已按 68d49de 还原至冲突前基线 e8b4e14（见「审核发现（已修复）」）
- [x] [m] 后续证据锚定 commit + 工作树清洁度 + 依赖锁定状态（todo.md:30、31）——本轮结论锚定：代码 0708a67、文档 62730ba、依赖基线 8f2a5e3；go test ./... 39 包全绿基于该基线
