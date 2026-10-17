
# Quick Start

Walk Atlas's complete chain end to end: **bring a server online → player picks a server → character directory → admin operations → graceful shutdown**.
Every step is a paste-ready command with real responses; the whole run takes about 5 minutes.

## 0. Run it with one command

**Option A: source + in-memory storage (zero dependencies, best for a first experience)**

```bash
go run ./cmd/atlas
# …level=INFO msg="public API listening" addr=:8080
# …level=INFO msg="registry API listening" addr=:8081 scheme=http
# …level=INFO msg="admin API listening" addr=:8082
# …level=INFO msg="gRPC API listening" addr=:9090
```

Those four listening lines are the result — Atlas is up and running with in-memory storage, no PostgreSQL / Redis needed (the gRPC port starts by default; set `ATLAS_GRPC_ADDR=""` to turn it off).

**Option B: Docker Compose (PostgreSQL + Redis persistence)**

```bash
docker compose -f deployments/docker/docker-compose.yml up -d --build
```

It does everything automatically: creates the containers, runs all `migrations/` migrations, and starts Atlas. Data lands in volumes and survives restarts.

> Use Option A for development integration; use Option B for staging / load testing. Option A is wiped on restart — that is only an experience limitation.

## Three ports, three roles

Atlas is one service with three ports — tell them apart before you type commands: **registration goes to :8081,
player queries go to :8080, admin operations go to :8082**:

| Port | Service domain | Who calls it | Endpoints used on this page |
| --- | --- | --- | --- |
| `:8080` public | Discovery / Directory / Routing | Player clients, gateways | List / recommendation / characters / announcements |
| `:8081` internal | Registry | Game server processes | Register / heartbeat / unregister |
| `:8082` admin | Admin | Ops platforms, background jobs | Realms / announcements / maintenance / stats |

With a reverse-proxy merged deployment (APISIX in front), the three ports can collapse into one domain — see [Deployment Topology](/topology).

---

## 1. Hook the game server up (:8081)

A game server process registers its identity and endpoint at startup, then keeps itself online with periodic heartbeats:

```bash
curl -X POST http://localhost:8081/v1/registry/servers/register \
  -H 'Content-Type: application/json' -d '{
    "server_id": "game-1001",
    "name": "一区·青龙",
    "type": "game",
    "region": "cn-east",
    "realm_id": "realm-01",
    "shard_id": "shard-1001",
    "version": "1.8.2",
    "platform": "android",
    "endpoint": { "host": "10.0.1.21", "port": 30001 },
    "capacity": 2000
  }'
```

```json
{"server_id":"game-1001","status":"starting"}
```

`starting` = recorded, not yet confirmed by a heartbeat. **The first valid heartbeat automatically promotes it to `online`**:

```bash
curl -X POST http://localhost:8081/v1/registry/servers/game-1001/heartbeat \
  -H 'Content-Type: application/json' -d '{"players": 843, "load": 0.42}'
```

```json
{"next_heartbeat_in":10,"server_id":"game-1001","status":"online"}
```

Start one more server, `game-1002` (same region, emptier); the recommendation step later uses it for comparison:

```bash
curl -s -X POST http://localhost:8081/v1/registry/servers/register \
  -H 'Content-Type: application/json' \
  -d '{"server_id":"game-1002","name":"一区·白虎","region":"cn-east","endpoint":{"host":"10.0.1.22","port":30002},"capacity":2000}' > /dev/null
curl -s -X POST http://localhost:8081/v1/registry/servers/game-1002/heartbeat \
  -H 'Content-Type: application/json' -d '{"players":120,"load":0.11}' > /dev/null
```

> A static fleet can skip registration code entirely — declare servers in a config file and bring them up directly — see [Server Info as Configuration](/server-config).

## 2. Player side: server picking and characters (:8080)

**List and filter** — the client pulls playable servers, with `region / realm / shard / version / platform / status`
filters and cursor pagination:

```bash
curl 'http://localhost:8080/v1/discovery/servers?status=online'
```

```json
{
  "servers": [
    {
      "id": "game-1001", "name": "一区·青龙", "region": "cn-east",
      "endpoint": { "host": "10.0.1.21", "port": 30001 },
      "status": "online", "players": 843, "load": 0.42, "capacity": 2000
    },
    {
      "id": "game-1002", "name": "一区·白虎", "region": "cn-east",
      "endpoint": { "host": "10.0.1.22", "port": 30002 },
      "status": "online", "players": 120, "load": 0.11, "capacity": 2000
    }
  ]
}
```

**Server recommendation** — instead of doing your own load balancing, ask Atlas for the answer to "which server should I join". With no historical
characters it recommends by load (`lowest_load`):

