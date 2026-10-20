
# Performance Design

Atlas's performance goals are dictated by where it sits: it is **on every player's
login path** (server selection, character lookup, announcement reads), so the read path
must be cheap; it also receives heartbeats and character writes from **the entire
fleet**, so the write path must scale horizontally. This page covers the
implementation-level trade-offs — each one maps to real code in the repo, and the
numbers come from [Benchmarks](/benchmarks) (reproduction commands are there;
`go test -bench` verifies locally).

## 1. Technology stack and choices

| Layer | Choice | Why |
| --- | --- | --- |
| Language | Go (standard library `net/http`, Go 1.22+ method routing) | Compiled, goroutines absorb fan-in, and the deployable is a single static binary |
| PostgreSQL driver | pgx v5 native (extended protocol + `pgxpool`) | Prepared statements + binary transfer avoid re-parsing every time; pooling built in |
| Redis | go-redis v9, hash + pipeline | The runtime view (heartbeats) is a natural KV, and `HSET`+`EXPIRE` is one pipeline in a single RTT |
| gRPC | google.golang.org/grpc (pinned to v1.86.0-dev) | Cross-language SDKs share one set of service definitions; pinning avoids supply-chain drift |
| Kafka | segmentio/kafka-go | Pure Go without cgo — the container image depends on no system libraries, and cross-builds stay painless |
| Dashboard | React + antd (static build) | The dashboard is not bundled into the service process, so the API process carries zero frontend overhead |

## 2. Hot paths, one by one

### Heartbeats (the king of write paths)

100,000 servers heartbeating every 10s = 10k QPS of sustained writes. The
implementation is a Redis hash plus a pipeline (`internal/store/redisstore`):

```go
pipe := s.client.Pipeline()
pipe.HSet(ctx, key, fields)      // status/players/load/last_seen
pipe.Expire(ctx, key, runtimeTTL) // 120s —— lost contact disappears on its own, no cleanup job
```

One network round trip writes every field. The TTL only removes an expired
runtime key; it does not mean the server's persisted status is offline. The
background health sweep remains responsible for moving status to `suspect` or
`offline` based on `last_seen_at` age (see [lifecycle](/lifecycle)).
Benchmark: about 200k QPS on one core (`BenchmarkHeartbeat` ~5.1µs/op; 64-server
concurrent fan-in `BenchmarkHeartbeatParallel` ~4.4µs/op — essentially flat once
batching absorbs the contention) — a real 10k QPS load costs 5% of one core. Most of
the cost comes from the v0.2 command queue's per-item receipt (the old direct-lock path
was 129ns; see §8.5 for the comparison), which buys strict per-entity FIFO, idempotent
deduplication and replayability. The bottleneck was never the heartbeat itself, but
Redis and the network.

### Discovery / server selection (the king of read paths)

Players pull the server list on login. The `internal/discovery` read path does three
things: filtering (region/realm/status), runtime merging (online count/load), and
outbound tag filtering (`model.PublicTags` — a slice filter in memory, essentially
zero-allocation overhead). The list is **quasi-real-time** data, so the gateway can
safely cache it with a 1–5s short TTL; `region` + `limit` keep a single response around
the 100-server scale.

Service-layer benchmark (memory store, no network): 42µs for a full list of 100
servers, 243µs for 1,000, with near-constant service-layer allocations (123 allocs/op
at fleet_1000, from the ids slice and result map containers of the batch read) — the
memory cost is negligible. **The Redis read path has been pipelined (P1, landed
2026-10-04)**: list merging no longer calls `GetRuntime` per server, and
`RuntimeStore.GetRuntimes` batch reads are completed with a single Redis **pipeline
Exec** — **N network round trips → 1–2** (1 for a single node; in Cluster mode, one per
slot batch). The estimated Redis backend cost for 1,000 servers drops from ~300ms to
the ~2ms range (the [benchmarks page](/benchmarks#_3-reading-the-numbers) has the
detailed estimates). Single-server detail `GetServer` stays a single read, which is
naturally the fewest round trips. **Decision-path work finished in the same batch
(2026-10-05)**: the health sweep and Routing recommendation/diagnostics runtime merges
also moved from per-server reads to `GetRuntimes` batch reads — sweeping the whole
fleet went from N round trips per round to 1–2. Unlike the list display's "degradation
tolerance", failures in these three paths always propagate (the decision layer will not
rank by unknown load, nor declare death on unknown state).

### Registration (a single upsert statement)

Registration is an idempotent upsert: a single PostgreSQL `INSERT … ON CONFLICT` /
MySQL `ON DUPLICATE KEY UPDATE` performs the existence check and the field overwrite,
so there is **no** SELECT-then-INSERT race window and no transaction round trip. It
costs about 2.7× a heartbeat (validation + upsert + initial heartbeat), but only
happens at startup/recovery; a cold recovery of a 10k fleet ≈ 75k QPS·cores
(`BenchmarkRegister` single-core throughput), which a single replica absorbs. The tags
column is deliberately omitted from the upsert — the dashboard owns it exclusively, and
game-server re-registration never overwrites it.

### Character directory (sharded index)

Character writes are the largest write traffic in the system (one arrives on every
character creation/level-up/login). The index is sharded **by account modulo**
(`internal/store/sharded`): all operations for one account route to the same shard,
which means exactly one shard hop; cross-account queries (dashboard search) fan out to
all shards and merge, costing linear in the shard count — an explicit trade-off,
documented in the package comment. Scaling out uses `cmd/atlas-reshard` for online
redistribution.

