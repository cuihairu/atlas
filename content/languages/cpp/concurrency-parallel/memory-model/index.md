---
title: 内存模型（Memory Model）
---

定义与术语
- 数据竞争（data race）：两个线程并发访问同一对象，且至少一个为写，且未同步 → 未定义行为（UB）
- 顺序关系：`sequenced-before`（单线程内）、`happens-before`（跨线程，经由同步建立）
- 原子操作：`std::atomic<T>` 提供 tear-free 读写与排序保证
- 排序（order）：`relaxed`/`acquire`/`release`/`acq_rel`/`seq_cst`；以及栅栏 `std::atomic_thread_fence`

发布-订阅示例（release/acquire）
```cpp
#include <atomic>
std::atomic<bool> ready{false};
int data;

void producer(){
  data = 42;                    // 普通写
  ready.store(true, std::memory_order_release); // 发布
}
void consumer(){
  while(!ready.load(std::memory_order_acquire)){} // 获取
  // 读到 true 后，对 data 的读与 producer 的写建立 happens-before
  (void)data;
}
```

最佳实践
- 默认使用 `seq_cst` 保证正确性；热点路径再降级到 acquire/release/relaxed
- 共享对象以“拥有者线程”+“消息传递/转移所有权”为主，尽量减少共享可变状态
- 更倾向使用高层同步原语（`mutex/condition_variable/latch/barrier`）
- 不把普通变量当原子使用，避免“竞态读”

常见误区
- 仅对标志位使用原子，但与其关联的数据不是原子且缺少同步 → 读到旧数据
- 自旋等待未让出 CPU（缺少 `pause/yield` 或等待策略）导致性能抖动
- 误用 `memory_order_relaxed` 读取共享数据，破坏一致性
