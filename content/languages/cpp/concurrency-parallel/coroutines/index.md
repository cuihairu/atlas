---
title: 协程（Coroutines）
---

提要：C++20 引入“栈无关协程”（stackless coroutines）。协程是一种语言级状态机转换机制，本质是编译器把协程函数拆解为状态机，配合“可等待体”（awaitable）与“等待器”（awaiter）协议驱动调度。

核心概念
- 协议三件套：`co_await` / `co_yield` / `co_return`
- `awaitable`/`awaiter`：满足 `await_ready/await_suspend/await_resume` 协议的类型
- `promise_type`：由“返回对象”决定（如 `task<T>`/`generator<T>`），控制生命周期、异常、返回值
- 执行模型：对称（手写调度器） vs 非对称（典型 `co_await`）
- 取消与停止：与 `std::stop_token` 协作，或自定义取消协议

常见返回类型模型
- 单次结果：`task<T>`（用户自定义/库实现），语义类似 future；`co_return T`
- 产出序列：生成器（generator）语义，`co_yield` 逐步产出元素
- 事件/协作：`co_await` 等待 IO/定时器/信号（需运行时/事件循环对接）

最佳实践
- 封装统一 `task<T>`：屏蔽 `promise_type` 细节，集中管理异常、取消、调度
- 协程与线程池：`await_suspend` 中把控制权移交线程池/事件循环，避免阻塞
- 资源与异常：在 `promise_type::unhandled_exception` 及 `final_suspend` 做善后
- 结构化并发：封装“任务组”，父任务负责等待/取消子任务，避免“悬挂协程”
- 取消传播：协程入口接收 `stop_token`，在循环中检查并快速退出

常见陷阱
- 与 RAII：协程分段执行，注意局部对象跨挂起点的生命周期与移动/拷贝
- 悬挂协程：未被 `co_await` 的“火并忘”（fire-and-forget）协程泄漏
- 事件循环耦合：标准未内置事件循环，需要选择 asio/liburing/自研等方案对接
- 同步原语：协程内请用“协程友好”的等待（如 `co_await` event），避免阻塞线程

进一步阅读
- P0912、P1056（协程设计与语义）；`cppcoro`/`folly::coro` 等库实现
- 与 `std::jthread`/`stop_token`/`latch`/`semaphore` 协作的结构化并发模式
