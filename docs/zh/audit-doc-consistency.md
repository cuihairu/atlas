
# 文档与源码一致性审计报告（2026-10-08 合并收口）

## 审计范围与方法

- **文档面**：README.md、docs/index.md、api.md、api-quickstart.md、architecture.md、topology.md、ha.md、sync.md、concepts.md、lifecycle.md、realms-shards.md、migration.md、scenarios.md、server-config.md、config-center.md、operations.md、data-model.md、security.md、security-audit.md、apisix.md、performance.md、benchmarks.md、roadmap.md、sdk-{go,js,python,java,csharp,cpp}.md——全站 25 页分组全覆盖。
- **方法**：七组并行取证（架构/配置/API/概念生命周期/README/安全/SDK），每条可验证技术主张双向 file:line 对码；否定性结论（「X 不存在」）先验证搜索树（全仓 grep 含 internal/ cmd/ sdk/ examples/ plugins/ dashboard/）；release/端点数等外部主张以 `gh release list`、`git ls-remote` 与路由表逐条计数验证。
- **口径**：以源码为真相。三类差异——「文档超前于码」（描述了不存在的实现）、「码超前于文档」（实现存在文档缺失）、「描述不符」（两说并存且以码为准的一侧不符）。修复原则：禁反向改码。
- **修复进度**：本档为审计记录（③）；修文档批（④）已于 2026-10-08 四批执行完毕，46 行逐条勾销——状态与 commit 对照见文末「修文档批执行记录」（F1「不符（兼超前）」原计数漏入合计，本次一并更正为 46）。

## 总计数与 P0 清单

| 类别 | 条数 |
| --- | --- |
| 文档超前于码 | 10 |
| 码超前于文档 | 12 |
| 描述不符 | 24 |
| **合计** | **46** |

**P0 清单（9 条）**：

1. **OP-1** config-center.md:407 — 事件总线默认值写反：文档称 `ATLAS_EVENT_ADAPTER=redis` 为默认，码默认 `http`（config.go:279）。
2. **ARC-A1** topology.md:26、:55 — Prometheus 抓取端口写错：文档 `:8080/metrics`，实际 `/metrics` 挂 admin 口 `:8082`（main.go:535、config.go:266）。
3. **API-A1** api.md:171-172 — Discovery 声称支持 `language`/`game_mode` 过滤，`ServerFilter` 无此维度、handler 不解析，未知参数被静默忽略。
4. **F1** apisix.md 多处 — 「Atlas 消费 `X-Atlas-Player-ID`」不成立：全仓仅网关注入侧命中，Atlas 侧账号一律来自 `?account_id=`/路径/body 显式参数。
5. **F2** security.md:33 — RBAC operator 角色声称受限权限集，实际 operator 仅被解析未被强制，权限等同 admin；唯一强制是 viewer 只读。
6. **ID-1** migration.md:83 — 转服流程声称 `PATCH /v1/directory/characters/{id}` 改 `server_id` 完成索引迁移：该字段实为行定位符，`CharacterPatch` 无 ServerID，迁移能力不存在。
7. **ID-2** migration.md:216 — 迁移「进度游标、断点续传」不存在：Migration 无 progress 字段，Store 仅 Create/Get/UpdateStatus。
8. **ID-3** migration.md:220-229 — 「先复制→校验→切换→清理」自动编排不存在：唯一状态推进是 rollback，migrating/verifying/completed 无可达路径。
9. **ID-4** scenarios.md:96-97 — 维护窗口创建 curl 用 `POST /v1/admin/maintenance-windows`（不存在，404），实际为 `POST /v1/admin/servers/{id}/maintenance-window` 且 body 不带 server_id。

## 差异总表（七组，P0 → P2）

### 组一 · 配置 / 运维 / 数据模型（server-config / config-center / operations / data-model）

| ID | 文档页:行 | 主张 | 源码证据 | 类别 | 严重度 |
| --- | --- | --- | --- | --- | --- |
| OP-1 | config-center.md:407 | 「`ATLAS_EVENT_ADAPTER=redis`（默认，生产推荐）」 | config.go:89,279 默认 `http`；五适配器均存在 | 不符 | **P0** |
| OP-2 | data-model.md:34-52 | servers 表 DDL 无 `source` 列 | migrations/0004_servers_source.sql:12；model.go:182 | 码超前 | P1 |
| OP-3 | server-config.md:166-175 | 配置文件字段表缺 `metadata` | serversconfig.go:45,182 透传入库 | 码超前 | P2 |
| OP-4 | server-config.md:63 | 心跳响应示例仅 `server_id`/`status` | handlers.go:327-331 另含 `next_heartbeat_in` | 不符 | P2 |
| OP-5 | data-model.md:3 | 「PostgreSQL + Redis 双存储」未提可选与默认 | config.go:270 默认 `memory`；main.go 仅接 memory/postgres | 码超前 | P2 |

### 组二 · 架构 / HA / 同步 / 拓扑（architecture / ha / sync / topology）

| ID | 文档页:行 | 主张 | 源码证据 | 类别 | 严重度 |
| --- | --- | --- | --- | --- | --- |
| ARC-A1 | topology.md:26、:55 | Prometheus 抓 `:8080/metrics` | main.go:535 `/metrics` 挂 admin 口 :8082；sync.md:285 互证 | 不符 | **P0** |
| ARC-A2 | topology.md:35 | public :8080「只读」 | Directory 写端点挂 public 口（handlers.go:157,161,162） | 不符 | P1 |
| ARC-A3 | topology.md:35 | public/registry 行未提 crossserver 拉取 | 双口均挂（handlers.go:121,139-142） | 码超前 | P2 |
| ARC-A4 | architecture.md:303 | 令牌桶限流「加一个中间件就够了」（读作待建） | ratelimit.go:1-34 + main.go:453-470 已接线 | 不符 | P2 |

### 组三 · API 参考（api.md / api-quickstart.md）

