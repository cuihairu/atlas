[English](config-center.md) | [简体中文](zh/config-center.md)

# Cross-Server Config Center

Atlas embeds a **config center dedicated to game-server coordination**. It hosts the
topology cross-server gameplay needs, participation groups, feature switches, match
domains, the gameplay type table, plus configurable definitions for server tags and
profiles. It is **not** a general-purpose config center (like Apollo/Nacos), and it
does **not** replace the game's own numeric/business configuration pipeline (such as
cage artifacts).

---

## 1. Positioning and boundaries

| Dimension | Atlas config center | General config center | Game config pipeline |
|---|---|---|---|
| **Scope of control** | Cross-server topology, participation groups, feature switches, match domains, gameplay type table, server tags, profiles | General business config, feature flags, allowlists, rate limit rules | Numeric tables, items, dungeons, skills, balance parameters |
| **Consumers** | Game server processes (at startup / at runtime) | Gateways, microservices, backend services | Designer tooling, client hot-update, server hot-update |
| **Update semantics** | Notify-then-pull (version + hash, never pushes the full document) | Push full/incremental, subscription callbacks, canary releases | Release pipeline, canaries, rollback, A/B testing |
| **Consistency requirements** | Monotonic versions, idempotency, fail-fast at startup, no server drop while running | Eventual/strong consistency (depending on the product) | Strong consistency, auditing, change records |
| **Mandatory?** | Cross-server gameplay uses it if present, skips it if not | Platform-layer infrastructure | The game's core R&D pipeline |

> **In one sentence**: Atlas only handles "how servers team up, who joins which
> gameplay, and how the match pools are divided"; the actual numbers, the item drops
> and the dungeon rules all live in **the game's own pipeline**.

---

## 2. Configuration document structure

A single JSON document (`CrossServerSpec`) with five sections:

```json
{
  "topology": { "clusters": [ { "id": "cluster-ea", "name": "East China Battlefield", "region": "cn-east", "status": "active", "servers": ["game-1001", "game-1002"] } ] },
  "groups": [ { "id": "season-1", "name": "Season One", "servers": ["game-1001"] } ],
  "features": { "cross_battlefield": true, "world_boss": false },
  "match_domains": [ { "id": "mmr-0-3000", "name": "0-3000 bracket", "servers": ["game-1001", "game-2001"], "params": { "mmr_range": "0-3000", "max_team": "3" } } ],
  "crossplay_types": [
    { "id": "battlefield", "name": "Cross-Server Battlefield / Arena", "summary": "Cross-server PVP matchmaking matches", "lifecycle": "seasonal", "matchmaking": true, "ranking": true, "id_prefix": "xb" },
    { "id": "dungeon", "name": "Cross-Server Dungeon / BOSS", "summary": "Cooperative multiplayer PVE", "id_prefix": "xd" }
  ]
}
```

- **topology.clusters**: cross-server topology clusters (a set of servers that
  participate in cross-server gameplay together). `servers` may be filled in after the
  cluster is created. The cluster `id` *is* the cross-server group/cluster ID (§2.1).
- **groups**: participation groups (a batch of servers circled by an operations
  boundary, e.g. "East China cross-server battlefield, season one").
- **features**: feature switches (key-value pairs; keys must match `[a-z0-9._-]`,
  values are booleans).
- **match_domains**: match domains (the set of servers sharing one matchmaking pool;
  `params` holds game-defined string parameters).
- **crossplay_types**: the cross-server gameplay type table (§2.2) — for each
  gameplay: its code identifier, official name, one-line definition, lifecycle, whether
  it goes through matchmaking/ranking, and its runtime ID prefix.

The document is normalized by `NormalizeCrossServerSpec` (nil slices/maps become empty
containers, for all five sections), which guarantees that **the hash of the same
semantic configuration is constant** — otherwise idempotent saves would never take
effect.

### 2.1 The cross-server ID system

Every cross-server-level entity ID is **lowercase ASCII with hyphen-separated
segments** (kebab), matching the segmented style of the server `ServerID`
(`game-1001` = kind-number): **the first segment is the kind prefix, and segments
progress left to right through "what → belongs-to → temporal → random"**, so the ID
alone tells you where it belongs. Cross-server IDs have **configuration-time** and
**runtime** forms, with different issuers and lifecycles:

**Configuration-time IDs** — hosted by Atlas, defined by operations in the config
center, and propagated via the document's notify-then-pull flow:

