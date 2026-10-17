
# Source Code Review, Round Two (2026-10-09)

> Background: an external review (based on public material) raised five questions to
> verify and recommended "a second review pass against the source code". This page
> records that pass: three parallel readers walked `internal/registry` /
> `internal/health` / `internal/store` / `internal/directory` / `internal/event` /
> `internal/routing` / authn and the startup surface, answering each review question
> with file:line source evidence and classifying the outcome as **pinned** (asserted by
> tests) / **documented honestly** (a known trade-off consistent between docs and code)
> / **gap fixed** (fixed in this round) / **deferred** (real but not addressed this
> round). The source code is the truth; the docs are not read back.

## 1. Review question one: consistency between runtime and persisted state

| # | Review question | Source conclusion | Evidence | Disposition |
| --- | --- | --- | --- | --- |
| 1.1 | After a Redis loss, can you tell "never heartbeated" from "data was wiped"? | No, and it isn't needed: absence *is* absence, liveness rests on the PG profile and the age of `last_seen_at`, not on whether a Redis key survives; the next heartbeat simply refills that server | redisstore_test.go:51-81 (FLUSHALL → missing/absent → refill); monitor.go:134-147 (no runtime → judge dead by `CreatedAt` age) | Pinned |
| 1.2 | When the PG write succeeds but the Redis update fails, what state is exposed? | Heartbeat path: PG read → Redis write (a failure propagates as 500, no retry and no outbox) → then the PG write, conditionally (only for the starting→online promotion); the registration path is the opposite: PG first, Redis best-effort (a failure only logs a warning and still returns 201), and the missed first beat is caught by the sweep judging dead by `CreatedAt` age | registry.go:211 / :222-228 / :232-237 / :178-190; handlers.go:328 | Documented honestly (failure semantics are clear and self-healing) |
| 1.3 | With stale load left in Redis, will Routing recommend the wrong server? | The 120s TTL is the only freshness gate (Routing never reads `LastSeenAt`, 0 grep hits); once the key expires, candidates **missing a snapshot are ranked using the profile values captured at registration** (an archive Load=0 ranks first), and the code comment states this trade-off plainly | redis.go:21/:63; service.go:378-383 (missing ones skipped, profile values take effect) | **Gap fixed** (visibility): Diagnose gained a `live_runtime` field that flags profile-ranked candidates truthfully (routing service.go `ServerVerdict.LiveRuntime` + a pinned test) |
| 1.4 | Do multiple replicas sweeping at once cause duplicate state changes or duplicate alerts? | There is no coordination at all (0 grep hits for leader/election/redlock): a state transition is a blind same-value write, so repeating it converges on the same final value (idempotency holds) and only metrics/logs are double-counted; **alerts latch per replica, and both replicas fire the webhook** — ha.md documents this honestly (deduplicate on the webhook side, or set the threshold on a single replica); a failed latching is not cleared (a failed webhook delivery is only logged and is not resent until recovery crosses the threshold again) | monitor.go:79-96/:152-175/:187-196; alert.go:40-46/:83-92; ha.md:47/:54-56 | Documented honestly (duplicate alerts) + deferred (D9) |
| 1.5 | Is there a race window between re-registration and old heartbeats / the sweep? | Yes, recorded honestly: ① the HTTP layer does no per-server serialization; ② the memory queue's per-entity FIFO only holds **within one kind** — register goes to a control lane while heartbeat goes to a hot lane (queue.go:111 `hot()` only for opHeartbeat), so ordering can invert across kinds; ③ the sweep's blind write from a stale snapshot can knock a just-re-registered server to suspect/offline (`UpdateServerStatus` has no status precondition, postgres.go:214). Self-healing path: the next real heartbeat resets the age and the next sweep round restores the state | queue.go:111/:280-285; postgres.go:93/:214; registry.go:232-237 | Deferred (D1) |
| 1.6 | After going offline, what does the leftover Redis row leak? | Within the 120s TTL window: it is invisible in discovery listings (offline does not `Visible()`), it is not a Routing candidate (DB status filtering), **but single-server detail merges the unexpired runtime into the offline server**, and the fleet `GetStats` player total still counts it; the sweep does not clean Redis (`AutoManaged()` excludes offline, so that row is never revisited) — only unregister and admin disable call `DeleteRuntime` | model.go:109-130; discovery.go:91-107; postgres.go:33-38; registry.go:273; admin/service.go:98 | Deferred (low severity: the window is bounded, see D5) |

