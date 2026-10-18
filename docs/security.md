
# Security

Since v0.1.17 Atlas ships four layers of protection built in: Registry mTLS, Admin
RBAC, operation audit and rate limiting. mTLS / RBAC / rate limiting are **opt-in**
(leaving them unconfigured turns the corresponding protection off); auditing is **on
by default** (disable it with `ATLAS_AUDIT_ENABLED=0`).

## 1. TLS / mTLS (Registry :8081, gRPC :9090, Admin :8083)

The three internal listeners — the REST registration port, the five-service gRPC
port and the Admin HTTP port (dashboard + operator tools) — share the same TLS
semantics but are configured independently: supplying a certificate enables TLS;
additionally supplying a client CA upgrades it to **mutual TLS**, meaning clients
must present a certificate issued by a trusted CA.

```bash
# Registry port
ATLAS_REGISTRY_TLS_CERT=/etc/atlas/server.crt.pem
ATLAS_REGISTRY_TLS_KEY=/etc/atlas/server.key.pem
ATLAS_REGISTRY_CLIENT_CA=/etc/atlas/client-ca-bundle.pem   # optional: setting it enables mTLS

# gRPC port
ATLAS_GRPC_TLS_CERT=/etc/atlas/server.crt.pem
ATLAS_GRPC_TLS_KEY=/etc/atlas/server.key.pem
ATLAS_GRPC_TLS_CLIENT_CA=/etc/atlas/client-ca-bundle.pem   # optional: setting it enables mTLS

# Admin port
ATLAS_ADMIN_TLS_CERT=/etc/atlas/server.crt.pem
ATLAS_ADMIN_TLS_KEY=/etc/atlas/server.key.pem
ATLAS_ADMIN_TLS_CLIENT_CA=/etc/atlas/client-ca-bundle.pem   # optional: setting it enables mTLS
```

