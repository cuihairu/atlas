# C++ SDK

`sdk/cpp/`（v0.1.7 交付）。REST 客户端，与 Go SDK 同一 API 面。依赖已以单头
形式 vendor 在 `sdk/cpp/third_party/`（cpp-httplib v0.58.0、nlohmann/json
v3.12.0，见 [VENDORED.md](https://github.com/cuihairu/atlas/blob/main/sdk/cpp/third_party/VENDORED.md)），
无外部系统依赖，C++17 即可编译。

## 构建

```bash
cd sdk/cpp
cmake -S . -B build -DATLAS_SDK_BUILD_TESTS=ON
cmake --build build -j
ctest --test-dir build
```

在自己的工程里引用：`add_subdirectory` 本目录后链接 `atlas_sdk`，include
根为 `include/` 与 `third_party/`：

```cpp
#include "atlas/client.hpp"
```

## 快速上手

```cpp
using namespace std::chrono_literals;

atlas::Options o;
o.base_url = "http://atlas:8080";          // 公网 + Admin 域
o.registry_base_url = "http://atlas:8081"; // Registry 独立端口（可选）
o.registry_token = "...";                  // Registry 域 Bearer
o.admin_api_key = "...";                   // Admin 域 Bearer（ATLAS_ADMIN_API_KEYS 之一）
o.default_headers = {{"X-Request-ID", "..."}}; // 每次调用都带的静态头（可选，关联 id 见 api.md「请求追踪」）
atlas::Client c{o};

atlas::RegisterRequest req;
req.server_id = "game-1001"; req.name = "Game 1001";
req.region = "cn-east"; req.capacity = 2000;
req.endpoint = {"10.0.0.1", 30001};
auto reg = c.Register(req);                       // 注册

atlas::AutoHeartbeat loop(c, req.server_id, 10s); // 自动心跳（立即首发）
loop.Set(playerCount, loadFactor);                // 游戏线程更新负载

auto rec = c.Recommend(42, "cn-east");            // 接入推荐
auto page = c.ListCharactersByServer(rec.server.id); // 角色目录

c.Unregister(req.server_id);                      // 优雅下线
```

完整示例见 [`examples/cpp`](https://github.com/cuihairu/atlas/blob/main/examples/cpp/main.cpp)：

```bash
cd examples/cpp && cmake -S . -B build && cmake --build build
ATLAS_ADDR=http://localhost:8080 ATLAS_REGISTRY_ADDR=http://localhost:8081 ./build/atlas_example_cpp
```

## 关键语义

| 主题 | 说明 |
| --- | --- |
| 错误处理 | 所有方法失败抛 `atlas::Error`（`status()` / `code()` / `message()`），code 与 REST 错误码一致 |
| 重试 | 网络错误 + 5xx 自动全抖动指数退避（`max_retries` 默认 3）；4xx 不重试 |
| 心跳节奏 | 上报间隔 : 判死阈值 = 1:3（默认 10s / suspect 30s / offline 60s） |
| 目录写回复 | 同步 REST 返回扁平 Character 对象（SDK 归一化为 `status="created"/"updated"`）；异步适配器返回 `{"status":"queued"}`（`character` 为空） |
| 三端口 | `base_url` 覆盖公网/Admin；`registry_base_url` 覆盖 Registry 独立端口；反代合并部署时两者同值即可 |
| 空集合 | 服务端空列表可能序列化为 `null`，SDK 按 `[]` 处理 |