## 2. Review question two: Routing's responsibility boundary and correctness

| # | Review question | Source conclusion | Evidence | Disposition |
| --- | --- | --- | --- | --- |
| 2.1 | Do batch reads fail closed? | Yes: both Recommend and Diagnose propagate `GetRuntimes` errors, and the comment states plainly "the decision path does not rank on unknown load"; pinned by tests | service.go:136-138/:246-248/:362-377; runtimefail_test.go:27-48 | Pinned |
| 2.2 | Could an unknown status be mistaken for healthy? | The DB status is authoritative: candidate filtering happens in the store by status (online by default); a missing runtime does not exclude a candidate (profile values rank it, see 1.3); **a failure to parse a Redis field degrades to a zero value** (a partial heartbeat beats a read failure — the side effect is that a corrupt hash becomes the optimal candidate with Load=0) | service.go:335-346; redis.go:121-133 | Gap fixed (1.3) + deferred (D6) |
| 2.3 | Is the recommendation result explainable? | `/v1/routing/recommended` returns the winner plus a single reason (one of four constants); `/v1/admin/diagnose/routing` spreads the whole fleet server by server (stage/rank/two-pass hits/owned/maintenance window/eligible/rejection-reason enum) | handlers.go:525-528/:817-860; service.go:19-22/:157-192/:286-317 | Pinned (by the diagnose page decree) |
| 2.4 | Any complex scheduling algorithm? | None introduced — consistent with "explicitly not doing", keeping the "recommendation over metadata" boundary | roadmap "Explicitly not doing" | Documented honestly |

## 3. Review question three: Directory and event-sync reliability

| # | Review question | Source conclusion | Evidence | Disposition |
| --- | --- | --- | --- | --- |
| 3.1 | Is duplicate delivery idempotent? | Idempotency = overwrite via composite-key upsert (same shape in all three stores: `ON CONFLICT` / `ON DUPLICATE` / unconditional overwrite), with no event-version dedupe key; a repeated delete was previously **not** idempotent (`ErrNotFound` fed the retry loop) | events.go:20-42/:85; postgres.go:265-276; events_test.go:36-62 | **Gap fixed**: `applyDeleted` now succeeds as a no-op for an already-missing row or a parse failure (events.go), pinned by tests for three kinds of redelivery (events_test.go `TestApplyEventDeletedTwice`) |
| 3.2 | Does an out-of-order stale event overwrite newer data? | Yes — last-arrival-wins, with no timestamp/version gate; a redelivered old `created` overwrites a newer `last_login_at` with the old `Timestamp`. **sync.md §6 documents this honestly**: "v0.1 goes for eventual consistency and does not handle out-of-order delivery, relying on the next event to overwrite it"; the memory queue's `idemKey` dedupe mechanism is in place but has no producer setting it (a future contract hook) | events.go:81-83/:107-119; sync.md §6; queue.go:144/:380-391 | Documented honestly + deferred (D2, the event version contract) |
| 3.3 | Crash recovery and ACK ordering? | Redis Streams: shared group `atlas`, consumer name `hostname:pid`, **XACK strictly after ApplyEvent succeeds**, crashed entries stay in the PEL, `XAutoClaim` claims them after 30s, and after 5 retries they are logged as a dead letter; with multiple replicas in one group, each event is consumed exactly once | redis/adapter.go:28/:74-80/:251-258/:194-238 | Pinned |
| 3.4 | Kafka/NATS/RabbitMQ retries and dead letters? | All three ACK after apply and use shared groups / durable subscriptions / competing queues (consume once); none has a retry cap or DLQ (only undecodable payloads are acked and dropped): Kafka relies on rebalance redelivery (uncapped), NATS JetStream's default 30s AckWait redelivers indefinitely, and **RabbitMQ's Nack(requeue) used to be a hot loop** | kafka/adapter.go:217-230; nats/adapter.go:224-232; rabbitmq/adapter.go:232-241 | **Gap fixed** (the rabbitmq hot loop → configurable `RequeueBackoff`, 5s by default, at-least-once unchanged); the Kafka/NATS slow loop is deferred (D3) |
| 3.5 | Failure semantics of the synchronous HTTP path? | `internal/event/http` is an in-process synchronous adapter: an apply failure makes `Publish` return an error → HTTP 400/404/500 (`writeEventError`), never a silent drop; only the async adapters return 202 | http/adapter.go:1-7/:37-47; handlers.go:476-483/:531-542 | Pinned |
| 3.6 | Migration replay and rollback semantics? | In v0.1 a migration is a bare status record: no character index copying, no event linkage, and rollback only flips a status string — migration.md's "status in v0.1" matches word for word (no automatic advance, no atomic index endpoint), with no docs running ahead of code | admin/service.go:121-156/:167-183; store.go:137-149; migration.md "Status in v0.1" | Documented honestly (code and docs agree) |
| 3.7 | Is `character.moved` atomic? | It upserts the target before deleting the source, non-atomically — a crash in between leaves double ownership (the window is one write interval) | events.go:204-207 | Deferred (D4) |

