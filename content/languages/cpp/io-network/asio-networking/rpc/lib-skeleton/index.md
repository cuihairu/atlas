---
title: RPC 库脚手架（文件结构 + 最小代码）
---

用途
- 提供一个最小可用的 RPC 库骨架：`rpc::envelope / rpc::io / rpc::router / rpc::client`，以及示例 server/client。
- 你可以把下面的文件结构直接落到 `examples/rpc-coro/` 之类目录中编译运行。

目录结构（建议）
```
examples/rpc-coro/
  CMakeLists.txt
  include/rpc/envelope.hpp
  include/rpc/io.hpp
  include/rpc/router.hpp
  include/rpc/client.hpp
  src/server.cpp
  src/client.cpp
```

envelope.hpp（长度前缀 + 头部字段）
```cpp
#pragma once
#include <cstdint>
#include <string>
#include <cstring>
namespace rpc{
struct envelope{ uint16_t type{}, flags{}; uint32_t corr{}; std::string payload; };
inline void put_be32(uint32_t v, unsigned char* b){ b[0]=v>>24; b[1]=v>>16; b[2]=v>>8; b[3]=v; }
inline uint32_t be32(const unsigned char* b){ return (uint32_t(b[0])<<24)|(uint32_t(b[1])<<16)|(uint32_t(b[2])<<8)|b[3]; }
inline std::string encode(envelope const& e){ std::string out; out.resize(4+2+2+4+e.payload.size()); auto* p=(unsigned char*)out.data();
  put_be32(uint32_t(2+2+4+e.payload.size()), p); p+=4; p[0]=e.type>>8; p[1]=e.type&0xFF; p+=2; p[0]=e.flags>>8; p[1]=e.flags&0xFF; p+=2; put_be32(e.corr,p); p+=4; std::memcpy(p,e.payload.data(),e.payload.size()); return out; }
inline bool decode_header(std::string_view frame, envelope& e){ if(frame.size()<8) return false; auto* p=(unsigned char const*)frame.data();
  e.type=(uint16_t(p[0])<<8)|p[1]; p+=2; e.flags=(uint16_t(p[0])<<8)|p[1]; p+=2; e.corr=be32(p); p+=4; e.payload.assign((char const*)p, frame.size()-8); return true; }
}
```

io.hpp（Asio 协程读写帧）
```cpp
#pragma once
#include <boost/asio.hpp>
#include "rpc/envelope.hpp"
namespace rpc{ namespace asio = boost::asio; using tcp=asio::ip::tcp; using asio::awaitable; using asio::use_awaitable;
inline awaitable<std::string> read_frame(tcp::socket& s){ std::array<unsigned char,4> len{}; co_await asio::async_read(s, asio::buffer(len), use_awaitable); uint32_t n=be32(len.data()); std::string buf(n,'\0'); if(n) co_await asio::async_read(s, asio::buffer(buf), use_awaitable); co_return buf; }
inline awaitable<void> write_frame(tcp::socket& s, std::string_view frame){ std::array<unsigned char,4> len{}; put_be32((uint32_t)frame.size(), len.data()); std::array<asio::const_buffer,2> bufs{asio::buffer(len), asio::buffer(frame)}; co_await asio::async_write(s, bufs, use_awaitable); }
}
```

router.hpp（JSON 路由示例）
```cpp
#pragma once
#include <boost/asio.hpp>
#include <nlohmann/json.hpp>
#include <unordered_map>
namespace rpc{ namespace asio=boost::asio; using asio::awaitable; using nlohmann::json;
using handler_t = awaitable<std::string>(std::string_view);
struct router{ std::unordered_map<uint16_t, std::function<handler_t>> map; template<class F> void reg(uint16_t t, F&& f){ map[t]=std::forward<F>(f);} handler_t dispatch(uint16_t t, std::string_view pl){ if(auto it=map.find(t); it!=map.end()) co_return co_await it->second(pl); throw std::runtime_error("unknown type"); } };
inline void register_default(router& r){ r.reg(1, [](std::string_view pl)->awaitable<std::string>{ auto j=json::parse(pl); int a=j["a"], b=j["b"]; co_return json({{"sum",a+b}}).dump(); }); }
}
```