## 3. Concurrency model

- **Read-heavy, write-light → `sync.RWMutex` + copy-out** (`internal/store/memory`):
  all read methods return a **one-level-deep copy** (struct value copies, tags slices
  replaced wholesale), so callers get an object sharing nothing with the store's
  internals — the read side never traverses under lock and the write side cannot mutate
  data a reader is holding. A green `go test -race` is the baseline, not the goal.
- **Single-record in-place update → wholesale replacement**: `UpdateServerTags` never
  appends in place (it replaces the whole slice), so read/write races on a shared
  backing array are structurally impossible.
- **HTTP server timeouts are a safety valve**: all three listeners set
  `ReadTimeout/WriteTimeout 10s, IdleTimeout 60s` (`cmd/atlas/main.go`), so a slow
  client cannot occupy a goroutine.
- **Rate limiting uses "longest path prefix × client IP" token buckets**
  (`internal/httpapi/ratelimit.go`): a misbehaving game server exhausts only its own
  bucket and cannot starve others; the bucket count is capped, and beyond the cap new
  buckets are **rejected (fail closed)** — forged XFF headers cannot turn the rate
  limiter itself into a memory attack surface.

## 4. Pools and resource governance

Every pgx connection-pool parameter can be overridden by environment variable
(`ATLAS_PG_POOL_MAX_CONNS / MIN_CONNS / MAX_CONN_LIFETIME / MAX_CONN_IDLE_TIME /
HEALTH_CHECK_PERIOD`, `applyPGPoolOptions`); zero values fall back to library defaults
(scaled by CPU count), and `MinConns > MaxConns` converges automatically. A 1h
connection lifetime plus 1m health checks: dead connections after a deploy or failover
are recycled by the pool itself, with no process restart.

## 5. Queues and backpressure

Character creation/writes can be switched to **asynchronous buffering**: the write
endpoint returns `202` immediately and the event lands via the message bus
(`internal/event`) — the HTTP adapter is zero-copy in-process (semantically
exactly-once), Redis Streams reuses the existing Redis (at-least-once, with
`Directory.ApplyEvent` idempotent), and Kafka/NATS/RabbitMQ adapters serve deployments
that already run that middleware. One `event.Event` payload, five transports, and
switching is a single environment variable (`ATLAS_EVENT_ADAPTER`, see
[Data Sync](/sync)).

## 6. Baselines and guardrails

| Benchmark | Scenario | Single-core QPS |
| --- | --- | ---: |
| HeartbeatParallel | Concurrent heartbeat fan-in from 64 servers | ~228k × cores |
| GetServer | Single server detail (including runtime merge) | ~762k |
| ListServers/fleet_1000 | Full list of 1,000 servers (service layer) | ~4.1k |

Regression guardrails (investigate the diff beyond these): heartbeat > 1µs, register
> 10µs, fleet_1000 list > 1ms — these usually mean a new lock or a new allocation got
introduced. For the complete method, boundaries and reproduction commands see
[Benchmarks](/benchmarks); for consistency and failover in multi-replica deployments see
[High Availability](/ha).

## 7. Correctness over QPS

Absorbing 10k QPS of heartbeats is not hard (a single-core benchmark reaches about
200k QPS, see [Benchmarks](/benchmarks)). The hard part is staying correct while large
numbers of servers simultaneously start up / shut down / lose the network / rolling
restart / go into maintenance / migrate / release:

```text
never recommend a broken server  —— a half-dead server must never be recommended
never lose a character           —— the index projection loses no entries
never double-own                 —— one character never appears under two server_ids
never leave a stale endpoint     —— endpoints are removed/replaced promptly on shutdown / migration
migrations stay recoverable      —— any step can be rolled back / replayed
```

**Fault-injection drill checklist** (each scenario has an existing guarantee and a
verification method):

| Failure scenario | Existing guarantee (doc source) | Verification (automated location) |
| --- | --- | --- |
| Game server network partition | Heartbeat ages out → suspect → offline, no client-list flicker ([lifecycle.md](lifecycle.md) §3-§4) | ✅ Automated: `internal/health` sweep drills (OnlineToSuspect / SuspectToOffline / SuspectRecovered) + discovery visibility tests |
| Game server restart / heartbeat delay | Idempotent re-registration recovers; 3:1:6 pacing tolerates jitter (lifecycle §4.2) | ✅ Automated: `internal/store/storetest` re-registration contract (suspect/offline reset table) + redisstore miniredis contract |
| Duplicate registration / duplicate migration record | Registration is an idempotent upsert; migration endpoints create status records ([migration.md](migration.md) §1) | ✅ Automated: storetest idempotent upsert + `internal/admin` migration-record / rollback cases; no character relocation |
| Redis data loss | Profiles live in PG, TTL self-cleans, heartbeats refill ([data-model.md](data-model.md) §6) | ✅ Automated (2026-10-04): `internal/store/redisstore` `TestDataLossFlushAll_HeartbeatRefills` (miniredis FLUSHALL → missing means absent, heartbeats refill) |
| PG briefly unavailable | pgx pool + health checks automatically recycle dead connections (see §4) | Service-layer failure propagation ✅ (discovery backend degradation test, registry error paths); pool self-healing still belongs to deployment drills (ha.md) |
| Migration fails midway | Atlas stores migration status only; game servers move character data and Atlas does not orchestrate copy/cutover/cleanup (migration §1) | ✅ Automated: `internal/admin` tests rollback of the status record only; no character-relocation coverage |
| Atlas replica goes down | Stateless multi-replica + HAProxy heartbeat fan-in ([ha.md](ha.md)) | Deployment-level drill (ha.md; part of multi-replica orchestration, not in the unit suite) |

