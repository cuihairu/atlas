# Go SDK

`github.com/cuihairu/atlas/sdk/go/atlas`（v0.1.6 交付）。同一套 API 覆盖 REST 与 gRPC 双传输，示例见 [`examples/go`](https://github.com/cuihairu/atlas/blob/main/examples/go/main.go)。

## 安装

SDK 目前随主模块分发（同仓库单 module）。游戏服侧引入：

```go
import atlas "github.com/cuihairu/atlas/sdk/go/atlas"
```

## 创建客户端

```go
cli, err := atlas.New(atlas.Options{
    Addr:      "localhost:8080",             // REST 基地址；gRPC 用 ATLAS_GRPC_ADDR
    Transport: atlas.TransportREST,          // 默认 rest；可选 grpc
    RegistryToken: os.Getenv("ATLAS_REGISTRY_TOKEN"), // Registry 域 Bearer
    AdminAPIKey:   adminKey,                          // Admin 域 Bearer——值须为服务端 ATLAS_ADMIN_API_KEYS 之一（env 名部署自定）
    MaxRetries:    3,                        // 瞬时失败重试（网络错误 + 5xx）
    BaseBackoff:   100 * time.Millisecond,   // 指数退避基数（全抖动）
})
defer cli.Close()
```

## 注册与自动心跳

心跳循环**启动即发一次**（开服立刻可见），之后按固定间隔上报；游戏服只需在负载变化时调用 `Set`：

```go
reg, err := cli.Register(ctx, atlas.RegisterRequest{
    ServerID: "game-1001", Name: "Game 1001", Type: "game",
    Region: "cn-east", Version: "1.0.0", Platform: "any",
    Endpoint: atlas.Endpoint{Host: "10.0.0.1", Port: 30001},
    Capacity: 2000,
})

loop := cli.StartHeartbeat("game-1001", 10*time.Second, atlas.HeartbeatRequest{})
defer loop.Stop()
loop.OnError(func(err error) { log.Printf("heartbeat: %v", err) })

// 游戏循环里随负载更新（并发安全）
loop.Set(playerCount, loadFactor)
```

**节奏建议**：上报间隔与 Atlas 判死阈值保持 3:1——默认 `suspect_after=30s` / `offline_after=60s` 时，10s 一跳即可；只要循环在跑就不会被误判离线。

## 双传输

| | REST | gRPC |
| --- | --- | --- |
| 地址 | `ATLAS_HTTP_ADDR` / `ATLAS_REGISTRY_ADDR` / `ATLAS_ADMIN_ADDR`（:8080/:8081/:8082，通常经反代合一） | `ATLAS_GRPC_ADDR`（:9090） |
| 认证 | Bearer 头 | `authorization` metadata |
| 错误 | `*atlas.Error{Code: "SERVER_NOT_FOUND", ...}` | `*atlas.Error{Code: "NotFound", ...}`（gRPC 状态名） |
| 适用 | 运维工具、低频调用 | 游戏服高频心跳、SDK 内部 |

::: warning 错误码差异
REST 错误码区分 `SERVER_NOT_FOUND` / `CHARACTER_NOT_FOUND`；gRPC 统一为 `NotFound`。需要区分资源类型时检查 `Error.Message` 或按调用接口判断。
:::

## API 一览

五组方法与 REST 端点一一对应（详见 [API 参考](/api)）：

```go
// Registry
cli.Register(ctx, req) / cli.Heartbeat(ctx, id, req) / cli.Unregister(ctx, id)

// Discovery
cli.ListServers(ctx, atlas.ServerFilter{Region: "cn-east"})
cli.GetServer(ctx, "game-1001")

// Directory（异步事件适配器下写操作返回 Status: "queued"）
cli.CreateCharacter(ctx, req) / cli.GetCharacter(ctx, id)
cli.ListCharactersByAccount(ctx, accountID)
cli.ListCharactersByServer(ctx, serverID, limit, cursor)
cli.UpdateCharacter(ctx, id, req) / cli.DeleteCharacter(ctx, id)

// Routing
cli.Recommend(ctx, accountID, region, version, platform)

// Admin
cli.SetMaintenance(ctx, id) / cli.SetDrain / cli.Enable / cli.Disable
cli.Stats(ctx) / cli.SearchCharacters(ctx, filter)
cli.CreateMigration(ctx, req) / cli.GetMigration / cli.ListMigrations / cli.RollbackMigration
```

## 可运行示例

```bash
go run ./examples/go
```

完成注册 → 心跳 → 推荐 → 拉取角色 → 收到信号后注销的完整生命周期。
