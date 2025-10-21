---
title: 同步原语（Mutex/Condvar/Latch/Barrier/Semaphore）
---

清单
- 互斥与锁：`std::mutex`/`shared_mutex` + `lock_guard`/`unique_lock`/`shared_lock`
- 条件变量：`std::condition_variable(_any)`（与 `unique_lock` 配合，循环等待谓词）
- 闭锁/栅栏：`std::latch`/`std::barrier`（C++20）
- 信号量：`std::counting_semaphore`/`binary_semaphore`（C++20）

最佳实践
- 条件变量等待使用谓词：`cv.wait(lock, predicate)`，而不是裸 `wait`
- 锁粒度：缩小临界区；避免在持锁期间执行耗时 IO
- 无锁不等于高性能：优先选择清晰正确的锁设计
- 组合原语：以 `latch/barrier/semaphore` 实现阶段同步、限流/背压

常见陷阱
- 虚假唤醒：必须循环检查谓词
- 死锁：锁顺序不一致；递归持锁；异常路径未释放
- 优先级反转：必要时用协议/调度策略规避
