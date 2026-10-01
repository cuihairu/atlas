# TODO

> 每一项都是一个可独立提交的原子任务。完成后打勾并注明 commit。
>
> 版本策略：0.1.x 逐个推进，产品验证后再考虑 0.2.0。

---

## v0.1.0 — MVP ✅

- [x] Server Registry — register / heartbeat / unregister
- [x] Server Discovery — list / get
- [x] Character Directory — create / update / delete / account → characters
- [x] Health Monitor — automatic offline (online → suspect → offline)
- [x] REST API — 11 endpoints, Go 1.22+ pattern routing
- [x] Memory Store — in-memory implementation for tests/dev
- [x] PostgreSQL Store — pgx + pgxpool
- [x] Redis Store — runtime state (heartbeat/load/players)
- [x] Docker — multi-stage Dockerfile + docker-compose (atlas + PG + Redis)
- [x] CI — GitHub Actions for VitePress docs deployment
- [x] VitePress docs site — config + custom CSS + landing page + api-quickstart
- [x] README — logo + quick start + API reference + project structure

## v0.1.1 — Admin + Dashboard + Security ✅

- [x] Admin API — server lifecycle (maintenance / drain / enable / disable)
- [x] Admin API — global stats (servers by status/region/version, player counts)
- [x] Admin API — character search (by name/server/class/level range)
- [x] Admin API — migration CRUD (create / get / list / rollback)
- [x] Dashboard — React 19 + Vite 6 + Ant Design 5, dark theme
- [x] Dashboard — Overview / Servers / ServerDetail / Characters / Migrations
- [x] MySQL Store — database/sql + go-sql-driver/mysql
- [x] Metadata — Server.Metadata + Character.Metadata (JSON KV)
- [x] Auth — AdminAuth (API key + IP whitelist) + RegistryAuth (service token)
- [x] Security — three-port isolation (:8080 public / :8081 registry / :8082 admin)
- [x] Migration SQL — PostgreSQL + MySQL schema with metadata columns
- [x] Docs — architecture selection rationale

---

## v0.1.2 — Event Adapter

- [ ] Define `EventAdapter` interface in `internal/event/adapter.go`
- [ ] Implement `internal/event/http/` — HTTP sync adapter (current behavior)
- [ ] Implement `internal/event/redis/` — Redis Streams adapter (XADD / XREADGROUP)
- [ ] Add `ATLAS_EVENT_ADAPTER` config (http / redis)
- [ ] Wire adapter into Directory service, replace direct HTTP call
- [ ] Tests for both adapters
- [ ] Update docs/sync.md migration path

## v0.1.3 — Prometheus Metrics

- [ ] Add `internal/metrics/` package with Prometheus registry
- [ ] Expose `GET /metrics` on admin port (:8082)
- [ ] `atlas_registry_servers_total{status}` — gauge
- [ ] `atlas_registry_heartbeat_lag_seconds` — histogram
- [ ] `atlas_directory_characters_total` — gauge
- [ ] `atlas_discovery_requests_total{filter}` — counter
- [ ] `atlas_admin_requests_total{endpoint,status}` — counter
- [ ] `atlas_health_transitions_total{from,to}` — counter
- [ ] Grafana dashboard JSON in `deployments/grafana/`

## v0.1.4 — Server Routing

- [ ] Add `internal/routing/service.go` — recommendation logic
- [ ] Filter: region, version, platform, status=online
- [ ] Score: load (lower better), capacity remaining (higher better)
- [ ] Tiebreak: has existing character (account_id optional)
- [ ] `GET /v1/routing/recommended` on public port (:8080)
- [ ] Response: `{server, reason}` (lowest_load / highest_capacity / has_character / fallback)
- [ ] Tests

## v0.1.5 — gRPC API

- [ ] Define `api/proto/atlas.proto` — all service definitions
- [ ] Generate Go code (protoc / buf)
- [ ] Implement gRPC server in `internal/grpc/`
- [ ] Start on `:9090` (configurable `ATLAS_GRPC_ADDR`)
- [ ] Feature parity: Registry, Discovery, Directory, Routing, Admin
- [ ] Tests

## v0.1.6 — Go SDK

- [ ] `sdk/go/` — Go client library
- [ ] `Register` / `Heartbeat` / `Unregister`
- [ ] `ListServers` / `GetServer`
- [ ] `UpsertCharacter` / `ListByAccount` / `SearchCharacters`
- [ ] Auto-heartbeat goroutine with configurable interval
- [ ] Retry with exponential backoff
- [ ] Both REST and gRPC transport
- [ ] Example: `examples/go/`