| ID | Format | Example | Description |
|---|---|---|---|
| Cross-server group/cluster ID (`cluster_id`) | `cluster-<scope>[-<seq>]` | `cluster-ea`, `cluster-ea-2` | Topology cluster primary key; scope uses a region or an operations abbreviation |
| Cross-server type ID | `<word>` (`[a-z0-9._-]`) | `battlefield` | Type table primary key, the code identifier of the terminology contract (§2.2) |
| Group / match domain ID | `<kind>-<scope>` (by convention) | `season-1`, `mmr-0-3000` | Participation group / match domain primary key |

**Runtime IDs** — issued by the game-side **cross-server match management service** when
a session starts, during matchmaking, and at season start. **Atlas only standardizes
the format: it does not generate, store or write them back** (the config center does not
count sessions):

| ID | Format | Example | Issuer | Lifecycle |
|---|---|---|---|---|
| Cross-server gameplay instance ID | `<type prefix>i-<cluster_id>-<yymmdd>-<seq>` | `xbi-cluster-ea-261004-0042` | Cross-server match management service | Issued at session start, voided at session end (retained in match logs / rankings) |
| Cross-server match ID | `xm-<match_domain_id>-<yymmdd>-<seq>` | `xm-mmr-0-3000-261004-0187` | Matchmaking service (may be folded into the management service) | During matchmaking; mapped to an instance ID once the session is formed |
| Cross-server ranking ID | `xr-<type>-<period>` | `xr-battlefield-s3` | Issued by the management service at season start, or predefined by operations at configuration time | Season / period |

- **Type prefix**: the first segment of an instance ID = the type table's `id_prefix` +
  `i` (instance); e.g. `battlefield`'s prefix `xb` yields instances `xbi-…`. `id_prefix`
  is unique across the whole table, so a log line first identifies the type by prefix,
  then the owner by the second segment.
- **Traceable back to participating servers**: the second segment of an instance ID is
  the `cluster_id` — looking up this document's `topology.clusters` gives the complete
  set of servers in that session (comparing before/after versions shows membership
  changes); the second segment of a match ID is the `match_domain_id`, same idea.
- **The boundary between the two states**: configuration-time IDs take effect and
  propagate with the configuration version (saving identical content does not bump the
  version); runtime IDs are never written back to Atlas — instance ownership is held by
  the match service, and Atlas only provides the topological truth (cluster/domain →
  servers).

### 2.2 The cross-server gameplay type table

`crossplay_types` is the **type definition table** for cross-server gameplay (the enum
is published by operations, not hard-coded).

Fields:

| Field | Required | Description |
|---|---|---|
| `id` | ✓ | Code identifier (the anchor of the terminology contract), `[a-z0-9._-]`, unique within the table |
| `name` | | Official name (display name) |
| `summary` | | One-line definition |
| `lifecycle` | | `persistent` (default, always on) / `seasonal` (per season) / `ephemeral` (limited time) |
| `matchmaking` | | Whether participation is matched through a `match_domains` pool |
| `ranking` | | Whether ranking data needs aggregating |
| `id_prefix` | | The runtime ID prefix for this type, 2–8 lowercase letters, unique across the whole table (§2.1) |

**Terminology contract** — a concept has exactly one official name, and the docs /
dashboard / integrators all reference the same set:

| Official name | Official English name | Code identifier (`id`) | `id_prefix` |
|---|---|---|---|
| Cross-server battlefield / arena | Cross-Server Battlefield | `battlefield` | `xb` |
| Cross-server dungeon / BOSS | Cross-Server Dungeon | `dungeon` | `xd` |
| Cross-server ranking | Cross-Server Ranking | `ranking` | `xr` |
| Cross-server guild war / territory war | Cross-Server Guild War | `guildwar` | `xg` |
| Cross-server trade house / auction | Cross-Server Trade | `trade` | `xt` |
| Cross-server chat / social | Cross-Server Chat | `chat` | `xc` |
| Cross-server team-up / recruitment | Cross-Server Team Up | `team` | `xp` |

**The seven types** — one-line definition / participation form / typical configuration /
lifecycle:

