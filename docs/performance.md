# 性能设计

Atlas 的性能目标由它的位置决定：它坐在**所有玩家登录路径**上（选服、拉角色、读公告），所以读路径必须便宜；它同时接收**整个舰队**的心跳与角色写入，所以写路径必须横向可扩。本文讲实现层面的取舍——每一条都能对应到仓库里的真实代码，数字来自 [性能基准](/benchmarks)（复现命令在彼处，`go test -bench` 可本地验证）。

## 1. 技术栈与选型

| 层 | 选型 | 为什么 |
| --- | --- | --- |
| 语言 | Go（标准库 `net/http`，Go 1.22+ 方法路由） | 编译型、goroutine 承载扇入、部署物是单个静态二进制 |
| PostgreSQL 驱动 | pgx v5 原生（扩展协议 + `pgxpool`） | 预编译语句 + 二进制传输省去每次解析；连接池内建 |
| Redis | go-redis v9，hash + pipeline | 运行时视图（心跳）是天然 KV，`HSET`+`EXPIRE` 一条管线单 RTT |
| gRPC | google.golang.org/grpc（钉 v1.86.0-dev） | 跨语言 SDK 共用同一套服务定义；版本钉死避免供应链漂移 |
| Kafka | segmentio/kafka-go | 纯 Go 无 cgo——容器镜像不依赖系统库，交叉构建不受累 |
| 管理台 | React + antd（静态产物） | 管理台不打进服务进程，API 进程零前端开销 |

## 2. 热路径逐条拆

### 心跳（写路径之王）

10 万台服务器 × 10s 心跳 = 1 万 QPS 持续写。实现是 Redis hash + 管线（`internal/store/redisstore`）：

```go
pipe := s.client.Pipeline()
pipe.HSet(ctx, key, fields)      // status/players/load/last_seen
pipe.Expire(ctx, key, runtimeTTL) // 120s —— 失联自动消失,无清理任务
```

一次网络往返写完全部字段；TTL 到期即"下线"，**不需要任何后台清扫协程**。基准：单核 ~350 万 QPS（`BenchmarkHeartbeat` 287ns/op；64 台并发扇入 `BenchmarkHeartbeatParallel` 646ns/op ≈ 1.5M×核）——1 万 QPS 的真实负载用一个核的 0.3%。瓶颈从来不在心跳本身，而在 Redis 与网络。

### 发现 / 选服（读路径之王）

玩家登录拉服务器列表。`internal/discovery` 读路径做三件事：过滤（region/realm/状态）、运行时合并（在线人数/负载）、对外标记过滤（`model.PublicTags`——内存里的切片过滤，零分配级别的开销）。列表是**准实时**数据，网关可安全做 1–5s 短 TTL 缓存；`region` + `limit` 把单响应控制在 100 台量级。