| ID | 文档页:行 | 主张 | 源码证据 | 类别 | 严重度 |
| --- | --- | --- | --- | --- | --- |
| API-A1 | api.md:171-172 | Discovery 支持 `language`/`game_mode` 过滤 | ServerFilter（store.go:22-31）无此维度；handlers.go:360-367 不解析 | 超前 | **P0** |
| API-A2 | api.md:226-227 | 详情响应字段 `realm`/`shard` | json tag 为 `realm_id`/`shard_id`（model.go:165-166） | 不符 | P1 |
| API-A3 | api.md:706 | proto `rpc Unregister(...) returns (OkResponse)` | atlas.proto:99 实为 `UnregisterResponse` | 不符 | P1 |
| API-A4 | api.md:749 | `409 ALREADY_REGISTERED→AlreadyExists` 映射 | 全仓零命中；api.md:835 自注「保留给未来」此处未标 | 超前 | P1 |
| API-A5 | api.md:333 | 目录写示例 `"server_id": 1001`（数字） | ServerID string（model.go:302），数字即 400 | 不符 | P1 |
| API-A10 | api.md（无小节） | migrations 4 条 REST 端点缺位于正文与索引表 | handlers.go:184-187 实证存在 | 缺失（码超前） | P1 |
| API-A6 | api.md:209 | 默认只过滤 offline/disabled | `Visible()`=online/maintenance/suspect（model.go:109-115） | 不符 | P2 |
| API-A7 | api.md:571 | 窗口示例含 `"previous_status": ""` | omitempty 空值不序列化（model.go:421） | 不符 | P2 |
| API-A8 | api.md:51-56 | register 响应仅 `server_id`/`status` | handlers.go:289-296 另附 `crossserver_config{version,hash}` | 缺失（码超前） | P2 |
| API-A9 | api.md:674 | 「五个官方 SDK」后列六项 | sdk/ 恰 6 语言，六处头注入入口实证 | 不符 | P2 |

### 组四 · README / 首页 / 性能 / 基准 / roadmap（README / index / performance / benchmarks / roadmap）

| ID | 文档页:行 | 主张 | 源码证据 | 类别 | 严重度 |
| --- | --- | --- | --- | --- | --- |
| R1 | README.md:360-365 | 「对外发布两个 release」（表仅 v0.1.0/v0.1.1） | `gh release list`：v0.1.2 已发布（Latest，2026-10-03） | 码超前 | P1 |
| R2 | roadmap.md:24-29 | 同 R1 口径 | 同 R1 | 码超前 | P1 |
| R3 | roadmap.md:43 | 「Grafana 概览看板（v0.1.3 交付）✅」 | 全仓无看板实体；topology.md 仅外接指引 | 超前 | P1 |
| R4 | roadmap.md:55 | 「service/handler 覆盖率 >80% 门禁」 | ci.yml 无阈值失败步骤；codecov.yml informational | 不符 | P2 |
| R5 | README.md:230-254 | 项目结构树缺 `internal/tracing/` | tracing/ 已落地（0fe965d/f5359e2） | 码超前 | P2 |
| R6 | roadmap.md:137-173 | 同 R5 | 同 R5 | 码超前 | P2 |
| R7 | index.md:51-57 | 「看到三行 listening」 | 默认配置四行（含 gRPC :9090，main.go:659-669） | 不符 | P2 |

### 组五 · 安全 / 审计 / APISIX（security / security-audit / apisix）

| ID | 文档页:行 | 主张 | 源码证据 | 类别 | 严重度 |
| --- | --- | --- | --- | --- | --- |
| F1 | apisix.md:7-8,23,36-37,44-49,101,123 | Atlas 消费 `X-Atlas-Player-ID`（粘滞/展示） | 全仓仅注入侧 plugins/apisix/atlas-auth.lua:7,12,29 命中；Atlas 账号来自 `?account_id=`/路径/body（handlers.go:500,542；routing/service.go:33,453） | 不符（兼超前） | **P0** |
| F2 | security.md:33 | operator 角色受限权限集 | RoleOperator 无强制分支；唯一强制是 viewer 只读（auth.go:146-157；grpc/auth.go:151-155）——operator 实际等同 admin | 不符 | **P0** |
| F3 | security.md:71 | 「审计在认证之内、限流之外」 | 实链路 limiter→auth→audit（main.go:536-559）：限流在审计外层，被限流请求不入审计环 | 不符 | P2 |
| F4 | security.md:3 | 「四层防护均可选启用」 | 审计默认开启（config.go:296）；:47 自注矛盾 | 不符 | P2 |
| F5 | security.md:49,61 | actor 固定两段式 `role:key指纹` | mTLS 时为三段（auth.go:41-49 追加证书指纹） | 不符 | P2 |

### 组六 · 六语言 SDK（sdk-*.md）

| ID | 文档页:行 | 主张 | 源码证据 | 类别 | 严重度 |
| --- | --- | --- | --- | --- | --- |
| GO-1 | sdk-go.md:19 | env `ATLAS_REGISTRY_TOKEN`（单数） | config.go:76 为 `ATLAS_REGISTRY_TOKENS`（其余四语言页均复数） | 不符 | P1 |
| JS-1 | sdk-js.md:182 | 「parseServer 等转换函数可独立使用」 | sdk/js/src/index.ts 无此导出 | 超前 | P1 |
| GO-2 | sdk-go.md:67-69 | 「API 一览五组方法」 | Go SDK 另有 config-center 组（config.go:90,155），六页均未提 | 缺失（码超前） | P2 |
| CPP-1 | sdk-cpp.md:29-33 | Options 示例无 `admin_api_key` | client.hpp:50 字段存在，其余五语言页均有 | 缺失（码超前） | P2 |

### 组七 · 概念 / 生命周期 / 分服 / 迁移 / 场景（concepts / lifecycle / realms-shards / migration / scenarios）

