
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
