
# Python SDK

`sdk/python/` (delivered in v0.1.8). An httpx-based REST client with the same
API surface as the Go / C++ SDKs. The synchronous `AtlasClient` and the
asynchronous `AtlasAsyncClient` share method names, differing only in
coroutines; the only dependency is `httpx`, Python ≥ 3.9.

## Installation

```bash
pip install atlas-client
```

## Quick start

```python
from atlas_client import (
    AtlasClient, CreateCharacterRequest, Endpoint, RegisterRequest,
)

client = AtlasClient(
    "http://localhost:8080",
    registry_base_url="http://localhost:8081",  # Registry 独立端口（可选）
    registry_token="...",                       # Registry 域 Bearer
    admin_api_key="...",                        # Admin 域 API Key
    default_headers={"X-Request-ID": "..."},    # 每次调用都带的静态头（可选，关联 id 见 api.md「请求追踪」）
)

reg = client.register(RegisterRequest(            # 注册
    server_id="game-1001", name="Game 1001",
    region="cn-east", capacity=2000,
    endpoint=Endpoint(host="10.0.0.1", port=30001),
))
loop = client.start_heartbeat("game-1001", interval=10.0)  # 自动心跳（立即首发）
loop.set(players=120, load=0.35)                  # 游戏线程更新负载

rec = client.recommend(account_id=42, region="cn-east")    # 接入推荐
page = client.list_characters_by_server(rec.server.id, limit=10)  # 角色目录
wr = client.create_character(CreateCharacterRequest(
    account_id=42, server_id=rec.server.id,
    character_id=1001, name="Hero", level=1,
))

loop.stop()                                       # 优雅下线
client.unregister("game-1001")
client.close()
```

## Async client

Same method surface as the synchronous version, just `await`; `async with` is supported:

```python
from atlas_client import AtlasAsyncClient

async with AtlasAsyncClient("http://localhost:8080") as client:
    servers = await client.list_servers()
    loop = await client.start_heartbeat("game-1001", interval=10.0)
    ...
    await loop.stop()
```

For the complete example see [`examples/python`](https://github.com/cuihairu/atlas/blob/main/examples/python/main.py):

```bash
ATLAS_ADDR=http://localhost:8080 ATLAS_REGISTRY_ADDR=http://localhost:8081 \
    python3 examples/python/main.py
```

## Key semantics

| Topic | Description |
| --- | --- |
| Error handling | All methods throw `AtlasError` on failure (`status` / `code` / `message`); code matches the REST error codes, and `status=0` means a network error |
| Retries | Network errors + 5xx are retried automatically with full-jitter exponential backoff (`max_retries` defaults to 3); 4xx is not retried |
| Heartbeat cadence | Reporting interval : failure-detection threshold = 1:3 (defaults 10s / suspect 30s / offline 60s) |
| Automatic heartbeat | A background thread in the synchronous version (`AutoHeartbeat`), an asyncio task in the asynchronous version (`AsyncHeartbeatTask`); the `on_error` callback reports failures, and the loop does not stop |
| Directory write reply | Synchronous REST returns a flat Character object (the SDK normalizes it to `status="created"/"updated"`); the async adapter returns `{"status":"queued"}` (`character` is None) |
| Three ports | `base_url` covers the public network/Admin; `registry_base_url` covers the standalone Registry port; when merged behind a reverse proxy, both can have the same value |
| Canary weight | `set_weight(server_id, weight)` (Admin domain, same name in sync/async): `0` neutral, `1..1000`; out of range throws `AtlasError(INVALID_ARGUMENT)`; returns `WeightResult(server_id, weight)`; the `Server` model also carries `weight` (0 when unset), `started_at` (process start time), `source` (config = managed by config file) |
| Empty collections | An empty list from the server may be serialized as `null`; the SDK treats it as `[]` |