| ID | 文档页:行 | 主张 | 源码证据 | 类别 | 严重度 |
| --- | --- | --- | --- | --- | --- |
| ID-1 | migration.md:83 | PATCH 改 `server_id` 完成索引迁移 | server_id 是定位符（handlers.go:617-625,635）；CharacterPatch 无 ServerID（store.go:41-48）；SQL UPDATE 不碰 server_id | 超前 | **P0** |
| ID-2 | migration.md:216 | 「进度游标、断点续传」 | Migration 无 progress（model.go:340-347）；Store 仅 Create/Get/UpdateStatus（store.go:132-140） | 超前 | **P0** |
| ID-3 | migration.md:220-229 | 复制→校验→切换→清理自动编排 | 唯一推进是 rollback（admin/service.go:167-182）；migrating/verifying/completed 不可达 | 超前 | **P0** |
| ID-4 | scenarios.md:96-97 | `POST /v1/admin/maintenance-windows` 创建窗口 | 实为 `POST /v1/admin/servers/{id}/maintenance-window`（handlers.go:198），body 不带 server_id | 超前 | **P0** |
| ID-5 | scenarios.md:85 | 迁移 body `source_server_id`/`target_server_id` | 实为 `source_servers`[]/`target_server`（admin/service.go:16-19）；同文档集 migration.md 口径正确 | 不符 | P1 |
| ID-6 | scenarios.md:100-101 | 公告 body 仅 `{title,body,level}` | `Announcement.Validate` 强制 `starts_at<ends_at`（model.go:489-490），照发 400 | 超前 | P1 |
| ID-7 | migration.md:40-48 | 八步图步骤 5-8 状态自动推进 | 同 ID-3 证据；server_id 批量迁移无实现 | 超前 | P1 |
| ID-8 | lifecycle.md:124-132 | 触发表端点无 `/v1` 前缀 | 实路由带 `/v1`（handlers.go:168-171）；同页 §6 口径正确 | 不符 | P2 |
| ID-9 | lifecycle.md:164 | 无 runtime 服务器同套年龄判死 | monitor.go:134-147：仅 starting 参与、直接 offline 跳过 suspect、起点 CreatedAt | 不符 | P2 |
| ID-10 | realms-shards.md:32-33 | 档案「删除或缺失」不影响注册 | 平台无 realm/shard 删除 API（handlers.go:188-191 仅 POST/GET） | 不符 | P2 |
| ID-11 | scenarios.md:150 | compose 起「三件套」 | 四 service：atlas/migrate/postgres/redis（docker-compose.yml:26,61,78,99） | 不符 | P2 |

## 已核对无差异（各组抽样摘要，双向证据齐）

全量逐条 file:line 取证存于审计工作底稿；此处每组摘其要点：

- **组一**：server-config 全部错误文案逐字、profile 四规则、409 SERVER_MANAGED_BY_CONFIG、exit 1 不半生效；operations 公告/窗口 DTO 与模板逐字、巡检先窗口后心跳、ADMIN_API_KEYS 开发模式；config-center 五段 spec+hash 幂等、ETag/304、notify 三模式校验逐字、回调重试参数；data-model 七表 DDL 逐列、runtime 键/TTL、FNV-1a 分片。
- **组二**：四监听口与端点挂载、状态机阈值、双写顺序、Redis TTL/键名/拓扑优先级、事件五适配器（含重投/死信/手动 commit 细节）、8 指标名、HAProxy 参数、PG/Redis 连接池默认。
- **组三**：48 条生产路由与文档双向覆盖、admin limit 默认 50、发现 clamp 200、Lead 5m、RBAC 校验顺序、gRPC 22 RPC 计数、X-Request-ID 规则、OTLP 采样回退、8 指标名与错误码全实证。
- **组四**：compose 四服务与卷、GHCR tags、三 HTTP 口超时、「45 个业务端点」计数准确（路由表恰 45 条 `/v1/`）、gRPC 5 service 22 RPC、七态、9 个基准函数、限流 fail-closed 上限、grpc v1.86.0-dev 锁定、`model.PublicTags`。
- **组五**：环境变量全表、TLS 1.2 下限与客户端证书强制、RBAC 未知角色启动失败、审计环/字段/RFC3339Nano、限流前缀/429/Retry-After/CIDR、CORS 空即无头、APISIX 插件默认值与 config-example 逐值、周审计 workflow。
- **组六**：六语言包名/版本/零依赖口径、27 方法签名、心跳立即首发、重试参数（3 次/100ms/封顶 10s）、错误三字段、C++ vendored 版本号逐字、examples/ 六目录齐全。
- **组七**：七状态枚举与转移、重注册幂等语义、心跳 TTL 120s 仅自清理、告警三环境变量、维护窗口 DTO 与自动生效、七实体字段表逐一、标记 code 正则/五档位/六预设、no_register 双通道、metadata 过滤通道、弱引用拓扑、DDL 逐字、crossagent 控制面。

## 附注（不列入三类差异）

- 版本时间线主张（「v0.1.15/17/20 交付」）对的是 todo.md 版本台账，非 git tag（远端 tag 仅至 v0.1.2）——tag 补齐属仓库管理事项，R1/R2 修文档时一并处理 release 表。
- lifecycle.md:205 与 :221 重复同一句 webhook 表述，属编辑重复，修文时顺手合并。
- api-quickstart:144 create 响应示例含 `last_login_at`（omitempty 通常缺省）——影响极小，随 API-A7 顺手改。
- 组一备注：mysql store 全实现+契约在仓，但 `ATLAS_STORE` 目前仅接 memory/postgres（main.go:178）——码内现状，文档未超配。

## 附录 A · 性能批 bench 前后对照（2026-10-08）

