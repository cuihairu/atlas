# TODO

> 每一项都是一个可独立提交的原子任务。完成后打勾并注明 commit。

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
- [x] Dashboard — Overview page (stats cards + charts)
- [x] Dashboard — Servers page (filterable table + lifecycle actions)
- [x] Dashboard — ServerDetail page (info + metrics + characters)
- [x] Dashboard — Characters page (search form + results)
- [x] Dashboard — Migrations page (list + create modal + rollback)
- [x] MySQL Store — database/sql + go-sql-driver/mysql
- [x] Metadata — Server.Metadata + Character.Metadata (JSON KV)
- [x] Auth — AdminAuth middleware (API key + IP whitelist)
- [x] Auth — RegistryAuth middleware (service token + IP whitelist)
- [x] Security — three-port isolation (:8080 public / :8081 registry / :8082 admin)
- [x] Migration SQL — PostgreSQL + MySQL schema with metadata columns
- [x] Docs — architecture selection rationale (gateway / storage / language)

---

## v0.2 — Event Adapter + Observability

### Event Adapter

- [ ] Define `EventAdapter` interface in `internal/event/adapter.go`
  ```go
  type Adapter interface {
      Publish(ctx, topic, event) error
      Subscribe(ctx, topic, handler) error
      Close() error
  }
  ```
- [ ] Implement `internal/event/http/` — HTTP sync adapter (wraps current POST /v1/directory/characters)
- [ ] Implement `internal/event/redis/` — Redis Streams adapter (XADD / XREADGROUP)
- [ ] Add `ATLAS_EVENT_ADAPTER` config (http / redis)
- [ ] Wire adapter into `cmd/atlas/main.go` and Directory service
- [ ] Update docs/sync.md migration path (v0.1 HTTP → v0.2 Redis Streams)

### Prometheus Metrics

- [ ] Add `internal/metrics/` package with Prometheus registry
- [ ] Expose `GET /metrics` on admin port (:8082)
- [ ] Metrics to implement:
  - [ ] `atlas_registry_servers_total{status}` — gauge, by status
  - [ ] `atlas_registry_heartbeat_lag_seconds` — histogram
  - [ ] `atlas_directory_characters_total` — gauge
  - [ ] `atlas_discovery_requests_total{filter}` — counter
  - [ ] `atlas_admin_requests_total{endpoint,status}` — counter
  - [ ] `atlas_health_transitions_total{from,to}` — counter
- [ ] Add Grafana dashboard JSON in `deployments/grafana/`

### Health Alerts

- [ ] Add alert thresholds to config (`ATLAS_ALERT_SUSPECT_RATIO`, `ATLAS_ALERT_OFFLINE_RATIO`)
- [ ] Log structured alert when suspect/offline servers exceed threshold
- [ ] Optional: webhook notification (`ATLAS_ALERT_WEBHOOK_URL`)

---

## v0.3 — Routing + gRPC + Message Bus

### Server Routing

- [ ] Add `internal/routing/service.go` — recommendation logic
  - Filter: region, version, platform, status=online
  - Score: load (lower is better), capacity remaining (higher is better)
  - Tiebreak: has existing character (prefer player's home server)
- [ ] Add `GET /v1/routing/recommended` endpoint
- [ ] Request: `{region, platform, version, mode}` + optional `account_id`
- [ ] Response: `{server, reason}` where reason = lowest_load / highest_capacity / has_character / fallback
- [ ] Register on Public port (:8080)

### gRPC API

- [ ] Define `api/proto/atlas.proto` — all service definitions
- [ ] Generate Go code with `protoc` / `buf`
- [ ] Implement gRPC server in `internal/grpc/`
- [ ] Start gRPC server on `:9090` (configurable via `ATLAS_GRPC_ADDR`)
- [ ] Feature parity with REST: Registry, Discovery, Directory, Routing, Admin

### Message Bus Adapters

- [ ] Implement `internal/event/kafka/` — Kafka adapter (sarama or confluent-kafka-go)
- [ ] Implement `internal/event/nats/` — NATS adapter (nats.go)
- [ ] Implement `internal/event/rabbitmq/` — RabbitMQ adapter (amqp091-go)
- [ ] Document adapter selection guide in docs/sync.md

---

## v0.4 — SDK + Gateway Integration

### Go SDK

- [ ] `sdk/go/` — Go client library
  - `AtlasClient.Register(ctx, server)` / `Heartbeat(ctx, id, hb)` / `Unregister(ctx, id)`
  - `AtlasClient.ListServers(ctx, filter)` / `GetServer(ctx, id)`
  - `AtlasClient.UpsertCharacter(ctx, char)` / `ListByAccount(ctx, accountID)`
  - Auto-heartbeat goroutine with configurable interval
  - Retry with exponential backoff
  - Both REST and gRPC transport

### C++ SDK

- [ ] `sdk/cpp/` — C++ client library
  - HTTP client using libcurl or cpp-httplib
  - JSON serialization using nlohmann/json
  - Same API surface as Go SDK
  - CMake build system
  - Example: `examples/cpp/`

### APISIX Plugin

- [ ] `plugins/apisix/` — Lua plugin for APISIX
  - Route Atlas requests to Atlas backend
  - Optional: inject player token into Atlas headers
  - Rate limit config per endpoint group
- [ ] `plugins/apisix/README.md` — installation and config guide

### Realm / Shard Management API

- [ ] `POST /v1/admin/realms` — create realm
- [ ] `GET /v1/admin/realms` — list realms
- [ ] `POST /v1/admin/shards` — create shard
- [ ] `GET /v1/admin/shards` — list shards
- [ ] Wire into store interfaces and all implementations

---

## v1.0 — Production Ready

### High Availability

- [ ] Atlas multi-replica deployment guide (stateless, horizontal scaling)
- [ ] Redis Sentinel / Cluster config in docker-compose and docs
- [ ] PostgreSQL primary-replica with streaming replication guide
- [ ] Connection pool tuning documentation

### Character Index Sharding

- [ ] Add sharding strategy interface in `internal/store/`
- [ ] Implement hash-based sharding by `account_id`
- [ ] Cross-shard query support for admin search
- [ ] Migration tool for resharding

### Multi-language SDK

- [ ] `sdk/java/` — Java client (OkHttp + Gson)
- [ ] `sdk/csharp/` — C# client (HttpClient + System.Text.Json)

### Security

- [ ] mTLS for service-to-service (Registry API)
- [ ] RBAC for Admin API (role-based access control)
- [ ] Audit log — record all Admin API operations with actor + timestamp + diff
- [ ] Rate limiting middleware in Atlas itself (token bucket, per-endpoint)

### Dashboard Enhancements

- [ ] Real-time server map (geographic distribution)
- [ ] Player trend charts (daily/weekly active)
- [ ] Migration progress tracking with live updates
- [ ] Dark/light theme toggle
- [ ] i18n (English + Chinese)

---

## Ongoing

- [ ] Dependency updates — Dependabot auto-merge for patch versions
- [ ] Security audits — periodic review of auth and storage layers
- [ ] Performance benchmarks — benchmark Registry and Discovery QPS
- [ ] Documentation — keep docs/ in sync with implementation
- [ ] Test coverage — aim for >80% on service and handler layers