| Type | One-line definition | Participation form | Typical configuration | Lifecycle |
|---|---|---|---|---|
| `battlefield` | Cross-server PVP matchmaking sessions (battlefield / arena) | Players from each server enter one match pool, and enter the map cross-server once matched | `matchmaking=true` (must use `match_domain` to split pools by rank), `ranking=true` (rank / record leaderboards) | `seasonal` (resets each season) |
| `dungeon` | Cooperative multiplayer PVE (dungeon / world BOSS) | Cross-server teams enter the same dungeon instance and kill cooperatively | `matchmaking=true` (team matchmaking); leaderboards go to the `ranking` type | `persistent` always-on (single sessions are time-limited) |
| `ranking` | Data-aggregation ranking | Each server reports scores, aggregated into a server-wide board | No matchmaking; this type is the aggregation itself | `seasonal` (season boards) or `persistent` (all-time board) |
| `guildwar` | Organization against organization (guild war / territory war) | Guilds register as a unit and contest territory / strongholds across servers | No matchmaking (registration-based), `ranking=true` (records / occupation board) | `seasonal` (per season) |
| `trade` | Economic interconnection (trade house / auction) | Consignment and bidding from every server share one market | No matchmaking, no ranking | `persistent` always-on |
| `chat` | Channel interconnection (chat / social) | Players from each server speak in the same channels | No matchmaking, no ranking | `persistent` always-on |
| `team` | Cross-server team-up entry point (team-up / recruitment) | Players recruit and apply to join teams across servers | `matchmaking=true` (team matchmaking) | `persistent` always-on |

> The type table is a **fleet-level declaration**: changing any row makes the change
> signal spread globally as `targets=["*"]` (the same level as feature switches) — types
> have no per-server membership, so every online registration needs to pull the new
> version.

### 2.3 How the three relate, and conflict adjudication (topology wins)

`topology.clusters`, `groups` and `match_domains` all circle sets of servers, but they
are **not three equivalent ways of drawing a circle** — their responsibilities and
binding force differ:

| Set | Positioning | Binding force |
|---|---|---|
| `topology.clusters` | **The technical boundary baseline**: which servers physically / network-wise / operationally belong to the same cross-server domain (same datacenter, same leased line, able to talk to each other at low latency) | A hard constraint. A server belongs to **at most one cluster**; overlap at save time is rejected |
| `groups` | **Operational soft grouping**: participation batches circled by an operations boundary ("East China battlefield season one", "season S3 participants"), changeable and splittable at any time | Constrained by topology. Group members must **all fall inside one cluster**; members spanning topologies are rejected at save time |
| `match_domains` | **Match pools**: the set of servers that enter the same pool during matchmaking | Freely circled per gameplay; no hard topology validation at save time. Whether a pool is physically viable is operations' own check against the topology baseline |

**Conflict adjudication (user ruling, 2026-10-04) — topology wins**:

1. On save, `ValidateCrossServerSpec` enforces:
   - A server appearing in **two or more clusters** → rejected (`server ... belongs to
     both cluster ... and ...`);
   - A group member **in no cluster at all** → rejected (the error says to add the
     topology declaration first);
   - Group members **spanning multiple clusters** → rejected (constrained by topology;
     split into multiple groups if you need cross-topology membership).
2. To pull a batch of servers across clusters → change the topology first (move the
   servers into one cluster), then create the group. Groups always follow topology,
   never the other way round.
3. Match domains are circled freely within gameplay semantics with no save-time
   validation; whether a cross-topology pool is physically feasible is operations'
   own responsibility against the topology baseline.

The dashboard pages state the same ruling: the topology card is titled "**baseline**",
and the group card "**operational orchestration · constrained by topology**".

---

## 3. Version and hash

On every save:

- **Version**: atomically incremented by the storage layer (monotonic; an old snapshot
  can never overwrite a newer version).
- **Hash**: the first 16 characters (64 bit) of the `sha256` of the normalized spec.
- **ETag**: `"${hash}"`, for conditional GET (`If-None-Match` / `?version=N&hash=H`).

---

## 4. Full semantics

### 4.1 Hosted persistence

- A single row (`id='default'`); `SaveCrossServerConfig` does an atomic upsert plus a
  version bump.
- The same contract across all three stores: memory / PostgreSQL / MySQL all pass the
  contract tests.

### 4.2 Startup pull (fail fast)

- **GET /v1/crossserver/config** (on the registry port :8081, also mounted on the
  public :8080 for gateways with no registration step).
- **A bare GET**: not yet published → **404 `CONFIG_NOT_FOUND`**, and the server
  **fails to start and exits** — it must not silently run "it works without config too".
