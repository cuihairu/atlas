---
title: std::expected（C++23）
---

概念
- `expected<T,E>` 表示“成功的 T 或失败的 E”；避免异常或手写错误码样板

基本用法
```cpp
std::expected<int, std::string> parse(std::string_view s);
auto r = parse("42");
if(r) use(*r); else log(r.error());
```

组合
- 可与 Ranges 结合逐步传播错误；或封装 helper 将错误映射/附带上下文

建议
- 库边界返回 expected；应用层统一在边界转换为异常/错误码
