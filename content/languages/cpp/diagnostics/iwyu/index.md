title: Include-What-You-Use（IWYU）
---

作用
- 静态分析“应包含哪些头”，移除多余 `#include`，降低编译依赖与时间

用法（概念）
- 以 `iwyu_tool.py` 批量执行，或在 CMake 中配置 `CMAKE_CXX_INCLUDE_WHAT_YOU_USE`
- 根据报告替换“转发头”为具体声明；必要时 `// IWYU pragma: keep`

建议
- 逐模块引入，避免一次性大改；CI 中先做告警
- 配合 PImpl 与 clang-tidy 提升可维护性