```bash
curl 'http://localhost:8080/v1/routing/recommended?account_id=10001&region=cn-east'
```

```json
{"reason":"lowest_load","server":{"id":"game-1002","name":"一区·白虎","region":"cn-east","status":"online","players":120,"load":0.11,"capacity":2000,"endpoint":{"host":"10.0.1.22","port":30002},"type":"","version":"","platform":""}}
```

**Write the character index** — the player created a character on `game-1001`, and the game server reports the projection to Atlas (this is an index,
not authoritative data — the character store always belongs to the game server):

```bash
curl -X POST http://localhost:8080/v1/directory/characters \
  -H 'Content-Type: application/json' -d '{
    "account_id": 10001,
    "server_id": "game-1001",
    "character_id": 823712,
    "name": "剑无尘",
    "level": 182,
    "class_id": 3
  }'
```

```json
{"account_id":10001,"server_id":"game-1001","character_id":823712,"name":"剑无尘","level":182,"class_id":3,"last_login_at":"…","created_at":"…","updated_at":"…"}
```

**Character stickiness** — when the same account asks for a recommendation again, Atlas sends it back to the server where it already has a character (`has_character`),
which is the direct answer to "where are my characters":

```bash
curl 'http://localhost:8080/v1/routing/recommended?account_id=10001&region=cn-east'
```

```json
{"reason":"has_character","server":{"id":"game-1001","…":"…"}}
```

**Account cross-server characters** — pull everything in one call after login:

```bash
curl http://localhost:8080/v1/directory/accounts/10001/characters
```

```json
{"characters":[{"account_id":10001,"server_id":"game-1001","character_id":823712,"name":"剑无尘","level":182,"class_id":3,"last_login_at":"…","created_at":"…","updated_at":"…"}]}
```

> Large-scale write paths should not land one record at a time — the same write endpoint plus one environment variable
> switches to Message Bus async buffering (Atlas enqueues, a built-in consume loop replays), and the game server still only calls REST —
> see [Data Sync](/sync).

## 3. Admin side: realms, announcements, maintenance, stats (:8082)

**Realm / Shard** — operational records for realms and shards; a server's `realm_id` / `shard_id` maps onto them,
and discovery lists can then be filtered by those dimensions:

```bash
curl -X POST http://localhost:8082/v1/admin/realms \
  -H 'Content-Type: application/json' -d '{"id":"realm-01","name":"华东大区","region":"cn-east"}'
```

```json
{"id":"realm-01","name":"华东大区","region":"cn-east","status":"active","created_at":"…"}
```

```bash
curl -X POST http://localhost:8082/v1/admin/shards \
  -H 'Content-Type: application/json' -d '{"id":"shard-1002","name":"白虎服","realm_id":"realm-01"}'
```

```json
{"id":"shard-1002","realm_id":"realm-01","name":"白虎服","status":"active","created_at":"…"}
```

```bash
curl 'http://localhost:8080/v1/discovery/servers?shard=shard-1001'
# {"servers":[ …只有 game-1001… ]}
```

Full usage in [Realm and Shard](/realms-shards).

**Announcements** — one API call publishes a global announcement that clients pull the moment they log in:

```bash
curl -X POST http://localhost:8082/v1/admin/announcements \
  -H 'Content-Type: application/json' -d '{
    "title": "周末双倍经验",
    "body": "10月4日 10:00-22:00 全服双倍经验",
    "level": "info",
    "starts_at": "2026-10-04T02:00:00Z",
    "ends_at": "2026-10-04T14:00:00Z"
  }'
```

```json
{"id":"ann-1790909864389093095","title":"周末双倍经验","body":"10月4日 10:00-22:00 全服双倍经验","level":"info","starts_at":"2026-10-04T02:00:00Z","ends_at":"2026-10-04T14:00:00Z","created_at":"…"}
```

The player side (:8080) returns only announcements **inside their effective window** — the one above has a `starts_at` in the future, so a pull right now comes back empty;
it appears automatically when the time comes:

```bash
curl http://localhost:8080/v1/discovery/announcements
# {"announcements":[]}   ← 未到生效时间
```

**Planned maintenance** — declare a maintenance window; the server automatically enters maintenance when it starts and automatically recovers when it ends,
and a maintenance announcement is posted automatically too (its `announcement_id` is the one it created):

```bash
curl -X POST http://localhost:8082/v1/admin/servers/game-1002/maintenance-window \
  -H 'Content-Type: application/json' -d '{
    "start_at": "2026-10-02T03:00:00Z",
    "end_at": "2026-10-02T04:00:00Z"
  }'
```

```json
{"id":"mwin-1790909851023996409","server_id":"game-1002","start_at":"2026-10-02T03:00:00Z","end_at":"2026-10-02T04:00:00Z","announcement_id":"ann-1790909851023996409","created_at":"…"}
```

