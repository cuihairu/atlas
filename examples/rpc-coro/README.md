# RPC 协程示例（Boost.Asio + C++20）

最小的“长度前缀 + 协程 Asio”RPC 示例，含 server/client。

## 先决条件
- CMake ≥ 3.25
- C++20 编译器
- Boost（头文件即可）
- 可选：nlohmann_json（若要注册 JSON handler）

## 构建与运行
```bash
cd examples/rpc-coro
cmake -B build -S .
cmake --build build -j
# 终端1
./build/server
# 终端2
./build/client
```

默认路由未注册时，server 作为 echo 服务返回 `echo:<payload>`。
你可以在 `src/server.cpp` 中对 `rpc::router` 注册 JSON/Proto handler。

## 结构
```
include/rpc/
  envelope.hpp  # 信封：type/flags/corr/payload + 编解码
  io.hpp        # 帧 I/O：4字节长度前缀
  router.hpp    # 协程 handler 路由
  client.hpp    # pending 表 + 超时
src/
  server.cpp    # 协程化会话循环
  client.cpp    # 发请求 + 超时
```

## 可选：vcpkg/Conan
- vcpkg：`nlohmann_json`/`protobuf` 等可通过清单安装
- Conan：生成 `CMakeDeps/CMakeToolchain` 后，按 preset 构建
