---
title: 线程与 jthread/stop_token
---

核心要点
- `std::jthread`（C++20）：RAII 线程，析构自动请求停止并 `join`，避免泄漏
- 停止协作：`std::stop_source`/`std::stop_token`/`std::stop_callback`
- 线程所有权：避免 `detach`；明确主线程或者线程池管理者的生命周期
- 与协程/任务系统：将线程作为“执行资源”，优先以任务/工作队列抽象而非裸线程

示例：jthread + stop_token
```cpp
#include <thread>
#include <stop_token>
#include <atomic>
using namespace std::chrono_literals;

void worker(std::stop_token st){
  while(!st.stop_requested()){
    // do work
    std::this_thread::sleep_for(10ms);
  }
}

int main(){ std::jthread th(worker); /* RAII */ }
```

最佳实践
- 以 `jthread` 代替 `thread` + 手写 join；长任务可在循环里检查 `stop_token`
- 把取消语义贯穿到任务 API，避免“强行终止”
- 使用 `std::latch/barrier/semaphore` 组织并行阶段/限流
- 监控线程数量与绑定策略，避免过度上下文切换

常见陷阱
- `detach` 导致难以控制生命周期与资源回收
- 未 join/停止线程造成退出 hang 或资源泄漏
- 竞态关闭：主线程释放共享资源过早，工作线程仍在访问