Scenario-level details in [Announcements and Planned Maintenance](/operations).

**Fleet stats** — first-hand numbers for the ops dashboard:

```bash
curl http://localhost:8082/v1/admin/stats
```

```json
{"total_servers":2,"servers_by_status":{"online":2},"servers_by_region":{"cn-east":2},"servers_by_version":{"":1,"1.8.2":1},"total_players":963,"total_capacity":4000,"total_characters":1}
```

## 4. Graceful shutdown (:8082 + :8081)

Drain first (stop taking new players), unregister once the remaining population walks out on its own:

```bash
curl -X POST http://localhost:8082/v1/admin/servers/game-1002/drain
curl -X POST http://localhost:8081/v1/registry/servers/game-1002/unregister -H 'Content-Type: application/json' -d '{}'
```

```json
{"server_id":"game-1002","status":"offline"}
```

It no longer shows up in the `?status=online` list afterwards. The full lifecycle is in [Server Lifecycle](/lifecycle).

---

## Endpoint quick reference

| Domain | Method | Path | Port | Description |
| --- | --- | --- | --- | --- |
| Registry | `POST` | `/v1/registry/servers/register` | :8081 | Register (idempotent) |
| Registry | `POST` | `/v1/registry/servers/{id}/heartbeat` | :8081 | Heartbeat |
| Registry | `POST` | `/v1/registry/servers/{id}/unregister` | :8081 | Voluntary shutdown |
| Discovery | `GET` | `/v1/discovery/servers` | :8080 | List (filters + pagination) |
| Discovery | `GET` | `/v1/discovery/servers/{id}` | :8080 | Details |
| Discovery | `GET` | `/v1/discovery/announcements` | :8080 | Active announcements |
| Directory | `POST` | `/v1/directory/characters` | :8080 | Write character index (idempotent) |
| Directory | `GET` | `/v1/directory/accounts/{id}/characters` | :8080 | Account cross-server characters |
| Directory | `GET` | `/v1/directory/characters/{id}` | :8080 | Single character |
| Directory | `GET` | `/v1/directory/servers/{id}/characters` | :8080 | Server character list |
| Directory | `PATCH` / `DELETE` | `/v1/directory/characters/{id}` | :8080 | Update / delete |
| Routing | `GET` | `/v1/routing/recommended` | :8080 | Server recommendation |
| Admin | `POST` | `/v1/admin/realms` / `/v1/admin/shards` | :8082 | Realm / shard records |
| Admin | `POST` | `/v1/admin/servers/{id}/maintenance` / `drain` / `enable` / `disable` | :8082 | Lifecycle operations |
| Admin | `POST` | `/v1/admin/servers/{id}/maintenance-window` | :8082 | Planned maintenance window |
| Admin | `POST` / `GET` / `DELETE` | `/v1/admin/announcements[/{id}]` | :8082 | Announcement management |
| Admin | `POST` / `GET` | `/v1/admin/migrations` | :8082 | Server merge / transfer orchestration |
| Admin | `GET` | `/v1/admin/stats` | :8082 | Fleet stats |
| Admin | `GET` | `/v1/admin/audit` · `/metrics` | :8082 | Audit · Prometheus |

## Error responses

Uniform format:

```json
{"error":{"code":"SERVER_NOT_FOUND","message":"server game-9999 does not exist"}}
```

| HTTP | code | Scenario |
| --- | --- | --- |
| 400 | `INVALID_ARGUMENT` | Missing or malformed parameter |
| 404 | `SERVER_NOT_FOUND` / `CHARACTER_NOT_FOUND` | Server / character does not exist |
| 409 | `SERVER_MANAGED_BY_CONFIG` | The registration target is managed by the server config file (see [Configuration](/server-config)) |
| 429 | `RATE_LIMITED` | Rate limit triggered |
| 503 | `STORAGE_UNAVAILABLE` | Storage unavailable |

## Next steps

| What you want to do | Where to go |
| --- | --- |
| Declare servers in a config file instead of writing registration code | [Server Info as Configuration](/server-config) |
| Integrate the SDK from a game server (Go / C++ / Python / JS / Java / C#) | Any page under "SDK" in the sidebar — all with the same walkthrough |
| Route character writes through the Message Bus instead of one REST call each | [Data Sync](/sync) |
| Failure detection / state machine / admin operation semantics | [Server Lifecycle](/lifecycle) |
| Realm / shard operations | [Realm and Shard](/realms-shards) |
| Scenarios and boundaries of announcements / planned maintenance | [Announcements and Planned Maintenance](/operations) |
| All endpoints and parameters | [API Reference](/api) |
