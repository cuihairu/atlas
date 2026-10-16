[English](sdk-cpp.md) | [简体中文](zh/sdk-cpp.md)

# C++ SDK

`sdk/cpp/` (delivered in v0.1.7). A REST client with the same API surface as
the Go SDK. Dependencies are vendored as single headers under
`sdk/cpp/third_party/` (cpp-httplib v0.58.0, nlohmann/json
v3.12.0, see [VENDORED.md](https://github.com/cuihairu/atlas/blob/main/sdk/cpp/third_party/VENDORED.md)),
with no external system dependencies; C++17 is enough to compile.

## Build

```bash
cd sdk/cpp
cmake -S . -B build -DATLAS_SDK_BUILD_TESTS=ON
cmake --build build -j
ctest --test-dir build
```

To use it in your own project: `add_subdirectory` this directory and link
`atlas_sdk`; the include roots are `include/` and `third_party/`:

```cpp
#include "atlas/client.hpp"
```

## Quick start

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

For the complete example see [`examples/cpp`](https://github.com/cuihairu/atlas/blob/main/examples/cpp/main.cpp):

```bash
cd examples/cpp && cmake -S . -B build && cmake --build build
ATLAS_ADDR=http://localhost:8080 ATLAS_REGISTRY_ADDR=http://localhost:8081 ./build/atlas_example_cpp
```

## Key semantics

| Topic | Description |
| --- | --- |
| Error handling | All methods throw `atlas::Error` on failure (`status()` / `code()` / `message()`); code matches the REST error codes |
| Retries | Network errors + 5xx are retried automatically with full-jitter exponential backoff (`max_retries` defaults to 3); 4xx is not retried |
| Heartbeat cadence | Reporting interval : failure-detection threshold = 1:3 (defaults 10s / suspect 30s / offline 60s) |
| Directory write reply | Synchronous REST returns a flat Character object (the SDK normalizes it to `status="created"/"updated"`); the async adapter returns `{"status":"queued"}` (`character` is empty) |
| Three ports | `base_url` covers the public network/Admin; `registry_base_url` covers the standalone Registry port; when merged behind a reverse proxy, both can have the same value |
| Canary weight | `SetWeight(server_id, weight)` (Admin domain): `0` neutral, `1..1000`; out of range throws `atlas::Error(INVALID_ARGUMENT)`; returns `WeightResult{server_id, weight}`; the `Server` struct also carries `weight` (default 0), `started_at` (process start time), `source` (config = managed by config file) |
| Empty collections | An empty list from the server may be serialized as `null`; the SDK treats it as `[]` |
