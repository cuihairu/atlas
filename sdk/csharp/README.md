# Atlas.Client (C#)

Atlas 游戏服务器 Registry / Discovery / Directory / Routing / Admin 的
.NET 客户端 SDK，与 [Go](../go/) / [C++](../cpp/) / [Python](../python/) /
[JS](../js/) / [Java](../java/) SDK 同一 API 面：

- HttpClient + System.Text.Json，net8.0 / net10.0，零第三方依赖
- 五组 API 全量方法（注册心跳 / 发现 / 角色目录 / 推荐接入 / 管理）
- 瞬时失败重试（网络错误 + 5xx，全抖动指数退避；4xx 不重试）
- 自动心跳：`StartHeartbeat()` 立即首发、按间隔续报，`Set()` 更新负载
- Registry 独立端口拆分（`RegistryBaseUrl`）

## 安装

```bash
dotnet add package Atlas.Client
```

或直接引用本项目（`ProjectReference`，见 `examples/csharp/`）。

## 快速上手

```csharp
using Atlas;

var options = new AtlasClientOptions
{
    BaseUrl = "http://localhost:8080",
    RegistryBaseUrl = "http://localhost:8081", // Registry 独立端口（可选）
    RegistryToken = "...",                     // Registry 域 Bearer
    AdminApiKey = "...",                       // Admin 域 API Key
};
using var client = new AtlasClient(options);

// 1. 注册并同步首发一次心跳（在线后才可被发现/推荐），再开自动心跳
var reg = await client.RegisterAsync(new RegisterRequest(
    "game-1001", "Game 1001", "cn-east",
    new Endpoint { Host = "10.0.0.1", Port = 30001 }, 2000));
await client.HeartbeatAsync("game-1001", new HeartbeatRequest());
var loop = client.StartHeartbeat("game-1001", TimeSpan.FromSeconds(10));
loop.Set(players: 120, load: 0.35);            // 游戏线程随负载更新

// 2. 发现与目录
var rec = await client.RecommendAsync(42, "cn-east");
var page = await client.ListCharactersByServerAsync(rec.Server.Id, limit: 10);
await client.CreateCharacterAsync(new CreateCharacterRequest(42, rec.Server.Id, 1001, "Hero"));

// 3. 优雅下线
loop.Stop();
await client.UnregisterAsync("game-1001");
```

所有失败都会抛出 `AtlasError`（`Status` / `Code` / `Message`，
Status=0 表示网络错误）。完整示例见
[`examples/csharp`](../../examples/csharp/)，文档见
<https://cuihairu.github.io/atlas/sdk-csharp>。

## 开发

```bash
dotnet test Atlas.Client.Tests/Atlas.Client.Tests.csproj
dotnet pack  Atlas.Client/Atlas.Client.csproj -c Release   # 产出 nupkg
```
