---
title: 结构化并发与取消模型
---

原则
- 谁创建谁等待：父任务负责等待/收集子任务，避免“悬挂任务”
- 取消可传递：自顶向下传播 `stop_source/stop_token`，子任务按约定响应

模式
- 任务组：提交多个子任务，`join`/`when_all` 收敛结果，统一错误处理
- 超时与撤销：超时触发 `request_stop()`，子任务 `stop_requested()` 后尽快退出

协程样例（伪代码）
```cpp
asio::awaitable<void> parent(){
  std::stop_source ss; auto tok = ss.get_token();
  auto t1 = co_spawn(exec, child(tok), use_awaitable);
  auto t2 = co_spawn(exec, child(tok), use_awaitable);
  // 超时取消
  co_await async_wait(timer, use_awaitable);
  ss.request_stop();
  co_await (t1 && t2); // 等待收敛
}
```

最佳实践
- 设计 API 时显式传递取消句柄；避免“硬中断”
- 错误/取消统一到一种返回通道（异常/expected），简化上层处理
