[English](sdk-go.en.md) | [中文](sdk-go.md)

# Go SDK

`github.com/cuihairu/atlas/sdk/go/atlas` (delivered in v0.1.6). The same API surface covers both REST and gRPC transports; for an example see [`examples/go`](https://github.com/cuihairu/atlas/blob/main/examples/go/main.go).

## Installation

The SDK is currently distributed with the main module (single module in the same repository). On the game server side, import:

```go
import atlas "github.com/cuihairu/atlas/sdk/go/atlas"
```

## Creating a client

```go
cli, err := atlas.New(atlas.Options{
    Addr:      "localhost:8080",             // REST 基地址；gRPC 用 ATLAS_GRPC_ADDR
    RegistryAddr: "localhost:8081",           // Registry 域单独指向（ATLAS_REGISTRY_ADDR；空 = Addr，反代合一时用）
    Transport: atlas.TransportREST,          // 默认 rest；可选 grpc
    RegistryToken: os.Getenv("ATLAS_REGISTRY_TOKENS"), // Registry 域 Bearer（服务端 env 为 ATLAS_REGISTRY_TOKENS，值取其一）
    AdminAPIKey:   adminKey,                          // Admin 域 Bearer——值须为服务端 ATLAS_ADMIN_API_KEYS 之一（env 名部署自定）
    DefaultHeaders: map[string]string{                // 每次调用都带的静态头（REST）/ metadata（gRPC）
        "X-Request-ID": instanceID,                   // 例：进程实例级关联 id，见 api.md「请求追踪」
    },
    HTTPClient:    myHTTPClient,             // 自定义 REST 传输（超时、连接池）；空 = 10s 超时默认客户端
    GRPCTLS:       false,                    // gRPC 传输安全（服务端 ATLAS_GRPC_TLS_CERT/_KEY）；REST 不受影响
    GRPCTLSCACert: "",                       // GRPCTLS 开启时的 PEM CA 束（私有 CA）；空 = 系统根
    MaxRetries:    3,                        // 瞬时失败重试（网络错误 + 5xx）
    BaseBackoff:   100 * time.Millisecond,   // 指数退避基数（全抖动）
})
defer cli.Close()
```

## Registration and automatic heartbeat

The heartbeat loop **sends once as soon as it starts** (visible immediately when the server opens), then reports at a fixed interval; the game server only needs to call `Set` when the load changes:

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

**Cadence recommendation**: keep the failure-detection threshold and the reporting interval at 3:1 — with the defaults `suspect_after=30s` / `offline_after=60s`, one beat every 10s is enough; as long as the loop is running it will not be falsely marked offline. The loop also exposes `LastError()` (the reason for the most recent failure) and `LastSent()` (the time the last heartbeat was sent), which can be read directly in game server monitoring.

## Dual transport

| | REST | gRPC |
| --- | --- | --- |
| Address | `ATLAS_HTTP_ADDR` / `ATLAS_REGISTRY_ADDR` / `ATLAS_ADMIN_ADDR` (:8080/:8081/:8082, usually unified behind a reverse proxy) | `ATLAS_GRPC_ADDR` (:9090) |
| Authentication | Bearer header | `authorization` metadata |
| Correlation header | `DefaultHeaders` → HTTP headers | `DefaultHeaders` → metadata; gRPC can additionally attach metadata on the ctx for a single call (per-call id) |
| Transport security | `https://` base address | `GRPCTLS: true` + `GRPCTLSCACert` (private CA bundle; server side `ATLAS_GRPC_TLS_CERT`/`_KEY`, optionally `_CLIENT_CA`) |
| Errors | `*atlas.Error{Code: "SERVER_NOT_FOUND", ...}` | `*atlas.Error{Code: "NotFound", ...}` (gRPC status name) |
| Suitable for | admin tools, low-frequency calls | high-frequency game server heartbeats, SDK internals |

::: warning Error code differences
REST error codes distinguish `SERVER_NOT_FOUND` / `CHARACTER_NOT_FOUND`; gRPC unifies them as `NotFound`. To distinguish the resource type, check `Error.Message` or decide by the calling interface.
:::

## API overview

The six method groups map one-to-one to REST endpoints (see the [API reference](/api.en) for details):

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

// 跨服配置中心（REST-only 面的拉取与 watcher，见 config-center.md）
snap, err := cli.FetchCrossServerConfig(ctx)     // 拉取当前快照（ETag 协商）
watcher := atlas.NewConfigWatcher(cli, opts)     // 信号驱动 + 轮询兜底 watcher
watcher.Run(ctx, 3)                             // 启动；返回即视为放弃
watcher.Notify()                                // 请求立即重拉（配置变更信号）
applied, err := watcher.Pull(ctx)               // 手动拉取一次
current := watcher.Current()                     // 当前已应用快照
applied, idempotent, skipped, failures := watcher.Stats()
```

## Runnable example

```bash
go run ./examples/go
```

The complete lifecycle of register → heartbeat → recommend → fetch characters → unregister after receiving a signal.
