---
title: 时间与时钟（std::chrono）
---

核心概念
- `duration`（时长）、`time_point`（时间点）、`clock`（时钟）
- 时钟：`steady_clock`（单调，测量用）、`system_clock`（可调，真实时间）

用法
```cpp
using namespace std::chrono;
auto t0 = steady_clock::now();
// work
auto ms = duration_cast<milliseconds>(steady_clock::now()-t0).count();
```

日历与格式化（C++20/23）
- `year_month_day`、`zoned_time`；`std::format`/`chrono` I/O 改善

最佳实践
- 性能计时用 `steady_clock`；日志/对外显示用 `system_clock`/`zoned_time`
- 避免混用单位，统一 `ms`/`ns`；接口设计以 `duration` 表达超时