## 4. Review question four: secure defaults

| # | Review question | Source conclusion | Evidence | Disposition |
| --- | --- | --- | --- | --- |
| 4.1 | Is an unmapped key defaulting to admin ever signalled? | Admin is the default at two layers (the resolver maps `role=""` → admin, and an unresolvable key starts as admin), with **previously zero warnings**; there is no notion of a production mode (grep finds it only in comments) | auth.go:117/:135-139/:172-177; grpc/auth.go:132/:147-149 | **Gap fixed**: main.go now emits a five-part startup posture warning (admin/registry both empty = open, unmapped key count, audit disabled, rate limiting unconfigured) |
| 4.2 | Do operator and admin have the same permissions? | Yes — the only enforced boundary in the whole codebase is the viewer's read/write line (method-level for REST at :146, a 4-RPC read allowlist for gRPC at :151); `RoleOperator` is declared and then never compared; security.md states this honestly as "equal in power to admin … a reserved slot" | auth.go:146; grpc/auth.go:86-91/:151; security.md:33-34 | Documented honestly |
| 4.3 | Is the viewer's read scope consistent across the two transports? | No: a REST viewer may GET any admin endpoint, while a gRPC viewer gets only 4 read RPCs — security.md had not called this out | auth.go:146 vs grpc/auth.go:86-91 | **Gap fixed**: security.md gained the asymmetry note (narrowing the opening means narrowing both transports) |
| 4.4 | Default posture for TLS / audit / rate limiting? | TLS on :8081/:9090 is optional + fail fast (tlsutil raises a specific error, main exits 1); **:8082 has no TLS implementation** (0 grep hits, it relies on keys + IP allowlist + network position); the audit ring holds 1,000 entries in memory (main.go:447, consistent with security.md:54); rate limiting is off by default — and the startup log was previously silent about all of it | tlsutil.go:38-62; main.go:433-466 | Gap fixed (covered by the 4.1 warnings); admin TLS is deferred (D7) |

## 5. Review question five: evidence for the performance optimizations

| # | Review requirement | Status | Disposition |
| --- | --- | --- | --- |
| 5.1 | Comparative numbers on the same machine and data | performance.md §8.5: the pre-change path is replicated inside the same bench (7 server filters × 1k/10k, 3 character reads × 1k/10k, 4 write shapes), with an equivalence gate + min-of-5, and the environment and reproduction commands are complete | Satisfied |
| 5.2 | Merge rate / backpressure observability | QueueStats → 14 Prometheus series + an admin endpoint + a dashboard card; the bench reports %merged/%applied | Satisfied |
| 5.3 | No change to the API or consistency semantics | Public method signatures are unchanged (the caller blocks until its own instruction commits, so read-after-write stays exact); the storetest contract keeps all three stores aligned; the §8 preface states explicitly that "SQL stores do not introduce a queue" to prevent misreading | Satisfied |
| 5.4 | p50/p95/p99 and GC | The microbenchmark yardstick is ns/op + allocs (service-layer benchmarks are a separate page); percentiles and long-run GC are untested — that belongs to load-testing infrastructure | Deferred (D8) |

## 6. Fix list from this round (code)

