[English](scenarios.en.md) | [中文](scenarios.md)

# Scenario Tour

Use Atlas through the most common day of game operations. Each section = scenario description + real UI screenshots + concrete operation paths. All screenshots come from a real running demo cluster; for a first overall impression, go back to the [homepage carousel](/).

## 1. The page ops checks first every day {#scenario-overview}

**Scenario**: a morning glance at the global picture before servers open — how many servers online, how many players, capacity watermark, status distribution. Any number looking off (online count dropped, some region all suspect) becomes today's first task.

![Overview page — online servers/players/capacity cards plus status and region distribution charts](/screenshots/overview.png)

**Operation path**

- Dashboard: `Overview` — "online servers / total capacity / online players" cards + status distribution, region distribution, and 24-hour player trend.
- API: `GET /v1/admin/stats` (admin port :8082) — every number on the page comes from this one real endpoint:

```bash
curl -s localhost:8082/v1/admin/stats | jq
# "online_servers": 8, "total_players": 4628 ← 玩家数由 Redis 心跳视图实时合并
```

**Further reading**: [Architecture Design](/architecture.en) · [Benchmarks](/benchmarks)

## 2. Tagging servers {#scenario-tags}

**Scenario**: new-server launches highlight "New Server", sale-event servers carry "Hot", full ones get marked "Packed", and ones that should stop taking imports get "No Registration". Some tags face players (badges), some face ops only (like internal notes). **"No Registration" is not just display — the registration / character-creation APIs actually reject with a clear error.**

![Server list — the tag column renders badges: Hot (hot red) / New Server (new-server green) / Packed (warning yellow)](/screenshots/servers.png)

![Server detail — tag management panel: preset dropdown + custom (code/label/style tier/public toggle), tags split into public/internal](/screenshots/server-detail-tags.png)

**Operation path** (dashboard: `Servers` → open a server → the "Tag Management" card)

1. Pick a preset (with its own label and style tier), or fill in custom: tag code + display label + style tier (hot red / new-server green / warning yellow / info blue / neutral) + whether it is public.
2. Public tags immediately appear in the player-visible server list/details (badges); internal tags exist only in the dashboard.
3. "No Registration" takes effect on `POST /v1/directory/characters` and gRPC character creation; the "Under Maintenance" tag carries the same weight as the server-side maintenance state, and can be configured via `ATLAS_MAINTENANCE_ENFORCE=block|warn` to reject, or to pass through with a warning header.

Equivalent API operations and one real walkthrough (apply "Hot + No Registration" → badge appears on the player side → character creation rejected → remove and recover):

```bash
# 打标记（管理口 :8082）
curl -X POST localhost:8082/v1/admin/servers/game-1001/tags \
     -H 'Content-Type: application/json' -d '{"code":"hot"}'
curl -X POST localhost:8082/v1/admin/servers/game-1001/tags \
     -H 'Content-Type: application/json' -d '{"code":"no_register"}'

# 玩家侧（公网 :8080）只见对外标记 → 角标数据源
curl -s localhost:8080/v1/discovery/servers/game-1001 | jq .tags
# [{"code":"hot","label":"火热","tier":"hot","public":true},
#  {"code":"no_register","label":"禁止注册","tier":"warning","public":true}]

# 创角被拒，错误明确
curl -X POST localhost:8080/v1/directory/characters -H 'Content-Type: application/json' \
     -d '{"account_id":424242,"server_id":"game-1001","character_id":90001,"name":"demo"}'
# HTTP 403 {"error":{"code":"REGISTRATION_FORBIDDEN",
#        "message":"server game-1001 does not accept new characters (禁止注册)"}}

# 摘除 → 恢复注册
curl -X DELETE localhost:8082/v1/admin/servers/game-1001/tags/no_register
# 之后同一笔创角 → HTTP 201
```

