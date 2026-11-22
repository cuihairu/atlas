# Player Presence

A character's online state is one of the few facts a lobby / friend list / guild roster
needs that the game server already knows. Atlas records it so any component — not just
the server that saw the login — can answer "is this character online, and where?"

The presence projection is a small state machine over two rows:

- `character_index.presence_status` (`unknown` / `online` / `offline`) plus `last_login_at` / `last_logout_at`.
- One row in `player_sessions` per login, closed on logout.

A character is **online ⟺ it has exactly one open session**. Every transition below
preserves that invariant by construction: login opens the session and flips the
character online in a single critical section / transaction, logout closes it and
flips it offline in one.

## 1. Data model

| Field | Type | Meaning |
| --- | --- | --- |
| `PresenceStatus` | `unknown` \| `online` \| `offline` | Projected presence; `unknown` until the first login |
| `PlayerSession.SessionID` | string | Game-server generated session id (unique per login) |
| `PlayerSession.AccountID` | int64 | Owning account |
| `PlayerSession.CharacterID` | int64 | The character |
| `PlayerSession.ServerID` | string | The server the character logged into |
| `PlayerSession.StartedAt` | time | Login time |
| `PlayerSession.EndedAt` | *time | Logout time; nil while the session is open |
| `PlayerSession.EndReason` | `normal` \| `timeout` \| `kicked` \| `crash` | Why the session ended |

The `EndReason` set is fixed: a player quit (`normal`), the server timed the session
out (`timeout`), an operator kicked the character (`kicked`), or the server crashed /
lost the session (`crash`). Atlas closes a session as `crash` when it heals a stray
open session — for example the session a moved or deleted character left behind.

## 2. Report channel

Game servers report presence over the **registry trust zone**, the same channel as
register / heartbeat / unregister. It inherits `RegistryAuth`: a service token
(`Authorization: Bearer <token>` or `X-Atlas-Token`) and an optional IP whitelist.

`POST /v1/registry/servers/{id}/presence`

The path `{id}` is the **reporting server** — a game server may only report presence
for its own server, so `server_id` is deliberately not accepted in the body. The
server must be registered (404 otherwise).

**Request**

```json
{
  "action": "login",
  "account_id": 10001,
  "character_id": 823712,
  "session_id": "sess-9f2c",
  "timestamp": "2026-11-22T12:30:00Z"
}
```

For logout, add the reason:

```json
{
  "action": "logout",
  "account_id": 10001,
  "character_id": 823712,
  "session_id": "sess-9f2c",
  "end_reason": "normal",
  "timestamp": "2026-11-22T12:40:00Z"
}
```

- `action` — `login` or `logout` (required).
- `account_id` / `character_id` — the reporting pair (required, > 0).
- `session_id` — required for `login`; on `logout` it names the session being closed
  (empty means "close whatever is open").
- `end_reason` — optional on `logout`, one of the four; ignored on `login`.
- `timestamp` — producer occurrence time; omitted means "now".

**Response** `200 OK` (synchronous adapter) / `202 Accepted` (asynchronous)

```json
{
  "server_id": "game-1001",
  "character_id": 823712,
  "presence_status": "online",
  "presence": {
    "presence_status": "online",
    "active_session": {
      "session_id": "sess-9f2c",
      "started_at": "2026-11-22T12:30:00Z"
    }
  }
}
```

`202` means **queued**, not accepted by the state machine: the checks that depend on
current state (a conflicting login, a logout with no open session) run at consumption
time and surface as handler failures — retried and dead-lettered per the adapter's
delivery semantics ([sync.md §4](sync.md#_4-message-bus-selection)), not as a `409`.

**Errors**

| Code | HTTP | Case |
| --- | --- | --- |
| `INVALID_ARGUMENT` | 400 | Bad action, missing `session_id` on login, invalid `end_reason` |
| `SERVER_NOT_FOUND` | 404 | The path server is not registered |
| `CHARACTER_NOT_FOUND` | 404 | The character is not in the index |
| `PRESENCE_CONFLICT` | 409 | Illegal transition (login while online in another session, logout of a non-open session) |
| `MISSING_TOKEN` / `INVALID_TOKEN` | 401 | Missing / bad service token |
| `IP_NOT_ALLOWED` | 403 | Client IP outside the whitelist |

## 3. State machine

Login and logout are the **only** operations that move presence. A generic
`character.updated` patch, a `character.created` upsert, or a `character.moved`
re-home never touch `presence_status`.

| Current | Report | Result |
| --- | --- | --- |
| `unknown` | login | Open session, flip `online`, stamp `last_login_at` |
| `unknown` | logout | **`INVALID_ARGUMENT`** — no open session to close |
| `online` | login, same `session_id` | **Idempotent no-op** (at-least-once transports re-deliver) |
| `online` | login, different `session_id` | **`PRESENCE_CONFLICT`** — close the old session first (`logout` with `crash`) |
| `online` | logout, open session | Close session, flip `offline`, stamp `last_logout_at` |
| `online` | logout, non-open `session_id` | **`PRESENCE_CONFLICT`** |
| `offline` | login | Open a fresh session, flip `online` |
| `offline` | logout | **Idempotent no-op** (duplicate delivery) |

**Crash healing.** If the projection is ever left with an open session but an offline
character — a move or delete is the usual culprit — the next `login` closes the stray
session as `crash` before opening the new one, so the invariant is restored and the
history stays self-consistent. A `moved` event resets the destination row to
`unknown`; presence never travels with the character.

## 4. Query API

Reads go through the directory surface.

### GET /v1/directory/characters/{character_id}/presence

角色在线查询 — the character's presence state and, when online, its active session.

**Response** `200 OK`

```json
{
  "presence_status": "online",
  "active_session": {
    "session_id": "sess-9f2c",
    "account_id": 10001,
    "character_id": 823712,
    "server_id": "game-1001",
    "started_at": "2026-11-22T12:30:00Z"
  },
  "character": {
    "account_id": 10001,
    "server_id": "game-1001",
    "character_id": 823712,
    "name": "Jian Wuchen",
    "presence_status": "online"
  }
}
```

`active_session` is omitted when the character is offline. `404` when the character is
not in the index.

### GET /v1/directory/servers/{server_id}/presence

某服在线名单与计数 — the online roster of a server with a live count and cursor
pagination.

**Query**: `?limit=` (default 50, max 200), `?cursor=` (resume token).

**Response** `200 OK`

```json
{
  "server_id": "game-1001",
  "online_count": 2,
  "characters": [
    { "character_id": 823712, "name": "Jian Wuchen", "presence_status": "online" },
    { "character_id": 823713, "name": "Wuchen", "presence_status": "online" }
  ]
}
```

`next_cursor` is the last returned `character_id` — a resume token, populated even on
the final page. Reading past it returns an empty roster.

### Session history

`GET /v1/admin/...` does not expose sessions; they are reachable through the store
contract and the directory service for operators and future admin surfaces.

## 5. Storage notes

- **Migration** `0010` (PostgreSQL) and `0010_player_presence_mysql` (MySQL) add
  `presence_status` / `last_logout_at` to `character_index` and create `player_sessions`
  with an index on `(server_id, presence_status)` for the roster query.
- The store contract (`storetest`) pins the state machine across the memory,
  PostgreSQL and MySQL implementations — the idempotency and illegal-transition rules
  are identical in all three.
- In the memory store, login/logout are control-lane instructions that are **never
  coalesced** (a state machine is not last-writer-wins) and ride the same capture ring
  as every other write, so `ReplayInto` reproduces the session history.
