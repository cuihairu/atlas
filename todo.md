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

## 待办

- [ ] API 稳定化与 v1.0：生产验证充分后冻结 REST/gRPC 契约发 v1.0（BLOCKED：待用户拍板与生产部署实证）
- [ ] 生产形状验收实跑：100/1k/5k 全规模 + ≥24h 浸泡 + 部署级演练（BLOCKED：待用户指定机器与排期，内部基础设施约束未解）

## 审核发现（已修复）

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

## 审核发现（进行中）

- [x] 本轮 diff（7a4f62f2..HEAD）复核未见问题 — 核实属实：范围 7a4f62f2..HEAD（HEAD=becd00b）仅 docs 变更、无 .go 改动，复核无新问题；go test ./...: exit 0；commit becd00b；todo.md:54

- [x] 审核发现：本轮 diff 未见问题（08bf1b7f..HEAD） — 核实属实：范围 08bf1b7f..HEAD（HEAD=becd00b）仅 docs 变更、无 .go 改动；范围内长代码行不换行问题（下一条发现）已由同范围 becd00b 修复，终态无问题；go test ./...: exit 0；docs build 绿；commit becd00b
- [x] 审核发现：代码块换行样式只设置在 pre 上，VitePress 默认给子元素 code 显式设置 white-space: pre 和 word-break: normal，导致长代码行仍不换行（docs/.vitepress/theme/custom.css:260） — 核实属实：VitePress 默认 vp-doc.css:312-314 给 code 设 white-space:pre/word-break:normal，原仅覆盖 pre 不生效；becd00b 增加 pre code 选择器（specificity 更高）已修复；go test ./...: exit 0；docs build 绿；commit becd00b；custom.css:260
- [x] 审核发现：本轮 diff 未见问题（3ec5e77c..HEAD） — 核实属实：范围 3ec5e77c..HEAD（HEAD=becd00b）仅 custom.css 一处（pre code 换行修复 + !important），无 .go 改动，复核无新问题；go test ./...: exit 0；docs build 绿；commit becd00b
- [x] 审核发现：本轮 diff 未见问题（becd00b..2ac4a9e） — 核实属实：范围 becd00b..2ac4a9e 仅 todo.md 变更（5 增 1 删）、无 .go 改动，复核无新问题；go test ./...: exit 0；commit 2ac4a9e；todo.md:59
- [x] 审核发现：本轮 diff 未见问题（2ac4a9e2..HEAD） — 核实属实：范围 2ac4a9e2..HEAD（HEAD=dde65b6）仅 todo.md 变更（1 增）、无 .go 改动，复核无新问题；go test ./...: exit 0；commit dde65b6；todo.md:60
