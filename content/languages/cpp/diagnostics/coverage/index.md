---
title: 覆盖率（gcov / llvm-cov / lcov）
---

工具链
- GCC：`--coverage`（生成 .gcno/.gcda）+ `gcov/lcov` 汇总
- Clang：`-fprofile-instr-generate -fcoverage-mapping` + `llvm-profdata` + `llvm-cov show/report`

建议
- 在 CI 中收集并上传报告；设定阈值但避免“一刀切”的刚性指标
- 与 sanitizer/TSan 任务分离跑，避免干扰
