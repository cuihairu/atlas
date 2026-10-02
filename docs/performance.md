# 性能设计

Atlas 的性能目标由它的位置决定：它坐在**所有玩家登录路径**上（选服、拉角色、读公告），所以读路径必须便宜；它同时接收**整个舰队**的心跳与角色写入，所以写路径必须横向可扩。本文讲实现层面的取舍——每一条都能对应到仓库里的真实代码，数字来自 [性能基准](/benchmarks)（复现命令在彼处，`go test -bench` 可本地验证）。

## 1. 技术栈与选型

| 层 | 选型 | 为什么 |
| --- | --- | --- |
| 语言 | Go（标准库 `net/http`，Go 1.22+ 方法路由） | 编译型、goroutine 承载扇入、部署物是单个静态二进制 |
| PostgreSQL 驱动 | pgx v5 原生（扩展协议 + `pgxpool`） | 预编译语句 + 二进制传输省去每次解析；连接池内建 |
| Redis | go-redis v9，hash + pipeline | 运行时视图（心跳）是天然 KV，`HSET`+`EXPIRE` 一条管线单 RTT |
| gRPC | google.golang.org/grpc（钉 v1.86.0） | 跨语言 SDK 共用同一套服务定义；版本钉死避免供应链漂移 |
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

一次网络往返写完全部字段；TTL 到期即"下线"，**不需要任何后台清扫协程**。基准：单核 ~350 万 QPS（`BenchmarkHeartbeatParallel` 64 并发扇入 646ns/op）——1 万 QPS 的真实负载用一个核的 0.3%。瓶颈从来不在心跳本身，而在 Redis 与网络。

### 发现 / 选服（读路径之王）

玩家登录拉服务器列表。`internal/discovery` 读路径做三件事：过滤（region/realm/状态）、运行时合并（在线人数/负载）、对外标记过滤（`model.PublicTags`——内存里的切片过滤，零分配级别的开销）。列表是**准实时**数据，网关可安全做 1–5s 短 TTL 缓存；`region` + `limit` 把单响应控制在 100 台量级。

服务层基准（memory store，不含网络）：100 台全量 34µs、1,000 台 419µs。**已知边界**：生产用 Redis 时当前实现对每台服务器做一次 `GetRuntime`（N 次往返，1,000 台 ≈ 300ms）——[基准页](/benchmarks#_3-读数)把这条列为最值得做的后续优化（pipeline 合并），不藏着。

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