Implemented behavior in this matrix has `go test` coverage, apart from deployment-level
drills; character migration/cutover is not implemented and is outside the automated
guarantees. See [Benchmarks](/benchmarks) for the complete benchmark methodology.

## 8. v0.2 index and write-path design (landed 2026-10-08)

> This chapter matches the shipped implementation, and every claim carries a
> file:line. Before/after numbers come from replicas measured inside the same bench
> (§8.5 — the pre-change path is replicated inside the bench, no need to check out old
> commits). The SQL stores (postgres/mysql) do **not** introduce a queue — the
> command-based write path is an implementation choice of the memory store, and all
> three stores are pinned to the same behaviour by the storetest contract
> (`internal/store/storetest`).

### 8.1 Six kinds of inverted index (read path)

The **server directory** builds inverted indexes over six fields plus public tags
(`internal/store/memory/indexes.go:50`): `region / realm / shard / version / platform /
status` are each a `map[value]→set of IDs`, and tags only collect codes with
`Public:true` (projection `projectServer`, indexes.go:32) — internal tags never take
part in player-side filtering, consistent with the `model.PublicTags` semantics of the
discovery endpoint.

The **character directory** is indexed at the same level as the server directory
(indexes.go:213): four inverted indexes, `byAccount / byServer / byAccountServer /
byCharID`. The composite key `accountID:serverID:characterID` never changes, so
**updates never touch an index** and only insert/delete maintain it in O(1) (see the
comment at indexes.go:229); a global character-ID point lookup goes from a full scan
to O(1) (indexes.go:242).

**Index selection and cursors** (`ListServers`, memory.go:143): when an indexed filter
is present it takes the **smallest candidate bucket** and verifies the full filter
entry by entry — one dimension drives, the remaining fields double-check, which is
equivalent to a full intersection without the set arithmetic (candidates,
indexes.go:118); with no indexed filter at all it scans the table directly
(memory.go:160, materializing the full key slice is pure overhead). An unknown tag or
a value with no match returns an **empty, non-nil bucket: an empty result, not a
full scan** (contract assertion storetest/contract.go:486). Candidates are sorted by
ascending ID and the cursor is located by binary search (memory.go:160-174) —
pagination semantics are byte-for-byte identical to before, locked down by the contract
regression.

| Operation | Before | After |
| --- | --- | --- |
| Filtered list | O(N) full table, entry by entry | O(\|smallest bucket\|) candidates + O(C log C) sort (required by pagination semantics) |
| List characters by account/server | O(N) full scan | O(\|bucket\|) |
| Global character ID point lookup | O(N) full scan | O(1) |

**On the SQL side only tag filtering is added incrementally** (not one existing SQL
filter was touched): postgres `tags @> $n::jsonb` (postgres.go:179), mysql
`JSON_CONTAINS(tags, ?)`, and both predicates are constrained to `"public":true` — the
same AND semantics across all three stores are aligned by the storetest contract
(contract.go:461-490: public matches, AND, internal tags never match, unknown tags give
an empty set).

### 8.2 The command-based write queue

All hot writes are first wrapped in a **command envelope** `{kind, target, payload,
base_version, idempotency_key}` (an `instruction`, queue.go:130); public method
signatures and synchronous semantics are unchanged — callers block until **their own**
instruction commits (every write method is `return s.submit(ctx, ins).err`,
memory.go:128 et al), so read-after-write is still exact.

- **Lane separation**: 4 control lanes + 2 hot lanes, hashed per entity with FNV-1a
  (queue.go:231, 282-284) — the same entity always lands in the same lane, FIFO with a
  single writer, so races are structurally impossible. Configuration changes (control
  lanes) never queue behind heartbeats (hot lanes), and a hot-lane backlog cannot take
  down the control plane (`hot()`, queue.go:111).
- **Merge rules hang off the type** (`mergeable()`, queue.go:119): only overwrite-style
  writes — heartbeat / server-status / server-tags — may merge; within one batch the
  same entity keeps the newest and the rest are superseded. Instructions handed back to
  the caller (register / character upsert) and order-sensitive ones (delete, character
  patch) never merge, never drop and never duplicate. **Superseded envelopes still get
  a batch commit receipt** (the superseded notification in applyBatchN, from
  queue.go:350) — under last-writer-wins semantics this means "your write landed with
  the newer one"; callers are unaware of the merge. A regression test pins that
  concurrent merging never swallows a receipt (queue_test.go:281).
- **The merge window follows supply**: keep draining a non-empty lane and commit as
  soon as a drain comes up empty (`drainAvailable`, queue.go:317), batch cap 256. There
  is no timed window — an idle single instruction commits immediately (no latency
  floor), and under a storm the batch naturally fills to its cap. The merge window *is*
  the supply window: high-frequency writes to one entity are debounced inside a batch,
  and index maintenance happens once per batch.
