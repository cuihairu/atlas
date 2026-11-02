
# Documentation / Source Consistency Audit (2026-10-08, merged and closed out)

> This is a marker page, not a translation. The full audit report is the Chinese
> original at [审计-文档一致性.md](zh/audit-doc-consistency.md) (377 lines).

## Why there is no English version yet

This page is an **internal audit work record**, not user-facing documentation. It is
dense with Chinese-named files, internal process references and per-row
`page.md:line → file.go:line` evidence, and its entire value is precision. A
mechanical translation would be exactly the kind of low-quality output worth avoiding,
so the page was left with this marker instead.

It is tracked for translation in
[todo.md](https://github.com/cuihairu/atlas/blob/main/todo.md) alongside
[the API reference](api.md).

## What the report covers

For the maintainer's benefit, the Chinese original covers:

| Section | Contents |
| --- | --- |
| Audit scope and method | The 25 documentation pages covered, the seven parallel evidence-gathering groups, and the yardstick used (source code is the truth; fixing docs is allowed, changing code to match docs is not) |
| Totals and the P0 list | 46 discrepancies split into *docs ahead of code* (10), *code ahead of docs* (12) and *description mismatch* (24), with the 9 P0 items enumerated |
| Difference tables (7 groups) | Configuration/operations/data model, architecture/HA/sync/topology, API reference, README/home/performance/benchmarks/roadmap, security/audit/APISIX, the six-language SDKs, and concepts/lifecycle/realms-shards/migration/scenarios — each row citing the doc line, the claim, the source evidence and a severity |
| Verified as consistent | Per-group spot-check summaries with two-way evidence |
| Notes | Items deliberately excluded from the three discrepancy classes (release-tag bookkeeping, an editorial duplication, and a note that the mysql store is fully implemented in the repo but not yet wired into `ATLAS_STORE`) |
| Appendix A | Before/after benchmark comparison for the performance batch, with the method and reproduction command |
| Closure section | The four documentation-fixing batches executed on 2026-10-08, each row cancelled against its commit |

---

## 2026-11-02 Incremental Re-review (AI-flavor sweep + full re-check)

> Full six-step pass per the audit specification, plus a style-spec sweep over README ×2 and docs/** (review archives excluded). The GitHub About line was checked via `gh repo view`: description and homepageUrl match the README positioning — no drift.

### Re-check verdict

- README technical claims verified claim by claim, all matching: Go 1.27 = `go.mod:3`; "50 business endpoints" = exactly 50 deduped (method, path) pairs registered in non-test `internal/httpapi` files; gRPC "five services, 23 RPCs" = 5 service definitions / 23 rpc entries in `api/proto/atlas.proto`; six-language SDKs = `sdk/{go,cpp,python,js,java,csharp}`; releases v0.1.0–v0.1.4 = `gh release list`; the project-structure tree matches the real `internal/`, `cmd/`, `internal/store/` entries; runtime `ATLAS_STORE` accepts memory/postgres only = `cmd/atlas/main.go:113,182`.
- Buckets: **0 / 0 / 0 new items**.

### AI-flavor sweep (fixed in place this batch)

- README.md and README.zh.md badge rows carried orphan duplicate badges (Release / License / Codecov each rendered twice, as unlinked bare `<img>` copies) — duplicates removed, one linked set kept; en keeps the docs badge, zh the 文档 badge. The Codecov badge URL was also normalized to the `img.shields.io/codecov.io/…` form (was the legacy `img.shields.io/codecov/…`).
- Banned-phrase sweep review: `永不` hits under docs/zh are technical semantics (lifecycle/config-center design invariants such as "never auto-expires", "never mutually exclusive", "never pushes full config") — kept; `里程碑` are factual internal-milestone labels — kept; roadmap ✅/❌/🔒 marks are legend-defined status values — kept.
- Post-fix self-check: no duplicate badges, no empty anchors, no double slashes.