**方法**：同一 bench 文件（`internal/store/memory/bench_test.go`，仅公开 API）分别跑在
HEAD(565256a) worktree（改前，全表扫描实现）与本批实现上；同机交错取样——正序
（前→后）2+2 轮 + 反序（后→前）2+2 轮，各取中位数后做几何均值对消轮次顺序偏差。
14 逻辑核，`-benchmem -count 2`。数据集：2000 台服务器（4 region 均分）+ 200 账号
× 20 服角色（4000 行目录）。

### A.1 读路径（本批收益）

| 基准 | 改前 | 改后 | 无偏加速比 |
| --- | --- | --- | --- |
| ListServers 无过滤 | 4.64 ms | 1.57 ms | 2.40x |
| ListServers region | 1.54 ms | 0.51 ms | 2.36x |
| ListServers version | 4.99 ms | 1.12 ms | 3.26x |
| ListServers status | 5.36 ms | 1.18 ms | 3.35x |
| 伸缩 N=2000（region） | 1.76 ms | 0.43 ms | 2.84x |
| **伸缩 N=20000（region）** | 13.94 ms | 1.63 ms | **7.03x** |
| ListCharactersByAccount | 218 µs | 16.6 µs | **14.6x** |
| ListCharactersByServer | 790 µs | 171 µs | 4.10x |
| **GetCharacterByCharacterID** | 76.5 µs | 0.27 µs | **282x** |
| `?tags=` 未知 tag（空桶短路） | 全表扫描（ms 级） | 0.3 µs | 空结果不再付全扫 |

收益来源：过滤查询从 O(全表) 降为 O(最小候选桶)；角色目录 byCharID 点查从全扫变
O(1)。ms 级行不受轮次噪音影响（同码对照 GetServer 的双向残差 ~1.4x 只出现在 µs
级行，µs 级数字按 ±40% 噪音看待）。

### A.2 写路径（如实成本，换取批合并与序保证）

| 基准 | 改前 | 改后 | 说明 |
| --- | --- | --- | --- |
| RecordHeartbeat 无竞争单写 | 0.2 µs | 3–9 µs | 入队 + 等本条提交回执的信道握手 |
| RecordHeartbeatBurst 14 核风暴 | 0.4 µs/op（≈2.5M ops/s） | 9.3 µs/op（≈107k ops/s） | 逐条回执的 goroutine 唤醒成本主导 |

换来：风暴中 256 条批内同实体合并到 ≤50 条（水位推进与临界区次数约 5x 减少）、
同实体严格 FIFO 不乱序、幂等键防重、指令流可回放。生产语境：真实心跳路径在内存
队列之外还有 PG 双写 + Redis HSET（ms 级），µs 级增量不可见；107k ops/s 对 10k
舰队 × 0.1Hz ≈ 1k ops/s 的真实需求仍有约两个数量级余量。

### A.3 复现

```
go test ./internal/store/memory/ -run '^$' -bench . -benchmem -count 2
```
改前基准：`git worktree add /tmp/atlas-before 565256a`，拷入同一 bench 文件并去掉
`defer s.Close()` 两行（旧实现无该方法）。

## 收口声明

- 差异全部有 file:line 双向证据；P0 9 条已逐条列明。总表实际 **46 行**（组一 5 / 组二 4 / 组三 10 / 组四 7 / 组五 5 / 组六 4 / 组七 11，无重复 ID；落档时「45 条」为计数笔误，本次收口一并更正——「45 个业务端点」系另一处已核对无差异的路由计数，与此无关）。
- 本档先于修文档批落档（审计表即台账）；修文档批以源码为真相逐条修文，禁反向改码，完成后在本档标记各条状态。

## 修文档批执行记录（2026-10-08，46 行全勾销）

四批执行，每批 vitepress build（涉及 Go 面的另有 gofmt/vet/test/race/build 全门禁）通过后独立提交；末批含两条**复核撤账**。

### 批① — P0 九条全修 + 同页连带 P1（commit `7ba93ba`）

| ID | 状态 | 修法摘要 |
| --- | --- | --- |
| OP-1 | ✅ | 总线默认 `http`（config.go:279），redis 改多副本/生产推荐 |
| ARC-A1 | ✅ | Prometheus 抓取口 → admin `:8082/metrics`（mermaid + 端口矩阵双处） |
| API-A1 | ✅ | 删 `language`/`game_mode` 幻影过滤维，列实有六维 + 分页；注明 tags 过滤仅存储层契约未暴露 HTTP 参数 |
| F1 | ✅ | apisix.md 三处改口径：Atlas 不读取身份头，账号来自路径/body 显式参数；信任规则第 2 条重写 |
| F2 | ✅ | operator 如实标注：解析入上下文无差异化强制、与 admin 等权；viewer 只读是唯一强制 |
| ID-1 | ✅ | 删「PATCH 改 server_id」；改「建新删旧」两步现状路径（非事务、受注册门禁） |
| ID-2 | ✅ | 幂等/可重放/可回滚/可校验表改「设计 vs v0.1 现状」两列 |
| ID-3 | ✅ | 复制→校验→切换→清理标明设计蓝图；GET 示例 verifying→pending |
| ID-4 | ✅ | 维护窗口 curl 改 `POST /v1/admin/servers/{id}/maintenance-window`（body 不带 server_id） |
| ID-5 | ✅ | 迁移 body 字段 `source_servers`[]/`target_server` |
| ID-6 | ✅ | 公告 body 补必填 `starts_at`/`ends_at` |
| ID-7 | ✅ | 八步图 2-5/8 标注为运维与游戏服动作；Atlas 不自动推进状态（附现状引注） |
| ID-11 | ✅ | compose「三件套」→ 四 service |

### 批② — 组一/二/四（commit `d431633`）

