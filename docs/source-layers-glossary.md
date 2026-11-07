
[中文](/zh/source-layers-glossary)

# Source Layer Glossary

This glossary records the abstractions found in the implementation, not the
directory layout. Paths are repository-relative source anchors.

| Term | English | Definition | Evidence |
| --- | --- | --- | --- |
| 控制面 | Control plane | Atlas answers registration, discovery, directory, routing, and administrative questions for game fleets; it does not execute game runtime logic. | `docs/architecture.md:10` |
| 配置契约 | Configuration contract | One environment-variable-backed `Config` aggregate defines listeners, backends, timeouts, security, event transport, audit, tracing, and pool settings. | `internal/config/config.go:11`, `internal/config/config.go:312` |
| 服务器 | Server | The durable registration profile: identity, endpoints, topology, capacity, tags, routing weight, and current effective status. | `internal/model/model.go:176` |
| 服务器状态 | Server status | The explicit state machine used by registration, health, and admin transitions; helpers distinguish visible, traffic-accepting, and auto-managed states. | `internal/model/model.go:60`, `internal/model/model.go:110` |
| 临时注册 | Temporary registration | A registration marked `auto`, with its own validation and expiry semantics for bootstrap-style servers. | `internal/model/model.go:277`, `internal/model/model.go:284` |
| 心跳 | Heartbeat | The observed runtime sample containing players, load, version, and metadata, validated before it may update runtime state. | `internal/model/model.go:244` |
| 运行时快照 | Runtime snapshot | The short-lived per-server state derived from heartbeats and kept separate from the durable server profile. | `internal/model/model.go:265` |
| 角色索引 | Character index | A projection keyed by account, server, and character; Atlas stores index/search state, not authoritative character data. | `internal/model/model.go:364`, `docs/architecture.md:10` |
| 迁移记录 | Migration record | A persistent orchestration fact describing source servers, target server, status, and timestamps. | `internal/model/model.go:404` |
| 跨服配置 | Cross-server config | A versioned, hashed, validated coordination document containing clusters, groups, match domains, and selectors. | `internal/model/crossserver.go:16`, `internal/model/crossserver.go:109` |
| 存储契约 | Store contract | Separate interfaces for server, character, migration, stats, realm, shard, runtime, maintenance, announcement, and queue state. | `internal/store/store.go:80`, `internal/store/store.go:124`, `internal/store/store.go:203`, `internal/store/store.go:309` |
| 状态条件写 | Compare-and-set status transition | A status write applies only when the server still holds the expected status, preventing stale health sweeps from overwriting newer decisions. | `internal/store/store.go:98`, `internal/store/store.go:108` |
| 游标分页 | Cursor pagination | Store lists return bounded pages plus an opaque cursor; the shared cap is 200 rows per call. | `internal/store/store.go:78`, `internal/store/store.go:145` |
| 运行时存储 | Runtime store | The dedicated volatility boundary for heartbeats and snapshots; fleet statistics require reading this state in addition to persistent totals. | `internal/store/store.go:203`, `internal/store/store.go:218` |
| 事件 | Event | The transport-neutral unit carrying character lifecycle changes or a config-update signal. | `internal/event/adapter.go:17`, `internal/event/adapter.go:65` |
| 事件主题 | Event topic | A transport-level routing key: character events use `atlas.characters`; config signals use `atlas.config`. | `internal/event/adapter.go:39`, `internal/event/adapter.go:108` |
| 事件适配器 | Event adapter | Publish/subscribe abstraction with explicit synchronous semantics, acknowledgement, and lifecycle close. | `internal/event/adapter.go:129` |
| 死信主题 | Dead-letter topic | A sibling topic suffixed `.dl` where an exhausted event is copied so normal consumers cannot retry it forever. | `internal/event/adapter.go:50` |
| 注册面 | Registry API | The internal server-to-Atlas HTTP surface for register, heartbeat, and unregister. | `internal/httpapi/handlers.go:117` |
| 发现面 | Discovery API | The public HTTP query surface listing or retrieving fleet state. | `internal/httpapi/handlers.go:121` |
| 目录面 | Directory API | The HTTP projection surface accepting character writes and exposing account/server/search reads. | `internal/httpapi/handlers.go:129` |
| 管理面 | Admin API | The protected HTTP operations surface for lifecycle, topology, statistics, migrations, announcements, and cross-server configuration. | `internal/httpapi/handlers.go:137` |
| gRPC 契约 | gRPC contract | Protocol Buffers define Registry, Discovery, Directory, Routing, and Admin services with REST feature parity. | `api/proto/atlas.proto:96`, `api/proto/atlas.proto:127`, `api/proto/atlas.proto:207`, `api/proto/atlas.proto:231`, `api/proto/atlas.proto:360` |
| gRPC 服务器 | gRPC server | The transport adapter that injects the same service implementations used by HTTP handlers and registers all five protobuf services. | `internal/grpc/server.go:26`, `internal/grpc/server.go:83` |
| 服务层 | Service layer | State-changing business modules that receive validated transport input and delegate persistence to store contracts; representative implementations live under `internal/*`. | `internal/registry/registry.go:72`, `internal/discovery/discovery.go:15`, `internal/directory/directory.go:17`, `internal/health/monitor.go:16` |
| 健康监视器 | Health monitor | The background liveness state machine that detects suspect and offline servers and reclaims expired temporary registrations. | `internal/health/monitor.go:16`, `internal/health/monitor.go:262`, `internal/health/monitor.go:184` |
| 舰队索引 | Fleet index | The in-memory decorated fleet aggregate maintained by registering, updating, removing, and rebuilding from persistent server rows. | `internal/fleet/index.go:31`, `internal/fleet/index.go:118`, `internal/fleet/index.go:233` |
| 跨服服务 | Cross-server service | The config-center state machine that persists versioned specs, computes affected targets, and emits notify-then-pull signals. | `internal/crossserver/service.go:52`, `internal/crossserver/service.go:311`, `internal/crossserver/service.go:459` |
| 可观测性 | Observability | The metrics, tracing, telemetry, and audit support set injected around stores, services, transports, and lifecycle startup/shutdown. | `internal/metrics/metrics.go:36`, `internal/tracing/tracing.go:22`, `internal/audit/entry.go:14`, `cmd/atlas/main.go:846` |
| 审计事件 | Audit event | A normalized actor/action/result record with masking, rendering, and pluggable persistence sinks. | `internal/audit/entry.go:14`, `internal/audit/jsonl.go:15`, `internal/audit/sql.go:15` |
| 进程编排 | Process orchestration | The process root that parses config, constructs backend-specific stores and adapters, wires services and middleware, starts listeners, and reverses dependencies during shutdown. | `cmd/atlas/main.go:113`, `cmd/atlas/main.go:269`, `cmd/atlas/main.go:799` |
| CLI 工具 | CLI tool | Independent command-line operators and load/demo programs that call Atlas surfaces rather than importing server internals as runtime dependencies. | `cmd/dlq/main.go:1`, `cmd/atlas-reshard/main.go:1`, `cmd/loadgen/main.go:1`, `cmd/soakcheck/main.go:1` |
| 客户端 SDK | Client SDK | Language-facing adapters that encode the public REST/gRPC contract for game servers and operators. | `sdk/go/atlas/client.go:47`, `sdk/js/src/client.ts:28` |

## Related Documents

- [Architecture Design](/architecture#9-source-layers)
- [架构设计](/zh/architecture#9-源码分层)

