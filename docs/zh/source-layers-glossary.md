
[English](/source-layers-glossary)

# 源码分层术语表

本术语表记录实现中存在的抽象，而不是目录名。路径均为仓库相对源码锚点。

| 术语 | 英文 | 定义 | file:line |
| --- | --- | --- | --- |
| 控制面 | Control plane | Atlas 为游戏服务器集群回答注册、发现、目录、路由和管理问题，不执行游戏运行时逻辑。 | `docs/architecture.md:10` |
| 配置契约 | Configuration contract | 一个基于环境变量的 `Config` 聚合定义监听器、后端、超时、安全、事件传输、审计、追踪和连接池设置。 | `internal/config/config.go:11`, `internal/config/config.go:312` |
| 服务器 | Server | 持久注册档案：标识、端点、拓扑、容量、标签、路由权重和当前生效状态。 | `internal/model/model.go:176` |
| 服务器状态 | Server status | 注册、健康和管理转移共用的显式状态机；辅助方法区分可见、可接流和自动管理状态。 | `internal/model/model.go:60`, `internal/model/model.go:110` |
| 临时注册 | Temporary registration | 标记为 `auto` 的注册，为引导类服务器提供独立校验和过期语义。 | `internal/model/model.go:277`, `internal/model/model.go:284` |
| 心跳 | Heartbeat | 包含玩家数、负载、版本和元数据的观测样本，必须先校验才能更新运行时状态。 | `internal/model/model.go:244` |
| 运行时快照 | Runtime snapshot | 由心跳派生的短生命周期服务器状态，与持久服务器档案分离。 | `internal/model/model.go:265` |
| 角色索引 | Character index | 以账号、服务器和角色为键的投影；Atlas 只保存索引/搜索状态，不保存角色权威数据。 | `internal/model/model.go:364`, `docs/architecture.md:10` |
| 迁移记录 | Migration record | 描述源服务器、目标服务器、状态和时间戳的持久编排事实。 | `internal/model/model.go:404` |
| 跨服配置 | Cross-server config | 版本化、哈希化且经过校验的协调文档，包含集群、分组、匹配域和选择器。 | `internal/model/crossserver.go:16`, `internal/model/crossserver.go:109` |
| 存储契约 | Store contract | 为服务器、角色、迁移、统计、Realm、Shard、运行时、维护窗口、公告和队列状态分离出的接口。 | `internal/store/store.go:80`, `internal/store/store.go:124`, `internal/store/store.go:203`, `internal/store/store.go:309` |
| 状态条件写 | Compare-and-set status transition | 只有服务器仍持有预期状态时才写入新状态，防止过期的健康扫描覆盖更新的决策。 | `internal/store/store.go:98`, `internal/store/store.go:108` |
| 游标分页 | Cursor pagination | 存储列表返回有边界页和透明游标；单次调用共享上限为 200 行。 | `internal/store/store.go:78`, `internal/store/store.go:145` |
| 运行时存储 | Runtime store | 专用于心跳和快照的易变状态边界；舰队统计除持久总数外还必须读取该状态。 | `internal/store/store.go:203`, `internal/store/store.go:218` |
| 事件 | Event | 携带角色生命周期变更或配置更新信号的传输无关数据单元。 | `internal/event/adapter.go:17`, `internal/event/adapter.go:65` |
| 事件主题 | Event topic | 传输级路由键：角色事件使用 `atlas.characters`，配置信号使用 `atlas.config`。 | `internal/event/adapter.go:39`, `internal/event/adapter.go:108` |
| 事件适配器 | Event adapter | 发布/订阅抽象，显式表达同步语义、确认机制和生命周期关闭。 | `internal/event/adapter.go:129` |
| 死信主题 | Dead-letter topic | 以 `.dl` 结尾的同族主题；投递耗尽的事件复制到此，普通消费者不会无限重试。 | `internal/event/adapter.go:50` |
| 注册面 | Registry API | 服务器到 Atlas 的内部 HTTP 面，处理注册、心跳和注销。 | `internal/httpapi/handlers.go:117` |
| 发现面 | Discovery API | 公共 HTTP 查询面，列出或读取舰队状态。 | `internal/httpapi/handlers.go:121` |
| 目录面 | Directory API | HTTP 投影面，接收角色写入并暴露账号、服务器和搜索读取。 | `internal/httpapi/handlers.go:129` |
| 管理面 | Admin API | 受保护的 HTTP 操作面，处理生命周期、拓扑、统计、迁移、公告和跨服配置。 | `internal/httpapi/handlers.go:137` |
| gRPC 契约 | gRPC contract | Protocol Buffers 定义 Registry、Discovery、Directory、Routing 和 Admin 五个服务，与 REST 能力对齐。 | `api/proto/atlas.proto:96`, `api/proto/atlas.proto:127`, `api/proto/atlas.proto:207`, `api/proto/atlas.proto:231`, `api/proto/atlas.proto:360` |
| gRPC 服务器 | gRPC server | 传输适配器注入与 HTTP handler 相同的服务实现，并注册全部五个 protobuf 服务。 | `internal/grpc/server.go:26`, `internal/grpc/server.go:83` |
| 服务层 | Service layer | 接收已校验传输输入、把持久化委托给存储契约的状态变更业务模块；代表实现位于 `internal/*`。 | `internal/registry/registry.go:72`, `internal/discovery/discovery.go:15`, `internal/directory/directory.go:17`, `internal/health/monitor.go:16` |
| 健康监视器 | Health monitor | 后台存活状态机，检测 suspect/offline 服务器并回收过期临时注册。 | `internal/health/monitor.go:16`, `internal/health/monitor.go:262`, `internal/health/monitor.go:184` |
| 舰队索引 | Fleet index | 通过注册、更新、删除和从持久服务器行重建维护的内存舰队聚合，并叠加装饰器。 | `internal/fleet/index.go:31`, `internal/fleet/index.go:118`, `internal/fleet/index.go:233` |
| 跨服服务 | Cross-server service | 配置中心状态机，持久化版本化 spec、计算受影响目标并发出先通知后拉取的信号。 | `internal/crossserver/service.go:52`, `internal/crossserver/service.go:311`, `internal/crossserver/service.go:459` |
| 可观测性 | Observability | 围绕存储、服务、传输和进程启动/关闭注入的指标、追踪、遥测与审计支持集合。 | `internal/metrics/metrics.go:36`, `internal/tracing/tracing.go:22`, `internal/audit/entry.go:14`, `cmd/atlas/main.go:846` |
| 审计事件 | Audit event | 归一化的 actor/action/result 记录，包含脱敏、渲染和可插拔持久 sink。 | `internal/audit/entry.go:14`, `internal/audit/jsonl.go:15`, `internal/audit/sql.go:15` |
| 进程编排 | Process orchestration | 进程根：解析配置，构造特定后端的存储和适配器，装配服务与中间件，启动监听器，并在关闭时反向释放依赖。 | `cmd/atlas/main.go:113`, `cmd/atlas/main.go:269`, `cmd/atlas/main.go:799` |
| CLI 工具 | CLI tool | 独立命令行运维与负载/演示程序，调用 Atlas 外部面，而不是把服务器内部实现作为运行时依赖导入。 | `cmd/dlq/main.go:1`, `cmd/atlas-reshard/main.go:1`, `cmd/loadgen/main.go:1`, `cmd/soakcheck/main.go:1` |
| 客户端 SDK | Client SDK | 面向语言的适配器，为游戏服务器和运维方编码公共 REST/gRPC 契约。 | `sdk/go/atlas/client.go:47`, `sdk/js/src/client.ts:28` |

## 相关文档

- [架构设计](/zh/architecture#9-源码分层)
- [Architecture Design](/architecture#9-source-layers)