服务层基准（memory store，不含网络）：100 台全量 34µs、1,000 台 419µs，管线化后服务层分配每千台仅 +5 allocs（~1,068 allocs/1,000 台，批量读的 ids 切片与结果 map 容器），内存代价可忽略。**Redis 读路径已管线化（P1，2026-10-04 落地）**：列表合并不再逐台 `GetRuntime`，`RuntimeStore.GetRuntimes` 批量读由 Redis **pipeline 一次 Exec** 完成——**N 次网络往返 → 1–2 次**（单节点 1 次；Cluster 模式按 slot 分批，每批 1 次），1,000 台的 Redis 后端估算从 ~300ms 降到 ~2ms 量级（[基准页](/benchmarks#_3-读数)有明细估算）。单点详情 `GetServer` 维持单次读，天然最少往返。**决策面同批收尾（2026-10-05）**：健康巡检 sweep 与 Routing 推荐/诊断的运行时合并也从逐台读换 `GetRuntimes` 批量——巡检整舰队每轮从 N 次往返降到 1–2 次。与列表展示的「降级容忍」不同，这三处失败一律传播（决策层不拿未知负载排序、不拿未知状态判死）。

### 注册（upsert 单语句）

注册是幂等 upsert：PostgreSQL 一条 `INSERT … ON CONFLICT` / MySQL `ON DUPLICATE KEY UPDATE` 完成存在性判断 + 字段覆盖，**没有**先 SELECT 再 INSERT 的竞态窗口，也没有事务往返。比重心跳贵约 8 倍（校验 + upsert + 初始心跳），但只在启动/恢复时发生；1 万台舰队冷恢复 ≈ 2.3 万 QPS·核，单副本可吸收。标记（tags）列在 upsert 里被刻意省略——管理台独占维护，游戏服重注册永不覆盖。

### 角色目录（分片索引）

角色写入是全系统最大的写流量（每次创角/升级/登录都来一笔）。索引按 **account 取模分片**（`internal/store/sharded`）：单账号的全部操作路由到同一个分片，恰好一次分片跳转；跨账号查询（管理台搜索）fan-out 到全部分片归并，代价随分片数线性——这是明示的取舍，写在包注释里。扩容用 `cmd/atlas-reshard` 在线重分布。

## 3. 并发模型

- **读多写少 → `sync.RWMutex` + 拷贝出库**（`internal/store/memory`）：所有读方法返回**深一层拷贝**（struct 值拷贝、tags 切片整块替换），调用方拿到的对象与存储内部零共享——读侧不持锁遍历、写侧改不动读侧手里的数据，`go test -race` 全绿是底线而非目标。
- **单条记录原位更新 → 整块替换**：`UpdateServerTags` 从不原地 append（替换整个切片），共享底层数组的读写竞态在结构上不存在。
- **HTTP 服务器的超时是安全阀**：三个监听全部 `ReadTimeout/WriteTimeout 10s、IdleTimeout 60s`（`cmd/atlas/main.go`），慢客户端不能占住 goroutine。
- **限流按"最长路径前缀 × 客户端 IP"令牌桶**（`internal/httpapi/ratelimit.go`）：一台抽风的游戏服吃满自己的桶，饿不到别人；桶总数封顶、超了**拒绝新桶（fail closed）**——伪造 XFF 不能把限流器本身变成内存攻击面。

## 4. 池与资源治理

pgx 连接池全部参数可用环境变量覆盖（`ATLAS_PG_POOL_MAX_CONNS / MIN_CONNS / MAX_CONN_LIFETIME / MAX_CONN_IDLE_TIME / HEALTH_CHECK_PERIOD`，`applyPGPoolOptions`），零值回落库默认（随 CPU 数），`MinConns > MaxConns` 自动收敛。连接生命周期 1h + 健康检查 1m：发布/故障切换后的死连接被池自己回收，不需要重启进程。

## 5. 队列与背压

创角/角色写入可切换为**异步缓冲**：写端点立即 `202`，事件经消息总线落地（`internal/event`）——HTTP 适配器进程内零拷贝（语义 exactly-once），Redis Streams 复用既有 Redis（at-least-once，`Directory.ApplyEvent` 幂等），Kafka/NATS/RabbitMQ 适配器用于已有中间件的机房。同一套 `event.Event` 载荷，五种传输，切换是一个环境变量（`ATLAS_EVENT_ADAPTER`，见[数据同步](/sync)）。

## 6. 基线与警戒线

| 基准 | 场景 | 单核 QPS |
| --- | --- | ---: |
| HeartbeatParallel | 64 台并发心跳扇入 | ~1.5M×核 |
| GetServer | 单服详情（含运行时合并） | ~3.6M |
| ListServers/fleet_1000 | 1,000 台全量列表（服务层） | ~2.4k |

回归警戒线（超出即查 diff）：心跳 > 1µs、注册 > 10µs、fleet_1000 列表 > 1ms——通常意味着引入了新锁或新分配。完整方法、边界与复现命令见[性能基准](/benchmarks)；多副本部署下的一致性与故障切换见[高可用](/ha)。

## 7. 正确性优先于 QPS

吞下 1 万 QPS 心跳不难（单核基准 350 万 QPS，见[性能基准](/benchmarks)），难点在大批服务器同时
startup / shutdown / 断网 / 滚动重启 / 维护 / 迁移 / 发版时仍然：

```text
不错推服务器     —— 半死不活的服绝不能被推荐给客户端
不丢角色         —— 索引投影不丢条目
不双归属         —— 同一角色不同时出现在两个 server_id 下
不残留 stale endpoint —— 下线 / 迁服的端点及时摘除与更替
迁移可恢复       —— 任何一步失败均可回滚 / 重放
```

**基于故障注入的演练清单**（每个场景都有对应的既有保证与验证方式）：

| 故障场景 | 既有保证（文档出处） | 验证方式（自动化位置） |
| --- | --- | --- |
| 游戏服网络分区 | 心跳超龄 suspect → offline，客户端列表不抖动（[lifecycle.md](lifecycle.md) §3-§4） | ✅ 自动化：`internal/health` 巡检演练（OnlineToSuspect / SuspectToOffline / SuspectRecovered）+ discovery 可见性测试 |
| 游戏服重启 / 心跳延迟 | 重注册幂等恢复；3:1:6 节奏容忍抖动（lifecycle §4.2） | ✅ 自动化：`internal/store/storetest` 重注册契约（suspect/offline 重置表）+ redisstore miniredis 契约 |
| 重复注册 / 重复迁移 | 注册幂等 upsert；迁移幂等可重放（[migration.md](migration.md) §7） | ✅ 自动化：storetest 幂等 upsert + `internal/admin` 迁移编排 / rollback 用例 |
| Redis 数据丢失 | 档案在 PG，TTL 自清理，心跳重填（[data-model.md](data-model.md) §6） | ✅ 自动化（2026-10-04）：`internal/store/redisstore` `TestDataLossFlushAll_HeartbeatRefills`（miniredis FLUSHALL → 缺失即缺席、心跳重填） |
| PG 短暂不可用 | pgx 池 + 健康检查自动回收死连接（本文 §4） | 服务层故障传播 ✅（discovery 后端降级测试、registry 错误路径）；连接池自愈仍属部署演练（ha.md） |
| 迁移中途失败 | 先复制、后切换、再清理，源数据常驻（migration §7） | ✅ 自动化：`internal/admin` rollback 流程用例（pending/completed/not-found 三分支） |
| Atlas 副本宕机 | 无状态多副本 + HAProxy 心跳扇入（[ha.md](ha.md)） | 部署级演练（ha.md，属多副本编排，不进单元套件） |

除部署级两行（PG 连接池自愈、Atlas 副本宕机）外，矩阵已全部变成
`go test` 可复现的故障用例——从此"正确性不靠人品"。完整基准方法见
[性能基准](/benchmarks)。

## 8. v0.2 索引与写路径设计（2026-10-08 落地）

> 本章对齐已落码实现，每条主张给到 file:line；前后对照数字来自同 bench 内复刻
> 实测（§8.5，改前路径在 bench 内复刻、无需切旧提交）。SQL store（postgres/mysql）
> 不引入队列——指令化写路径是 memory store 的实现选择，三库以 storetest 契约
> 钉住同一行为（`internal/store/storetest`）。

### 8.1 六类倒排索引（读路径）

**服务器目录**按六个字段 + public tags 建倒排（`internal/store/memory/indexes.go:50`）：
`region / realm / shard / version / platform / status` 各是 `map[值]→ID集合`，tags 只收
`Public:true` 的 code（投影 `projectServer`，indexes.go:32）——内部标记永不参与玩家侧
过滤，与发现接口的 `model.PublicTags` 口径一致。

**角色目录**与服务器目录同级索引化（indexes.go:213）：`byAccount / byServer /
byAccountServer / byCharID` 四张倒排。复合键 `accountID:serverID:characterID`
终生不变，所以**更新不触索引**、只有插入/删除维护 O(1)（indexes.go:229 注释）；
全局角色 ID 点查从全扫变 O(1)（indexes.go:242）。

**索引选择与游标**（`ListServers`，memory.go:143）：有索引过滤时取**最小候选桶**
逐条验全过滤器——选一个维度做驱动、其余字段复核，等价于全交集而免去集合运算
（candidates，indexes.go:118）；无任何索引过滤则直接扫表（memory.go:160，先物化
全量 key 切片是纯开销）。未知 tag / 无匹配值返回**空非 nil 桶：空结果，不是全扫**
（契约断言 storetest/contract.go:486）。候选按 ID 升序排序后二分定位游标
（memory.go:160-174）——分页语义与改前逐字一致，契约回归锁住。

| 操作 | 改前 | 改后 |
| --- | --- | --- |
| 过滤列表 | O(N) 全表逐条验 | O(|最小桶|) 候选 + O(C log C) 排序（分页语义要求） |
| 角色按账号/按服列表 | O(N) 全扫 | O(|桶|) |
| 全局角色 ID 点查 | O(N) 全扫 | O(1) |

**SQL 侧只增量补 tags 过滤**（既有 SQL 过滤一字未动）：postgres `tags @> $n::jsonb`
（postgres.go:179）、mysql `JSON_CONTAINS(tags, ?)`，谓词都限定 `"public":true`——
三库同一 AND 语义由 storetest 契约对齐（contract.go:461-490：public 匹配、AND、
内部 tag 永不匹配、未知 tag 空集）。

### 8.2 指令化写队列

所有热写先封装为**指令信封** `{kind, target, payload, base_version, idempotency_key}`
（`instruction`，queue.go:130），公开方法签名与同步语义不变——调用方阻塞到**本条**
提交（各写方法都是 `return s.submit(ctx, ins).err`，memory.go:128 等），read-after-write
依旧精确。

- **分道**：4 控制道 + 2 热道，按实体 FNV-1a 哈希（queue.go:231, 282-284）——同实体
  恒落同道，FIFO 单写者，竞态在结构上不存在。配置变更（控制道）不排队在心跳
  （热道）后面，热道积压拖不垮控制面（`hot()`，queue.go:111）。
- **合并规则挂在类型上**（`mergeable()`，queue.go:119）：只有覆盖写语义的
  heartbeat / server-status / server-tags 可合并——同批同实体留最新、多余的被
  「 supersede」；带调用方拷回的（register / 角色 upsert）与顺序敏感的（删除、
  角色 patch）永不合并、不丢不重。**被合并的信封也拿批次提交回执**（applyBatchN
  的 superseded 通知，queue.go:350 起）——对 last-writer-wins 语义，"你的写已随
  更新者落地"，调用方不感知合并；回归测试钉住并发合并不吞回执（queue_test.go:281）。
- **合并窗随供给走**：道内非空续捞、捞空即提交（`drainAvailable`，queue.go:317），
  批量上限 256。没有定时窗——空闲单条立即提交（无延迟地板），风暴下天然攒满
  上限批。合并窗=供给窗口：同实体高频写在批内去抖，索引增量随批一次维护。
- **背压**：道深 4096 满→该条**内联同步写**（submit 的 default 分支，queue.go:254）
  ——调用方自己当自己的单写者，写绝不失败也绝不无限排队。
- **幂等与回放**：幂等键 LRU 1024 防重试重放（queue.go:470-487）；已应用指令落
  1024 深拷贝 capture 环——`ReplayInto` 在沙箱重建写历史、`DryRun` 预检不动线上
  （queue.go:206, 221）。合并语义测试：合并不丢（enqueued = applied + merged +
  在途）、同实体不乱序、幂等重放一致（queue_test.go:25, 122, 230）。

### 8.3 原子性：data + index 同临界区（拍板）

**实际采用：同一把锁同一个临界区**。每批提交在 `mu + charMu` 内一次完成数据变更
与索引维护（applyBatchN，queue.go:340-395）——索引与数据永不互相领先，读侧持
对应读锁看到的必是一致的 (data, index) 对。三锁域与锁序：mu（servers+serverIndex）
→ charMu（characters+charIndex）→ rtMu（runtimes），无反向获取路径
（memory.go:10-14, 47-48）。

**否决方案（如实记录）**：整体快照 + `atomic.Pointer` 发布。每条写都要拷贝整表
（O(N) 写放大，2000 台舰队心跳风暴下不可接受），而读侧在 RWMutex 读锁 + 拷贝
出库下已经无写阻塞（§3）；收益只剩"读侧免锁"，代价不成比例。索引维护是增量的
O(1)/条（indexes.go:74, 92——变更前快照旧投影、只摘/挂变化的键，空桶即回收），
**禁全量重建**。RCU 同理评估否决；心跳批合并已把高频写的临界区摊薄（风暴下
256 条一次临界区）。

### 8.4 版本水位

每应用一条指令递增 monotonic 水位（`s.watermark.Add(1)`，queue.go:428）。可见性
语义：**读侧在锁内看到的水位即已提交状态**；调用方的写返回 = 本条已应用（等回执
语义，§8.2）；被合并的写返回 = 其后继已应用（状态 ≥ 自己的写）。base_version 在
入队时盖章；现有指令类型都是按实体覆盖交换的，水位不符即按重算消化——未来
compare-and-set 类指令在此拒绝（errVersionConflict 预留，queue.go:62-67）。当前
水位经 `QueueStats.Watermark` 暴露（store.go:318-323）。

### 8.5 前后对照（同 bench 内复刻实测）

**方法**：改前路径在 bench 内复刻，不切旧提交（`internal/store/memory/bench_compare_test.go`）——
`listServersFullScan` 是旧 `ListServers` 的核心循环（全表逐条验过滤器含 tags、排序、分页），
`benchDirectHeartbeat` 是旧写路径的临界区（构造快照、锁内赋值）；角色目录同法复刻——
`getCharacterByCharIDFullScan` / `listCharactersByAccountFullScan` /
`listCharactersByServerFullScan` 是索引化前的全表扫描（按全局 ID 点查、按账号、按服务器）。
两侧**同数据集、同机、同一次运行**。等价门：每个 规模×过滤 组合在计时前断言复刻路径与索引
路径返回的 ID 序列完全一致（bench 内 `benchAssertSameList` / `benchAssertSameChars`，漂移即
fail）——表内两列永远是同一个查询。计 5 轮取
**最小值**（min-of-5：长跑降频与调度噪声下，最小值是最接近无干扰真值的估计；中位数会随
热累积漂移——实测 index/status 的 b.N 逐轮 5108→2635→1310 递减）。

**环境**：Intel Core i9-10880H @ 2.30GHz（14 逻辑核）、Go 1.27.1、linux/amd64。

复现命令：

```bash
go test ./internal/store/memory/ -run '^$' \
  -bench 'BenchmarkReadCompare|BenchmarkCharacterReadCompare|BenchmarkWriteCompare' \
  -benchmem -count 5
```

**读路径**（数据集：N 台服务器，维度均布——region 4 桶、version 3 桶、platform 4 桶、
status online 80%、public tags hot 25%；`limit=200`；单位 ns/op）：

| 过滤（命中率） | 全扫 N=1k | 索引 N=1k | 全扫 N=10k | 索引 N=10k | 10k 加速 |
| --- | ---: | ---: | ---: | ---: | ---: |
| 无过滤 | 287 µs | 291 µs | 2.89 ms | 2.87 ms | 1.00x（同路） |
| region（25%） | 177 µs | 124 µs | 1.55 ms | 0.58 ms | **2.66x** |
| version（33%） | 200 µs | 141 µs | 1.78 ms | 0.85 ms | **2.10x** |
| platform（25%） | 174 µs | 137 µs | 1.93 ms | 0.68 ms | **2.85x** |
| status（80%） | 268 µs | 222 µs | 3.49 ms | 2.08 ms | 1.68x |
| tags hot（25%） | 173 µs | 122 µs | 1.43 ms | 0.64 ms | **2.22x** |
| tags 未知（0%） | 43 µs | 130 ns | 665 µs | 127 ns | **~5,200x** |

分配同型收敛：region N=10k 每操作 169 KB → 109 KB（-35%）、allocs 217 → 207；
维度越少命中省得越多。三行如实注解：**无过滤两列同路**（candidates 返回 nil 走同一
扫表，作基线 sanity）；**status 收益最小**——最小桶仍占全表 80%，索引省的是另外
20% 的行访问与排序规模；**未知 tag 是指数级短路**——空非 nil 桶在排序前直接返回
（§8.1 契约），全扫侧则是完整一轮表遍历。

**角色目录**（数据集：N 条角色索引 = N/10 个账号 × 10 角色、50 台服务器均布；
按服桶受 200 上限约束与全扫侧同语义；单位 ns/op）：

| 读路径（10k 桶大小） | 全扫 N=1k | 索引 N=1k | 全扫 N=10k | 索引 N=10k | 10k 加速 |
| --- | ---: | ---: | ---: | ---: | ---: |
| 全局角色 ID 点查（1 行） | 71 µs | 1.6 µs | 3.05 ms | 5.3 µs | **~579x** |
| 按账号列角色（10 行） | 253 µs | 49 µs | 5.50 ms | 30 µs | **~185x** |
| 按服务器列角色（200 行） | 413 µs | 81 µs | 10.3 ms | 0.88 ms | **11.6x** |

分配侧两侧**逐位相同**（点查 144 B/1 allocs、按账号 1.7 KB/17、按服 33 KB/211）——
拷贝出库是两条路径共同的分配底盘，索引省下的全是扫描时间，不是分配。加速比随表
线性拉大：点查 1k 时 46x、10k 时 ~579x；桶越大摊薄越狠（按服桶 200 行，11.6x）。

**写路径**（ns/op，min-of-5；队列侧 = 公开 API 逐条等回执）：

| 形态 | 直锁（改前复刻） | 指令队列（现行） |
| --- | ---: | ---: |
| 单条无竞争 | 129 ns，0 B/op | 2,839 ns，1.4 KB/op，5 allocs |
| 风暴 50 服 × 14 核 | 352 ns（锁竞争拖慢 2.7x） | 2,561 ns（攒批吸收，几乎不恶化） |
| 风暴同目标 | — | 2,218 ns，**合并率 55%**（applied 45%） |

解读（口径同前批，数字为本法实测）：逐条回执的信道握手是队列的单条成本
（129 ns → 2.8 µs，~22x）——换来同实体严格 FIFO、幂等防重与可回放（§8.2）。
风暴下直锁路径被锁竞争拖慢 2.7x，队列几乎不吃竞争：同道指令攒批、一次临界区
提交一批。同目标风暴（合并案例）55% 的写在批内被 superseded——被合并信封照拿
批次回执，调用方无感，等效临界区进入次数近乎减半。生产语境不变：真实心跳路径
在内存队列之外还有 PG 双写 + Redis HSET（ms 级），µs 级差异不可见；~390k ops/s
（风暴单服）对 10k 舰队 × 0.1Hz ≈ 1k ops/s 仍约两个数量级余量。

历史交叉验证：worktree 切旧提交法（同 bench 文件跑改前/改后两棵树）的同型数字
见[审计文档附录 A](./审计-文档一致性.md)（N=2000 数据集：region 2.36x、角色按账号
14.6x、角色 ID 点查 282x），与本表量级一致——按账号 14.6x 落在本表 1k 档 5.1x 与
10k 档 185x 之间，两法互为独立复现。

### 8.6 dash 队列观测（已落地：快照 → Prometheus / admin 端点 / 管理台卡）

**数据面**：队列快照 `QueueStats`（store.go:318）——深度（DepthControl/DepthHot）、
入队/合并/应用计数（Enqueued/Merged/Applied）、幂等命中、背压退化次数
（BackpressureSync）、水位（Watermark）、最近 16 次 flush 记录、按类型应用分布——
由 `store.QueueStatusProvider` 接口（store.go:358）统一供出，队列测试消费验证
（queue_test.go 各断言）。**一个快照，三个读者**（store.go 头注的 no-drift 契约）：

1. **Prometheus `atlas_store_queue_*` 指标族**（`internal/metrics/queue.go`，注册
   于 metrics.go:95）：抓取时读快照——与 `storeCollector` 同姿势，零写路径开销。
   `depth_control` / `depth_hot`（gauge）、`watermark`、`enqueued_total` /
   `applied_total` / `merged_total` / `idempotent_hits_total` /
   `backpressure_total`（counter）、`applied_by_kind_total{kind}`、
   `flush_batch_size` / `flush_duration_seconds`（最近一批 gauge）与
   `capture_entries`。入队/合并速率按 PromQL 由 `*_total` 差分，不另设第二
   计数器。SQL 后端经转发快照报 `Enabled=false` 时整族不出序列——有指标即有队列。
2. **`GET /v1/admin/indexqueue/status`**（httpapi/handlers.go:211 路由、:1794
   handler）：返回同一快照原文。装饰层不藏快照——fleet 装饰器与 main 的
   composite 都透传 `QueueStats`（fleet/track.go:75、main.go:880）；SQL 库的
   空快照（`Enabled=false`）与非 provider 一律 `503 INDEX_QUEUE_DISABLED`，
   不发空 200。curl 实证：memory 后端 200（watermark=enqueued=applied=4，
   recent_flushes 真实记录，临界区 ~7-17µs）；`/metrics` 上 14 条序列同水位。
3. **管理台「存储队列」卡**（dashboard `QueueStatusCard.tsx`，挂载于
   Overview.tsx:93）：压力灯阈值变色（深度 ≥500 黄 / ≥2000 红，背压退化
   ≥10 黄 / ≥100 红——severityColor，QueueStatusCard.tsx:16）、版本号（水位）、
   合并率、六计数、最近 flush 三元组与 `recent_flushes` 表、按类型应用标签；
   10s 轮询（:8），503 时显示「未启用」（SQL 后端语义化降级，非报错）。

指标语义以本节与 `QueueStats` 字段注释为准；契约测试钉住三面：metrics 抓取
序列（metrics/queue_test.go）、admin 端点 200/503 双路径
（httpapi/coverage_test.go）、队列侧字段由 queue_test.go 既有断言覆盖。
