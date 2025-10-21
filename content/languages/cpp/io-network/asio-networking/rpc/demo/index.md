---
title: 协程化 RPC Demo（可编译示例：server/client/CMake）
---

说明
- 使用 Boost.Asio + C++20 协程，基于“长度前缀”一问一答协议；包含超时、基础错误处理。
- 仅示例骨架，未做生产级健壮性（认证、限流、压缩、协议版本等）。

server.cpp
```cpp
#include <boost/asio.hpp>
#include <array>
#include <string>
#include <iostream>
namespace asio = boost::asio; using asio::awaitable; using asio::use_awaitable; using asio::ip::tcp;

inline uint32_t be32(const unsigned char* b){ return (b[0]<<24)|(b[1]<<16)|(b[2]<<8)|b[3]; }
inline void put_be32(uint32_t v, unsigned char* b){ b[0]=v>>24; b[1]=v>>16; b[2]=v>>8; b[3]=v; }

awaitable<std::string> read_msg(tcp::socket& s){
  std::array<unsigned char,4> hdr{}; co_await asio::async_read(s, asio::buffer(hdr), use_awaitable);
  uint32_t n = be32(hdr.data()); std::string buf(n, '\0');
  if(n) co_await asio::async_read(s, asio::buffer(buf), use_awaitable); co_return buf;
}
awaitable<void> write_msg(tcp::socket& s, std::string_view p){
  std::array<unsigned char,4> hdr{}; put_be32((uint32_t)p.size(), hdr.data());
  std::array<asio::const_buffer,2> bufs{asio::buffer(hdr), asio::buffer(p)};
  co_await asio::async_write(s, bufs, use_awaitable);
}

awaitable<void> session(tcp::socket s){
  try{
    for(;;){
      auto req = co_await read_msg(s);
      std::string rsp = "echo:" + req; // 业务处理
      co_await write_msg(s, rsp);
    }
  }catch(std::exception const& e){ std::cerr << "session err: " << e.what() << "\n"; }
}

awaitable<void> run_server(unsigned short port){
  auto ex = co_await asio::this_coro::executor; tcp::acceptor acc(ex, {tcp::v4(), port});
  for(;;){ tcp::socket s(ex); co_await acc.async_accept(s, use_awaitable); co_spawn(ex, session(std::move(s)), asio::detached); }
}

int main(){ try{ asio::io_context ctx; co_spawn(ctx, run_server(5555), asio::detached); ctx.run(); }catch(...){ return 1; } }
```

client.cpp（含超时）
```cpp
#include <boost/asio.hpp>
#include <chrono>
#include <iostream>
#include <array>
namespace asio = boost::asio; using asio::awaitable; using asio::use_awaitable; using tcp = asio::ip::tcp;
using namespace std::chrono_literals;

inline uint32_t be32(const unsigned char* b){ return (b[0]<<24)|(b[1]<<16)|(b[2]<<8)|b[3]; }
inline void put_be32(uint32_t v, unsigned char* b){ b[0]=v>>24; b[1]=v>>16; b[2]=v>>8; b[3]=v; }

awaitable<std::string> read_msg(tcp::socket& s){ std::array<unsigned char,4> h{}; co_await asio::async_read(s, asio::buffer(h), use_awaitable); uint32_t n=be32(h.data()); std::string b(n,'\0'); if(n) co_await asio::async_read(s, asio::buffer(b), use_awaitable); co_return b; }
awaitable<void> write_msg(tcp::socket& s, std::string_view p){ std::array<unsigned char,4> h{}; put_be32((uint32_t)p.size(), h.data()); std::array<asio::const_buffer,2> bufs{asio::buffer(h), asio::buffer(p)}; co_await asio::async_write(s, bufs, use_awaitable); }

template<class Aw>
awaitable<std::string> with_timeout(asio::any_io_executor ex, std::chrono::milliseconds d, Aw aw){
  asio::steady_timer t(ex); t.expires_after(d); bool timeout=false; std::string out;
  auto g = asio::experimental::make_parallel_group(aw, t.async_wait(use_awaitable));
  auto [idx, r1, r2] = co_await g.async_wait(asio::experimental::wait_for_one(), use_awaitable);
  if(idx==1) timeout=true; else out = std::move(std::get<0>(r1));
  if(timeout) throw std::runtime_error("timeout");
  co_return out;
}

awaitable<void> run(){
  auto ex = co_await asio::this_coro::executor; tcp::socket s(ex); co_await s.async_connect({asio::ip::make_address("127.0.0.1"), 5555}, use_awaitable);
  std::string payload = "hello";
  co_await write_msg(s, payload);
  auto rsp = co_await with_timeout(ex, 500ms, read_msg(s));
  std::cout << rsp << "\n";
}

int main(){ asio::io_context ctx; co_spawn(ctx, run(), asio::detached); ctx.run(); }
```

CMake 最小片段
```cmake
cmake_minimum_required(VERSION 3.25)
project(rpc_demo LANGUAGES CXX)
set(CMAKE_CXX_STANDARD 20)
find_package(Boost REQUIRED)
add_executable(server server.cpp)
target_link_libraries(server PRIVATE Boost::boost)
add_executable(client client.cpp)
target_link_libraries(client PRIVATE Boost::boost)
```

提示
- 如果使用独立 Asio（非 Boost），`find_package(asio CONFIG REQUIRED)` 并链接 `asio::asio`
- Windows 上需定义 `_WIN32_WINNT` 目标版本，或在编译选项中指定
```