Tags are maintained exclusively by the dashboard: game server re-registration / heartbeats **never** overwrite or clear them. See [Concepts § Server Tags](/concepts.en#服务器标记).

## 3. Players finding characters / support looking up accounts {#scenario-characters}

**Scenario**: a player asks "what was my character's name on server 3 again"; support handling an appeal first verifies the characters under an account. The cross-server character index pulls everything in one call, filterable by name/server/level.

![Character search — combined filters by character name/server/level, cross-server results](/screenshots/characters.png)

**Operation path**

- Dashboard: `Characters` — combined filtering by character name / server / level range.
- API: `GET /v1/admin/characters/search?q=名字&server_id=…&min_level=…` (admin port); the player side's own list goes through `GET /v1/directory/accounts/{id}/characters` (public port).
- The only entry point for character writes is the game server write endpoint (optionally buffered async via the message bus, landing at-least-once and idempotent) — see [Data Sync](/sync.en).

## 4. Server merge / transfer {#scenario-migration}

**Scenario**: two servers have grown thin and need merging. Ops orchestrates it along the "copy first, switch next, clean up last" blueprint; Atlas records the migration and provides failure rollback.

![Migration management — create migrations, state transitions, failure rollback](/screenshots/migrations.png)

**Operation path**

- Dashboard: `Migrations` — fill in source/target servers to create a migration; the page refreshes state in real time and failures can be rolled back in one click.
- API: `POST /v1/admin/migrations` (body specifies the `source_servers` array / `target_server`), `POST /v1/admin/migrations/{id}/rollback`.
- Details in [Server Merge / Transfer / Migration](/migration).

## 5. Server opening and maintenance announcements {#scenario-ops}

**Scenario**: servers go down for maintenance at 2 AM tonight — create the maintenance window in advance, the server automatically enters maintenance when it starts and automatically recovers when it ends, and the maintenance announcement is posted automatically over the same window; for an emergency fault, one API call immediately publishes a fleet-wide announcement. Player clients see it on login.

```bash
# 计划维护窗口（到点自动进维护/恢复；announce 默认 true 会自动挂出覆盖窗口的公告）
curl -X POST localhost:8082/v1/admin/servers/game-1001/maintenance-window -H 'Content-Type: application/json' \
     -d '{"start_at":"2026-10-03T02:00:00+08:00","end_at":"2026-10-03T04:00:00+08:00"}'

# 全服公告（starts_at/ends_at 必填，end 必须晚于 start）
curl -X POST localhost:8082/v1/admin/announcements -H 'Content-Type: application/json' \
     -d '{"title":"今晚例行维护","body":"02:00-04:00","level":"info","starts_at":"2026-10-03T01:30:00+08:00","ends_at":"2026-10-03T04:30:00+08:00"}'
```

Server-side emergency maintenance / drain / disable lives in the per-row action buttons on the `Servers` page (enter maintenance, drain, disable, enable) — the action column in the [Scenario 2](#scenario-tags) screenshot. Details in [Announcements and Planned Maintenance](/operations).

## 6. Who tells me about a midnight disconnection {#scenario-alerts}

**Scenario**: a server's heartbeats stop. Health monitoring runs `suspect → offline` two-stage failure detection and fires an alert — it will not instantly kick a lagging server offline, nor let a truly dead one keep receiving players.

**Operation path**: no manual polling — once the alert channel is configured (webhook), events push automatically; afterwards replay who touched which server and when in the `GET /v1/admin/audit` audit stream. Details in [Server Lifecycle § Health Alerting](/lifecycle.en#_4-1-健康警报-v0-1-15-交付).

## 7. Multi-region expansion {#scenario-realms}

**Scenario**: one realm (Realm) each for North China and South China, sharded (Shard) inside each realm to scale horizontally. Server membership is declared at registration; the player side filters by realm.

**Operation path**: `Realms`/`Shards` management is on the admin port (`POST /v1/admin/realms`, `POST /v1/admin/shards`); the player side uses `GET /v1/discovery/servers?realm=realm-cn`. Details in [Realm and Shard](/realms-shards.en).

## 8. Cross-server config hot updates {#scenario-crossserver}

**Scenario**: cross-server gameplay opens a new season — ops changes the config once, and servers in all three forms (poll / callback / subscribe) each get the signal, pull, and hot-apply, with no restarts anywhere. If one loses connectivity or another's pull fails, the old config keeps running and catches up automatically after recovery.

**Operation path**: edit on the dashboard `跨服配置` page and click "Publish", or the equivalent API (`atlas-crossagent` is a full receiver for all three modes; `go run ./cmd/atlas-crossagent -h` shows the flags):

```bash
# 发布全文（PUT，替换）→ version 8 → 9
curl -X PUT localhost:8082/v1/admin/crossserver/config \
  -H 'Content-Type: application/json' -d @spec.json
# {"config":{"version":9,"hash":"3768c1708e690107"},
#  "notify":{"targets":["*","season-1"],"receivers":["*"],
#   "bus":"redis","callbacks":{"targets":1,"delivered":1,"failed":0}}}
```

Real walkthrough transcript (demo stack, `ATLAS_EVENT_ADAPTER=redis`):

1. **Startup pull** — three agents register (`poll` / `callback` / `subscribe`); the register response carries `config_version: 8`, and the startup pull adopts it directly:
   `CONFIG APPLIED (hot) version=8 hash=b0c7e5cc595bf888 clusters=3 groups=2 features_on=1`
2. **Config change while running** — publish v9 (`cross_arena` enabled + groups gain members); all three hot-apply on their own:
   poll (5s interval) `config_version: 9`; callback pulls as soon as it receives `POST /notify`; subscribe pulls as soon as it receives the bus `config.updated`.
3. **Failed pull, old config keeps running** — enable fault injection on the poll instance (`POST /ctl/failpull {"on":true}`) and publish v10:
   `{"config_version":9,"pull_failing":true,"pull_failures":3}` — version does not move, process does not exit; after the injection is removed, the next poll automatically catches up to v10.
4. **Catch-up pull after disconnect** — send the subscribe instance `POST /ctl/bus-off` (subscription dropped); while v11 is published (only matching-domain params changed, `receivers` targeted as `["game-1001","game-9001"]`) it stays on v10; `POST /ctl/bus-reset` rebuilds the subscription and it catches up: `config_version: 11`.
5. **Callback arrives** — publish v12 (adds a walkthrough group); Atlas proactively `POST`s the callback URL (carrying only version/signal), agent log:
   `callback signal received type=config.updated version=12` →
   `CONFIG APPLIED (hot) version=12 … groups=3`; publish response `callbacks:{targets:2, delivered:2}`.

Semantics and boundaries in [Config Center](/config-center).

---

The UI above comes from [dashboard/](https://github.com/cuihairu/atlas/tree/main/dashboard) (React + antd). To run it locally: `docker compose up -d` brings up the full Atlas stack (atlas, postgres, redis, plus a one-shot schema migration job — four services in total), and `cd dashboard && npm run dev` starts the dashboard (the dev proxy auto-routes between the public and admin ports).