- **Backpressure**: when a lane is full at depth 4096, that instruction is written
  **inline and synchronously** (the default branch of submit, queue.go:254) — the caller
  becomes its own single writer, so a write never fails and never queues forever.
- **Idempotency and replay**: an idempotency-key LRU of 1024 guards against retries and
  replays (queue.go:470-487); applied instructions land in a 1024-deep deep-copy capture
  ring — `ReplayInto` rebuilds write history in a sandbox, and `DryRun` pre-checks
  without touching production (queue.go:206, 221). Merge-semantics tests: merging loses
  nothing (enqueued = applied + merged + in-flight), same-entity ordering holds, and
  idempotent replay is consistent (queue_test.go:25, 122, 230).

### 8.3 Atomicity: data + index in one critical section (decided)

**What was actually adopted: one lock, one critical section.** Each batch commit
completes the data mutation and index maintenance together inside `mu + charMu`
(applyBatchN, queue.go:340-395) — index and data can never lead each other, and a
reader holding the corresponding read lock necessarily sees a consistent (data, index)
pair. The three lock domains and their order: mu (servers+serverIndex) → charMu
(characters+charIndex) → rtMu (runtimes), with no reverse acquisition path
(memory.go:10-14, 47-48).

**Rejected alternatives (recorded honestly)**: whole-snapshot + `atomic.Pointer`
publication. Every write would have to copy the entire table (O(N) write amplification,
unacceptable under a heartbeat storm on a 2000-server fleet), while readers already
have no write blocking under RWMutex read locks + copy-out (§3); the remaining benefit
is only "lock-free reads", at a disproportionate cost. Index maintenance is incremental
O(1) per item (indexes.go:74, 92 — snapshot the old projection before mutating, unhook /
attach only the changed keys, and recycle empty buckets), and **full rebuilds are
forbidden**. RCU was evaluated and rejected for the same reason; heartbeat batching has
already amortized the critical section of high-frequency writes (one critical section
per 256 instructions under a storm).

### 8.4 Version watermark

Each applied instruction increments a monotonic watermark (`s.watermark.Add(1)`,
queue.go:428). Visibility semantics: **the watermark a reader sees under the lock *is*
the committed state**; a caller's write returning means its own instruction was applied
(wait-for-receipt semantics, §8.2); a merged write returning means its successor was
applied (state ≥ its own write). base_version is stamped at enqueue time; existing
instruction types are all overwrite-and-swap by entity, so a mismatched watermark is
absorbed by recomputation — future compare-and-set style instructions will be rejected
here (errVersionConflict reserved, queue.go:62-67). The current watermark is exposed via
`QueueStats.Watermark` (store.go:318-323).

### 8.5 Before/after comparison (replicas measured in the same bench)

**Method**: the pre-change path is replicated inside the bench, without checking out
old commits (`internal/store/memory/bench_compare_test.go`) — `listServersFullScan` is
the core loop of the old `ListServers` (full table scan verifying filters including
tags, sorting, pagination), and `benchDirectHeartbeat` is the old write path's critical
section (build a snapshot, assign under lock); the character directory is replicated the
same way — `getCharacterByCharIDFullScan` / `listCharactersByAccountFullScan` /
`listCharactersByServerFullScan` are the pre-index full table scans (point lookup by
global ID, by account, by server). Both sides use the **same dataset, same machine, same
run**. Equivalence gate: before timing, every size × filter combination asserts that
the replica path and the indexed path return exactly the same ID sequence (in-bench
`benchAssertSameList` / `benchAssertSameChars`; any drift fails) — so the two columns of
every table are always the same query. Five rounds are run and the **minimum** is taken
(min-of-5: under long runs, clock throttling and scheduling noise, the minimum is the
closest estimate of the undisturbed truth; medians drift with accumulated heat — in
practice the b.N for index/status declined 5108→2635→1310 round over round).

**Environment**: Intel Core i9-10880H @ 2.30GHz (14 logical cores), Go 1.27.1 (the repo
is now pinned to 1.27.2; a patch-level difference does not affect this table),
linux/amd64.

Reproduction command:

```bash
go test ./internal/store/memory/ -run '^$' \
  -bench 'BenchmarkReadCompare|BenchmarkCharacterReadCompare|BenchmarkWriteCompare' \
  -benchmem -count 5
```

**Read path** (dataset: N servers, dimensions evenly distributed — region 4 buckets,
version 3 buckets, platform 4 buckets, status online 80%, public tags hot 25%; `limit=200`;
unit ns/op):

| Filter (selectivity) | Full scan N=1k | Indexed N=1k | Full scan N=10k | Indexed N=10k | 10k speedup |
| --- | ---: | ---: | ---: | ---: | ---: |
| No filter | 287 µs | 291 µs | 2.89 ms | 2.87 ms | 1.00x (same path) |
| region (25%) | 177 µs | 124 µs | 1.55 ms | 0.58 ms | **2.66x** |
| version (33%) | 200 µs | 141 µs | 1.78 ms | 0.85 ms | **2.10x** |
| platform (25%) | 174 µs | 137 µs | 1.93 ms | 0.68 ms | **2.85x** |
| status (80%) | 268 µs | 222 µs | 3.49 ms | 2.08 ms | 1.68x |
| tags hot (25%) | 173 µs | 122 µs | 1.43 ms | 0.64 ms | **2.22x** |
| tags unknown (0%) | 43 µs | 130 ns | 665 µs | 127 ns | **~5,200x** |

