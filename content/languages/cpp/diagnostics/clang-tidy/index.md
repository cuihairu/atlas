---
title: clang-tidy 规则与配置
---

基础配置（.clang-tidy）
```yaml
Checks: >
  clang-analyzer-*,bugprone-*,performance-*,modernize-*,readability-*,
  cppcoreguidelines-*,hicpp-*,misc-*,portability-*
WarningsAsErrors: '-*'
HeaderFilterRegex: '.*'
AnalyzeTemporaryDtors: true
```

最佳实践
- 按目录分层配置，逐步在模块内收紧检查范围
- 把“现代化改造”类（`modernize-*`）与“缺陷”类（`bugprone-*`）分批引入
- 在 CI 中与编译警告共同 gating；引入基线（baseline）逐步清零
- 本地以 `clangd`/`clang-tidy --fix` 辅助重构；谨慎自动修复范围

常见问题
- 第三方头触发检查：通过 `HeaderFilterRegex` 或 `NOLINT` 抑制
- 误报：用 `NOLINTNEXTLINE(rule)` 精确抑制，附注释
