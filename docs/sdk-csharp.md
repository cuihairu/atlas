# C# SDK

`sdk/csharp/`（v0.1.11 交付）。HttpClient + System.Text.Json 的 REST
客户端，与 Go / C++ / Python / JS / Java SDK 同一 API 面。目标
net8.0 + net10.0，零第三方依赖，NuGet 包名 `Atlas.Client`。

## 安装

```bash
dotnet add package Atlas.Client
```

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

var reg = await client.RegisterAsync(new RegisterRequest(   // 注册
    "game-1001", "Game 1001", "cn-east",
    new Endpoint { Host = "10.0.0.1", Port = 30001 }, 2000));

await client.HeartbeatAsync("game-1001", new HeartbeatRequest()); // 首跳同步：在线后才可被发现
var loop = client.StartHeartbeat("game-1001", TimeSpan.FromSeconds(10), // 自动心跳
    onError: e => Console.Error.WriteLine(e));
loop.Set(players: 120, load: 0.35);                         // 游戏线程更新负载

var rec = await client.RecommendAsync(42, "cn-east");       // 接入推荐
var page = await client.ListCharactersByServerAsync(rec.Server.Id, 10); // 角色目录
await client.CreateCharacterAsync(
    new CreateCharacterRequest(42, rec.Server.Id, 1001, "Hero"));

loop.Stop();                                                // 优雅下线
await client.UnregisterAsync("game-1001");
```

完整示例见 [`examples/csharp`](https://github.com/cuihairu/atlas/blob/main/examples/csharp/Program.cs)：

```bash
dotnet run --project examples/csharp
```

## 关键语义

| 主题 | 说明 |
| --- | --- |
| 错误处理 | 所有方法失败抛 `AtlasError`（`Status` / `Code` / `Message`），code 与 REST 错误码一致，Status=0 为网络错误 |
| 重试 | 网络错误 + 5xx 自动全抖动指数退避（`MaxRetries` 默认 3，上限 10s）；4xx 不重试 |
| 超时 | `HttpClient.Timeout`（`TimeoutMs` 默认 10000，0 关闭）；超时按网络错误参与重试 |
| 心跳节奏 | 上报间隔 : 判死阈值 = 1:3（默认 10s / suspect 30s / offline 60s）；示例先同步首发再开循环，规避"注册未在线即推荐"竞态 |
| 自动心跳 | `AutoHeartbeat`：`Timer` 立即首发；`Set()` 线程安全更新负载；`onError` 回调上报失败，循环不停；`Stop()` 幂等 |
| 目录写回复 | 同步 REST 返回扁平 Character 对象（SDK 按 JSON 形状解析并归一化为 `status="created"/"updated"`）；异步适配器返回 `{"status":"queued"}`（`Character` 为 null） |
| 三端口 | `BaseUrl` 覆盖公网/Admin；`RegistryBaseUrl` 覆盖 Registry 独立端口；反代合并部署时两者同值即可 |
| 空集合 | 服务端空列表可能序列化为 `null`，SDK 按 `[]` 处理（servers / characters / migrations 全部兜底） |
| 命名 | 传输层 snake_case，SDK 层 PascalCase（`JsonNamingPolicy.SnakeCaseLower` 双向映射），时间戳为 `DateTimeOffset` |
| 外部注入 | `HttpInvoker` 可传入自管 `HttpClient`（Polly/IHttpClientFactory 场景），此时 SDK 不负责其释放 |