Allocations converge in the same shape: for region at N=10k, 169 KB → 109 KB per
operation (-35%), allocs 217 → 207; the fewer dimensions a filter has, the more it
saves. Three honest notes: **with no filter both columns follow the same path**
(candidates returns nil and it does the same table scan — a baseline sanity check);
**status yields the least** — the smallest bucket still covers 80% of the table, so the
index saves the row accesses and sort size of the other 20%; **an unknown tag is an
exponential short-circuit** — the empty non-nil bucket returns before sorting (§8.1
contract), whereas the full-scan side pays for a complete table traversal.

**Character directory** (dataset: N character index rows = N/10 accounts × 10
characters, 50 servers evenly distributed; per-server buckets are bounded at 200 with
the same semantics as the full-scan side; unit ns/op):

| Read path (10k bucket size) | Full scan N=1k | Indexed N=1k | Full scan N=10k | Indexed N=10k | 10k speedup |
| --- | ---: | ---: | ---: | ---: | ---: |
| Global character ID point lookup (1 row) | 71 µs | 1.6 µs | 3.05 ms | 5.3 µs | **~579x** |
| List characters by account (10 rows) | 253 µs | 49 µs | 5.50 ms | 30 µs | **~185x** |
| List characters by server (200 rows) | 413 µs | 81 µs | 10.3 ms | 0.88 ms | **11.6x** |

On the allocation side both paths are **byte-for-byte identical** (point lookup 144
B/1 alloc, by account 1.7 KB/17, by server 33 KB/211) — copy-out is the shared
allocation floor of both paths, and what the index saves is scan time, not allocations.
The speedup grows linearly with table size: 46x for a point lookup at 1k, ~579x at 10k;
the larger the bucket, the more it amortizes (a 200-row per-server bucket gives 11.6x).

**Write path** (ns/op, min-of-5; queue side = the public API waiting for a receipt per
instruction):

| Shape | Direct lock (replica of before) | Command queue (current) |
| --- | ---: | ---: |
| Single, no contention | 129 ns, 0 B/op | 2,839 ns, 1.4 KB/op, 5 allocs |
| Storm, 50 servers × 14 cores | 352 ns (lock contention slows it 2.7x) | 2,561 ns (batching absorbs it, barely degrades) |
| Storm, same target | — | 2,218 ns, **55% merge rate** (45% applied) |

Reading it (same yardstick as the previous batch, numbers measured with this method):
the channel handshake for per-item receipts is the queue's single-instruction cost
(129 ns → 2.8 µs, ~22x) — bought in exchange for strict per-entity FIFO, idempotent
dedup and replayability (§8.2). Under a storm the direct-lock path slows 2.7x from lock
contention while the queue barely feels contention: same-lane instructions batch and one
critical section commits a whole batch. In the same-target storm (the merging case), 55%
of writes are superseded inside the batch — superseded envelopes still take the batch
receipt, callers feel nothing, and the effective number of critical-section entries is
nearly halved. Production context is unchanged: the real heartbeat path has PG
double-write plus Redis HSET beyond the in-memory queue (milliseconds), so
microsecond-level differences are invisible; ~230k ops/s (storm, single server,
re-measured 2026-10-13) still leaves roughly two orders of magnitude of headroom against
10k fleet × 0.1Hz ≈ 1k ops/s.

Historical cross-validation: the same-shape numbers from the old-commit worktree method
(running the same bench file across two trees, before and after) are in the
[audit document appendix A](zh/审计-文档一致性.md) (N=2000 dataset: region 2.36x,
characters by account 14.6x, character ID point lookup 282x), consistent in magnitude
with this table — by-account 14.6x falls between this table's 1k tier (5.1x) and 10k tier
(185x), and the two methods independently reproduce each other.

### 8.6 Queue observability (landed: snapshot → Prometheus / admin endpoint / dashboard card)

