---
title: 原子操作（Atomics）
---

原子类型
- `std::atomic<T>`：对 `T` 的读写不可分割；对标量、指针等有特化；自定义 `T` 需满足 TriviallyCopyable
- 操作族：`load/store/exchange/compare_exchange_{weak,strong}/fetch_add/sub/...`
- 排序语义：`memory_order_relaxed/acquire/release/acq_rel/seq_cst`

示例：一次性初始化（双检锁应首选 `call_once`）
```cpp
#include <atomic>
std::atomic<bool> inited{false};
void init(){ /* heavy work */ }
void ensure(){
  bool expected=false;
  if(inited.compare_exchange_strong(expected,true,std::memory_order_acq_rel)){
    init();
  } else {
    while(!inited.load(std::memory_order_acquire)){}
  }
}
```

最佳实践
- 尽量以高层原语（`mutex`）实现正确性；在热点路径再以原子化提升
- `compare_exchange_weak` 允许伪失败，适合自旋；强语义用 `strong`
- 跨线程发布用 `release`，订阅用 `acquire`；全序需求再用 `seq_cst`
- 警惕 ABA 问题：指针无版本/标签时 lock-free 结构可能出错

常见陷阱
- 非原子读共享数据导致数据竞争
- relaxed 读写未与其他变量建立顺序，读取到旧值
- 使用 `volatile` 代替原子（错误做法）：C++ 中 `volatile` 与并发无关
