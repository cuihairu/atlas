---
title: 客户端路由与请求表（corr_id → promise）
---

目标
- 单连接多并发请求：每个请求分配 `corr_id`，将 `corr_id → promise` 存入表，收到响应时完成对应 promise。

核心结构（概念化）
```cpp
struct pending_entry{ std::promise<std::string> p; std::chrono::steady_clock::time_point ddl; };
struct client{
  std::unordered_map<uint32_t, pending_entry> pending;
  std::mutex m; std::atomic<uint32_t> next{1};
  asio::awaitable<std::string> call(uint16_t type, std::string payload, std::chrono::milliseconds to);
  asio::awaitable<void> reader_loop(); // 持续读取响应，根据 corr_id 完成 promise
};
```

调用路径（简化）
```cpp
asio::awaitable<std::string> client::call(uint16_t type, std::string payload, std::chrono::milliseconds to){
  uint32_t id = next.fetch_add(1, std::memory_order_relaxed);
  // 写入 pending 表并设置超时（轮询或定时器扫描）
  // 发送信封+载荷；等待对应 promise future
  co_return co_await to_awaitable(fut, to);
}
```

reader_loop
- 读取信封 → 查找 pending 表 → 完成 promise（set_value 或 set_exception）→ 移除
- 处理超时：后台协程扫描 `ddl`，对超时项 `set_exception(timeout)` 并移除

注意事项
- 线程安全：写入/读取 pending 表需加锁或 sharded map；corr_id 单调递增并回绕
- 退出：连接关闭时把所有挂起 promise 以异常完成
- 背压：限制 pending 数量；超界快速失败或排队
