# atlas-client (Python)

Atlas 游戏服务器 Registry / Discovery / Directory / Routing / Admin 的 Python
客户端 SDK，与 [Go SDK](../go/) / [C++ SDK](../cpp/) 同一 API 面：

- 同步 `AtlasClient` 与异步 `AtlasAsyncClient`（同一方法面，httpx 传输）
- 五组 API 全量方法（注册心跳 / 发现 / 角色目录 / 推荐接入 / 管理）
- 瞬时失败重试（5xx 与网络错误，全抖动指数退避；4xx 不重试）
- 自动心跳：`start_heartbeat()` 立即首发、按间隔续报，`set()` 并发更新负载
- Registry 独立端口拆分（`registry_base_url`）

## 安装

```bash
pip install atlas-client
```

依赖仅 `httpx`，Python ≥ 3.9。

## 快速上手

```python
from atlas_client import (
    AtlasClient, CreateCharacterRequest, Endpoint, RegisterRequest,
)

client = AtlasClient(
    "http://localhost:8080",
    registry_base_url="http://localhost:8081",  # Registry 独立端口（可选）
    registry_token="...",                       # Registry 域 Bearer
    admin_api_key="...",                        # Admin 域 API Key
    default_headers={"X-Request-ID": "..."},    # 每次调用都带的静态头（可选，关联 id 见 docs/api.md「请求追踪」）
)

# 1. 注册并开启自动心跳（立即首发，之后每 10s 一次）
reg = client.register(RegisterRequest(
    server_id="game-1001",
    name="Game 1001",
    region="cn-east",
    endpoint=Endpoint(host="10.0.0.1", port=30001),
    capacity=2000,
))
loop = client.start_heartbeat("game-1001", interval=10.0)
loop.set(players=120, load=0.35)   # 游戏线程随负载更新

# 2. 发现与目录
rec = client.recommend(account_id=42, region="cn-east")
page = client.list_characters_by_server(rec.server.id, limit=10)
result = client.create_character(
    CreateCharacterRequest(account_id=42, server_id=rec.server.id,
                           character_id=1001, name="Hero", level=1)
)

# 3. 优雅下线
loop.stop()
client.unregister("game-1001")
client.close()
```

异步用法相同，方法皆为协程：

```python
from atlas_client import AtlasAsyncClient

async with AtlasAsyncClient("http://localhost:8080") as client:
    servers = await client.list_servers()
    loop = await client.start_heartbeat("game-1001", interval=10.0)
    ...
    await loop.stop()
```

所有失败都会抛出 `AtlasError`（`code` / `status` / `message`，`status=0`
表示网络错误）。完整示例见 [`examples/python`](../../examples/python/)，
文档见 <https://cuihairu.github.io/atlas/sdk-python>。

## 开发

```bash
pip install -e .[dev]
pytest sdk/python/tests -v
```
