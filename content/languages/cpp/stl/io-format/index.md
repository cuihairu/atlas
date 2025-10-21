---
title: I/O 与格式化（iostream / std::format / fmt）
---

建议
- 输出：`std::format`（C++20/23）或 fmtlib，性能与可读性优于 iostream 拼接
- 输入：避免复杂的 `operator>>`，更倾向解析库或 `from_chars`

示例
```cpp
#include <format>
std::string s = std::format("{} + {} = {}", 2, 3, 5);
```

注意
- 本地化与编码处理：结合 `std::format` 的 locale 支持；跨平台统一 UTF-8
