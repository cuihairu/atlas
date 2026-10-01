# Atlas C++ SDK

REST client for Atlas（与 Go SDK 同一 API 面）。单头依赖已 vendor 在
`third_party/`（cpp-httplib v0.58.0、nlohmann/json v3.12.0，见
[VENDORED.md](third_party/VENDORED.md)），无外部系统依赖，C++17 即可编译。

## 构建

```bash
cmake -S . -B build -DATLAS_SDK_BUILD_TESTS=ON
cmake --build build -j
ctest --test-dir build
```

在自己的工程里引用：把本目录 `add_subdirectory` 进来，链接 `atlas_sdk`；
include 根为 `include/` 与 `third_party/`（`#include "atlas/client.hpp"` 即可用）。

## 快速上手

```cpp
#include "atlas/client.hpp"
using namespace std::chrono_literals;

atlas::Client c; // 默认 http://localhost:8080
atlas::Options o;
o.base_url = "http://atlas:8080";
o.registry_token = "..."; // Registry 域 Bearer
atlas::Client c2{o};

auto reg = c2.Register(req);                          // 注册
atlas::AutoHeartbeat loop(c2, "game-1001", 10s);      // 自动心跳（立即首发）
loop.Set(playerCount, loadFactor);                    // 游戏线程更新负载
auto rec = c2.Recommend(42, "cn-east");               // 接入推荐
auto page = c2.ListCharactersByServer(rec.server.id); // 角色目录
c2.Unregister("game-1001");                           // 优雅下线
```

## 错误处理

所有方法失败时抛 `atlas::Error`（`status()` / `code()` / `message()`，
code 与 REST 错误码一致，如 `SERVER_NOT_FOUND`）。网络错误与 5xx 自动
全抖动指数退避重试（`Options::max_retries`，默认 3），4xx 不重试。

## 心跳节奏

上报间隔与 Atlas 判死阈值保持 3:1（默认 suspect 30s / offline 60s →
10s 一跳）。`AutoHeartbeat` 构造后立即首发一次，`Set()` 并发安全。
