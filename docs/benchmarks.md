
# Benchmarks

Service-layer benchmarks for Registry and Discovery (`go test -bench`, memory store,
excluding network and serialization). They answer two capacity questions: **how many
heartbeats a single Atlas can absorb** and **what one list call actually costs**.
End-to-end numbers for the Redis / PostgreSQL backends depend on deployment topology
(see [ha.md](ha)); what follows is a reference for the lower (upper) bound of the
service-layer logic cost.

## 1. How to reproduce

```bash
# All benchmarks (~15s)
go test ./internal/registry/ ./internal/discovery/ -run xxx -bench . -benchtime 1s -benchmem

# A single one (use -count=5 and take the median for regression comparisons)
go test ./internal/discovery/ -run xxx -bench BenchmarkListServers -benchmem
```

QPS ≈ `1e9 / ns_per_op` (single goroutine). The allocs/op figure from `-benchmem`
reflects how many allocations each call makes, and is a steadier regression signal
than wall-clock time.

## 2. Baseline numbers

2026-10, Linux / amd64 / i9-10880H (14 logical cores), Go 1.27. The ns/op, B/op,
allocs/op and QPS columns are pinned values from a 2026-10-13 re-measurement
(min-of-5, idle machine; allocation counts are deterministic and unaffected by machine
load). The write-path numbers include the per-item receipt cost of the v0.2 command
queue (landed 2026-10-08, see [Performance Design](/performance#8-2)); for the
before/after comparison against the old direct-lock path see Performance Design §8.5:

| Benchmark | Scenario | ns/op | B/op | allocs/op | Single-core QPS |
| --- | --- | ---: | ---: | ---: | ---: |
| `BenchmarkRegister` | New server registration (write path + initial heartbeat) | 13,378 | 4,671 | 25 | ~75k |
| `BenchmarkReRegister` | Idempotent re-registration (control-plane restart recovery path) | 11,038 | 5,001 | 28 | ~91k |
| `BenchmarkHeartbeat` | Heartbeat hot path (once per server every 10s) | 5,055 | 1,992 | 11 | ~198k |
| `BenchmarkHeartbeatParallel` | Concurrent heartbeat fan-in from 64 servers | 4,378 | 1,916 | 13 | ~228k×14 cores |
| `BenchmarkGetServer` | Single server detail (including runtime merge) | 1,313 | 656 | 7 | ~762k |
| `BenchmarkListServers/fleet_100` | Full list (100 servers, including runtime merge) | 41.6µ | 30KB | 120 | ~24k |
| `BenchmarkListServers/fleet_1000` | Full list (1,000 servers) | 243µ | 60KB | 123 | ~4.1k |
| `BenchmarkListServers/fleet_5000` | Full list (5,000 servers) | 1.30m | 260KB | 127 | ~772 |
| `BenchmarkListServersRegion` | region filter + limit 100 (over 1,000 servers) | 569µ | 67KB | 216 | ~1.8k |

## 3. Reading the numbers

- **Heartbeats are not the bottleneck.** ~200k QPS on one core means 100,000 servers
  heartbeating every 10s = 10k QPS, i.e. 5% of a single core. Under fan-in concurrency
  the per-op cost stays essentially flat versus a single goroutine (the command queue
  batches and absorbs the contention, see Performance Design §8.5), leaving roughly
  20× headroom. When planning capacity, start with heartbeats — **but the real
  constraint is the Redis writes and the network** (see below).
- **Registration costs about 2.7× a heartbeat** (validation + upsert + initial
  heartbeat), but registration only happens at startup/recovery, and its peak is the
  whole fleet re-registering at once. A cold recovery of a 10k fleet ≈ 75k QPS·cores
  (`BenchmarkRegister` single-core throughput), which a single replica absorbs (10k
  registrations drain in about 0.13s).
- **Listing is O(fleet)**: time grows linearly with fleet size while allocations stay
  near-constant (120→127 allocs). The production Redis read path has been
  **pipelined (P1, landed 2026-10-04)**: `RuntimeStore.GetRuntimes` batch reads are
  done with a single pipeline Exec, so listing no longer calls `GetRuntime` per server
  — **network round trips drop from N to 1–2** (1 for a single node; for Cluster, one
  per slot batch). Estimated cost on the pipelined Redis backend (0.3ms/RTT plus
  payload transfer, 1Gbps-class LAN):

  | Fleet size | Service-layer time | Redis backend estimate (1 round trip + transfer) |
  | --- | ---: | ---: |
  | 100 | 42µs | ~0.4ms (~20KB) |
  | 1,000 | 243µs | ~2ms (~200KB) |
  | 5,000 | 1.3ms | ~4ms (~1MB) |

  Service-layer allocations stay near-constant (123 allocs/op at fleet_1000, from the
  ids slice and result map containers of the batch read; judge regressions by
  allocation count, see §4) — the entire benefit of pipelining is on the network round
  trips.

  Mitigations (still effective): the gateway can put a short-TTL cache in front of
  `/v1/discovery/servers` (the list is quasi-real-time data, a 1–5s cache does not
  change semantics); `region` / `limit` filters keep a single response around the
  100-server scale.

- **Regression guardrails**: Heartbeat > 10µs, Register > 20µs, or a fleet_1000 list
  > 1ms (service-layer numbers) means you should stop and inspect the diff — it usually
  means an extra lock or extra allocation crept in.

## 4. Method and boundaries

- Memory store: this measures **service-layer logic** (validation, assembly, filtering,
  merging) as an upper-bound reference, excluding network, TLS, JSON encoding and
  storage I/O.
- A single run at `-benchtime 1s` carries ±5% noise; use `-count=5` plus `benchstat`
  when comparing optimizations.
- The baseline table was measured on an idle machine. If you re-measure under
  concurrent load (a build, CI), ns/op inflates several-fold while **allocs/op and B/op
  stay unchanged** — judge regressions by allocation count, or wait for an idle machine.
- Heartbeat / registration numbers come from fresh writes with unique IDs; idempotent
  re-registration is listed separately.
- End-to-end percentile latency, GC behavior and soak are deliberately outside this
  method — the production-shaped plan (deployment, load model, acceptance criteria)
  lives in [Performance](/performance) §9 (design draft; execution gated on a designated
  deployment).
