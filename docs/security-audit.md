[English](security-audit.md) | [简体中文](zh/security-audit.md)

# Security Audit

A security audit is a periodic re-examination, not a one-off delivery. This page
records Atlas's audit scope, checklist and the conclusions of each past audit; the
automated part is handled by CI ([workflow](https://github.com/cuihairu/atlas/blob/main/.github/workflows/security-audit.yml):
`govulncheck` plus two npm `audit` runs every Monday), while the manual part runs at
release milestones and is appended here.

## 1. Audit scope and checklist

Each audit (CI cycle or pre-release) walks the same layers:

| Layer | Check | Code location |
| --- | --- | --- |
| Authentication | Registry token / mTLS handshake policy (TLS 1.2 minimum, fail fast) | `internal/httpapi/registry_auth.go`, `internal/tlsutil` |
| Authentication | Admin RBAC role resolution (unknown role fails startup, viewer is read-only) | `internal/httpapi/auth.go` |
| Audit | Actor fingerprint is irreversible (first 12 hex of the key / client certificate SHA-256); diff truncated at 4KB | `internal/httpapi/audit.go` |
| Rate limiting | Bucket table cap prevents XFF-driven memory growth; rule parsing fails fast | `internal/httpapi/ratelimit.go` |
| Storage | SQL fully parameterized (concatenation only ever builds placeholder indices and fixed clause names) | `internal/store/postgres`, `internal/store/mysql` |
| Storage | Redis key construction (the injection surface where IDs land directly in keys) | `internal/store/redisstore` |
| Supply chain | Dependency vulnerabilities (Go modules + dashboard / docs npm production dependencies) | CI `govulncheck` / `npm audit` |
| Secrets | Secrets in environment variables never reach logs or audit diffs | `internal/config`, `internal/httpapi` |

## 2. v0.1.20 audit record (2026-10)

**Conclusion: one dependency vulnerability was found and fixed (GO-2026-6443); one
low-severity hardening recommendation is on record.**

**Finding and remediation (dependency vulnerability)**:

- `govulncheck` reported **GO-2026-6443**: the gRPC server panics when the authority /
  Host header is missing (remote DoS), in `google.golang.org/grpc@v1.84.0`, reachable
  via `cmd/atlas/main.go → grpc.Server.Serve → http2Server.HandleStreams` (the :9090
  internal port — limited exposure, but real).
- Remediation: first pinned to the fixing pseudo-version
  (`v1.85.0-dev.0.20260825072537-93e31b48545e`); a 2026-10-02 inspection round moved to
  `v1.86.0-dev` via Dependabot PR #6 (which carries the fix and released the
  pseudo-version pin, pulling `protobuf v1.36.12` along with it). After the fix
  `govulncheck` passes and all test gates pass.
- Residual (unfixable, unreachable): **GO-2026-5932** — `golang.org/x/crypto/openpgp`
  is deprecated and no longer maintained (`Found in v0.57.0`, `Fixed in: N/A`). Atlas
  does not import `openpgp`, and govulncheck judges it as "present at module level but
  not on any call path" (affected = 0). `x/crypto` is still an indirect dependency of
  `tlsutil` and elsewhere, so it cannot be removed; on each re-scan, just confirm this
  entry has been downgraded from "affected" to "required only".

Confirmed (spot checks plus a full `go vet` / `go test`):

- **SQL injection**: every query in both the postgres and mysql stores goes through
  placeholder parameters; `fmt.Sprintf` only ever assembles `$N` / `?` indices and
  `SET` clause names hard-coded in the source, so no user data enters SQL text.
- **Irreversible fingerprints**: the audit actor's key fingerprint and the mTLS client
  certificate fingerprint added in v0.1.20 are both SHA-256 truncated to 12 hex, which
  cannot be reversed to the original value.
- **Log hygiene**: audit diffs are truncated at 4KB and invalid JSON is wrapped and
  stored as a string, so log volume is not amplified; secret-bearing environment
  variables exist only in config loading and are never written to the structured log.
- **Rate limiting resists abuse**: the bucket table is capped at 65,536, and when XFF
  is forged to mint a flood of IPs new clients fail closed.
- **npm**: `npm audit --omit=dev` reports 0 vulnerabilities for both the dashboard and
  docs production dependencies.

**Finding (low severity, on record)**:

- `Server.ID` is only checked for non-emptiness, with no character-set constraint.
  Callers of the registration endpoint (protected by token / mTLS) can submit IDs
  containing `:`, spaces or excessive length, and they land verbatim in the Redis key
  (`atlas:server:{id}:runtime`) and in the SQL primary key. There is no privilege
  escalation or injection consequence today (RESP is binary-safe and SQL is
  parameterized), but a later version should tighten this to `[A-Za-z0-9._-]{1,128}`,
  matching the convention already used for character-index shard keys.

## 3. Automated gates

```yaml
# .github/workflows/security-audit.yml (Mondays + manual dispatch)
govulncheck ./...                    # known vulns in the Go stdlib / dependencies; any finding fails
npm audit --omit=dev --audit-level=high   # dashboard and docs, production deps only, fails at high+
```

Day-to-day dependency updates are Dependabot's job (patch auto-merges, minor/major get
human review, see `dependabot-auto-merge.yml`); this workflow is a second, independent
re-check.