## v0.1.7 — C++ SDK

- [ ] `sdk/cpp/` — C++ client library
- [ ] HTTP client (libcurl or cpp-httplib)
- [ ] JSON serialization (nlohmann/json)
- [ ] Same API surface as Go SDK
- [ ] CMake build system
- [ ] Example: `examples/cpp/`

## v0.1.8 — Python SDK

- [ ] `sdk/python/` — Python client library
- [ ] `atlas_client.py` — sync client using httpx
- [ ] `AtlasAsyncClient` — async client using httpx + asyncio
- [ ] `Register` / `Heartbeat` / `Unregister`
- [ ] `ListServers` / `GetServer`
- [ ] `UpsertCharacter` / `ListByAccount` / `SearchCharacters`
- [ ] Auto-heartbeat background task
- [ ] PyPI package config (`pyproject.toml`)
- [ ] Example: `examples/python/`

## v0.1.9 — JavaScript/TypeScript SDK

- [ ] `sdk/js/` — TypeScript client library
- [ ] `AtlasClient` — sync/fetch based client
- [ ] Same API surface as Go SDK
- [ ] npm package config (`package.json`)
- [ ] Works in Node.js and browser
- [ ] Example: `examples/js/`

## v0.1.10 — Java SDK

- [ ] `sdk/java/` — Java client library
- [ ] `AtlasClient` — OkHttp + Gson
- [ ] Same API surface as Go SDK
- [ ] Maven + Gradle config
- [ ] Example: `examples/java/`

## v0.1.11 — C# SDK

- [ ] `sdk/csharp/` — C# client library
- [ ] `AtlasClient` — HttpClient + System.Text.Json
- [ ] Same API surface as Go SDK
- [ ] NuGet package config
- [ ] Example: `examples/csharp/`

## v0.1.12 — Message Bus Adapters

- [ ] Implement `internal/event/kafka/` (sarama or confluent-kafka-go)
- [ ] Implement `internal/event/nats/` (nats.go)
- [ ] Implement `internal/event/rabbitmq/` (amqp091-go)
- [ ] Config: `ATLAS_EVENT_ADAPTER=kafka|nats|rabbitmq`
- [ ] Adapter selection guide in docs/sync.md

## v0.1.13 — APISIX Plugin

- [ ] `plugins/apisix/` — Lua plugin
- [ ] Route Atlas requests to Atlas backend
- [ ] Inject player token into Atlas headers
- [ ] Rate limit config per endpoint group
- [ ] Installation and config guide

## v0.1.14 — Realm / Shard Management

- [ ] `POST /v1/admin/realms` / `GET /v1/admin/realms`
- [ ] `POST /v1/admin/shards` / `GET /v1/admin/shards`
- [ ] Wire into store interfaces and all implementations
- [ ] Tests

## v0.1.15 — Health Alerts

- [ ] Alert thresholds in config (`ATLAS_ALERT_SUSPECT_RATIO`, `ATLAS_ALERT_OFFLINE_RATIO`)
- [ ] Structured log alert when suspect/offline exceeds threshold
- [ ] Optional: webhook notification (`ATLAS_ALERT_WEBHOOK_URL`)

## v0.1.16 — Character Index Sharding

- [ ] Sharding strategy interface in `internal/store/`
- [ ] Hash-based sharding by `account_id`
- [ ] Cross-shard query for admin search
- [ ] Migration tool for resharding

## v0.1.17 — Security Hardening

- [ ] mTLS for service-to-service (Registry API)
- [ ] RBAC for Admin API (role-based access control)
- [ ] Audit log — all Admin API operations with actor + timestamp + diff
- [ ] Rate limiting middleware in Atlas itself (token bucket, per-endpoint)

## v0.1.18 — Dashboard Enhancements

- [ ] Real-time server map (geographic distribution)
- [ ] Player trend charts (daily/weekly active)
- [ ] Migration progress with live updates
- [ ] Dark/light theme toggle
- [ ] i18n (English + Chinese)

## v0.1.19 — High Availability

- [ ] Atlas multi-replica deployment guide (stateless, horizontal scaling)
- [ ] Redis Sentinel / Cluster config
- [ ] PostgreSQL primary-replica with streaming replication
- [ ] Connection pool tuning documentation

---

## Ongoing

- [ ] Dependency updates — Dependabot auto-merge for patch versions
- [ ] Security audits — periodic review of auth and storage layers
- [ ] Performance benchmarks — Registry and Discovery QPS
- [ ] Documentation — keep docs/ in sync with implementation
- [ ] Test coverage — aim for >80% on service and handler layers