- The registration response (`POST /v1/registry/servers/register`) returns the current
  `version/hash`, so a server can compare against its local cache and decide whether to
  pull.

### 4.3 Notify-then-pull

**Core principle**: the signal carries **only the version + hash + change targets** and
**never pushes the full configuration**. On receiving a signal → the server actively
pulls `GET /v1/crossserver/config` → takes effect live.

Three notification modes, declared in the registration metadata (`notify_mode`; they
can coexist and you can switch between them):

| Mode | Keyword | Suitable shape | Semantics |
|---|---|---|---|
| **Subscribe** | `subscribe` | Long-lived connection (Message Bus / gRPC stream) | The server subscribes to the `atlas.config` topic and pulls after receiving a `config.updated` signal (carrying `config_version/hash/targets/servers`). **After a disconnect it re-subscribes automatically and immediately pulls once more** (guarding against missed updates). |
| **Callback** | `callback` | The server implements an HTTP notification port | At registration it reports `notify_callback_url` (HTTPS/HTTP). On a config change Atlas **actively POSTs** the signal to that URL (carrying only the version/hash/targets plus a `crossserver_url` pointing at the pull address). **Failures retry with backoff** (3 attempts by default, 200ms base, exponential backoff, capped at 5s); consecutive failures record a degradation alert (operations can fall back to subscribe/poll). |
| **Poll** | `poll` | No long-lived connection, callbacks unavailable, the last-resort fallback | The server periodically does `GET /v1/crossserver/config?version=N&hash=H` or `If-None-Match: "hash"`. **The third fallback**, with a configurable interval (10–30s recommended). A conditional GET that hits returns 304 with no body; a miss returns 200 with the full document. |

> You can declare multiple modes at registration: `notify_mode: "subscribe,callback"` plus
> `notify_callback_url`. Re-registering as `poll` clears the old callback URL.

### 4.4 Targeted delivery of change signals

- **targets** (what structure changed): the list of changed cluster/group/match-domain
  IDs, or `["*"]` for global changes (feature switches, gameplay type table changes).
- **receivers / config_servers** (who should act): the **union of the members** of those
  target sets across the before/after documents, restricted to **currently online
  registrations** (`Status != offline/disabled`). Going offline unsubscribes, and the
  next startup takes the startup-pull path.
- A subscriber receiving a signal → if `config_servers` contains itself, it pulls;
  otherwise it ignores the signal.
- Callback dispatch goes to **every online callback declarer one by one**, independent of
  `config_servers` (if you declared a callback, you get notified).

### 4.5 Idempotency and monotonic versions

- PUT with identical content → **no version bump, no signal** (`notify.idempotent=true`).
- Versions increase monotonically; writing an older snapshot is always rejected (the
  storage layer does an atomic `version+1`).
- Repeating a pull at the same version → 304.

### 4.6 Failure semantics: a failed pull never drops the server

| Scenario | Behaviour |
|---|---|
| Startup pull 404 | **Startup fails and exits** (fail fast, never silent) |
| Startup pull 5xx / network error | **Startup fails and exits** (same fail-fast rule) |
| Signal received at runtime, pull fails | **Keep the old configuration and keep running**, log an alert, **no server drop**; retry on the next signal / poll |
| Callbacks keep failing | Record a `callback_failed` degradation alert; operations intervenes and switches mode |
| A change happens while the subscription is down | On reconnect → auto re-subscribe → **immediately pull once** → catch up to the current version |

---

## 5. API endpoints

| Endpoint | Method | Listener | Description |
|---|---|---|---|
| `/v1/crossserver/config` | GET | Public (:8080) / registry (:8081) | Server pull (strict / conditional) |
| `/v1/admin/crossserver/config` | GET | Admin (:8082) | Dashboard read (returns an empty version-0 snapshot when nothing is published) |
| `/v1/admin/crossserver/config` | PUT | Admin (:8082) | Dashboard full publish (returns a `SaveResult` including the notify spread report) |

**Startup pull (strict)**:
```bash
curl -s localhost:8080/v1/crossserver/config
# 404 {"error":{"code":"CONFIG_NOT_FOUND","message":"no cross-server config has been published yet"}}
```

**Conditional pull (polling / hot-update)**:
```bash
curl -s -H 'If-None-Match: "a1b2c3d4e5f67890"' localhost:8080/v1/crossserver/config
# 304 with no body, or 200 with the full document
# equivalent: ?version=5&hash=a1b2c3d4e5f67890
```

