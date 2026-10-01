# High Availability (v0.1.19)

Atlas is a stateless service: every replica reads servers / characters from
PostgreSQL and heartbeats from Redis, so horizontal scaling is a matter of
putting a TCP load balancer in front of the three HTTP ports and making the
two backing stores highly available. This guide covers the full topology and
the configuration knobs introduced in v0.1.19.

```
                        ┌────────────┐
   clients ─────────────│  APISIX /  │────────── public :8080
                        │  HAProxy   │────────── admin   :8082
                        └─────┬──────┘
                              │ TCP
   game servers ──────────────┤
   (heartbeats)         ┌─────▼──────────────────────┐
                        │  Atlas replica × N          │
                        │  (public / registry / admin)│
                        └──────┬──────────────┬───────┘
                               │              │
                    ┌──────────▼───┐   ┌──────▼─────────────────┐
                    │ Redis        │   │ PostgreSQL             │
                    │ master+replica│  │ primary + hot standby  │
                    │ + 3 Sentinels│   │ (streaming replication)│
                    └──────────────┘   └────────────────────────┘
```

Contents:

1. [Multi-replica Atlas](#1-multi-replica-atlas)
2. [HAProxy TCP load balancer](#2-haproxy-tcp-load-balancer)
3. [Redis Sentinel / Cluster](#3-redis-sentinel--cluster)
4. [PostgreSQL primary-replica](#4-postgresql-primary-replica)
5. [Connection pool tuning](#5-connection-pool-tuning)
6. [Lab stack](#6-lab-stack)

## 1. Multi-replica Atlas

Atlas keeps no per-instance session state:

| State | Where it lives | Multi-replica impact |
|---|---|---|
| Server registry | PostgreSQL (`ATLAS_STORE=postgres`) | shared, no conflict |
| Runtime / heartbeats | Redis (runtime store) | shared, TTL-based |
| Character index | PostgreSQL (+ optional sharding, `ATLAS_CHAR_SHARDS`) | shared |
| Audit log ring | in-memory per replica | **per-replica view** (see below) |
| Health monitor sweeps | in-process | each replica sweeps independently |

Run N identical replicas with the same env (point them at the same
PostgreSQL / Redis) and scale horizontally. Two caveats:

- **Health alerts / monitors run per replica.** Every replica evaluates
  suspect/offline ratios and may fire the same alert (v0.1.15). Deduplicate
  on the webhook side, or set `ATLAS_ALERT_*` ratios only on one designated
  replica.
- **Audit log is per replica.** The v0.1.17 ring buffer lives in process
  memory, so `GET /v1/admin/audit` reflects only requests that hit that
  replica. Pin admin traffic to one replica (HAProxy `balance source`, or a
  dedicated admin endpoint) until the audit log moves to a shared store.

All replicas can serve any heartbeat: registration and heartbeat are
idempotent upserts keyed by server ID, so plain round-robin at the registry
port is safe.

## 2. HAProxy TCP load balancer

For the Registry port the LB must be transparent TCP — game servers speak
raw HTTP with optional mTLS (v0.1.17) end-to-end to Atlas. HAProxy in TCP
mode works; APISIX stream proxy (`proxy_protocol` off, `enable_tcp_udp: true`)
is equivalent. A ready-to-use config ships at
[`deploy/haproxy/haproxy.cfg`](https://github.com/cuihairu/atlas/blob/main/deploy/haproxy/haproxy.cfg):

- `:8081` → registry fan-in, round-robin with active TCP health checks
  (`inter 5s fall 3 rise 2`) so a dead replica leaves rotation in ≤15 s.
- `:8080` → public API round-robin (use APISIX with auth/rate-limit here in
  production).
- `:8082` → admin, `balance source` sticky so the audit ring is coherent.
- `:8404/stats` → HAProxy dashboard.

Heartbeat cadence interacts with failover speed: with the defaults
(`ATLAS_SUSPECT_AFTER=30s`, `ATLAS_OFFLINE_AFTER=60s`) a replica crash
impacts at most one heartbeat interval of fan-in before the surviving
replicas' monitors mark the affected servers suspect. See
[lifecycle.md](./lifecycle) for the state machine.

## 3. Redis Sentinel / Cluster

The runtime store only needs the current heartbeat per server (hash +
TTL ≤ 120 s), which Sentinel failover covers. v0.1.19 adds topology config:

| Env | Meaning |
|---|---|
| `ATLAS_REDIS_CLUSTER` | comma-separated cluster seed nodes — **highest precedence** |
| `ATLAS_REDIS_SENTINELS` | comma-separated Sentinel `host:port` list |
| `ATLAS_REDIS_MASTER_NAME` | master set the Sentinels monitor (default `mymaster`) |
| `ATLAS_REDIS_URL` | credentials / logical DB for every mode (`redis://user:pass@host:6379/0`) |
| `ATLAS_REDIS_POOL_SIZE` | per-instance connection pool cap (default 10 × GOMAXPROCS) |

Precedence: `ATLAS_REDIS_CLUSTER` > `ATLAS_REDIS_SENTINELS` > single node.
Both the runtime store and the Redis Streams event adapter
(`ATLAS_EVENT_ADAPTER=redis`) use the same resolved topology.

Sentinel example:

```bash
ATLAS_REDIS_SENTINELS=s1:26379,s2:26379,s3:26379 \
ATLAS_REDIS_MASTER_NAME=mymaster \
ATLAS_REDIS_URL=redis://:secret@redis.internal:6379/0 \
./atlas
```

Notes:

- Run **3 Sentinels in independent failure domains** (different hosts/AZs);
  quorum is 2.
- On failover clients re-resolve the master automatically (go-redis
  failover mode); Atlas keeps serving stale runtime data for the TTL window
  (≤ 120 s) at worst.
- Redis Cluster (`ATLAS_REDIS_CLUSTER=c1:6379,c2:6379,c3:6379`) is supported
  but usually unnecessary: the runtime key set (`atlas:server:{id}:runtime`)
  is small and shardable by replica count instead.
- mTLS for Redis is not yet supported — keep Redis on a private network and
  require auth (`requirepass` / ACL).

## 4. PostgreSQL primary-replica

Atlas writes on every register / heartbeat / character op, so the primary is
the single write point; replicas serve reads only when you route them
explicitly (pgxpool does not do read/write splitting — see §5 for
single-pool sizing). Minimal streaming replication:

```sql
-- on the primary
ALTER ROLE atlas WITH REPLICATION;
-- pg_hba.conf
host replication atlas 10.0.0.0/8 scram-sha-256
```

```bash
# on the replica (initial seed + join)
pg_basebackup -h primary -U atlas -D /var/lib/postgresql/data \
  -R -P -X stream   # -R writes standby.signal + primary_conninfo
# postgresql.conf
hot_standby = on
```

Primary flags: `wal_level=replica`, `max_wal_senders≥5`,
`max_replication_slots≥5`. The lab stack
([`deploy/docker-compose.ha.yaml`](https://github.com/cuihairu/atlas/blob/main/deploy/docker-compose.ha.yaml))
automates all of this; for production use managed replication (RDS / Cloud
SQL / patroni) and keep `synchronous_commit` at the default — Atlas
tolerates a re-registration after a primary crash, losing in-flight
heartbeats only degrades servers to `suspect` until the next beat.

Failover runbook: promote the replica → update `ATLAS_DATABASE_URL` (a
`postgresprimary` DNS name behind the promotion avoids the restart) →
replicas reconnect on the next health check period.

## 5. Connection pool tuning

v0.1.19 exposes the pgxpool + go-redis pool knobs. Defaults are fine to
start; tune when you see queueing.

**PostgreSQL** (`ATLAS_DATABASE_URL` may also carry `pool_*` params; env
wins):

| Env | Default | Guidance |
|---|---|---|
| `ATLAS_PG_POOL_MAX_CONNS` | max(4, NumCPU) per replica | Σ MaxConns across replicas × 2 < `max_connections` (100 default). 2 replicas × 32 is a good ceiling for a 4-core primary. |
| `ATLAS_PG_POOL_MIN_CONNS` | 0 | Set to your steady heartbeat concurrency (e.g. 4–8) to avoid connect storms. Must stay ≤ MaxConns (clamped at startup). |
| `ATLAS_PG_POOL_MAX_CONN_LIFETIME` | 0 (never) | 30m — recycles connections through LBs/proxies and after primary promotion. |
| `ATLAS_PG_POOL_MAX_CONN_IDLE_TIME` | 0 (never) | 5m — trims the pool after traffic spikes. |
| `ATLAS_PG_POOL_HEALTH_CHECK_PERIOD` | 1m | 15s — detects a promoted/dead primary faster. |

**Redis** (`ATLAS_REDIS_POOL_SIZE`): heartbeats are 2–3 pipelined ops per
beat; 32 per replica covers ~10k servers at a 10 s cadence. Sentinel mode
adds 1 connection per Sentinel for topology discovery.

Rule of thumb for the registry hot path: 1 heartbeat ≈ 1 Redis op + 1 PG
upsert every `ATLAS_HEALTH_INTERVAL`. Size
`PG_POOL_MAX_CONNS × replicas ≥ peak_beats_per_second / expected_stmt_tps`.

## 6. Lab stack

```bash
docker compose -f deploy/docker-compose.ha.yaml up -d
```

Brings up: 2× Atlas + HAProxy (`:8080/:8081/:8082`, stats on `:8404/stats`),
PostgreSQL primary + hot-standby replica (streaming replication), Redis
master + replica + 3 Sentinels. Watch a failover:

```bash
docker compose -f deploy/docker-compose.ha.yaml pause redis-master
docker logs -f deploy-redis-sentinel-1   # +sdown … -sdown … +failover
docker compose -f deploy/docker-compose.ha.yaml unpause redis-master
```

The stack is a lab: replace the bind-mounted Sentinel configs, the
`scram-sha-256` passwords and the absence of TLS before production.