**Data plane**: the queue snapshot `QueueStats` (store.go:318) — depth
(DepthControl/DepthHot), enqueue/merge/apply counters (Enqueued/Merged/Applied),
idempotent hits, backpressure-degradation count (BackpressureSync), the watermark,
the last 16 flush records, and the applied-by-kind distribution — exposed uniformly
through the `store.QueueStatusProvider` interface (store.go:358) and consumed/verified
by queue tests (the various assertions in queue_test.go). **One snapshot, three
readers** (the no-drift contract in store.go's header comment):

1. **Prometheus `atlas_store_queue_*` metric family** (`internal/metrics/queue.go`,
   registered at metrics.go:95): reads the snapshot at scrape time — same posture as
   `storeCollector`, zero write-path overhead. `depth_control` / `depth_hot` (gauges),
   `watermark`, `enqueued_total` / `applied_total` / `merged_total` /
   `idempotent_hits_total` / `backpressure_total` (counters), `applied_by_kind_total{kind}`,
   `flush_batch_size` / `flush_duration_seconds` (latest batch gauges) and
   `capture_entries`. Enqueue/merge rates are derived from the `*_total` counters via
   PromQL rather than a second set of counters. When SQL backends report
   `Enabled=false` through the forwarded snapshot, the entire family is not emitted —
   if the metric exists, there is a queue.
2. **`GET /v1/admin/indexqueue/status`** (httpapi/handlers.go:213 routing, :1832
   handler): returns the same snapshot verbatim. The decoration layer does not hide the
   snapshot — the fleet decorator and main's composite both pass `QueueStats` through
   (fleet/track.go:75, main.go:912); the SQL stores' empty snapshot
   (`Enabled=false`) and non-providers return `503 INDEX_QUEUE_DISABLED` rather than an
   empty 200. Verified with curl: memory backend returns 200 (watermark=enqueued=applied=4,
   genuine `recent_flushes` records, critical sections ~7-17µs); `/metrics` shows 14
   series at the same watermark.
3. **Dashboard "Storage Queue" card** (dashboard `QueueStatusCard.tsx`, mounted at
   Overview.tsx:93): pressure-light thresholds that change colour (depth ≥500 yellow /
   ≥2000 red, backpressure degradation ≥10 yellow / ≥100 red — severityColor,
   QueueStatusCard.tsx:16), version number (watermark), merge rate, the six counters,
   the latest three flush triples and the `recent_flushes` table, and applied-by-kind
   tags; 10s polling (:8), and a "not enabled" display on 503 (a semantic degradation
   for SQL backends, not an error).

Metric semantics are defined by this section and the `QueueStats` field comments;
contract tests pin all three surfaces: metrics scrape series (metrics/queue_test.go),
both the 200 and 503 admin endpoint paths (httpapi/coverage_test.go), and the queue-side
fields covered by the existing assertions in queue_test.go.

## 9. Production-shaped load and soak (design 2026-10-19; harness, drills and plateau checker delivered and locally verified; acceptance run pending a machine and a schedule)

Everything above and all of [Benchmarks](/benchmarks) measures the service layer: memory
store, one process, minutes-long runs, no network / TLS / JSON / storage I/O. Nothing
measured today answers the deployment question — end-to-end p95/p99, GC behavior, and
what a multi-hour run does to memory and file descriptors (the gap named by the
2026-10-18 review, Table 3, P2). This section fixes the missing first half — the
representative deployment, the load model and the acceptance criteria — so that running
it becomes a scheduling decision instead of a design exercise. Nothing here is a measured
claim; [Benchmarks](/benchmarks) stays the only baseline until a run exists.

**Representative deployment.** The run uses the HA topology the repo already ships, not a
bespoke rig: `deploy/docker-compose.ha.yaml` — 2 stateless Atlas replicas behind HAProxy
(public :8080, registry heartbeat fan-in :8081), PostgreSQL primary + replica, Redis
Sentinel ×3; event adapter fixed to redis streams so the newest bounded-delivery path
(delivery cap + `.dl` dead letters, [Data Sync](/sync) §4) is exercised. Broker adapters
differ too much in footprint to compare inside one run; other adapters are follow-ups,
not part of the first pass.

**Load model** — ratios fixed, absolute values scale with fleet size; scale points
100 / 1,000 / 5,000 servers match the estimate table in [Benchmarks](/benchmarks) §3 so
end-to-end numbers can be read against the service-layer bound:

| Stream | Shape |
| --- | --- |
| Heartbeats | N servers × SDK default cadence (all six SDKs ship automatic heartbeat) — the sustained write floor |
| Registration storms | a rolling-restart wave: a slice of the fleet re-registers inside one suspect window (30s default, `ATLAS_SUSPECT_AFTER`) |
| Reads | discovery list at fleet scale + single-server detail + routing recommendation (account-sticky) at gateway-realistic mix |
| Character events | created / login at a fixed ratio to fleet size — drives the projection write path and the §8 inverted-index update |
| Ops trickle | windows / announcements / audit-sink writes at a low rate, enough to keep the audit dispatcher and hourly retention sweep observable |

**Acceptance criteria** — the shape of "pass"; numbers get pinned by the first run and
then become guardrails (same philosophy as the pinned baselines in [Benchmarks](/benchmarks) §2):

- p95/p99 per endpoint class (heartbeat, register, list, detail, routing) under sustained
  load. Write paths already expose duration histograms on `:8082`
  (`atlas_registry_write_duration_seconds{op}`, `atlas_directory_write_duration_seconds`);
  read paths get percentiles from the OTel spans of the landed tracing phase, or one
  added read-side histogram — run prep, not a redesign.
- Fault recovery mid-run folds in the two drills §7 leaves at deployment level: kill one
  Atlas replica under HAProxy (failover time, no lost heartbeats) and a brief PG outage
  (pool self-healing, error propagation instead of hangs).
- Soak: ≥24h at the mid scale point. The pass condition is plateaus, not peaks —
  steady-state heap with no monotonic growth, stable goroutine and fd counts.

**Runtime telemetry (delivered 2026-10-19)**: `/metrics` is a custom registry
(`prometheus.NewRegistry()` in `internal/metrics/metrics.go`), so the standard Go and
process collectors are registered on it explicitly — GC pause, goroutine count,
`go_memstats_*`, RSS (`process_resident_memory_bytes`), open fds and CPU come off the
same scrape, no new dependency and no instrumentation on the hot paths (metrics tests
assert the series exist). That closes the last in-repo prerequisite of this section — the
representative deployment and the load driver named below are delivered too, so only the
run itself is blocked.

**The run waits on** scheduling only: (1) a designated machine — a 24h soak with PG +
Redis + HAProxy cannot land on shared demo infrastructure; (2) acceptance numbers are
outputs of the first run, not preconditions. The load-driver choice that used to be open
here was decided and delivered 2026-10-19 as `cmd/loadgen` on `sdk/go` (see "Driver
decision" below).

**Driver decision (decided 2026-10-19, patrol-branch delegation)**: the load driver is
the in-repo Go harness `cmd/loadgen`, driving everything through `sdk/go` — the same
client path real integrators use, so the SDK's serialization and heartbeat pacing are
part of the measured system. The §9 model is *cadence-shaped* (N servers each on their
own beat), and that is the deciding argument: RPS-shaped tools cannot express "1,000
servers heartbeating at SDK cadence" without standing the model on its head. Rejected:
k6 (RPS-shaped, separate runtime to install on the soak machine), vegeta (HTTP-only —
no gRPC leg), ghz (gRPC-only — no REST majority).

**Harness (delivered 2026-10-19)**: `cmd/loadgen` implements the five §9 streams —
fleet register + heartbeat with per-server jitter and drifting players, a 5:3:2
list/detail/recommend read mix at an aggregate rate, character writes into the
directory index, an admin stats trickle, and a rolling-restart storm that re-registers
the whole fleet inside one suspect window (`3 × heartbeat`, capped at 30s). Retries are
off (one honest attempt per measurement); in-flight saturation is reported as skips,
never silently retried away; per-flow p50/p95/p99 + last-error land in a text table and
a JSON report (`go test -race ./cmd/loadgen/`). Smoke-verified locally 2026-10-19:
100 servers, 90s, 100 read rps, storm every 30s — all five streams live, zero errors,
read mix within 1% of 5:3:2. Local numbers are not acceptance evidence; §9 acceptance
stays defined by the criteria above, run on the designated machine.

**Integration smoke on the representative topology (2026-10-20, not acceptance evidence)**:
the HA stack behind this section was repaired and exercised for real — as shipped it could
not be brought up at all: `build: ..` pointed at a Dockerfile that does not exist at the
repo root (now the explicit `build: {context: .., dockerfile: deployments/docker/Dockerfile}`
pair the compose in `deployments/docker/` uses), there was no one-shot `migrate` service
even though Atlas never auto-migrates a fresh PostgreSQL (now wired like the root compose,
`service_completed_successfully` before both replicas), and the image pins drifted behind
the other two surfaces (postgres 16→17, redis 7→8, matching `deploy/k8s/` and the root
compose). Then the §9 drill was folded in for real: 100-server fleet at 100 read rps plus
char writes and an admin trickle through HAProxy, with `atlas2` killed mid-run at t≈80s of
a 150s window — heartbeat p95 held at 28ms with a p99/max blip to 300/603ms as HAProxy
drained the dead backend, and no flow lost a call; registration, storm, list, recommend,
character write and ops all came back clean. The stack also carries a real lesson for the
driver itself, since that run produced exactly one `read-detail` 404: the read stream had
started racing the boot herd, so a client asked for a server that had not registered yet.
`cmd/loadgen` now parks its rate loops behind a fleet-ready gate (closed once every
virtual server has registered, bounded by the run ctx so a fleet that cannot boot still
tears down), which is what real clients do — reads address a fleet that already exists.
Post-fix rerun: 100 servers, 60s, all eight flows at zero errors. Numbers here are local
lab evidence that the harness and the topology agree; §9 acceptance still means the ≥24h
run on the designated machine.

**Drill runner (delivered 2026-10-20)**: `deploy/perf/drills.sh` automates the two
deployment-level drills §7 leaves out of the unit suite, so the 24h soak folds them in
without hand-typed docker commands mid-run — `replica-kill` (stop one Atlas replica,
wait, bring it back; `--service atlas1|atlas2` for coverage of both across a long run)
and `pg-outage` (stop the PostgreSQL primary, wait, bring it back). Both log ISO
timestamps at window open/close so a loadgen report can be correlated against the
injected window, and both fail loudly if the service does not come back healthy.
Live-verified against the repaired HA stack in a 300s / 100-server / 100-read-rps run
with both drills folded in (replica down 30s at t≈60s, PG down 20s at t≈180s):
perturbation is visible exactly inside the windows — heartbeat 500s ≈ observed rate ×
21s outage window, character-write errors ≈ rate × 16s, reads blipped ≈ 0.7s total
(served from Redis), storm/register/ops never errored — and recovery needed no manual
Atlas action: both replicas kept `RestartCount=0`, the surviving replica was never
restarted, and every flow resumed on its own once the stopped service reported healthy.
(Operator-visible side note: `compose start` re-runs the one-shot migrate service; the
migrations are idempotent and it exits 0, so this is harmless.)

**Mid-scale local pass (2026-10-20, 1,000 servers, not acceptance evidence)**: the first
1k-shaped run found a real harness defect, not a system one — a 1,000-way boot herd
saturates the 256-slot default in-flight cap, and a boot register that lost the
admission-control race was never retried, so 744 virtual servers never existed while
their heartbeats and detail reads 404'd for the whole run (register 256/1,000, heartbeat
5,336 errors, read-detail 3,462). Real game servers retry registration until the registry
accepts them, so now the boot register retries through skips with jittered backoff
(25–75ms, ctx-bounded), and the fleet-ready gate still opens the moment the last server
is truly registered — an intermediate `defer` version accidentally moved the gate to
teardown and was caught by the existing admission-control pin before any rerun. Post-fix
rerun (1,000 servers, 300s, 200 read rps, 20 char rps, ops 5s, storm 60s, seed 42,
replica-kill folded at t≈150s): all 1,000 registers land (7,144 admission skips absorbed
by retry), **zero errors across all eight flows including the drill window**, heartbeat
p50/p95 1/7ms, list 2/5ms, detail 1/3ms, recommend 4/8ms, char-write 3/8ms; the
replica-kill window shows up only as heartbeat p99 83ms / max 4.1s and list max 355ms,
with `RestartCount=0` on both replicas. The 5k point and the ≥24h soak stay on the
designated machine; everything here is local lab evidence that the harness now holds its
shape at the middle scale point.

**5k local pass and a second harness fix (2026-10-20, not acceptance evidence)**: at the
largest scale point the runs found the harness's own second scaling limit before the
system's — the read stream was issued by a single goroutine, which tops out at 1/latency:
a 500-rps target delivered ~70 rps at 5k (reads now run through a worker pool,
`-read-workers`, default 16 — tests-first, `TestEngineReadRateDeliveredUnderLatency` was
red at 70% undershoot before the pool). Three 5k runs on the dev box, all seeds fixed:
(a) single-issuer baseline: all eight flows zero errors with `replica-kill` on atlas1
folded in (coverage of both replicas across runs), `RestartCount=0`; (b) 16 workers plus
`pg-outage` (20s down, 7s recovery) folded mid-run: every error lands inside the injected
window — heartbeat 1,700 ≈ persist-path rate × window, reads 756, char-writes 89,
one storm wave partially lost to the window — then every flow resumes on its own, exactly
the error-propagation-not-hangs shape §9 asks of the drill; reads delivered 217 of 500
rps with the box visibly CPU-saturated (heartbeat p50 6→47ms once read in-flight
tripled); (c) 32 workers, no drill: 446 of 500 rps delivered (89%), heartbeat 510/s at
p50 11ms, zero errors apart from one teardown-instant decode error when the run's own
transport closes. Register retries absorbed 160–400k admission skips per boot across
these runs (all 5,000 always landed). Local numbers stay directional — the box saturates
before the service does — and the acceptance run sizes `-read-workers` to its rate plan
on the designated machine.

**Plateau checker (delivered 2026-10-20)**: `cmd/soakcheck` turns §9's "plateaus, not
peaks" clause into a mechanical verdict — the same move the drills got with
`deploy/perf/drills.sh`. Before it, judging a 24h soak meant eyeballing three Prometheus
series for a day. It samples each replica's `:8082/metrics` on a fixed interval
(`--interval 60s --duration 24h`) and prints the window as ISO timestamps at open and
close, so a loadgen report and a drill window line up against the same clock. Only the
three §9 series are kept (`go_memstats_heap_inuse_bytes`, `go_goroutines`,
`process_open_fds`), and the verdict is per replica per metric. The gate compares the
trailing quarter of the run against the leading quarter — ±20% by default, `--drift` — in
both directions: positive drift is a leak, negative drift is a restart or a crash that no
verdict can be trusted across. Extremes decide nothing, so GC waves and a rolling-restart
storm stay invisible, which is what "plateaus, not peaks" means; under 8 samples the
verdict reports "insufficient samples" instead of passing. Exit codes are 0/1/2 (all pass /
a series failed or is too thin to judge / nothing answered), so it can sit in a runbook or
a gate next to loadgen. `--out` records the samples as TSV and `--sample` re-verdicts that
file offline, so a soak's verdict stays auditable after the machine goes away.

Three things the live pass forced, all into the tool rather than around it:

- **Publish each replica's admin port.** As shipped, `:8082` is HAProxy-fronted and
  sticky, so one URL cannot produce two series — the verdict table needs one per replica.
- **`--warmup` drops the first N samples per series.** A fleet registering through sticky
  HAProxy lands on one replica first: in the live 100-server run atlas2's first sample was
  111 goroutines and the next sixteen were 21–23. Cold, that single point fails the run
  (−44.1% drift on the leading quarter) — and correctly so, because a run whose judged
  window opens inside the boot herd has no steady state to bless. `--warmup 1` over the
  same recorded samples: all six series plateau, exit 0.
- **A drop is not automatically a restart.** The live FAIL came from the boot transient
  above, not a crash (`RestartCount=0` on both replicas), so the note names the causes
  rather than asserting one.

Live-verified 2026-10-20 against the repaired HA stack, not acceptance evidence: 170s at a
10s interval under 100 servers / 100 read rps / 1 char rps / an ops trickle every 10s —
102 points, 17 per replica per metric, all eight flows at zero errors (register 100,
heartbeat 1,758, list 8,428, detail 5,082, recommend 3,450, char-write 169, ops 16), both
replicas `RestartCount=0`. Cold verdict FAIL on the boot-transient series; warmed verdict
PASS, with heap +4.2% / +5.1%, goroutines −2.5% / +1.1% and open fds +0.4% / +0.0% across
atlas1 / atlas2. The 24h run on the designated machine still stands as the acceptance
event; this only means the last piece of judging machinery is built and exercised.