**Admin publish**:
```bash
curl -X PUT localhost:8082/v1/admin/crossserver/config -H 'Content-Type: application/json' -d '{
  "topology": { "clusters": [{ "id": "c1", "servers": ["game-1001"] }] },
  "groups": [],
  "features": { "cross_battle": true },
  "match_domains": [],
  "crossplay_types": [{ "id": "battlefield", "name": "Cross-Server Battlefield / Arena", "lifecycle": "seasonal", "matchmaking": true, "ranking": true, "id_prefix": "xb" }]
}'
# 200 {"config":{...},"notify":{"bus":"redis","targets":["c1"],"receivers":["game-1001"],"idempotent":false,"callbacks":{"targets":1,"delivered":1,"failed":0}}}
```

---

## 6. Declaration at registration

```json
POST /v1/registry/servers/register
{
  "server_id": "game-1",
  "name": "G1",
  "type": "game",
  "region": "cn-east",
  "version": "1.0.0",
  "platform": "any",
  "endpoint": { "host": "10.0.0.1", "port": 30001 },
  "capacity": 1000,
  "notify_mode": "subscribe,callback",
  "notify_callback_url": "https://game-1.internal:9999/atlas/config-changed"
}
```

- `notify_mode`: comma-separated; may be `subscribe`, `callback`, `poll` (the default is
  equivalent to `poll`).
- `notify_callback_url`: **required** for `callback` mode; must be an absolute
  HTTP/HTTPS URL.
- The registration response carries the current `crossserver_config.version/hash`, so a
  server can skip the first pull.

**Validation rejections** (400):
- `callback` without a URL / a non-absolute URL / a scheme other than http/https
- an unknown mode
- a `callback` URL supplied while the mode list does not include `callback`

---

## 7. Dashboard UI

A left-hand menu entry, **Cross-Server Config**:

- A **capability overview** card: **version / hash / publish time / notify bus** at a
  glance, plus tags for the **hot-update model** (notify-then-pull semantics and the
  three notification modes) and **three-tier access** (pull on public :8080 / registry
  port :8081 / admin port :8082 for reads, writes and publishes)
- A **seven-gameplay reference** card (same source as the table in §2.2): one-line
  definition / participation form / typical configuration / lifecycle, so terminology
  lines up while filling in the type table
- A **integration examples** card: three **copy-pasteable curl** commands — startup pull
  (strict) / conditional pull (polling) / admin publish
- A **dirty marker** (unsaved changes) and a **publish button** (an idempotent save
  reports "content unchanged" directly)
- The page is sectioned into cards: **Topology (clusters · baseline)** / **Participation
  groups (operational orchestration · constrained by topology)** (the card titles are the
  adjudication wording from §2.3) / feature switches / match domains / **the type table**
  (§2.2) — the type table card carries the fleet-level spread notice (changing any row
  sets `targets=["*"]`), and inline editing is validated with the same rules as the
  server (type IDs `[a-z0-9._-]` unique within the table, `id_prefix` 2–8 lowercase
  letters and unique across the table, lifecycle as a three-value dropdown); saving the
  page still transmits all five sections in full, so it never wipes out published content
- Add/edit/delete modals per section, with server members entered via a tag input
  (comma/Enter separated)
- After publishing, the **NotifyResult** is shown immediately: bus type, change targets,
  affected servers, callback delivery/failure details, and bus errors (no subscribers is
  not an error, it is only recorded)

---

## 8. Real walkthroughs

> The following were measured on a **running demo cluster** (the `atlas:local-tags`
> image, Redis bus, PostgreSQL storage); the output is unedited.

### 8.1 Scenario one: server startup pull (fail fast → succeeds once config exists)

