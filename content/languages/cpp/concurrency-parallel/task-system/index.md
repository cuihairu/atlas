---
title: 任务系统与线程池（Task System / Thread Pool）
---

目标
- 用“任务”抽象替代裸线程：提交可调用对象到线程池；支持优先级/限流/取消
- 结构化并发：父任务等待子任务，传递取消与错误

基本接口草案
```cpp
struct task_handle{ void cancel(); bool done() const; };
struct thread_pool{
  task_handle submit(std::function<void(std::stop_token)> fn);
};
```

设计要点
- 队列：无锁队列 vs MPMC；work-stealing 以降低争用
- 取消：使用 `stop_token`，任务循环中检查并尽快退出
- 背压：以 `semaphore` 限制排队/并发；拒绝/降级策略
- 观测：队列长度/等待时间/执行时间/失败率

与协程整合
- 线程池作为执行器：在 `co_await` 点切换到池线程执行；封装 awaiter
- I/O 线程与计算线程分离：I/O 事件循环 + 计算池
