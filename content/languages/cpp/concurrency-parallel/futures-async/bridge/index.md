---
title: 协程 ↔ future 桥接（互操作示例）
---

场景
- 代码库混合使用 `std::future` 与协程（`co_await`），需要桥接：
  - 在协程内等待一个 `std::future`
  - 将 `awaitable<T>` 暴露为 `std::future<T>`

在协程中等待 std::future（概念示例）
```cpp
template<class T>
struct future_awaiter{
  std::future<T>& f;
  bool await_ready() const noexcept { return f.wait_for(std::chrono::seconds(0)) == std::future_status::ready; }
  void await_suspend(std::coroutine_handle<> h){ std::thread([&f=f,h]{ f.wait(); h.resume(); }).detach(); }
  T await_resume(){ return f.get(); }
};

template<class T>
auto as_awaitable(std::future<T>& f){ return future_awaiter<T>{f}; }
```

将 awaitable 暴露为 std::future（概念示例）
```cpp
template<class T, class Aw>
std::future<T> to_future(Aw aw){
  std::promise<T> p; auto fut = p.get_future();
  std::thread([aw=std::move(aw), p=std::move(p)]() mutable {
    try{ p.set_value(co_await aw); } catch(...){ p.set_exception(std::current_exception()); }
  }).detach();
  return fut;
}
```

注意
- 简化示例：生产代码需纳入执行器/事件循环、取消（`stop_token`）、生命周期与异常安全
- 可直接使用已有桥接库或在任务系统中提供统一 API，以减少样板
