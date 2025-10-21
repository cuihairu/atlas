---
title: Proto 版本 RPC 示例（.proto + 生成 + handler）
---

目标
- 在现有协程 RPC 骨架上，使用 Protocol Buffers 作为载荷编码：定义 `.proto`、CMake 集成 `protoc` 生成、在 handler 中反/序列化。

示例 proto（hello.proto）
```proto
syntax = "proto3";
package demo;

message HelloReq { string name = 1; }
message HelloRsp { string msg  = 1; }
```

CMake 集成（摘要）
```cmake
find_package(Protobuf REQUIRED)
protobuf_generate_cpp(PROTO_SRCS PROTO_HDRS ${CMAKE_CURRENT_SOURCE_DIR}/hello.proto)

add_library(proto-hello ${PROTO_SRCS} ${PROTO_HDRS})
target_link_libraries(proto-hello PUBLIC protobuf::libprotobuf)
target_include_directories(proto-hello PUBLIC ${CMAKE_CURRENT_BINARY_DIR})
```

编码/解码工具
```cpp
#include <google/protobuf/message.h>
inline std::string to_bytes(const google::protobuf::Message& m){ std::string out; m.SerializeToString(&out); return out; }
template<class T>
inline T from_bytes(std::string_view sv){ T t; t.ParseFromArray(sv.data(), (int)sv.size()); return t; }
```

注册 handler（type=1001）
```cpp
#include "hello.pb.h"    // 由 protoc 生成
// 请求: HelloReq，响应: HelloRsp
r.reg(1001, [](std::string_view pl) -> awaitable<std::string>{
  demo::HelloReq req = from_bytes<demo::HelloReq>(pl);
  demo::HelloRsp rsp; rsp.set_msg("hello, "+req.name());
  co_return to_bytes(rsp);
});
```

客户端调用
```cpp
demo::HelloReq req; req.set_name("atlas");
auto rsp_env = co_await client.call(1001, to_bytes(req), 500ms);
demo::HelloRsp rsp = from_bytes<demo::HelloRsp>(rsp_env.payload);
```

建议
- 大吞吐可考虑 Arena 分配器、zero-copy I/O（配合 Asio buffer sequence）
- 协议版本：在 type 空间或 payload 第一字节引入 version；注意前后兼容