- Cert/key only: server-side TLS (the client can verify the server's identity).
- Adding `*_CLIENT_CA`: `RequireAndVerifyClientCert`, so connections without a
  certificate fail during the handshake.
- TLS 1.2 minimum. Invalid certificate/CA material makes the process fail at startup
  (fail fast). Any port can be enabled on its own; the Admin port defaults to plaintext,
  so operator tools that already assume network isolation keep working unchanged.
- Client integration: the Go SDK's gRPC transport turns this on with `GRPCTLS: true` +
  `GRPCTLSCACert` (a PEM CA bundle); the REST transport just uses the `https://` base
  address as usual. The other language SDKs currently ship the REST transport only
  (gRPC is Go-SDK-only), so pointing them at an `https://` base address covers TLS
  without involving the gRPC port. The Admin port is an `https://` base address to the
  dashboard and any operator tooling; when `ATLAS_ADMIN_TLS_CLIENT_CA` is set the tool's
  HTTP client must additionally present a client certificate it trusts.

## 2. Admin RBAC

The Admin API grants one of three roles per API key; the role is resolved in middleware
and written into the request context:

| Role | Permissions |
| --- | --- |
| `admin` | All Admin endpoints (the default) |
| `operator` | Equal in power to `admin`: the role is resolved and written into the request context, but the current implementation enforces no differentiated endpoints beyond `viewer` (finer-grained permissions are a reserved slot, still planned) |
| `viewer` | Read-only (GET/HEAD); any mutation returns `403 ROLE_NOT_ALLOWED` — the only role that is actually enforced differently |

```bash
ATLAS_ADMIN_API_KEYS=key-read,key-write,key-full
ATLAS_ADMIN_ROLES=key-read:viewer,key-write:operator,key-full:admin
```

- Keys absent from `ATLAS_ADMIN_ROLES` default to `admin` (backwards compatible with
  deployments that only set `ATLAS_ADMIN_API_KEYS`).
- Entries missing the colon are treated as admin; an unknown role name fails startup.
- **gRPC follows the same rules**: the 11 AdminService RPCs (:9090) use the same
  keys + roles, and a viewer may call only the 5 read RPCs (GetStats / GetLoadHeatmap /
  GetMigration / ListMigrations / SearchCharacters); write RPCs return gRPC
  `PermissionDenied ROLE_NOT_ALLOWED`. The 3 RegistryService RPCs are likewise
  protected by `ATLAS_REGISTRY_TOKENS` + IP allowlist. Both transports share the same
  set of environment variables. **The viewer's read scope is asymmetric across the two
  transports**: REST decides by HTTP method (any GET/HEAD is allowed), while gRPC
  decides by a method-name allowlist (gRPC has no uniform "read" semantics) — the same
  viewer key can read through REST management data that gRPC does not expose (such as
  `GET /v1/admin/servers`), so narrowing the opening means narrowing both transports
  together.

## 3. Operation audit

Once enabled (on by default, disable with `ATLAS_AUDIT_ENABLED=0`), every Admin request
is recorded:

- **actor**: two-part `role:key-fingerprint` (the first 12 hex digits of the key's
  SHA-256; the original key cannot be derived from it); under mTLS it becomes
  three-part `role:key-fingerprint:cert-fingerprint` (`auth.go` `Actor.String()`), and
  the development mode without configured keys shows `role:anonymous`;
- **timestamp**: RFC3339 with nanosecond precision;
- **method / path / query / status**;
- **diff**: the request body of mutating methods (POST/PUT/PATCH/DELETE), capped at
  4KB; invalid JSON is wrapped and stored as a string.

Records go to both the structured log (`slog.Info`, `msg=admin audit`) and an in-memory
ring buffer (1,000 entries by default), and the most recent entries are served through
`GET /v1/admin/audit?limit=50`:

```json
{
  "entries": [
    {
      "time": "2026-10-01T12:00:00Z",
      "actor": "operator:3f8a1c2b9d0e",
      "method": "POST",
      "path": "/v1/admin/servers/game-1001/drain",
      "status": 200,
      "body": {"reason": "patch"}
    }
  ]
}
```

Audit sits **inside** authentication (it has the resolved actor) and **inside** rate
limiting — the chain is limiter → authn (with the audit middleware inside it) →
business logic, so a request rejected by the limiter never reaches authn and therefore
never enters the ring. The ring is not persisted to disk; durability is the log
collection pipeline's job (the structured log lines).

The dashboard's "Operation Audit" page reads this ring directly (`/audit` route): it
shows the change body for each write operation, marks read operations as having none,
colours status codes by 2xx / 4xx / 5xx, and changing the entry count just changes
`limit`. The bottom of the page states plainly that this is an in-memory ring that is
lost on restart — good enough for investigating who changed what recently, while
compliance archiving should still rely on the structured log.

**Both transports**: AdminService RPCs on gRPC (:9090) are recorded in the same ring —
`path` is the fully-qualified method name (e.g. `/atlas.v1.AdminService/Disable`), read
RPCs are recorded as `GET` (no diff) and write RPCs as `POST` (the diff is the protojson
request, subject to the same 4KB cap); gRPC status codes are mapped to their equivalent
HTTP status following google.rpc convention before being recorded. The actor is likewise
`role:key-fingerprint` (validated by the gRPC authn interceptor; the keyless development
mode shows `:anonymous`). Registry / Discovery / Directory / Routing RPCs do not enter
the audit ring (consistent with REST auditing only `/v1/admin/*`).

## 4. Rate limiting (token bucket)

Rules are matched by **path prefix** (longest prefix wins) and buckets are kept per
**client IP**, so a single misbehaving server cannot take down its neighbours. It is
off by default and turns on once explicitly configured:

```bash
# prefix=rate:burst (burst defaults to the rounded rate), multiple rules separated by ;
ATLAS_RATE_LIMITS="/v1/registry/servers/register=10:20;/v1/registry/=200:400;/v1/admin/=50:100"
ATLAS_RATE_LIMIT_DEFAULT="1000:2000"   # fallback rule for unmatched prefixes
```

- Buckets are isolated per `clientIP × matched rule`; tokens refill continuously over
  time, and burst is the ceiling.
- Exceeding the limit returns `429 RATE_LIMITED` + `Retry-After: 1`.
- The bucket table is capped at 65,536 to prevent memory growth from forged XFF headers
  (beyond the cap, new clients are rejected — fail closed).
- A rule parsing error exits at startup.

**Both transports**: gRPC (:9090) shares the same limiter and bucket table with REST —
prefix matching is done against fully-qualified method names (e.g.
`/atlas.v1.RegistryService/Register=10:20`), and unmatched calls fall into the default
bucket. The Registry and Admin domains are limited; the three Public services are not
(consistent with the public port :8080). Rejections return gRPC `ResourceExhausted`
with a `RATE_LIMITED` message (gRPC has no `Retry-After` header, so back off according
to the code), and the same rejection is counted in the REST view's
`GET /v1/admin/rate-limits` under `rejected_by_endpoint` / `rejected_by_client` — you
cannot escape the bucket by switching transports.

## Configuration quick reference

| Environment variable | Default | Purpose |
| --- | --- | --- |
| `ATLAS_REGISTRY_TLS_CERT` / `_KEY` | (empty) | Registry TLS server certificate |
| `ATLAS_REGISTRY_CLIENT_CA` | (empty) | Client CA; setting it enables mTLS |
| `ATLAS_GRPC_TLS_CERT` / `_KEY` | (empty) | gRPC TLS server certificate |
| `ATLAS_GRPC_TLS_CLIENT_CA` | (empty) | gRPC client CA; setting it enables mTLS |
| `ATLAS_ADMIN_ROLES` | (empty) | Admin RBAC `key:role` list |
| `ATLAS_AUDIT_ENABLED` | `1` | Admin operation audit |
| `ATLAS_RATE_LIMITS` | (empty) | Per-prefix rate limit rules |
| `ATLAS_RATE_LIMIT_DEFAULT` | (empty) | Rate limit fallback rule |
| `ATLAS_CORS_ORIGINS` | (empty) | CORS allowlist (comma-separated, applies to the public/admin ports); empty means no CORS headers are emitted, `*` is for development only |

Rate recommendations: registration endpoints get the strictest limits (low RPS plus a
small burst), heartbeats come next (estimate from server count × heartbeat frequency),
and discovery/directory reads get the widest opening.

## Periodic audits

Dependency vulnerability scanning (`govulncheck` + npm audit) runs weekly in CI; the
per-layer manual review checklist and the conclusions of past audits are in
[security-audit.md](security-audit).
