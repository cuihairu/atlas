---
title: 错误与异常处理（异常/错误码/expected）
---

模型对比
- 异常：跨层传播、分离错误路径与主干逻辑；成本与可预测性需评估
- 错误码：显式返回，路径清晰；样板多；易被忽略
- `std::expected<T,E>`（C++23）：统一“结果或错误”，模式类似 Rust `Result`

建议
- 库层：抛出语义清晰的异常类型；或统一 `expected`（header-only 库友好）
- 应用层：边界处捕获并转换为错误码/日志；关键路径可关闭异常（`-fno-exceptions`）
- 约定：失败不改变对象不变式；异常安全等级（基本/强/不抛）

示例：`std::expected`
```cpp
#include <expected>
std::expected<int, std::string> parse(std::string_view s);
```