| ID | 状态 | 修法摘要 |
| --- | --- | --- |
| OP-2 | ✅ | servers DDL 补 `source` 列 + 档案归属语义注 |
| OP-3 | ✅ | serversconfig 字段表补 `metadata` 行 |
| OP-4 | ✅ | 心跳响应示例补 `next_heartbeat_in` |
| OP-5 | ✅ | 存储开篇改如实两层口径（默认 memory / postgres 生产 / mysql 装配未接） |
| ARC-A2 | ✅ | public `:8080` 删「只读」——Directory 写端点挂公网口 |
| ARC-A3 | ✅ | public/registry 两行补跨服配置拉取双口同挂 |
| ARC-A4 | ✅ | 限流改已落地口径（ratelimit.go 令牌桶 + 环境变量启用） |
| R1 | ✅ | README release 表补 v0.1.2（后续批③④再补 v0.1.3，见下） |
| R2 | ✅ | roadmap 同 R1 |
| R3 | ✅ | 删「Grafana 概览看板」——改外接指引并注明仓库无看板实体 |
| R4 | ✅ | 覆盖率 >80% 改「目标（CI 报告 + Codecov 上报），非失败门禁」 |
| R5 | ✅ | README 结构树补 `internal/tracing/` |
| R6 | ✅ | roadmap 结构树补 `internal/tracing/` |
| R7 | ✅ | listening 示例补 gRPC `:9090` 第四行 |

### 批③④ — 组三 + 组五/六（commit `ec22a0f`）

| ID | 状态 | 修法摘要 |
| --- | --- | --- |
| API-A2 | ✅ | 详情示例 `realm_id`/`shard_id`（json tag 实名） |
| API-A3 | ✅ | proto 摘要 Unregister → `UnregisterResponse` |
| API-A4 | ✅ | 错误映射表标「保留码，当前不会发出」（幂等 upsert） |
| API-A5 | ✅ | 目录写示例 server_id 数字 → 字符串 |
| API-A6 | ✅ | discovery 默认行为改 `Visible()` 实口径（online/maintenance/suspect） |
| API-A7 | ✅ | 删示例空串 `previous_status`，补 omitempty 时机注 |
| API-A8 | ✅ | register 响应补 `crossserver_config{version,hash}` |
| API-A10 | ✅ | Admin 节补 migrations 端点小节（创建只记记录、rollback 仅状态迁移，含真实错误码） |
| F3 | ✅ | 审计链路改「限流之内」——被限流请求不入审计环 |
| F4 | ✅ | 四层防护改如实：mTLS/RBAC/限流可选、审计默认开启 |
| F5 | ✅ | actor 补 mTLS 三段式与 anonymous 形态 |
| GO-1 | ✅ | env 改 `ATLAS_REGISTRY_TOKENS` |
| GO-2 | ✅ | API 一览五组→六组，补跨服配置中心（FetchCrossServerConfig + NewConfigWatcher） |
| JS-1 | ✅ | 删「parseServer 等转换函数可独立使用」 |
| CPP-1 | ✅ | Options 示例补 `admin_api_key` |
| （R1/R2 增补） | ✅ | 远端新增 release v0.1.3（tag 指向 7ba93ba），README/roadmap release 表补行 |
| （构建坑记录） | — | 新标题尾部裸 `{id}` 被 markdown-it-attrs 解析为空 id，与既有 `GET /v1/discovery/servers/{id}` 撞「id not unique」构建失败——单条小节并入列表小节消解 |

### 批⑤ — 组七余量 + 附注（commits `671ccb9` + 本档收口）

| ID | 状态 | 修法摘要 |
| --- | --- | --- |
| API-A9 | ✅ | 「五个官方 SDK」→ 六个（后列六项本就对，计数改齐） |
| ID-8 | ✅ | 触发表四端点补 `/v1` 前缀 |
| ID-9 | ✅ | 无 runtime 判死改实口径：仅 starting 参与、CreatedAt 起算、直接 offline 不经 suspect |
| ID-10 | ✅ | 弱引用注改实口径：缺失不影响注册与心跳；平台无删除端点 |
| （附注）lifecycle 重复句 | ✅ | webhook 投递失败句删第二处 |
| （附注）版本时间线 | ✅ | v0.1.3 tag 已由仓库自动化补齐（2026-10-08），release 表同步 |

### 复核撤账（2 条）

- **附注 · api-quickstart create 响应含 `last_login_at`**：源码证明该示例是对的——`directory.Service.applyCreated` 创建时即置 `LastLoginAt = now`（internal/directory/events.go:79），`omitempty` 不缺省。不改文档，审计该条撤账。
- **表头「45 条」计数**：总表实为 46 行且无重复 ID，落档笔误；本档已更正为 46 行口径。

---

# 2026-10-13 轮：巡检驱动的依赖告警修复 + 文档漂移复扫

## 审计范围与方法

- **触发**：巡检单 PATROL1013——todo.md 5 个未勾项全部标注挂起/待操作（凭据、审核、上游阻塞），无自主推进项；转查「新出现的可自主推进项」，命中依赖告警与文档漂移两类。
- **依赖面**：Dependabase API 30 条告警逐条核对锁定版本（stale）；新命中 Go 1.27.1 标准库漏洞 11 条（GO-2026-6599 等 11 个编号，net/http、http2、textproto、tls、httputil、html/template），govulncheck 复跑确认。
- **文档面**：承接 2026-10-08 轮未修完的 P1 余量（SYNC-1/2、DM-1、PF-1/2、BM-1、G01–G04、CS01、R05、R06），逐条重新取证；否定性结论先验证搜索树存在。
- **口径**：以源码为真相；基准数字为空载 min-of-5 复测（2026-10-13，i9-10880H / Go 1.27.2），文档标注测量环境。

## 总计数与 P0 清单

| 类别 | 条数 |
| --- | --- |
| 文档超前于码 | 7 |
| 码超前于文档 | 13 |
| 描述不符 | 21 |
| **合计** | **41** |

**P0 清单（3 条）**：