1. **`applyDeleted` idempotent no-op** (internal/directory/events.go) — an already-deleted row or an unparseable payload both return success, so at-least-once duplicate deletes no longer feed the retry loop. Test: `TestApplyEventDeletedTwice` (three kinds of redelivery + never-existed).
2. **RabbitMQ redelivery backoff** (internal/event/rabbitmq) — after a handler failure it Nacks(requeue) with `RequeueBackoff` (configurable via Options, 5s by default), turning a hot loop into a slow one; at-least-once semantics unchanged. The test fixture injects 1ms.
3. **Diagnose `live_runtime` field** (internal/routing) — flags per server, truthfully, which candidates are "ranked using profile values"; the comment and a pinned test (`TestDiagnoseFlagsMissingRuntimeSnapshot`) explain why Load=0 archive behaviour no longer disguises itself as the lowest load.
4. **Startup security posture warnings** (cmd/atlas/main.go) — five Warn lines naming each issue (with the corresponding env var names): admin/registry auth both empty, the count of keys defaulting to admin, audit explicitly disabled, and rate limiting unconfigured.
5. **security.md** — one sentence on the viewer's asymmetric read scope; plus a re-check that ha.md's multi-replica alert/audit entries are accurate.

## 7. Deferred items (real, not addressed this round, by priority)

| # | Item | Risk | Notes |
| --- | --- | --- | --- |
| D1 | Sweep blind-write race | Medium-low | A stale snapshot can knock a just-re-registered server to suspect/offline; self-healing relies on the next heartbeat. Design sketch: add a status-precondition variant of `UpdateServerStatus` (`WHERE status IN (...)`), have the monitor pass the expected prior state, change all three stores + the contract |
| D2 | Event version / sequence contract | Medium | An out-of-order stale event overwrites a newer row (sync.md §6 already notes the eventual-consistency trade-off); the `idemKey` hook is in place with no producer. Option: add a source-side monotonic sequence to events, or gate the upsert on `Timestamp` with `WHERE updated_at < EXCLUDED...` across all three stores |
| D3 | Kafka/NATS have no retry cap or DLQ | Medium | A decodable but persistently failing poison event loops slowly forever; redis already caps at 5 and logs a dead letter. Option: align with redis's counter + cap + dead-letter log (or introduce a DLQ stream/topic) |
| D4 | `applyMoved` is not atomic | Low | A crash between upsert and delete leaves double ownership, with a very short window; the store layer has no cross-row transaction interface, so record it honestly for now |
| D5 | Leftover Redis rows for offline servers | Low | Within 120s single-server detail merges a live snapshot and Stats still counts players; the sweep does not clean Redis. Option: best-effort `DeleteRuntime` on the offline transition (the same as unregister/disable) |
| D6 | Redis field parse failure degrades to zero | Low | A corrupt hash becomes the optimal candidate with Load=0; the trade-off comment is in place (a partial heartbeat beats a read failure). `live_runtime` already makes profile-ranked rows visible, but corrupt rows still disguise themselves as live snapshots — tightening this needs per-field validation at the cost of a more complex read path |
| D7 | No TLS on the admin port (:8082) | Medium | Relies on keys + IP allowlist + network position; TLS exists only on :8081/:9090. Option: `ATLAS_ADMIN_TLS_CERT/KEY` reusing tlsutil, the same way as gRPC |
| D8 | Percentile latency / long-run GC load testing | Medium | Beyond microbenchmarks you need shaped load testing (the review's P0/P1 recommendation); this is load-test infrastructure, not a single-point fix |
| D9 | A failed webhook does not clear the latch | Low | A failed delivery is only logged and is not resent until recovery crosses the threshold; option: clear the latch on failure (retry next round), at the cost of possibly duplicate alerts |
| D10 | memory queue ctx cancellation semantics | Low | After cancellation the instruction still lands (a 500 is reported to the caller but the write took effect); the comment states best effort, which belongs to the queue-semantics documentation |

## 8. Mapping back to the review's conclusions

- "You cannot infer production reliability from documentation" — holds, and has been partly
  redeemed: the 11 "pinned / documented honestly" entries above come from existing tests
  and honest documentation; the 5 "gap fixed" entries are the direct output of this
  source pass; the 10 "deferred" entries are each on the books (§7) rather than pretended
  away.
- "Don't assume defaults are safe just because the docs have RBAC/mTLS sections" —
  adopted: the 4.1 warnings landed directly in this round; a "production mode" is not
  introduced for now (that is configuration-baseline design, see the review's P0 table,
  kept as a trigger-based item).
- "Do a second review pass against the source, down to files, functions and risk level" —
  that is this page.
