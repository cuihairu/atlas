---
title: RPC 路由与协议封装（type + corr_id + payload）
---

目标
- 在最小协程 RPC 骨架上，加入“请求类型 + 关联 ID（correlation id）”的信封（envelope）
- 支持多 handler 路由、请求/响应匹配、JSON/Proto 作为载荷

信封格式（示意）
- `uint16_t type` + `uint16_t flags` + `uint32_t corr_id` + `uint32_t payload_len` + `bytes`
- 字节序统一为大端；flags 可拓展（压缩/错误码/序列化格式）

路由器接口
```cpp
using handler_t = asio::awaitable<std::string>(std::string_view);
struct router{
  std::unordered_map<uint16_t, std::function<handler_t>> map;
  template<class F> void reg(uint16_t type, F&& f){ map[type]=std::forward<F>(f); }
  handler_t dispatch(uint16_t type, std::string_view payload){
    if(auto it=map.find(type); it!=map.end()) co_return co_await it->second(payload);
    throw std::runtime_error("unknown type");
  }
};
```

服务端流程
```cpp
// 读取信封 → 根据 type 调度 → 回写同 corr_id 的响应
awaitable<void> handle(tcp::socket s, router& r){
  for(;;){
    envelope env = co_await read_envelope(s);
    std::string out;
    try{
      out = co_await r.dispatch(env.type, env.payload);
      env.flags = 0; // ok
    }catch(std::exception const& e){ env.flags = 1; out = e.what(); }
    env.payload = out; co_await write_envelope(s, env);
  }
}
```

JSON 载荷（nlohmann::json）
```cpp
#include <nlohmann/json.hpp>
asio::awaitable<std::string> add_handler(std::string_view pl){
  auto j = nlohmann::json::parse(pl);
  int a=j["a"], b=j["b"]; return nlohmann::json({{"sum", a+b}}).dump();
}
```

Proto 载荷
- 在 handler 内反序列化 `Request`，处理后序列化 `Response`
- 大吞吐可配合 arena allocator / zero-copy buffer

客户端匹配（单连接复用）
- 维护一个 `corr_id -> promise` 的表；请求发送时分配 corr_id，收到响应后完成对应 promise
- 超时/取消：挂起的 promise 设置定时器；取消时从表中移除并关闭 socket 或丢弃响应

最佳实践
- 统一错误返回：flags + payload（错误码/文本），避免协议歧义
- 限流与背压：队列长度上限 + 信号量；超界返回“系统繁忙”
- 观测：每类型请求的计数/延迟分位数/错误分布