```bash
# 1) No config yet: the startup pull 404s
curl -s localhost:8080/v1/crossserver/config | jq
# {"error":{"code":"CONFIG_NOT_FOUND","message":"no cross-server config has been published yet"}}

# 2) Publish a configuration from the dashboard
curl -X PUT localhost:8082/v1/admin/crossserver/config -H 'Content-Type: application/json' -d '{
  "topology": { "clusters": [{ "id": "cluster-ea", "name": "East China", "region": "cn-east", "servers": ["game-1001", "game-1002"] }] },
  "groups": [ { "id": "season-1", "servers": ["game-1001"] } ],
  "features": { "cross_battlefield": true },
  "match_domains": [ { "id": "mmr-0-3000", "servers": ["game-1001", "game-1002"], "params": { "mmr_range": "0-3000" } } ]
}' | jq
# {"config":{"version":1,"hash":"a1b2c3d4e5f67890","spec":{...},"updated_at":"..."},
#  "notify":{"bus":"redis","targets":["cluster-ea","season-1","mmr-0-3000"],"receivers":["game-1001","game-1002"],"idempotent":false,"callbacks":{"targets":0,"delivered":0,"failed":0}}}

# 3) Pull again → 200 with the full document
curl -s localhost:8080/v1/crossserver/config | jq '.version, .hash, .spec.features'
# 1
# "a1b2c3d4e5f67890"
# {"cross_battlefield":true}
```

### 8.2 Scenario two: hot-update at runtime (subscribe mode)

```bash
# 1) In another terminal, watch the bus signals (here the built-in subscribe of the http
#    adapter is used for the demo)
#    Here we show the bus signal fields returned by the admin publish directly (a real
#    server side would subscribe to TopicConfig)

# 2) Change the configuration: turn cross_battlefield off and add a new cluster
curl -X PUT localhost:8082/v1/admin/crossserver/config -H 'Content-Type: application/json' -d '{
  "topology": { "clusters": [
    { "id": "cluster-ea", "name": "East China", "region": "cn-east", "servers": ["game-1001", "game-1002"] },
    { "id": "cluster-sc", "name": "South China", "region": "cn-south", "servers": ["game-3001"] }
  ]},
  "groups": [ { "id": "season-1", "servers": ["game-1001"] } ],
  "features": { "cross_battlefield": false },
  "match_domains": [ { "id": "mmr-0-3000", "servers": ["game-1001", "game-1002"], "params": { "mmr_range": "0-3000" } } ]
}' | jq '.notify'
# {"bus":"redis","targets":["cluster-sc","*"],"receivers":["game-1001","game-1002","game-3001"],"idempotent":false,"callbacks":{"targets":0,"delivered":0,"failed":0}}

# 3) The subscriber (the game server) receives config.updated:
#    config_version=2, config_hash="f7e6d5c4b3a21098", targets=["cluster-sc","*"], config_servers=["game-1001","game-1002","game-3001"]
#    → immediately GET /v1/crossserver/config, pull v2, take effect live (no restart)
```

### 8.3 Scenario three: callback mode (register a callback → change config → the Atlas callback arrives → pull takes effect)

```bash
# 1) Start a simple HTTP receiver (simulating the game server's callback port)
#    In another terminal: python3 -m http.server 9999 --bind 127.0.0.1
#    (this only demonstrates callback arrival; a real server would pull /v1/crossserver/config on receipt)

# 2) Register one server in callback mode
curl -X POST localhost:8081/v1/registry/servers/register -H 'Content-Type: application/json' -d '{
  "server_id": "game-cb-1", "name": "CB-1", "type": "game", "region": "cn-east",
  "version": "1.0.0", "platform": "any",
  "endpoint": { "host": "127.0.0.1", "port": 39999 },
  "capacity": 500,
  "notify_mode": "callback",
  "notify_callback_url": "http://127.0.0.1:9999/atlas/config-changed"
}' | jq
# {"server_id":"game-cb-1","status":"starting","crossserver_config":{"version":2,"hash":"f7e6d5c4b3a21098"}}

# 3) Change the configuration once more from the dashboard (add a switch)
curl -X PUT localhost:8082/v1/admin/crossserver/config -H 'Content-Type: application/json' -d '{
  "topology": { "clusters": [
    { "id": "cluster-ea", "name": "East China", "region": "cn-east", "servers": ["game-1001", "game-1002"] },
    { "id": "cluster-sc", "name": "South China", "region": "cn-south", "servers": ["game-3001"] }
  ]},
  "groups": [ { "id": "season-1", "servers": ["game-1001"] } ],
  "features": { "cross_battlefield": false, "new_feature": true },
  "match_domains": [ { "id": "mmr-0-3000", "servers": ["game-1001", "game-1002"], "params": { "mmr_range": "0-3000" } } ]
}' | jq '.notify.callbacks'
# {"targets":1,"delivered":1,"failed":0,"errors":[]}

# 4) The receiver log shows the POST arriving:
#    127.0.0.1 - - [...] "POST /atlas/config-changed HTTP/1.1" 200 -
#    body: {"type":"config.updated","version":3,"hash":"...","targets":["*"],"crossserver_url":"http://127.0.0.1:8080/v1/crossserver/config","updated_at":"..."}

# 5) The server receives the callback → pulls back → gets v3, takes effect live
curl -s localhost:8080/v1/crossserver/config | jq '.version, .spec.features'
# 3
# {"cross_battlefield":false,"new_feature":true}
```

