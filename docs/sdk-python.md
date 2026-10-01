# Python SDK

`sdk/python/`（v0.1.8 交付）。基于 httpx 的 REST 客户端，与 Go / C++ SDK
同一 API 面。同步 `AtlasClient` 与异步 `AtlasAsyncClient` 方法同名，仅
协程之分；依赖仅 `httpx`，Python ≥ 3.9。

## 安装

```bash
pip install atlas-client
```

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

## 异步客户端

与同步版本同一方法面，`await` 即可；支持 `async with`：

```python
from atlas_client import AtlasAsyncClient

async with AtlasAsyncClient("http://localhost:8080") as client:
    servers = await client.list_servers()
    loop = await client.start_heartbeat("game-1001", interval=10.0)
    ...
    await loop.stop()
```

完整示例见 [`examples/python`](https://github.com/cuihairu/atlas/blob/main/examples/python/main.py)：

```bash
ATLAS_ADDR=http://localhost:8080 ATLAS_REGISTRY_ADDR=http://localhost:8081 \
    python3 examples/python/main.py
```

## 关键语义

| 主题 | 说明 |
| --- | --- |
| 错误处理 | 所有方法失败抛 `AtlasError`（`status` / `code` / `message`），code 与 REST 错误码一致，`status=0` 为网络错误 |
| 重试 | 网络错误 + 5xx 自动全抖动指数退避（`max_retries` 默认 3）；4xx 不重试 |
| 心跳节奏 | 上报间隔 : 判死阈值 = 1:3（默认 10s / suspect 30s / offline 60s） |
| 自动心跳 | 同步版后台线程（`AutoHeartbeat`），异步版 asyncio 任务（`AsyncHeartbeatTask`）；`on_error` 回调上报失败，循环不停 |
| 目录写回复 | 同步 REST 返回扁平 Character 对象（SDK 归一化为 `status="created"/"updated"`）；异步适配器返回 `{"status":"queued"}`（`character` 为 None） |
| 三端口 | `base_url` 覆盖公网/Admin；`registry_base_url` 覆盖 Registry 独立端口；反代合并部署时两者同值即可 |
| 空集合 | 服务端空列表可能序列化为 `null`，SDK 按 `[]` 处理 |
