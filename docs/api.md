
# API Reference

Atlas exposes six API groups, each with a clear, non-overlapping responsibility.

```text
/v1/registry/*     server registration
/v1/discovery/*    server discovery
/v1/directory/*    character directory
/v1/routing/*      connection recommendation
/v1/crossserver/*  cross-server config pull (game servers)
/v1/admin/*        operations administration
```

**Design principle: Discovery and Routing are separated at the API layer.**

`GET /v1/discovery/servers` answers "which servers exist"; `GET /v1/routing/recommended` answers "where should I go". The two have different semantics and are deliberately not merged into one endpoint.

Besides REST, the same capabilities are also exposed over gRPC (see [gRPC API](#grpc-api) below); the proto definition lives at [`api/proto/atlas.proto`](https://github.com/cuihairu/atlas/blob/main/api/proto/atlas.proto).

---

## Registry

Server registration, heartbeat and unregister. Called by the **game servers**.

### POST /v1/registry/servers/register

Register a server at startup.

**Request**

```json
{
  "server_id": "game-1001",
  "name": "Zone 1 · Azure Dragon",
  "type": "game",
  "region": "cn-east",
  "realm_id": "realm-01",
  "shard_id": "shard-1001",
  "version": "1.8.2",
  "platform": "android",
  "endpoint": {
    "host": "10.0.1.21",
    "port": 30001
  },
  "capacity": 2000,
  "reg_kind": "temporary",
  "ttl_seconds": 3600
}
```

**Response** `201 Created`

```json
{
  "server_id": "game-1001",
  "status": "starting",
  "crossserver_config": { "version": 9, "hash": "3768c1708e690107" }
}
```

> `crossserver_config` carries the current cross-server config version so a server knows what to pull right at startup (version 0 = no config has ever been published); see [config-center.md](config-center.md) for the cross-server config center.

Fields Atlas persists:

```text
server_id
name
type
region
realm_id    # owning realm (see Admin Realms); the ?realm= discovery filter relies on it
shard_id    # owning shard (see Admin Shards); the ?shard= discovery filter relies on it
version
platform
endpoint
capacity
status      # starts as `starting` on registration; the first valid heartbeat promotes it to `online` automatically
metadata    # registration metadata (optional, key-value pairs): the platform never interprets
            # the values — they are stored, echoed back, and support filtering in the admin
            # console list and via metadata_key=value on /v1/admin/servers
            # (e.g. cluster=cluster-ea, channel=tap). Same semantics as the metadata field of
            # a config-declared server (server-config.md).
notify_mode / notify_callback_url
            # cross-server config change notification declaration (subscribe / callback / poll,
            # see config-center.md): stored with the registration and used by the config
            # center for push delivery
reg_kind / ttl_seconds
            # registration lifecycle kind (optional, see lifecycle.md §3.1): reg_kind defaults
            # to permanent (never auto-expires; deletion goes through DELETE /v1/admin/servers/{id});
            # temporary requires ttl_seconds > 0, counted from registration time, after which
            # health monitoring automatically reclaims the record and its runtime data.
            # Re-registering replaces this field wholesale (omitting it returns to permanent)
```

**Idempotency**: re-registering the same `server_id` is treated as an update (idempotent upsert; mutable fields are overwritten) and also returns `201 Created`. An online server that re-registers is **not** knocked back to `starting`; an `offline` / `suspect` server resets to `starting` on re-registration, which is how it comes back online (see [lifecycle.md](lifecycle.md)).

> **Config-declared exception**: servers declared via `ATLAS_SERVERS_CONFIG` do not go through the registration API — calls against them return `409 SERVER_MANAGED_BY_CONFIG`, and their profile fields are owned by the config file (heartbeats work as usual). Static fleets can be declared via config; see [server-config.md](server-config.md).

---

### POST /v1/registry/servers/{id}/heartbeat

Periodic report; the interval is set by the game deployment (5–15 seconds recommended).

**Request**

```json
{
  "players": 843,
  "load": 0.42,
  "status": "online"
}
```

**Response** `200 OK`

```json
{
  "server_id": "game-1001",
  "status": "online",
  "next_heartbeat_in": 10
}
```

Atlas advances health status automatically based on heartbeats:

```text
online
   ↓
heartbeat timeout
   ↓
suspect
   ↓
offline
```

The `status` in the response is the server's **currently effective status**: normally `online`; if the server was previously marked `suspect` / `offline` due to missed heartbeats, the response reports it as-is, and the server should call `register` again (idempotent) to come back online.

See [lifecycle.md](lifecycle.md) for details.

---

### POST /v1/registry/servers/{id}/unregister

Voluntary shutdown of a server.

**Request**

```json
{
  "reason": "shutdown"
}
```

**Response** `200 OK`

```json
{
  "server_id": "game-1001",
  "status": "offline"
}
```

Difference from heartbeat timeout: `unregister` is a **proactive** shutdown — Atlas sets the status to `offline` immediately and stops recommending the server to clients. A heartbeat timeout is a **passive** inference and must pass through the `suspect` stage first.

---

## Discovery

Server discovery. Called by **clients and operations tools**.

### GET /v1/discovery/servers

List servers. Supports filtering by the following dimensions:

```text
region
realm
shard
version
platform
status
```

> Pagination additionally supports `limit` / `cursor`. Any other query parameter is silently ignored — tag filtering is currently a storage-layer contract capability; this endpoint does not yet expose a corresponding query parameter.

**Example**

```http
GET /v1/discovery/servers?region=cn-east&status=online
```

**Response** `200 OK`

```json
{
  "servers": [
    {
      "id": "game-1001",
      "name": "Zone 1 · Azure Dragon",
      "region": "cn-east",
      "status": "online",
      "players": 843,
      "capacity": 2000
    },
    {
      "id": "game-1002",
      "name": "Zone 2 · White Tiger",
      "region": "cn-east",
      "status": "online",
      "players": 1260,
      "capacity": 2000
    }
  ]
}
```

**Pagination**: `?limit=50&cursor=...`; the cursor is based on `server_id`. The server-side per-page cap is 200 — a larger `limit` is returned as 200; callers needing the full set page through with the cursor.

**Default behavior**: without a `status` parameter, only servers in "visible" states are returned — `online` / `maintenance` / `suspect` (`ServerStatus.Visible()`); `starting` / `draining` / `offline` / `disabled` never appear in discovery. Keeping maintenance and suspect visible is deliberate: clients do not see a server "vanish into thin air"; it stays listed with a badge.

---

### GET /v1/discovery/servers/{id}

Fetch a single server's details.

**Response** `200 OK`

```json
{
  "id": "game-1001",
  "name": "Zone 1 · Azure Dragon",
  "type": "game",
  "region": "cn-east",
  "realm_id": "realm-01",
  "shard_id": "shard-1001",
  "version": "1.8.2",
  "platform": "android",
  "endpoint": {
    "host": "game-1001.example.com",
    "port": 30001
  },
  "status": "online",
  "players": 843,
  "capacity": 2000,
  "load": 0.42
}
```

**Response** `404 Not Found` — server does not exist.

---

## Directory

Character directory. This is Atlas's biggest departure from ordinary service discovery.

### GET /v1/directory/accounts/{account_id}/characters

List all characters under an account (across servers).

**Response** `200 OK`

```json
{
  "characters": [
    {
      "server_id": "game-1001",
      "character_id": 823712,
      "name": "Jian Wuchen",
      "level": 182,
      "class_id": 3,
      "last_login_at": "2026-09-30T12:30:00Z"
    },
    {
      "server_id": "game-1008",
      "character_id": 923812,
      "name": "Wuchen",
      "level": 97,
      "class_id": 7,
      "last_login_at": "2026-09-25T08:20:00Z"
    }
  ]
}
```

A client can render this directly:

```text
My Characters

Zone 1 · Azure Dragon
Jian Wuchen        Lv.182
[Enter Game]

Zone 8 · White Tiger
Wuchen             Lv.97
[Enter Game]
```

**Note**: the returned `name` / `level` are projected values; second-to-minute lag is allowed and they are for list display only.

---

### GET /v1/directory/characters/{character_id}

Fetch a single character's index record.

**Response** `200 OK`

```json
{
  "account_id": 10001,
  "server_id": "game-1001",
  "character_id": 823712,
  "name": "Jian Wuchen",
  "level": 182,
  "class_id": 3,
  "avatar": "avatar_03",
  "last_login_at": "2026-09-30T12:30:00Z"
}
```

---

### GET /v1/directory/servers/{server_id}/characters

List a server's character index; mainly for operations and merge-server verification.

**Query**: `?limit=`, `?cursor=`

---

### POST /v1/directory/characters

Write to the character index. Called by the **game servers**.

**Request**

```json
{
  "account_id": 10001,
  "server_id": "1001",
  "character_id": 823712,
  "name": "Jian Wuchen",
  "level": 1,
  "class_id": 3
}
```

**Response** `201 Created`

**Idempotency**: `(account_id, server_id, character_id)` is the unique key; duplicate writes are treated as updates.

**Status codes**: for the three write endpoints in this section (POST / PATCH / DELETE), a synchronous event adapter returns `201 Created` / `200 OK` / `200 OK`; an asynchronous one (Kafka / NATS / RabbitMQ etc.) always returns `202 Accepted` with body `{"status":"queued"}`.

---

### PATCH /v1/directory/characters/{character_id}

Update a character index record (e.g. level-up, rename).

```json
{
  "level": 183,
  "name": "Jian Wuchen II",
  "metadata": {"class": "warrior", "guild": "Changfeng"}
}
```

---

### DELETE /v1/directory/characters/{character_id}

Delete a character index record. **Only the index is deleted, never the character data** — deleting the actual character is the game server's responsibility.

---

## Routing

Connection recommendation.

### GET /v1/routing/recommended

**Query parameters** (all optional)

| Parameter | Description |
| --- | --- |
| `region` | Target region, exact match |
| `version` | Client version, exact match |
| `platform` | Platform, exact match |
| `account_id` | Player account; when provided, servers that already hold the player's characters are preferred |

Without an explicit `status`, only `online` servers are recommended.

**Pre-maintenance steering** (window-aware, shipped as a v0.2+ candidate): a server inside an **active maintenance window**, or within `ATLAS_ROUTING_MAINTENANCE_LEAD` (default 5 minutes) of a window's start, is not recommended — even if its lifecycle status is still `online` (health patrol hasn't flipped it yet). Players are always landed on a server that "won't go down for maintenance in the next few minutes". Window exclusion is a **safety constraint**, not a preference filter: it applies in both the strict and fallback phases, and if the whole fleet is inside windows the endpoint honestly returns `404 NO_SERVER_AVAILABLE` (better no server to recommend than steering into one about to go down). Servers whose window has ended (before the patrol deletes the record) become recommendable again.

```text
GET /v1/routing/recommended?region=cn-east&platform=android&account_id=10001
```

**Response** `200 OK`

```json
{
  "server": { "id": "game-1001", "endpoint": {"host": "10.0.0.1", "port": 30001} },
  "reason": "lowest_load"
}
```

`server` is the full Server object (see Discovery). With no available server, the endpoint returns `404 NO_SERVER_AVAILABLE`.

**Decision basis** (compared in order)

```text
1. Player Character   servers that already hold account_id's characters first
2. Effective Load     lower effective load first (load × 100 / weight; weight 100 = neutral)
3. Capacity           with equal effective load, higher remaining capacity first
4. Server ID          deterministic tie-breaker
```

**Weights (the gray-release knob)**: a server record's `weight` (set by operators via the admin API, 1..1000; 0 = unset = neutral) only affects step 2's comparison: **effective load = load × 100 / weight**. A server with weight > 100 looks emptier and, at steady state, attracts roughly proportional extra traffic (200 ≈ double); < 100 is suppressed — for a new-server ramp-up, keep the canary server at a low weight and raise it step by step; combined with a private tag (e.g. `canary`) to scope the canary group, that is a gray-release allowlist. Weights only change recommendation ordering (recommendation metadata), never any safety constraint: status filtering, maintenance-window exclusion and has-character priority all behave as before; the diagnostics page reports `weight` and `effective_load` per server. Admin entry point: [POST /v1/admin/servers/{id}/weight](#post-v1-admin-servers-id-weight).

Possible `reason` values: `lowest_load` / `highest_capacity` / `has_character` / `fallback` (strict filtering found nothing and relaxed to status-only recommendation), useful for debugging odd recommendations.

**Difference from Discovery**: Discovery returns a **candidate set**; Routing returns a **single decision**. The client's server-select screen uses Discovery; the "Start Game" button uses Routing.

---

## Admin

Operations administration. Called by **operations tools**; never exposed to clients.

Three groups of extended admin endpoints are documented in their topic pages and have no gRPC counterpart (REST-only):

| Endpoint | Purpose | Docs |
| --- | --- | --- |
| `GET/POST /v1/admin/servers/{id}/tags`, `DELETE …/tags/{code}` | Operational tags (incl. visibility control) | [concepts.md, tags section](concepts.md) |
| `GET/PUT /v1/admin/crossserver/config` | Cross-server config read / full publish (incl. notify fan-out report) | [config-center.md](config-center.md) |
| `GET /v1/admin/audit` | Admin operation audit stream (`ATLAS_AUDIT_ENABLED`, on by default) | [security.md §3 Operation audit](security.md#3-operation-audit) |

### POST /v1/admin/servers/{id}/maintenance

Enter maintenance mode. New players cannot enter; existing players may keep playing.

### POST /v1/admin/servers/{id}/drain

Enter drain mode. Stop accepting new connections and let existing players leave naturally.

### POST /v1/admin/servers/{id}/enable

Re-enable a server.

### POST /v1/admin/servers/{id}/disable

Disable a server. Completely invisible to clients.

### DELETE /v1/admin/servers/{server_id}

**Delete a server record** (path parameter `{server_id}`, i.e. `{id}` in the actual route; runtime data included). This is an **explicit deletion instruction** — permanent registrations never auto-expire, so reclamation can only happen here (semantics in [lifecycle.md](lifecycle.md) §3.1 registration lifecycle).

- Unknown server: `404 SERVER_NOT_FOUND`;
- Config-declared server (`source=config`): `409 SERVER_CONFIG_MANAGED` —
  edit the [servers config](server-config.md) file, don't use the API;
- Success `200 OK`: `{"server_id": "game-1001", "status": "deleted"}`;
- The operation enters the Admin audit stream (`GET /v1/admin/audit`).

### POST /v1/admin/servers/{id}/weight

Set a server's routing weight (the gray-release knob). **Body**: `{"weight": N}` (0 clears back to neutral, valid range 1..1000, out-of-range `400`). The weight only affects the effective-load comparison in `/v1/routing/recommended` ordering (load × 100 / weight); semantics in [Routing](#get-v1-routing-recommended). Like tags, it is admin-only configuration: register / heartbeat never overwrite it; unknown server `404`.

**Response** `200 OK`

```json
{ "server_id": "game-1001", "weight": 200 }
```

For state-machine semantics see [lifecycle.md](lifecycle.md).

### GET /v1/admin/audit

Admin operation audit stream (`ATLAS_AUDIT_ENABLED`, on by default). **Query**: `?limit=N` (default 50, positive integer).

**Response** `200 OK`

```json
{
  "entries": [
    {
      "time": "2026-10-14T02:10:00.123456Z",
      "actor": "operator:3f8a1c2b9d0e",
      "method": "POST",
      "path": "/v1/admin/servers/game-1001/weight",
      "status": 200,
      "body": {"weight": 200}
    }
  ]
}
```

- **body**: present only for mutating methods (POST/PUT/PATCH/DELETE), kept as the change record; truncated to a fixed 4096 bytes (hard-coded at startup, not configurable); invalid JSON is wrapped as a string.
- **actor**: `role:key-fingerprint`; with mTLS `role:key-fingerprint:cert-fingerprint`; in dev mode without configured keys, `role:anonymous`.
- It reads an **in-memory ring** (fixed 1000 entries, hard-coded at startup, not configurable); lost on restart. REST and gRPC write operations flow into the same ring; durable archival is the optional audit sink (`ATLAS_AUDIT_SINK`, see security.md §3). Semantics and chain structure: [security.md](security.md#3-operation-audit).

### GET /v1/admin/stats

Fleet statistics (total servers / online / players / capacity + facet counts for `servers_by_status/region/version/type/realm/shard/tag` and `server_metadata_keys`). Aggregates are read from the in-memory fleet index — maintained live by registration / heartbeat / unregister and the same source as the /servers filters and list (consistent numbers by construction).

### GET /v1/admin/servers

Admin-console fleet list (adds control-plane fields the public discovery endpoint doesn't return, e.g. last heartbeat / metadata).
**Query**: `?id=` (substring), `?status=`, `?region=`, `?type=`, `?realm=`,
`?shard=`, `?version=`, `?platform=`, `?tag=`, `?metadata_key=` +
`?metadata_value=` (key-value pair), `?limit=`, `?cursor=`. Filters combine freely;
cursor pagination follows the same contract as the public list (next page takes IDs strictly greater than the cursor).
`limit` caps at 200; exceeding it returns `400 INVALID_ARGUMENT` (the public discovery endpoint silently clamps to 200 — intentionally different).

### GET /v1/admin/characters/search

Character-index search. **Query**: `?q=` (name substring), `?server_id=`, `?account_id=`
(player ID, opaque reference), `?metadata_key=` + `?metadata_value=` (a business-concept
filter channel, e.g. `class=warrior`; the platform has no built-in class field), `?min_level=`,
`?max_level=`, `?limit=`, `?cursor=`.

### GET /v1/admin/diagnose/routing

Player-perspective troubleshooting (backend of the admin console's "Troubleshoot/Diagnostics" page). **Query**: same as
`/v1/routing/recommended` (`?account_id=`, `?region=`, `?version=`,
`?platform=`), plus `?status=` (`online` / `suspect` / `offline`; default treated as
`online`). Returns the **middle stages of the same recommendation pipeline, laid open**: matching phase
(strict/fallback), per-server verdicts (rank position, strict/fallback hit, has-character flag,
eligibility, maintenance window `maintenance_window` — active/lead-in windows are attached verbatim to the verdict,
rejection reason `status=…` / `maintenance_window=active|upcoming`), the champion and its attribution;
with `account_id`, the account's directory entries are attached. The pipeline is exactly the recommendation pipeline: window exclusion applies
in diagnostics too and is visible there; the per-server `eligible` field accounts for windows.

### GET /v1/admin/rate-limits

Read-only view of gateway rate limiting: the resolved effective rules (longest path-prefix match) + the default rule +
429 counters (per endpoint rule / per client IP). Configuration itself goes through environment variables
(`ATLAS_RATE_LIMITS` / `ATLAS_RATE_LIMIT_DEFAULT`) + a restart to take effect; if only
`ATLAS_RATE_LIMITS` is set without `_DEFAULT`, the fallback rule is 100 rps / 200 burst.

### GET /v1/admin/indexqueue/status

Read-only store-queue status (the admin console's "Store Queue" card). Returns a `QueueStats` snapshot of the memory store's command write path: `enabled` is always true, `watermark` (version watermark: the monotonically increasing applied sequence number), `depth_control` / `depth_hot` (in-flight depth per lane class), `lanes_control` / `lanes_hot` (lane count per lane class), `lane_cap` / `batch_cap`, cumulative counters (`enqueued` / `applied` / `merged` / `idempotent_hits` / `backpressure_sync`), the most recent commit (`last_flush*`) and the last 16 flush records (`recent_flushes[]`, with batch size / merge count / critical-section duration), applied distribution by kind (`applied_by_kind`), and replay-ring length (`capture_len`).

SQL stores (postgres/mysql) have no command queue: the endpoint returns `503` +
`INDEX_QUEUE_DISABLED` — **not** an empty snapshot — which is how the admin console shows "not enabled". The same snapshot is also the sole data source for the `atlas_store_queue_*` metric family on `/metrics` (read at scrape time, zero write-path overhead).

### GET /v1/admin/load-series

Load time series (overview-page charts). **Query**: `?window=` (`5m|10m|30m|1h|10h`),
`?server_id=`, `?region=` (scope priority server > region > fleet). Returns
`points[]` (`t` / `players` / `load`), from an in-memory ring sampler (15s samples,
≥24h retention; reset on restart, no TSDB).

### GET /v1/admin/load-heatmap

Server heatmap (dashboard heatmap page): one row per live server, each row holding bucketed load averages over the window. **Query**: `?window=` (`1h` → 12×5m buckets, default; `24h` →
24×1h buckets). Returns `window` / `bucket` (bucket span) / `generated_at` /
`servers[]` (`server_id` / `name` / `region` / `type` / `status` /
`players` / `capacity` / `load` are a live snapshot; `buckets[]` holds `t` / `load` /
`players` / `samples` bucketed averages). A bucket with `samples == 0` means no samples in that period
(restart, mid-window registration, etc.); consumers must render it as a distinct "no data" state, never as zero load.
Embedders without the sampler get empty `buckets`, never null.

### GET /v1/admin/bus-series

Message-bus curves (overview-page bus panel). **Query**: `?window=` as above. Per topic, returns the backlog depth `depth[]` curve, publish/consume rates (counter deltas within the window) and current totals
(`published` / `consumed` / `in_flight`); `adapter` names the event adapter type.


### POST /v1/admin/migrations

Create a migration record (shared by merge / transfer / move semantics, see [migration.md](migration.md)).

```json
{
  "source_servers": ["game-1001", "game-1002"],
  "target_server": "game-2001"
}
```

**Response** `201 Created`: the migration record (`id` looks like `mig-<unixnano>`, `status` starts at `pending`).

**Migration-window linkage (since 2026-10-14)**: creating a migration automatically lays a maintenance window over the **source servers**
(`start_at` = now, `end_at` = `start_at + 24h`, `announce` defaults to `true`);
the window reuses the same creation path as the batch window API. If the windows can't be laid the endpoint **fails closed**: it returns
`500` (`create maintenance windows: ...`) and the migration record is **not** written.
Windows are not auto-cancelled (they expire naturally after a rollback); operators can cancel manually via `DELETE /v1/admin/maintenance-windows/{id}`.
See [migration.md §9.1](migration.md#91-migration-window-linkage).

> **v0.1 status**: this endpoint only creates the record — it does not start orchestration or advance state (`migrating` / `verifying` / `completed` are unreachable); unknown source servers return `404 SERVER_NOT_FOUND`. The only reachable state transition is rollback below.

### GET /v1/admin/migrations

Migration record list (newest→oldest). **Query**: `?limit=` (default 50). Single-record lookup is `GET /v1/admin/migrations/{id}`; missing records return **404** `MIGRATION_NOT_FOUND`.

### POST /v1/admin/migrations/{id}/rollback

Mark a migration record `rolled_back` (a state transition only, no data actions; `pending` / `migrating` / `failed` are rollbackable). **400** `INVALID_ARGUMENT` if the state is not rollbackable (e.g. already completed), **404** `MIGRATION_NOT_FOUND` if the record doesn't exist.

**Response** `200 OK`: `{"id": "…", "status": "rolled_back"}`

### POST /v1/admin/realms

Create a Realm. ID conflict returns `409 REALM_EXISTS`; missing `id` / `name` returns `400`.

```json
{
  "id": "realm-01",
  "name": "East China Region",
  "region": "cn-east",
  "status": "active"
}
```

`status` defaults to `active` when omitted. Returns `201` with the full Realm object.

### GET /v1/admin/realms

List realms by `created_at`, newest first. `?limit=` caps the count (default 50).

```json
{ "realms": [ ... ] }
```

### POST /v1/admin/shards

Create a Shard under a realm. `realm_id` must point to an existing realm, otherwise `404 REALM_NOT_FOUND`; ID conflict returns `409 SHARD_EXISTS`.

```json
{
  "id": "shard-0101",
  "realm_id": "realm-01",
  "name": "Zone 1",
  "status": "active"
}
```

### GET /v1/admin/shards

List shards by `created_at`, newest first. `?realm_id=` filters to one realm; `?limit=` caps the count.

```json
{ "shards": [ ... ] }
```

Servers can carry `realm_id` / `shard_id` at registration to attach to their realm and shard (see Registry); discovery filtering supports `realm` / `shard` (see Discovery).

### POST /v1/admin/servers/{id}/maintenance-window

Schedule a maintenance window (v0.1.20): when `start_at` arrives, health monitoring automatically puts the server into `maintenance`; after `end_at` it restores the original status (see lifecycle.md §5). `start_at` defaults to now; `announce` defaults to `true`, which auto-creates a server-level warning announcement covering the same period and links it to the window.

**Request**

```json
{
  "start_at": "2026-10-02T02:00:00Z",
  "end_at": "2026-10-02T04:00:00Z",
  "announce": true
}
```

**Response** `201 Created`

```json
{
  "id": "mwin-1759376400000000000",
  "server_id": "game-1001",
  "start_at": "2026-10-02T02:00:00Z",
  "end_at": "2026-10-02T04:00:00Z",
  "announcement_id": "ann-1759376400000000000",
  "created_at": "2026-10-01T12:00:00Z"
}
```

> `previous_status` is written only when health monitoring applies the window (`omitempty`; the response doesn't include the field before it is applied); empty after application means the server was already in maintenance and has nowhere to return to.

Errors: `404 SERVER_NOT_FOUND` (server doesn't exist), `400 INVALID_ARGUMENT` (`end_at` not later than `start_at`).

### GET /v1/admin/maintenance-windows

List maintenance windows (newest→oldest). `?server_id=` filters to one server; `?limit=` caps the count.

```json
{ "maintenance_windows": [ ... ] }
```

### DELETE /v1/admin/maintenance-windows/{window_id}

Cancel a window (`204 No Content`). A server already in maintenance is not pulled out automatically — use the regular `enable` / `maintenance` transitions; announcements already auto-created are not retracted.

### POST /v1/admin/maintenance-windows/batch

Fleet-wide batch creation: one window template laid over a `server_ids` list; `announce` semantics match single-server creation (default `true`; each server gets its own warning announcement auto-created and linked).

**Request**

```json
{
  "server_ids": ["game-1001", "game-1002", "game-1003"],
  "start_at": "2026-10-20T02:00:00Z",
  "end_at": "2026-10-20T04:00:00Z",
  "stagger_seconds": 600,
  "announce": true,
  "dry_run": false
}
```

- `start_at` defaults to now; `end_at` is required.
- `stagger_seconds`: server i's window (start and end together) is shifted later by `i × stagger_seconds` — rolling maintenance that avoids the whole fleet going down at once; `0` (default) is simultaneous.
- `dry_run`: validate only (servers exist, time ranges valid) and return the planned windows (ids are preview values, not persisted) without creating any windows or announcements.
- `server_ids` is de-duplicated preserving order; empty entries and unknown servers are recorded per-server in `failed` without affecting the rest — the batch has partial-success semantics (the store has no cross-server transaction).

**Response** `200 OK`

```json
{
  "windows": [
    {
      "id": "mwin-1759376400000000000-0",
      "server_id": "game-1001",
      "start_at": "2026-10-20T02:00:00Z",
      "end_at": "2026-10-20T04:00:00Z",
      "announcement_id": "ann-1759376400000000000-0",
      "created_at": "…"
    },
    {
      "id": "mwin-1759376400000000000-2",
      "server_id": "game-1003",
      "start_at": "2026-10-20T02:20:00Z",
      "end_at": "2026-10-20T04:20:00Z",
      "announcement_id": "ann-1759376400000000000-2",
      "created_at": "…"
    }
  ],
  "failed": [
    { "server_id": "game-1002", "error": "maintenance window for unknown server game-1002: …" }
  ]
}
```

Errors: `400 INVALID_ARGUMENT` (empty `server_ids`, missing `end_at`, inverted template time range).

### POST /v1/admin/announcements

Create an announcement. Without `server_id` it is a global announcement; `level` ∈ `info` / `warning` / `critical` (default `info`).

**Request**

```json
{
  "server_id": "game-1001",
  "title": "Double Loot Weekend",
  "body": "Oct 5 00:00 – Oct 7 24:00",
  "level": "info",
  "starts_at": "2026-10-04T16:00:00Z",
  "ends_at": "2026-10-07T16:00:00Z"
}
```

**Response** `201 Created` (the full announcement object).

Errors: `404 SERVER_NOT_FOUND`, `400 INVALID_ARGUMENT` (missing `title`, unknown `level`, `ends_at` not later than `starts_at`).

### GET /v1/admin/announcements

List announcements (newest→oldest). `?server_id=` = global + that server; `?active=true` returns only currently effective ones; `?limit=` caps the count.

```json
{ "announcements": [ ... ] }
```

### DELETE /v1/admin/announcements/{announcement_id}

Delete an announcement (`204 No Content`).

### GET /v1/discovery/announcements

Player clients fetch **currently effective** announcements: global ones plus (optionally) those of the `?server_id=` server. Read-only, no authentication.

**Request**

```text
GET /v1/discovery/announcements?server_id=game-1001&limit=20
```

**Response** `200 OK`

```json
{
  "announcements": [
    {
      "id": "ann-1759376400000000000",
      "server_id": "game-1001",
      "title": "Maintenance Notice",
      "body": "Server game-1001 will be under maintenance from ...",
      "level": "warning",
      "starts_at": "2026-10-02T02:00:00Z",
      "ends_at": "2026-10-02T04:00:00Z",
      "created_at": "2026-10-01T12:00:00Z"
    }
  ]
}
```

### GET /metrics

Prometheus scrape endpoint (admin port :8082, protected by Admin auth). Exposes:

| Metric | Type | Description |
| --- | --- | --- |
| `atlas_registry_servers_total{status}` | gauge | Servers per lifecycle status |
| `atlas_directory_characters_total` | gauge | Total character-index entries |
| `atlas_registry_heartbeat_lag_seconds` | histogram | Heartbeat lag observed by the health patrol |
| `atlas_discovery_requests_total{filter}` | counter | Discovery list requests (per filter) |
| `atlas_admin_requests_total{endpoint,status}` | counter | Admin API requests (per route and status; `endpoint` is the route pattern for REST and the full method name for gRPC Admin RPCs; `status` is the HTTP status code on both transports — gRPC codes map per google.rpc convention; rate-limited / auth-denied calls are counted too) |
| `atlas_health_transitions_total{from,to}` | counter | Server lifecycle status transitions |
| `atlas_directory_write_duration_seconds{op}` | histogram | Character-directory write-path latency (`op` = service-layer `create`/`update`/`delete`, or the event projection name `created`/`updated`/`deleted`/… — production write paths on both REST and gRPC go through the event bus projection) |
| `atlas_registry_write_duration_seconds{op}` | histogram | Registry write-path latency (`op` = `register` / `heartbeat` / `unregister`) — the hottest write path in the fleet: if heartbeat persistence slows, this metric rises first, and false suspect/offline patrol verdicts only appear afterwards |

### Request Tracing (X-Request-ID)

All three HTTP listeners (public / registry / admin) and the gRPC port (:9090) do per-request tracing:

- An inbound `X-Request-ID` (gRPC: `x-request-id` metadata; valid chars `[A-Za-z0-9._:-]`, ≤64 chars) is reused and echoed back in the response headers (gRPC echoes into the response metadata; rejected RPCs are echoed too); otherwise a 128-bit random hex id is generated.
- On completion an access log line is emitted (REST `msg=http request`: `request_id`, `method`, `path`, `status`, `duration_ms`; gRPC `msg=grpc request`: `request_id`, `method`, `code`, `duration_ms`). `/healthz`, `/readyz` and `/metrics` — the periodic-probe paths — only echo the header, without logging.
- A gateway (APISIX/nginx) forwards the same header, and all six official SDKs have a fixed correlation-header injection point (Go `Options.DefaultHeaders`, C++ `Options.default_headers`, Python `default_headers=`, JS `defaultHeaders`, Java `setDefaultHeaders`, C# `DefaultHeaders` — REST headers / gRPC metadata, same semantics; on gRPC you can also attach metadata on the context of an individual call), letting you stitch one request across REST and gRPC multi-hop logs. Rate-limited / auth-denied requests also get an id (the tracing middleware / interceptor is outermost).

### Distributed Tracing (OpenTelemetry OTLP)

X-Request-ID is a cross-hop correlation id, not a span tree. When you need a trace view, set `ATLAS_OTLP_ENDPOINT` (e.g. `http://localhost:4318`) to enable OpenTelemetry trace export over OTLP/HTTP — Jaeger, Tempo, or any OTLP-speaking collector can receive it (the protocol is the commitment; the backend isn't locked). Unset, it's a global no-op tracer provider — spans aren't recorded and the export pipeline costs nothing (the same default philosophy as gRPC TLS: unconfigured means plaintext). An endpoint with an `https://` prefix uses TLS; one with a path (a collector behind a reverse proxy) gets the standard `/v1/traces` appended automatically.

- **Span shape**: one server root span per request across the three HTTP listeners and the gRPC port. REST spans are named by route pattern (`GET /v1/discovery/servers/{id}` — low cardinality, aggregation-friendly; the raw path with the entity id lives in the `url.path` attribute); gRPC spans are named by full method name (`/admin.AdminService/GetStats`). Attributes follow OTel semconv (`http.*` / `rpc.*`) plus `atlas.request_id` — the same id as X-Request-ID, so logs and traces can cross-reference each other.
- **Service-layer child spans**: the four services — registry / discovery / routing / directory — open `internal` child spans under the same request context (`registry.register` / `registry.heartbeat` / `registry.unregister`, `discovery.list_servers` / `discovery.get_server`, `routing.recommend` / `routing.diagnose`, `directory.create` / `directory.update` / `directory.delete` / `directory.get`, etc.), automatically attached under the transport root as a tree with zero extra configuration; error status is carried by the transport root while child spans record nesting and timing.
- **Error semantics**: spans are set to `Error` on HTTP ≥500 or gRPC non-OK; 4xx stays Unset (client-caused, matching the semconv guidance).
- **Sampling**: `ATLAS_TRACING_SAMPLE_RATIO` (default `1.0`), `ParentBased(TraceIDRatioBased)` — when the upstream traceparent already carries a sampling decision it wins; the ratio only governs new roots; values outside (0,1] fall back to `1.0`.
- **Silent paths**: `/healthz`, `/readyz`, `/metrics` follow the same rule as the access log — no spans.
- **Metrics untouched**: metrics stay on Prometheus (`GET /metrics`); tracing goes over OTLP — no second metrics backend is introduced.
- On shutdown the export queue is flushed (10s cap) so trailing spans aren't lost.

---

## gRPC API

The second transport besides REST: the five services share the same internal services with REST, so both paths behave identically (filtering, ordering, event publishing, error semantics). Coverage is divided: Registry / Discovery / Directory / Routing map one-to-one to REST endpoints; AdminService covers 11 RPCs (maintenance mode, enable/disable, stats, heatmap, character search, migration orchestration and rollback), while realms / shards / tags / maintenance windows / announcements / audit / cross-server config / diagnose / rate limits / series endpoints are currently REST-only (see the [Admin](#admin) section above for the index).

- **Listen address**: `:9090` (change via `ATLAS_GRPC_ADDR`; setting it to the empty string disables gRPC)
- **Security parity**: the same security domains, environment variables, audit ring and rate-limit buckets as REST — RegistryService is protected by `ATLAS_REGISTRY_TOKENS` + IP allowlist, AdminService by `ATLAS_ADMIN_API_KEYS` + RBAC + IP allowlist, the three Public services stay open; unconfigured means open (dev mode); over-limit returns `ResourceExhausted` (`RATE_LIMITED`). Transport security is optional: `ATLAS_GRPC_TLS_CERT`/`_KEY` (add `_CLIENT_CA` for mTLS); unconfigured means plaintext. See [Authentication](#authentication) and [security.md](security.md).
- **Observability parity**: Admin RPCs count into the same `atlas_admin_requests_total` (`endpoint` is the gRPC full method name, `status` the mapped HTTP status code; rate-limited / auth-denied calls are counted too); registry/heartbeat write latency and discovery request volume already come from service-layer metrics — both transports flow into the same series; see the `GET /metrics` endpoint above for the metric table.
- **proto definition**: [`api/proto/atlas.proto`](https://github.com/cuihairu/atlas/blob/main/api/proto/atlas.proto), Go package `github.com/cuihairu/atlas/api/pb`
- **Relationship to REST**: gRPC is not a replacement — the Go SDK (v0.1.6+) supports both transports (other language SDKs are REST clients); high-frequency game-server heartbeats are cheaper over gRPC, while ops tools are more comfortable over REST

```protobuf
package atlas.v1;

service RegistryService {    // corresponds to /v1/registry/*
  rpc Register(RegisterRequest) returns (RegisterResponse);
  rpc Heartbeat(HeartbeatRequest) returns (HeartbeatResponse);
  rpc Unregister(UnregisterRequest) returns (UnregisterResponse);
}

service DiscoveryService {   // corresponds to /v1/discovery/*
  rpc ListServers(ListServersRequest) returns (ListServersResponse);
  rpc GetServer(GetServerRequest) returns (GetServerResponse);
}

service DirectoryService {   // corresponds to /v1/directory/*
  rpc CreateCharacter(CreateCharacterRequest) returns (CreateCharacterResponse);
  rpc GetCharacter(GetCharacterRequest) returns (GetCharacterResponse);
  rpc ListCharactersByAccount(ListCharactersByAccountRequest) returns (ListCharactersByAccountResponse);
  rpc ListCharactersByServer(ListCharactersByServerRequest) returns (ListCharactersByServerResponse);
  rpc UpdateCharacter(UpdateCharacterRequest) returns (UpdateCharacterResponse);
  rpc DeleteCharacter(DeleteCharacterRequest) returns (DeleteCharacterResponse);
}

service RoutingService {     // corresponds to /v1/routing/recommended
  rpc Recommend(RecommendRequest) returns (RecommendResponse);
}

service AdminService {       // corresponds to /v1/admin/*
  rpc SetMaintenance(ServerIdRequest) returns (OkResponse);
  rpc SetDrain(ServerIdRequest) returns (OkResponse);
  rpc Enable(ServerIdRequest) returns (OkResponse);
  rpc Disable(ServerIdRequest) returns (OkResponse);
  rpc GetStats(GetStatsRequest) returns (GetStatsResponse);
  rpc GetLoadHeatmap(LoadHeatmapRequest) returns (LoadHeatmapResponse);
  rpc SearchCharacters(SearchCharactersRequest) returns (SearchCharactersResponse);
  rpc CreateMigration(CreateMigrationRequest) returns (CreateMigrationResponse);
  rpc GetMigration(GetMigrationRequest) returns (GetMigrationResponse);
  rpc ListMigrations(ListMigrationsRequest) returns (ListMigrationsResponse);
  rpc RollbackMigration(RollbackMigrationRequest) returns (RollbackMigrationResponse);
}
```

### Error mapping

| REST | gRPC status | Scenario |
| --- | --- | --- |
| 400 `INVALID_ARGUMENT` | `InvalidArgument` | Missing or malformed parameters |
| 401 `MISSING_API_KEY` / `INVALID_API_KEY` / `MISSING_TOKEN` / `INVALID_TOKEN` | `Unauthenticated` | Missing or invalid credentials in the Admin / Registry domains |
| 403 `IP_NOT_ALLOWED` / `ROLE_NOT_ALLOWED` | `PermissionDenied` | Client IP not in the allowlist; a viewer attempting to write an Admin RPC |
| 404 `SERVER_NOT_FOUND` / `CHARACTER_NOT_FOUND` | `NotFound` | Server / character / migration doesn't exist |
| 409 `ALREADY_REGISTERED` | — | **Reserved code, currently never emitted**: registration is an idempotent upsert, so duplicate registration is an update (see the [Error responses](#error-responses) table); it would be mapped if strict validation is introduced later |
| 409 `SERVER_MANAGED_BY_CONFIG` | `AlreadyExists` | The registration target is owned by a server config file (see [server-config.md](server-config.md)) |
| 429 `RATE_LIMITED` | `ResourceExhausted` | The shared token bucket tripped (rules match on gRPC full-method-name prefix, see [security.md](security.md)) |
| 500 / 503 | `Internal` | Storage-layer error |

### Field conventions

- Timestamps are RFC3339 strings, matching the REST JSON representation
- Optional update fields use proto3 `optional` (`name` / `level` / `class_id` / `avatar` on `UpdateCharacterRequest`); unset fields are not modified — same semantics as `PATCH`
- Directory write RPCs, like the REST write endpoints, go through event adapters: a synchronous adapter returns `{"character": ...}` + `status: "created|updated|deleted"`; an asynchronous adapter (e.g. Redis Streams) returns `status: "queued"`

## Common Conventions

### Listen addresses and probes

All four listeners are individually configurable (the defaults are the common deployment shape):

| Listener | Environment variable | Default | Serves |
| --- | --- | --- | --- |
| Public | `ATLAS_HTTP_ADDR` | `:8080` | Discovery / Directory / Routing recommendation + `GET /healthz` |
| Registry | `ATLAS_REGISTRY_ADDR` | `:8081` | Registry register / heartbeat / unregister + cross-server config pull + `GET /healthz` |
| Admin | `ATLAS_ADMIN_ADDR` | `:8082` | Admin API + `GET /readyz` + `GET /metrics` (Prometheus) |
| gRPC | `ATLAS_GRPC_ADDR` | `:9090` | Five services, 23 RPCs; set to empty string to disable |

Production deployments usually merge :8080/:8081/:8082 behind a reverse proxy (see the dual-transport table in [sdk-go.md](sdk-go.md)). Probes differ per port: public and registry ports are `GET /healthz`; the admin port is `GET /readyz`.

**Cross-origin (CORS)**: the public and admin ports mount CORS middleware; origins are only honored from an explicit `ATLAS_CORS_ORIGINS` (comma-separated, e.g. `https://ops.example.com,https://app.example.com`; `*` opens everything, for development only). Unset, no CORS headers are emitted at all — anything outside browser same-origin is unusable; nothing is opened by default. The registry port serves game servers (machine traffic) and has no CORS. Admin CORS sits outside authentication: a preflight `OPTIONS` without an Admin Key is still let through by browsers.

### Authentication

Atlas divides the API into three security domains, each configured independently:

```text
┌──────────────────────────────────────────────────────────────┐
│                        Atlas API                              │
│                                                              │
│  ┌──────────────────┐  ┌──────────────────┐  ┌────────────┐ │
│  │  Registry        │  │  Discovery /     │  │  Admin     │ │
│  │  /v1/registry/*  │  │  Directory reads │  │  /v1/admin │ │
│  │                  │  │  /v1/discovery/* │  │  /*        │ │
│  │  Service Token   │  │  /v1/directory/* │  │  API Key   │ │
│  │  + IP allowlist  │  │                  │  │  + IP      │ │
│  │                  │  │  Gateway handles │  │  allowlist │ │
│  │  Internal network│  │  auth            │  │  Ops tools │ │
│  └──────────────────┘  │  Public internet │  └────────────┘ │
│                        └──────────────────┘                  │
└──────────────────────────────────────────────────────────────┘
```

| Domain | Endpoints | Callers | Auth | Configuration |
| --- | --- | --- | --- | --- |
| **Registry** | `/v1/registry/*` | Game servers (internal network) | Service Token (`Authorization: Bearer <token>` or `X-Atlas-Token`) + IP allowlist | `ATLAS_REGISTRY_TOKENS` + `ATLAS_REGISTRY_IP_WHITELIST` |
| **Public** | `/v1/discovery/*` `/v1/directory/*` `/v1/routing/*` | Clients (public internet) | Handled at the gateway (player AccessToken); Atlas does not verify | Gateway config |
| **Admin** | `/v1/admin/*` | GM tools / operations | API Key (`Authorization: Bearer <key>`) + RBAC + IP allowlist | `ATLAS_ADMIN_API_KEYS` + `ATLAS_ADMIN_ROLES` + `ATLAS_ADMIN_IP_WHITELIST` |

Admin RBAC: `ATLAS_ADMIN_ROLES` authorizes as a comma list of `key:role` (`admin`/`operator`/`viewer`); viewer is read-only (GET/HEAD), operator can perform routine operational writes, keys not listed default to `admin` — backward compatible with deployments that only set `ATLAS_ADMIN_API_KEYS`. Check order: IP allowlist (403 `IP_NOT_ALLOWED`) → API Key (401 `MISSING_API_KEY`/`INVALID_API_KEY`) → role (403 `ROLE_NOT_ALLOWED`). Also: `GET /v1/crossserver/config` is mounted on two ports — the registry port :8081 (game-server pull, the main path) and the public port :8080 (for setups without a registry gateway); see [config-center.md](config-center.md).

**gRPC transport (:9090) authentication**: the same security domains and the same environment variables as REST. The three RegistryService RPCs are protected by `ATLAS_REGISTRY_TOKENS` + the registry IP allowlist (same credentials as REST — `authorization: <token>` or `x-atlas-token` metadata); the eleven AdminService RPCs are protected by `ATLAS_ADMIN_API_KEYS` + `ATLAS_ADMIN_ROLES` + the admin IP allowlist (viewer may call only the five read RPCs: GetStats / GetLoadHeatmap / GetMigration / ListMigrations / SearchCharacters); Discovery / Directory / Routing belong to the Public domain and stay open. With a domain unconfigured, gRPC is fully open just like REST (dev mode). Errors are expressed as gRPC statuses: `Unauthenticated` (missing/bad credentials), `PermissionDenied` (IP or role mismatch), with messages reusing the REST error-code vocabulary like `MISSING_API_KEY` / `INVALID_TOKEN`. Operation audit is dual-transport too: AdminService RPCs enter the same audit ring as REST (`path` is the full method name such as `/atlas.v1.AdminService/Disable`; reads record `GET`, writes record `POST` with a protojson request diff, and gRPC codes map to equivalent HTTP statuses); auth-failed calls don't enter the ring — the same "audit sits inside authentication" semantics as REST. The Go SDK attaches the owning domain's credentials automatically on both transports (see [sdk-go.md](sdk-go.md)); callers never stuff metadata by hand.

**Why aren't Public endpoints authenticated inside Atlas?**

Client requests pass through a gateway (APISIX / Kong / Nginx) that has already done TLS termination and player-identity verification. Atlas handles only business logic and does not re-verify tokens. The benefits:

1. The gateway can centralize authentication for all backend services, not just Atlas
2. Auth policy (token expiry, refresh, blocklists) is managed centrally at the gateway layer
3. Atlas saves one JWT parse per request

**Development mode**

With all auth configuration empty, the corresponding endpoints are fully open. This is for local development convenience — **production must configure them**.

### Error responses

```json
{
  "error": {
    "code": "SERVER_NOT_FOUND",
    "message": "server game-1001 does not exist"
  }
}
```

| HTTP | code | Scenario |
| --- | --- | --- |
| 400 | `INVALID_ARGUMENT` | Missing or malformed parameters |
| 404 | `SERVER_NOT_FOUND` | Server doesn't exist |
| 404 | `CHARACTER_NOT_FOUND` | Character index entry doesn't exist |
| 409 | `ALREADY_REGISTERED` | Registration conflict (reserved for future strict-validation scenarios) |
| 409 | `SERVER_MANAGED_BY_CONFIG` | The registration target is owned by a server config file (see [server-config.md](server-config.md)) |
| 429 | `RATE_LIMITED` | The built-in token-bucket rate limiter tripped (longest path-prefix match × client IP, see [security.md](security.md)); APISIX gateway-layer limiting is an optional layer on top |
| 503 | `STORAGE_UNAVAILABLE` | Health check `/readyz` only: storage unreachable; data endpoints return 500 `INTERNAL` on storage errors |

### Versioning

All paths start with `/v1`. Breaking changes bump the major version and keep the old version for at least one release cycle.
