---
title: 协程化 RPC 骨架（Asio + length-prefix + 超时/取消）
---

目标
- 以 C++20 协程整合 Asio，构建“长度前缀”的最简 RPC：请求/响应一问一答，带超时与取消。

消息格式
- `uint32_t length (BE)` + `payload bytes`
- 编码/解码：先读固定 4 字节，再读 payload；写时先写长度再写负载

工具函数
```cpp
#include <boost/asio.hpp>
#include <array>
namespace asio = boost::asio;
using asio::awaitable; using asio::use_awaitable; using asio::ip::tcp;

inline uint32_t be32(void const* p){ auto b=(unsigned char const*)p; return (b[0]<<24)|(b[1]<<16)|(b[2]<<8)|b[3]; }
inline void put_be32(uint32_t v, unsigned char* b){ b[0]=v>>24; b[1]=v>>16; b[2]=v>>8; b[3]=v; }

awaitable<std::string> read_msg(tcp::socket& s){
  std::array<unsigned char,4> hdr{}; co_await asio::async_read(s, asio::buffer(hdr), use_awaitable);
  uint32_t n = be32(hdr.data()); std::string buf(n, '\0');
  if(n>0) co_await asio::async_read(s, asio::buffer(buf), use_awaitable); co_return buf;
}

awaitable<void> write_msg(tcp::socket& s, std::string_view p){
  std::array<unsigned char,4> hdr{}; put_be32((uint32_t)p.size(), hdr.data());
  std::array<asio::const_buffer,2> bufs{asio::buffer(hdr), asio::buffer(p)};
  co_await asio::async_write(s, bufs, use_awaitable);
}
```

超时封装（计时器 + 并发等待）
```cpp
template<class Awaitable>
awaitable<typename Awaitable::await_result_t> with_timeout(asio::any_io_executor ex, std::chrono::steady_clock::duration d, Awaitable aw){
  asio::steady_timer t(ex); t.expires_after(d);
  bool timed=false; typename Awaitable::await_result_t res{};
  co_await (
    (aw && t.async_wait(use_awaitable))
    | asio::experimental::make_parallel_group
  ).async_wait(
    asio::experimental::wait_for_one(),
    [&](auto which, auto r1, auto){ if(which.index()==1){ timed=true; } else { res = std::move(std::get<0>(r1)); } },
    use_awaitable
  );
  if(timed) throw std::runtime_error("timeout");
  co_return res;
}
```

服务器骨架
```cpp
awaitable<void> handle(tcp::socket s){
  try{
    for(;;){
      auto req = co_await read_msg(s);
      std::string rsp = "echo:" + req; // TODO: 调用业务handler
      co_await write_msg(s, rsp);
    }
  }catch(...){}
}
awaitable<void> server(unsigned short port){
  auto ex = co_await asio::this_coro::executor; tcp::acceptor acc(ex, {tcp::v4(), port});
  for(;;){ tcp::socket s(ex); co_await acc.async_accept(s, use_awaitable); co_spawn(ex, handle(std::move(s)), asio::detached); }
}
```

客户端骨架（超时 + 取消）
```cpp
awaitable<std::string> rpc_call(tcp::socket& s, std::string_view payload, std::chrono::milliseconds to){
  co_await write_msg(s, payload);
  auto ex = co_await asio::this_coro::executor;
  co_return co_await with_timeout(ex, to, read_msg(s));
}
```

实践建议
- 每连接一个 `strand` 确保同一连接的顺序；跨连接并发由多 socket 自然实现
- 请求上下文传入取消句柄；取消时关闭 socket/取消所有待挂起 I/O
- 编解码层抽象为独立单元，便于替换成 JSON/Proto 等