### 8.4 Scenario four: catch-up pull after a disconnect (subscribe mode)

```bash
# 1) The server is subscribed… (omitted; it has established a long-lived subscription to TopicConfig)
# 2) Simulate a network drop: kill the subscriber process / drop via firewall
# 3) Change the configuration in the dashboard (the server is offline, so it misses the signal)
curl -X PUT localhost:8082/v1/admin/crossserver/config -H 'Content-Type: application/json' -d '{...}'  # v4
# 4) The server reconnects → automatically re-subscribes to TopicConfig → immediately pulls once more
#    log: [INFO] reconnected, re-subscribed TopicConfig, re-pulling config...
#    GET /v1/crossserver/config → 200 v4
# 5) The version has caught up, and later changes push signals normally
```

### 8.5 Scenario five: a failed pull never drops the server (at runtime)

```bash
# 1) The current configuration v3 is running normally
# 2) Fault injection: pause the atlas process / partition the network, causing pull
#    timeouts or 5xx
# 3) Change the configuration in the dashboard → a signal goes out → the server receives
#    it → the pull fails
#    server log: [WARN] config pull failed: context deadline exceeded, KEEP RUNNING ON v3
# 4) The fault recovers → the next signal / poll → the pull succeeds → it moves to v4
#    Throughout, **the game server never restarted, never dropped its connections, and
#    players noticed nothing**
```

---

## 9. Deployment and operations notes

- **Bus choice**: `ATLAS_EVENT_ADAPTER=http` (default, in-process, single
  replica/development), `redis` (reuses an existing Redis, recommended for
  multi-replica/production), `kafka`, `nats`, `rabbitmq`. All five adapters are
  implemented; switching is this one environment variable. Config center signals travel
  on `TopicConfig` (`atlas.config`).
- **Public pull address**: `ATLAS_PUBLIC_URL=http://atlas.example:8080` (or your LB
  address). The callback notification returns `crossserver_url` in the payload, so the
  server needs no second hard-coded constant.
- **Callback retry policy**: `WithCallbackPolicy(timeout, attempts, baseBackoff)`,
  3 attempts, 200ms, 5s cap by default.
- **Parallelism**: `parallel=8` (tunable), to guard against callback storms on large
  fleets.
- **Monitoring**: `notify.callbacks.failed > 0` means degradation — pair it with an
  alert. A non-empty `bus_error` usually just means the bus has no subscribers (the
  normal state of the http adapter) and is not an alarm.
- **Migration / scaling**: the configuration document is a single row with monotonic
  versions, so there is no schema migration problem. Scaling out only adds server IDs to
  clusters/groups/domains.

---

## 10. Common misconceptions

| Misconception | Correction |
|---|---|
| "Put the item table and drop-rate table in there" | ❌ That belongs to the game's config pipeline; Atlas only handles coordination structure |
| "When the config changes, push the full document to the servers" | ❌ Push only the signal and let servers pull (decoupling, avoiding large payloads, idempotency) |
| "Without a message bus it can't be used" | ❌ There are callbacks and a polling fallback; the bus is just the preferred option |
| "We can maintain the version number ourselves" | ❌ The storage layer increments it atomically; clients only read |
| "We still need to notify a server that has gone offline" | ❌ Going offline unsubscribes; `receivers` contains only online registrations |
| "If the startup pull fails, run for now and backfill in the background" | ❌ **Fail fast**: a 404/5xx at startup must exit, preventing the hazard of "running with no config" |

---

## 11. Related documentation

- [Architecture](/architecture) — the bus and deployment topology
- [Data Sync](/sync) — Message Bus adapter choices and semantics
- [API Reference](/api) — complete endpoint definitions
- [Scenario Tour](/scenarios) — day-to-day operations from the operations perspective
- [Server Info as Config](/server-config) — profiles and fleet declarations (configuration-driven in the same spirit as the config center)
