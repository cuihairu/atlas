---
title: RPC 全家桶示例（路由 + JSON/Proto + 超时/取消/限流/指标）
---

目的
- 在“长度前缀 + 协程 Asio”的基础上，给出可直接复用的 RPC 组件骨架：
  - Envelope 编解码（type/flags/corr_id/payload）
  - 路由注册与分发（支持 JSON/Proto 两栈）
  - 客户端 corr_id → promise 匹配、超时/取消
  - 限流与指标钩子（requests_inflight、latency 等）

Envelope（header-only）
```cpp
#pragma once
#include <cstdint>
#include <string>
#include <array>
struct envelope {
  uint16_t type{};   // 消息类型（1:add、2:ping ...）
  uint16_t flags{};  // 0=OK, 1=ERR, bit 扩展：压缩/编码
  uint32_t corr{};   // 关联 ID
  std::string payload;
};

inline void put_be32(uint32_t v, unsigned char* b){ b[0]=v>>24; b[1]=v>>16; b[2]=v>>8; b[3]=v; }
inline uint32_t be32(const unsigned char* b){ return (uint32_t(b[0])<<24)|(uint32_t(b[1])<<16)|(uint32_t(b[2])<<8)|b[3]; }

inline std::string encode(const envelope& e){
  std::string out; out.resize(4 + 2+2+4 + e.payload.size());
  auto* p = reinterpret_cast<unsigned char*>(out.data());
  put_be32(uint32_t(2+2+4 + e.payload.size()), p); p+=4; // frame length（不含自身）
  p[0]=e.type>>8; p[1]=e.type&0xFF; p+=2;
  p[0]=e.flags>>8; p[1]=e.flags&0xFF; p+=2;
  put_be32(e.corr, p); p+=4;
  std::memcpy(p, e.payload.data(), e.payload.size());
  return out;
}

inline bool decode_header(std::string_view frame, envelope& e){
  if(frame.size() < 2+2+4) return false;
  auto* p = reinterpret_cast<const unsigned char*>(frame.data());
  e.type = (uint16_t(p[0])<<8)|p[1]; p+=2;
  e.flags= (uint16_t(p[0])<<8)|p[1]; p+=2;
  e.corr = be32(p); p+=4;
  e.payload.assign(reinterpret_cast<const char*>(p), frame.data()+frame.size()-reinterpret_cast<const char*>(p));
  return true;
}
```

Asio 协程 I/O（length‑prefix）
```cpp
#include <boost/asio.hpp>
namespace asio = boost::asio; using asio::awaitable; using asio::use_awaitable; using tcp = asio::ip::tcp;

awaitable<std::string> read_frame(tcp::socket& s){
  std::array<unsigned char,4> len{}; co_await asio::async_read(s, asio::buffer(len), use_awaitable);
  uint32_t n = be32(len.data()); std::string buf(n, '\0');
  if(n) co_await asio::async_read(s, asio::buffer(buf), use_awaitable); co_return buf;
}
awaitable<void> write_frame(tcp::socket& s, std::string_view frame){
  std::array<unsigned char,4> len{}; put_be32((uint32_t)frame.size(), len.data());
  std::array<asio::const_buffer,2> bufs{asio::buffer(len), asio::buffer(frame)};
  co_await asio::async_write(s, bufs, use_awaitable);
}
```

路由（JSON 版本示例）
```cpp
#include <nlohmann/json.hpp>
#include <unordered_map>
using json = nlohmann::json;

using handler_t = awaitable<std::string>(std::string_view);
struct router {
  std::unordered_map<uint16_t, std::function<handler_t>> map;
  template<class F> void reg(uint16_t type, F&& f){ map[type]=std::forward<F>(f); }
  handler_t dispatch(uint16_t type, std::string_view payload){
    if(auto it=map.find(type); it!=map.end()) co_return co_await it->second(payload);
    throw std::runtime_error("unknown type");
  }
};

// 注册 add: {"a":1,"b":2} -> {"sum":3}
inline void register_handlers(router& r){
  r.reg(1, [](std::string_view pl) -> awaitable<std::string>{
    auto j = json::parse(pl);
    int a=j["a"], b=j["b"]; co_return json({{"sum", a+b}}).dump();
  });
}
```

