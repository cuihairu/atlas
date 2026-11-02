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
- [ ] DOC-GIF（P2）：配置开关效果动态图，每开关 off→on 对比 + GIF