1. **R2-1** README.md:75 / README.zh.md:75 — 合服声称「Migration orchestration + character-index event replay；`rollback` reverts in one call」：Atlas 不重放事件、不做合服执行，角色搬迁由游戏服执行；Atlas 只提供迁移**记录**与状态回退。
2. **R2-2** docs/index.md:25 — hero 卡「合服与迁移编排」同 R2-1（页面标题级主张）。
3. **R2-3** docs/api.md:323 — `GET /v1/discovery/servers/{id}/characters?account_id=`：服务端无此参数解析（仅 `?limit`/`?cursor`），照抄会静默忽略。

## 差异总表

### 组一 · README / index / 门面

| ID | 文档页:行 | 主张 | 源码证据 | 类别 | 严重度 | 修法 |
| --- | --- | --- | --- | --- | --- | --- |
| R2-1 | README.md:75、README.zh.md:75 | 合服=迁移编排+事件重放，rollback 一次回退 | 路由表无合服执行端点；迁移仅记录状态机 internal/admin/migration.go | 超前 | **P0** | 改「迁移记录与回退」如实口径 |
| R2-2 | docs/index.md:25 | hero 卡「合服与迁移编排…角色索引事务内原子切换、可重放可回滚」 | 同上：无事件重放实现 | 超前 | **P0** | 改「迁移记录」，角色搬迁归游戏服 |
| R2-3 | docs/api.md:323 | 角色按服列表支持 `?account_id=` | handlers.go 仅解析 limit/cursor | 超前 | **P0** | 删 `?account_id=` |
| R2-4 | README.md:169、README.zh.md:169 | 45 个业务端点 | 路由表 49 HandleFunc − healthz/readyz = 47 | 不符 | P1 | 改 47 |
| R2-5 | README.md:361、README.zh.md:361、roadmap.md:24 | 「四个 release」 | gh release list：v0.1.0–v0.1.4 五个 | 不符 | P1 | 改五/五个 |
| R2-6 | README.md:19、README.zh.md:19 | 存储选型未提 MySQL 存在 | MySQL store 已实现有契约测试；ATLAS_STORE 装配未接 memory/postgres | 不符 | P1 | 括注「MySQL 实现已备、主程序装配未接」 |
| R2-7 | docs/index.md:7 | 「回答三个问题」 | hero 卡实际四问 | 不符 | P2 | 改四 |
| R2-8 | docs/index.md:23 | 角色目录回答「角色名称、等级和职业」 | 目录行=名称/等级/标签（tags），无职业字段 | 不符 | P2 | 改「等级与标签」 |
| R2-9 | GitHub repo About（R06 门面） | description 用旧词「character catalogs」 | README 首段与 topics 统一「character-directory」 | 不符 | P1 | `gh repo edit` 已改（catalogs→directory） |

### 组二 · API 面

| ID | 文档页:行 | 主张 | 源码证据 | 类别 | 严重度 | 修法 |
| --- | --- | --- | --- | --- | --- | --- |
| A2-1 | docs/api.md:487 | diagnose 查询参数缺 `?status=` | handlers.go 解析 status（online/suspect/offline，缺省 online） | 码超前 | P2 | 补参数说明 |
| A2-2 | docs/api.md:496-499 | 限流配置未写兜底规则 | 仅配 ATLAS_RATE_LIMITS 时兜底 100rps/200burst | 不符 | P2 | 补兜底口径 |
| A2-3 | docs/api.md:468 | admin servers 未写 limit 上限 | limit≤200 超限 400 INVALID_ARGUMENT（公网口按 200 截） | 码超前 | P2 | 补上限与错误码 |
| A2-4 | docs/api.md:731 | gRPC 访问日志字段写成 `status` | grpc 访问日志字段为 `code`（REST 侧为 status） | 不符 | P1 | 分传输注明 |
| A2-5 | docs/api.md:754 | AdminService「10 个核心运维操作」 | api.proto AdminService 11 个 RPC（含热力图） | 不符 | P1 | 改 11 并列全 |
| A2-6 | docs/api.md:61-79 | register 字段表缺 notify_mode / notify_callback_url | model.Server 含两字段，随注册入库 | 码超前 | P1 | 补字段 |
| A2-7 | docs/api.md:502 | indexqueue 快照缺 lanes_control / lanes_hot | store.QueueStats 含道类 lane 数 | 码超前 | P2 | 补字段 |
| A2-8 | docs/api.md:348-350 | 目录写端点未写异步语义 | 同步 201/200/200；异步一律 202 `{"status":"queued"}` | 码超前 | P2 | 补响应码注 |
| A2-9 | docs/api.md:359 | PATCH 示例缺 metadata | CharacterPatch 含 metadata | 码超前 | P2 | 示例补字段 |
| A2-10 | docs/migration.md:88-90 | PATCH 字段表缺 metadata | 同上 | 码超前 | P2 | 补字段 |
| A2-11 | docs/api-quickstart.md | `go run ./atlas` 输出「三行 listening」 | 实际四行（含 gRPC :9090）；ATLAS_GRPC_ADDR="" 可关 | 不符 | P1 | 补第四行 |

### 组三 · 架构 / 数据模型 / HA

