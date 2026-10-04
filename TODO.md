# TODO

> 每一项都是一个可独立提交的原子任务：完成后打勾并注明 commit，测试 / 门禁
> 全绿才提交推送（fetch + rebase origin/main，禁 tag / release / force push）。
>
> 当前批次：**工程落地清单（P1）**——从 docs/roadmap.md「v0.2+ 候选方向」与
> docs/performance.md §7 立档项加载，按「正确性优先于 QPS」排序：
> Discovery 读路径管线化、故障模式回归套件打头。
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
> 最后一个「Atlas 副本宕机」是部署级演练，以清单形式保留在 ha.md 不自动化。

- [ ] 游戏服网络分区：心跳停止 → suspect → offline 推进 + 客户端列表不抖动
      （lifecycle §3-§4 语义自动化）
- [ ] 游戏服重启 / 心跳延迟：重注册幂等恢复；3:1:6 节奏容忍抖动
      （lifecycle §4.2）
- [ ] 重复注册 / 重复迁移：upsert 幂等；迁移幂等可重放（migration §7）
- [ ] Redis 数据丢失（flushall）：档案仍在 PG，心跳重填，无残留 stale 视图
      （data-model §6）
- [ ] PG 短暂不可用：连接池自愈后写路径恢复，读路径不阻塞
      （performance §4）
- [ ] 迁移中途失败：先复制后切换再清理，任意一步失败可回滚 / 重放
      （migration §7）
- [ ] 巡检任务：跑绿后把用例接进 CI（ci.yml 同包内自然纳入）

---

## 待命（条件触发，不再展开——提升即从 roadmap 择优）

- [ ] 状态三态显式化（Desired / Observed / Effective）——概念已定稿
      （lifecycle §0 / architecture §4.4），代码字段命名演化只在 v1.0
      API 冻结窗口做，避免破坏契约
- [ ] Migration Controller 独立模块——概念边界已定（migration §9），
      代码拆分只在独立扩缩容有真实需求时做

---

## 归档（全部已完成，逐段压缩；细节与 commit 见 git log）

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

### 可观测性深化 · 首期（2026-10-03）✅

- [x] X-Request-ID 贯通三监听口（生成/沿用/回显/访问日志，健康路径免打扰）
- [x] `atlas_directory_write_duration_seconds{op}` 目录写路径延迟指标

### 跨服 ID 体系与玩法类型表（2026-10-04）✅

- [x] config-center §2.1 跨服 ID 两态规范（配置态 Atlas 托管 / 运行时实例 ID 只规范格式不生成不存储）
- [x] §2.2 七类玩法类型枚举 + 类型表进 Spec 第五段（Normalize / Validate / 契约夹具 / dashboard 卡片）