---
title: 正则表达式（std::regex 与替代）
---

概览
- `std::regex` 提供 ECMAScript 风格，性能一般；复杂场景可用 RE2/PCRE2

基础用法
```cpp
std::regex r("(\\w+)-(\\d+)");
std::smatch m; if(std::regex_match(s, m, r)){ /* m[1], m[2] */ }
```

建议
- 性能敏感场合选更高性能库；或使用简单解析（find/split）替代