| ID | 文档页:行 | 主张 | 源码证据 | 类别 | 严重度 | 修法 |
| --- | --- | --- | --- | --- | --- | --- |
| M2-1 | docs/architecture.md:248 | 「3 个容器」 | compose 四 service（atlas/migrate/postgres/redis），migrate 一次性任务 | 不符 | P2 | 改 4 个 service |
| M2-2 | docs/architecture.md metrics 表 | 缺队列指标行 | atlas_store_queue_* 14 序列 | 码超前 | P2 | 补行 |
| M2-3 | docs/data-model.md servers 表 | 缺 tags/notify_mode/notify_callback_url 列 | migrations 三列均有 | 码超前 | P1 | 补列+注 |
| M2-4 | docs/data-model.md | 缺 maintenance_windows / announcements 两表与 8 索引 | migrations/0002、0003 | 码超前 | P1 | 补表与索引 |
| M2-5 | docs/data-model.md:277 | ATLAS_CHAR_SHARDS 多库拆分 | 仅 cmd/atlas-reshard 支持，主程序装配未接（同 MySQL 口径） | 不符 | P1 | 括注 |
| M2-6 | docs/data-model.md:199,218,246 | Discovery/巡检「逐台合并/读 Redis runtime」 | 实为一次 GetRuntimes 批量 pipeline（discovery.go:70、monitor.go:126） | 不符 | P1 | 改「批量读」 |
| M2-7 | docs/ha.md:171-172 | PG 池 lifetime/idle 缺省 0（never） | 未配→pgxpool 库默认 1h/30m | 不符 | P1 | 改实口径 |
| M2-8 | docs/server-config.md:174 | realm_id/shard_id「须已通过 Admin API 创建」 | 弱引用，不做外键校验 | 不符 | P2 | 改弱引用口径 |
| M2-9 | docs/sync.md:285 | 「事件同步没有专属指标」 | atlas_directory_write_duration_seconds{op}（events.go:27 投影路径观测） | 码超前 | P1 | 改「有专属指标」 |
| M2-10 | docs/sync.md:151 | RabbitMQ nack「立即回队」 | 先 5s requeueBackoff 退避再 Nack(requeue)（adapter.go:33,247-257） | 不符 | P2 | 改退避口径 |

### 组四 · 性能 / 基准（2026-10-13 复测替换 2026-10-03 基线）

| ID | 文档页:行 | 主张 | 源码证据 | 类别 | 严重度 | 修法 |
| --- | --- | --- | --- | --- | --- | --- |
| P2-1 | docs/benchmarks.md §2 基线表 | 全表 2026-10-03 数字（心跳 287ns、fleet_1000 419µs/1,068 allocs 等 9 行） | 2026-10-13 空载 min-of-5 复测：心跳 5,055ns、fleet_1000 243µs/123 allocs 等（见 §2 新表） | 不符 | **P1** | 整表复测替换+测量环境标注 |
| P2-2 | docs/benchmarks.md §3 | 「单核 350 万 QPS」「注册贵 8 倍」「分配每千台 +5 allocs」 | 复测：~198k QPS、注册/心跳 ≈2.7 倍、分配近常数 | 不符 | P1 | 改读数 |
| P2-3 | docs/benchmarks.md §3 后端估算表 | 服务层耗时列（34µs/419µs/2.9ms） | 复测 42µs/243µs/1.3ms | 不符 | P2 | 更新 |
| P2-4 | docs/benchmarks.md 警戒线 | 心跳>1µs、注册>10µs | 复测基线 5.1µs/13.4µs | 不符 | P2 | 改 10µs/20µs |
| P2-5 | docs/performance.md:28 | 心跳「单核 ~350 万 QPS、287ns/op」 | 复测 5,055ns ≈ 198k；成本含 v0.2 指令队列逐条回执（§8.5 对照 129ns） | 不符 | P1 | 改数+归因 |
| P2-6 | docs/performance.md:34 | 「100 台 34µs、1,000 台 419µs、~1,068 allocs」 | 复测 42µs/243µs/123 allocs | 不符 | P1 | 改数 |
| P2-7 | docs/performance.md:38 | 「注册贵约 8 倍，冷恢复 ≈2.3 万 QPS·核」 | 复测 2.7 倍、7.5 万 QPS·核 | 不符 | P2 | 改数 |
| P2-8 | docs/performance.md §6 表 | HeartbeatParallel ~1.5M×核、GetServer ~3.6M、fleet_1000 ~2.4k | 复测 ~228k、~762k、~4.1k | 不符 | P1 | 改数 |
| P2-9 | docs/performance.md:71 | 「单核基准 350 万 QPS」 | 复测 ~198k | 不符 | P2 | 改数 |
| P2-10 | docs/performance.md:278 | 行号漂移：handlers.go:211/:1794、main.go:880 | 实为 :213/:1832、main.go:912 | 不符 | P2 | 改行号 |
| P2-11 | docs/performance.md:198 | 环境标注 Go 1.27.1 | 仓库已钉 1.27.2（补丁级不影响本表） | 不符 | P2 | 改注 |

### 组五 · SDK 与其余

| ID | 文档页:行 | 主张 | 源码证据 | 类别 | 严重度 | 修法 |
| --- | --- | --- | --- | --- | --- | --- |
| S2-1 | docs/sdk-go.md:50 | 「上报间隔与判死阈值保持 3:1」 | 实为判死阈值:上报间隔 = 3:1（30s/10s），比例写反 | 不符 | P2 | 改「判死阈值与上报间隔保持 3:1」 |
| S2-2 | docs/sdk-go.md:16-26 | Options 示例缺 RegistryAddr/HTTPClient/GRPCTLS/GRPCTLSCACert | client.go:14-60 四字段均在 | 码超前 | P2 | 示例补四字段 |
| S2-3 | docs/sdk-go.md:42-47 | 心跳循环未提 LastError()/LastSent() | heartbeat.go:74,81 两访问器 | 码超前 | P2 | 补注 |
| S2-4 | docs/sdk-go.md:95 | ConfigWatcher 仅一句「轮询 + 退避」 | Notify/Pull/Run/Current/Stats 五方法（config.go:174-250） | 码超前 | P2 | 补 API 一览 |
| S2-5 | docs/sdk-csharp.md:60 | HttpInvoker 注入 HttpMessageInvoker | AtlasClientOptions.cs:41 实为 HttpClient? | 不符 | P2 | 改 HttpClient |
| S2-6 | docs/roadmap.md Dashboard 行 | 缺服务器负载热力图交付记录 | a26f25e 等三提交（REST+gRPC+dash 页） | 码超前 | P2 | 补行 |
| S2-7 | todo.md | 534 行混杂过程叙述/装饰符号/情绪措辞 | — | 不符 | P1 | 重写 195 行：85 完成项一行证据、5 挂起项一句话需求 |
| S2-8 | docs/BUGS.md:20-21 | BUG-1 状态注声称三请求全兜底 | 仅 tags 请求已兜底；getServer/listCharactersByServer 仍留加载态 | 不符 | P1 | 改实口径（未完成余项写明） |
| S2-9 | （非文档）internal/event/redis/adapter_test.go | 全量 -race 并行下 TestDeadLetterAfterMaxRedelivery 消费循环被饿死 >15s（曾 >30s）超时 | 消费循环 goroutine 在全套件并行调度下长期无时间片 | 不符 | P1 | 测试改确定性：关闭循环后第二适配器同步驱动 sweep（adapter_test.go:273、233） |