client.hpp（最小 pending 表 + 超时）
```cpp
#pragma once
#include <boost/asio.hpp>
#include <unordered_map>
#include <future>
#include <mutex>
#include "rpc/envelope.hpp"; #include "rpc/io.hpp"
namespace rpc{ namespace asio=boost::asio; using asio::awaitable; using asio::use_awaitable; using tcp=asio::ip::tcp;
struct client{ tcp sock; std::mutex m; std::unordered_map<uint32_t, std::promise<envelope>> pending; std::atomic<uint32_t> next{1}; explicit client(asio::any_io_executor ex): sock(ex){}
  awaitable<void> reader(){ try{ for(;;){ auto frame=co_await read_frame(sock); envelope e{}; if(!decode_header(frame,e)) continue; std::promise<envelope> p; { std::scoped_lock lk(m); auto it=pending.find(e.corr); if(it==pending.end()) continue; p=std::move(it->second); pending.erase(it);} p.set_value(std::move(e)); } }catch(...){ std::scoped_lock lk(m); for(auto& [id,p]:pending){ try{ throw std::runtime_error("conn closed"); }catch(...){ p.set_exception(std::current_exception()); } } pending.clear(); } }
  awaitable<envelope> call(uint16_t type, std::string payload, std::chrono::milliseconds to){ uint32_t id=next.fetch_add(1); envelope req{.type=type,.flags=0,.corr=id,.payload=std::move(payload)}; std::promise<envelope> p; auto fut=p.get_future(); { std::scoped_lock lk(m); pending.emplace(id,std::move(p)); } co_await write_frame(sock, encode(req)); auto ex=co_await asio::this_coro::executor; asio::steady_timer t(ex); t.expires_after(to); auto g = asio::experimental::make_parallel_group( asio::async_initiate<decltype(use_awaitable), void()>([&](auto h){ std::thread([f=std::move(fut),h=std::move(h)]() mutable { f.wait(); asio::post([h=std::move(h)]() mutable { h(); }); }).detach(); }, use_awaitable), t.async_wait(use_awaitable)); auto [idx, r1, r2] = co_await g.async_wait(asio::experimental::wait_for_one(), use_awaitable); if(idx==1){ std::scoped_lock lk(m); if(auto it=pending.find(id); it!=pending.end()){ try{ throw std::runtime_error("timeout"); }catch(...){ it->second.set_exception(std::current_exception()); } pending.erase(it);} throw std::runtime_error("timeout"); } envelope rsp = p.get_future().get(); co_return rsp; }
}; }
```

server.cpp
```cpp
#include <boost/asio.hpp>
#include "rpc/envelope.hpp"; #include "rpc/io.hpp"; #include "rpc/router.hpp"
namespace asio=boost::asio; using asio::awaitable; using asio::use_awaitable; using tcp=asio::ip::tcp; using rpc::envelope;
awaitable<void> handle(tcp::socket s, rpc::router& r){ try{ for(;;){ auto frame=co_await rpc::read_frame(s); envelope req{}; if(!rpc::decode_header(frame, req)) throw std::runtime_error("bad"); envelope rsp{.type=req.type,.flags=0,.corr=req.corr}; try{ rsp.payload = co_await r.dispatch(req.type, req.payload);}catch(std::exception const& e){ rsp.flags=1; rsp.payload=e.what(); } co_await rpc::write_frame(s, rpc::encode(rsp)); } }catch(...){ co_return; } }
awaitable<void> run(unsigned short port){ auto ex=co_await asio::this_coro::executor; tcp::acceptor acc(ex,{tcp::v4(),port}); rpc::router r; rpc::register_default(r); for(;;){ tcp::socket s(ex); co_await acc.async_accept(s,use_awaitable); co_spawn(ex, handle(std::move(s), r), asio::detached);} }
int main(){ asio::io_context ctx; co_spawn(ctx, run(5555), asio::detached); ctx.run(); }
```

client.cpp
```cpp
#include <boost/asio.hpp>
#include <iostream>
#include "rpc/client.hpp"
namespace asio=boost::asio; using asio::awaitable; using asio::use_awaitable; using tcp=asio::ip::tcp; using namespace std::chrono_literals;
awaitable<void> run(){ auto ex=co_await asio::this_coro::executor; rpc::client c(ex); co_await c.sock.async_connect({asio::ip::make_address("127.0.0.1"),5555},use_awaitable); co_spawn(ex, c.reader(), asio::detached); auto rsp = co_await c.call(1, "{\"a\":2,\"b\":3}", 500ms); std::cout << rsp.payload << "\n"; }
int main(){ asio::io_context ctx; co_spawn(ctx, run(), asio::detached); ctx.run(); }
```

CMakeLists.txt（最小）
```cmake
cmake_minimum_required(VERSION 3.25)
project(rpc_coro LANGUAGES CXX)
set(CMAKE_CXX_STANDARD 20)
find_package(Boost REQUIRED)
find_package(nlohmann_json CONFIG REQUIRED)
add_executable(server src/server.cpp)
target_include_directories(server PRIVATE include)
target_link_libraries(server PRIVATE Boost::boost nlohmann_json::nlohmann_json)
add_executable(client src/client.cpp)
target_include_directories(client PRIVATE include)
target_link_libraries(client PRIVATE Boost::boost)
```

说明
- 该脚手架可直接迁移到独立仓库；根据需要替换 JSON 为 Proto，并加上限流/指标等增强。
