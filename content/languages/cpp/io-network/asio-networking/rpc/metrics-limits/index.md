---
title: 指标与限流（inflight / qps / 延迟直方图）
---

设计
- inflight 限流：对每连接/全局同时处理请求数量做上限（semaphore）
- QPS/延迟：按类型聚合计数与延迟分布（p50/p95/p99）
- 拒绝策略：超过上限返回“系统繁忙”，或排队 + 超时

示例：信号量限流
```cpp
#include <semaphore>
std::counting_semaphore<1024> sem(MAX_INFLIGHT);

awaitable<void> handle_conn(tcp::socket s, router& r){
  for(;;){
    co_await sem.async_acquire(use_awaitable); // C++ 还无标准 async；可封装 awaiter 或前置 acquire
    auto on_exit = gsl::finally([&]{ sem.release(); });
    envelope req = co_await read_envelope(s);
    // ... 处理
  }
}
```

示例：简易指标（文本导出）
```cpp
struct metrics{
  std::atomic<uint64_t> total{0};
  std::atomic<uint64_t> err{0};
  void on_req(){ total.fetch_add(1, std::memory_order_relaxed);} 
  void on_err(){ err.fetch_add(1, std::memory_order_relaxed);} 
  std::string dump(){ return "rpc_total " + std::to_string(total.load()) + "\nrpc_error " + std::to_string(err.load()) + "\n"; }
} M;
```

直方图（建议）
- 使用外部库（如 Prometheus C++）或自定义 bucket：记录 [0.5,1,2,5,10,20,50,100]ms 桶
- 收敛方式：周期性导出到 /metrics；或写日志由外部 agent 抓取

注意
- 指标采集开销控制：热点路径以原子计数 + 线程本地累积，批量 flush
- 拒绝/超时都计入 error；附上原因标签（reject/timeout/exception）