## 观察项（不属文档漂移，仅记录）

- **R05 版本号常量**：internal/version/version.go:6 与 Makefile:3 钉 `v0.1.1`，远端 tag 已到 v0.1.4。属代码侧卫生（版本号由用户手动给出的红线内），非文档-码漂移；本档不动码，记录待用户定版本时一并处理。
- **CI 绿**：最近 workflow runs 全 success（含 2026-10-12 安全审计例行失败后的 workflow_dispatch 复跑 success）；无 code-scanning 告警。

## 门禁与提交

- `GOTOOLCHAIN=go1.27.2 go test -race -count=1 ./...`：EXIT=0，32 包全绿（全仓 490 tests / 39 packages）。
- govulncheck：0 漏洞（1.27.1 的 11 条标准库漏洞已随 toolchain 钉 1.27.2 修复）。
- `cd docs && npm run build`：EXIT=0。
- 提交分三批：① `docs: fix N doc drift items`（12 文档页 + 本档）② `docs: tidy todo list` ③ `chore: bump go toolchain to 1.27.2`（go.mod + Dockerfile）。push 前 `git pull --rebase`；不打 tag、不 force push、不发 release。

---

# 2026-10-19 轮：文档时效性清理（用户令）

## 范围与方法

- **触发**：用户令（2026-10-18）「很多文档已过时」——通读 README/docs 找与当前代码不符的描述，以代码为准修正，命令示例逐条跑。
- **方法**：延续前两轮口径（源码为真相、禁反向改码），但改用全集 diff 而非逐页走查——env 变量全集（config.go `os.Getenv`/`envOr` vs 全站 `ATLAS_*` 提及）、HTTP 路由全集（handlers.go `mux.Handle*` vs 全站端点提及）、路径引用（Makefile 目标 / cmd/ 工具 / deploy 文件存在性）、release 口径（gh release list vs README/roadmap）四个维度双向比对；命令示例实机跑。
- **重点核对 10-13 轮之后六天的新交付**：reg_kind/ttl（lifecycle/zh-api ✅）、迁移窗口联动（migration §9.1 ✅）、weight/effective_load（zh-api/roadmap ✅）、batch windows（✅）、deploy/k8s（roadmap/README ✅）、Admin TLS（本轮另批交付时同步 security.md ✅）。

## 修复（1 条）

| ID | 文档页:行 | 主张 | 源码证据 | 类别 | 修法 |
| --- | --- | --- | --- | --- | --- |
| A3-1 | docs/zh/api.md:523,525 | body 截断与审计环可经 `ATLAS_AUDIT_MAX_BODY` / `ATLAS_AUDIT_MAX` 配置 | audit.go:42-54 两值为 NewAuditLog 参数缺省（1000/4096），main.go:467 调用处写死；全仓无任何 env 读取 | 超前（虚构配置项） | 改「固定 4096 字节 / 固定 1000 条，启动时写死，不可配」 |

## 命令示例实测（全部实机跑）

- `make build` / in-memory 启动：四行 listening（public/registry/admin/grpc）✅；README curl 三连（register → crossserver_config+starting、heartbeat → next_heartbeat_in+online、discovery?status=online → 含 `reg_kind:"permanent"` 新字段）+ admin stats ✅（本机 8080/18080 等被其他项目占用，换 127.0.0.1:28080 段实测）。
- `go run ./examples/go`（sdk-go.md:111 命令）：注册 + 路由推荐 `account 42 → demo-game-1 (lowest_load)` ✅。
- 根级 `docker compose config`：四 service（postgres/migrate/redis/atlas）与 container_name（atlas/atlas-postgres/atlas-redis）同 README「Four containers」口径 ✅。完整起栈受本机端口占用限制（环境问题非文档问题；deployments/docker 栈的 postgres healthy + migrate 跑通后已 `down -v` 清理）。
- 引用路径存在性：deploy/docker-compose.ha.yaml、deploy/k8s/README.md、deployments/docker/docker-compose.yml ✅。
- `cd docs && npm run build`：EXIT=0（已知 SSR getComputedStyle 日志不影响退出码）。

## 确认无漂移（本轮排除项）

- `ATLAS_IMAGE`（本地 compose `image: ${ATLAS_IMAGE:-…}` 消费）、`ATLAS_ADDR`（examples/ 六语言均读）、`ATLAS_SDK_BUILD_TESTS`（CMake option）——均真实存在，非虚构。
- 英文 api.md 18 行为**有意占位页**（明示指向中文原版），非缺失。
- README/roadmap release 口径「五个 release / v0.1.4 latest」与 `gh release list` 一致。
- 路由占位符命名差异（`{id}` vs `{server_id}`/`{window_id}` 等）为风格差异非漂移；`healthz`/`readyz`/`tags` 端点均在 architecture/concepts 有覆盖。
- demoagents 的 `ATLAS_ADMIN_KEY`/`ATLAS_DEMOAGENTS_HTTP_ADDR` 为 flag 缺省来源（`-h` 自解释的演示工具），不构成用户配置面文档缺口。

## 观察项（不动）

- Makefile:3 `VERSION := v0.1.1` 与远端 tag（v0.1.4+）不齐——版本号红线（只能用户手动给），延续 10-13 轮 R05 口径待用户定版本时一并处理。
