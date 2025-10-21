---
title: Asio / Networking TS（含协程整合）
---

模型概览
- 基础要素：`io_context`（事件循环）、`executor`、`socket/timer`、`strand`（串行执行）
- 异步风格：回调、`use_future`、`co_await`（C++20 协程）
- 线程模型：单线程循环（串行执行）或多线程池（并发服务）

最小示例：定时器 + 协程
```cpp
#include <boost/asio.hpp>
#include <chrono>
using namespace std::chrono_literals;
namespace asio = boost::asio;

asio::awaitable<void> task(){
  asio::steady_timer t(co_await asio::this_coro::executor);
  t.expires_after(100ms);
  co_await t.async_wait(asio::use_awaitable);
}
int main(){ asio::io_context ctx; co_spawn(ctx, task(), asio::detached); ctx.run(); }
```

TCP 回声服务器（骨架）
```cpp
asio::awaitable<void> session(asio::ip::tcp::socket s){
  try{
    std::array<char, 1024> buf{};
    for(;;){
      std::size_t n = co_await s.async_read_some(asio::buffer(buf), asio::use_awaitable);
      co_await async_write(s, asio::buffer(buf, n), asio::use_awaitable);
    }
  }catch(...){ co_return; }
}
asio::awaitable<void> run_server(unsigned short port){
  auto ex = co_await asio::this_coro::executor;
  asio::ip::tcp::acceptor acc(ex, {asio::ip::tcp::v4(), port});
  for(;;){ asio::ip::tcp::socket s(ex); co_await acc.async_accept(s, asio::use_awaitable); co_spawn(ex, session(std::move(s)), asio::detached); }
}
```

实践建议
- 用 `strand` 保证对同一连接的顺序化处理；避免竞态
- 将取消语义（`stop_token`）纳入任务 API；在 I/O 循环中检查取消并关闭 socket
- 连接池/限流：信号量（`std::counting_semaphore`）保护并发数量
- 度量与观测：每请求计时、错误分类、背压策略（拒绝/排队/降级）

注意事项
- Boost.Asio 与独立 Asio 写法相近；`asio::use_awaitable` 需要 `co_spawn` 与 `io_context::run`
- 与协程结合时，避免在 `await_suspend` 阶段做耗时工作；把计算放在线程池

相关章节
- [[rpc/demo|RPC Demo（server/client/CMake）]]
- [[rpc/envelope-format|RPC 信封（Envelope）与类型表]]
- [[rpc/router|RPC 路由与协议封装]]
- [[rpc/client-routing|客户端路由与请求表]]
- [[rpc/payload-formats|载荷编码（JSON/Proto）选择]]
- [[rpc/full-stack|RPC 全家桶（路由 + JSON/Proto + 超时/取消/限流/指标）]]