服务端
```cpp
awaitable<void> handle_conn(tcp::socket s, router& r){
  try{
    for(;;){
      auto frame = co_await read_frame(s);
      envelope req{}; if(!decode_header(frame, req)) throw std::runtime_error("bad frame");
      envelope rsp{.type=req.type, .flags=0, .corr=req.corr};
      try{ rsp.payload = co_await r.dispatch(req.type, req.payload); }
      catch(std::exception const& e){ rsp.flags=1; rsp.payload=e.what(); }
      co_await write_frame(s, encode(rsp));
    }
  }catch(...){ co_return; }
}
```

客户端（pending 表 + 超时）
```cpp
#include <mutex>
#include <unordered_map>
#include <future>
struct client{
  tcp::socket sock; std::mutex m; std::unordered_map<uint32_t, std::promise<envelope>> pending;
  std::atomic<uint32_t> next{1};
  explicit client(asio::any_io_executor ex): sock(ex){}

  awaitable<void> reader(){
    try{
      for(;;){
        auto frame = co_await read_frame(sock);
        envelope e{}; if(!decode_header(frame, e)) continue;
        std::promise<envelope> p; {
          std::scoped_lock lk(m); auto it=pending.find(e.corr); if(it==pending.end()) continue; p = std::move(it->second); pending.erase(it);
        }
        p.set_value(std::move(e));
      }
    }catch(...){
      std::scoped_lock lk(m); for(auto& [id,p]: pending){ try{ throw std::runtime_error("conn closed"); }catch(...){ p.set_exception(std::current_exception()); } } pending.clear();
    }
  }

  awaitable<envelope> call(uint16_t type, std::string payload, std::chrono::milliseconds to){
    uint32_t id = next.fetch_add(1, std::memory_order_relaxed);
    envelope req{.type=type,.flags=0,.corr=id,.payload=std::move(payload)};
    std::promise<envelope> p; auto fut = p.get_future(); { std::scoped_lock lk(m); pending.emplace(id, std::move(p)); }
    co_await write_frame(sock, encode(req));
    // with timeout
    auto ex = co_await asio::this_coro::executor; asio::steady_timer t(ex); t.expires_after(to);
    auto g = asio::experimental::make_parallel_group(
      asio::async_initiate<decltype(use_awaitable), void()>([&](auto h){ std::thread([f=std::move(fut),h=std::move(h)]() mutable { f.wait(); asio::post([h=std::move(h)]() mutable { h(); }); }).detach(); }, use_awaitable),
      t.async_wait(use_awaitable));
    auto [idx, r1, r2] = co_await g.async_wait(asio::experimental::wait_for_one(), use_awaitable);
    if(idx==1){ std::scoped_lock lk(m); if(auto it=pending.find(id); it!=pending.end()){ try{ throw std::runtime_error("timeout"); }catch(...){ it->second.set_exception(std::current_exception()); } pending.erase(it);} throw std::runtime_error("timeout"); }
    envelope rsp = p.get_future().get(); // already ready
    co_return rsp;
  }
};
```

限流与指标（思路）
- 限流：提交前获取 `std::counting_semaphore` 许可；完成后释放；队列超界直接返回“系统繁忙”。
- 指标：在入口/出口打点（计数、直方图），记录处理时延与错误码；在 server `handle_conn` 周期性导出。

CMake 片段
```cmake
find_package(Boost REQUIRED)
find_package(nlohmann_json CONFIG REQUIRED)
add_library(rpc SHARED rpc.cpp)
target_link_libraries(rpc PRIVATE Boost::boost nlohmann_json::nlohmann_json)
```

小结
- 该骨架覆盖“信封/路由/超时/取消/限流/指标”最小闭环；根据业务逐步引入压缩/鉴权/追踪等。
```
