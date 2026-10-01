# 性能基准

Registry 与 Discovery 的服务层基准（`go test -bench`，memory store，排除网络与
序列化），用于回答两个容量问题：**一台 Atlas 能接多少心跳**、**一次列表有多贵**。
Redis / PostgreSQL 后端的端到端数字取决于部署拓扑（见 [ha.md](ha)），本文给的是
服务层逻辑开销的下界（上界）参照。

## 1. 复现方式

```bash
# 全部基准（约 15s）
go test ./internal/registry/ ./internal/discovery/ -run xxx -bench . -benchtime 1s -benchmem

# 单项（回归对比时用 -count=5 取中位数）
go test ./internal/discovery/ -run xxx -bench BenchmarkListServers -benchmem
```

QPS ≈ `1e9 / ns_per_op`（单 goroutine）；`-benchmem` 的 allocs/op 反映每次调用的
分配次数，是回归检测比时间更稳定的信号。

## 2. 基线数字

2026-10，Linux / amd64 / i9-10880H（14 逻辑核），Go 1.27：

| 基准 | 场景 | ns/op | B/op | allocs/op | 单核 QPS |
| --- | --- | ---: | ---: | ---: | ---: |
| `BenchmarkRegister` | 新服务器注册（写路径 + 初始心跳） | 2,287 | 949 | 8 | ~437k |
| `BenchmarkReRegister` | 幂等重注册（控制面重启恢复路径） | 622 | 328 | 5 | ~1.6M |
| `BenchmarkHeartbeat` | 心跳热路径（每服务器每 10s 一次） | 287 | 256 | 1 | ~3.5M |
| `BenchmarkHeartbeatParallel` | 64 台服务器并发心跳扇入 | 646 | 272 | 3 | ~1.5M×14 核 |
| `BenchmarkGetServer` | 单服务器详情（含运行时合并） | 281 | 320 | 2 | ~3.6M |
| `BenchmarkListServers/fleet_100` | 全量列表（100 台，含运行时合并） | 34.2µ | 31KB | 160 | ~29k |
| `BenchmarkListServers/fleet_1000` | 全量列表（1,000 台） | 419µ | 277KB | 1,063 | ~2.4k |
| `BenchmarkListServers/fleet_5000` | 全量列表（5,000 台） | 2.9m | 1.4MB | 5,068 | ~345 |
| `BenchmarkListServersRegion` | region 过滤 + limit 100（1,000 台） | 443µ | 280KB | 1,113 | ~2.3k |

## 3. 读数

- **心跳不是瓶颈**：单核 350 万 QPS 意味着 10 万台服务器 × 10s 心跳 = 1 万 QPS，
  一个核的 0.3%；扇入并发下每 op 略涨（锁竞争），仍有两个数量级余量。容量规划
  时先算心跳，**但真正的约束在 Redis 写入与网络**（见下）。
- **注册比重心跳贵 8 倍**（校验 + upsert + 初始心跳），但注册只在启动/恢复时发生，
  峰值 = 全舰队同时重注册。1 万台舰队冷恢复 ≈ 2.3 万 QPS·核，单副本即可吸收。
- **列表是 O(fleet)**：时间与分配随舰队规模线性增长，且当前实现对每台服务器做
  一次独立 `GetRuntime`。生产用 Redis 时这就是 **N 次网络往返**——这是本文
  最重要的读数：

| 舰队规模 | 服务层耗时 | Redis 后端估算（0.3ms/RTT） |
| --- | ---: | ---: |
| 100 | 34µs | ~30ms |
| 1,000 | 419µs | ~300ms |
| 5,000 | 2.9ms | ~1.5s |

  缓解手段（当前即可用，无需改代码）：网关对 `/v1/discovery/servers` 做短 TTL
  缓存（列表是准实时数据，1–5s 缓存不改变语义）；`region` / `limit` 过滤把单次
  响应控制在 100 台量级。**管线化（MGET / pipeline）合并 GetRuntime 是后续
  版本最值得做的优化**（预期把 Redis 往返从 N 次降到 1–2 次）。

- **回归警戒线**：Heartbeat > 1µs、Register > 10µs、fleet_1000 列表 > 1ms
  （服务层数字）即应停下来查 diff，通常意味着多了锁或分配。

## 4. 方法与边界

- memory store：测的是**服务层逻辑**（校验、组装、过滤、合并）的上界参照，
  不含网络、TLS、JSON 编解码与存储 I/O。
- `-benchtime 1s` 的单次运行有 ±5% 噪声；对比优化效果请用 `-count=5` +
  `benchstat`。
- 心跳 / 注册数字来自唯一 ID 的全新写入；幂等重注册单独列出。
