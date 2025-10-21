---
title: 固定线程池最小骨架（jthread + 队列 + 停止）
---

目标
- 提供一个可取消的固定大小线程池：支持提交任务、优雅停止与背压控制

最小实现（示意）
```cpp
#include <thread>
#include <stop_token>
#include <condition_variable>
#include <queue>
struct thread_pool{
  std::vector<std::jthread> ws; std::mutex m; std::condition_variable_any cv;
  std::queue<std::function<void(std::stop_token)>> q; bool shutting=false;
  explicit thread_pool(size_t n){
    ws.reserve(n);
    for(size_t i=0;i<n;++i){
      ws.emplace_back([this](std::stop_token st){ worker(st); });
    }
  }
  ~thread_pool(){ shutdown(); }
  void worker(std::stop_token st){
    for(;;){
      std::function<void(std::stop_token)> job;
      { std::unique_lock lk(m); cv.wait(lk, st, [this]{ return shutting || !q.empty(); });
        if(shutting && q.empty()) return; job = std::move(q.front()); q.pop(); }
      if(st.stop_requested()) return; job(st);
    }
  }
  void submit(std::function<void(std::stop_token)> job){
    std::scoped_lock lk(m); if(shutting) throw std::runtime_error("stopped"); q.push(std::move(job)); cv.notify_one();
  }
  void shutdown(){ std::scoped_lock lk(m); shutting=true; cv.notify_all(); }
};
```

背压与限流
- 在 `submit` 前增加计数/`std::counting_semaphore` 限制队列长度
- 否则返回拒绝/阻塞等待（根据业务选择）

结构化并发
- 让拥有池的上层对象在析构时调用 `shutdown()`，并等待任务有序完成
- 任务循环中检查 `stop_token`，尽快响应取消
