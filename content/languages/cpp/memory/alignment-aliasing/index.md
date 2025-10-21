---
title: 对齐/别名/strict aliasing
---

对齐
- `alignof/alignas` 指定/查询对齐；按平台 ABI 对齐分配
- 未对齐访问可能降速甚至未定义行为（某些架构）

严格别名规则（strict aliasing）
- 禁止通过不兼容类型别名访问对象，除少数例外（`char/std::byte`）
- 合法类型重解释：`memcpy` 或 `std::bit_cast`（C++20）

示例：类型双关的正确做法
```cpp
float f=1.0f; uint32_t u; std::memcpy(&u, &f, sizeof u); // OK
```

最佳实践
- 以 `std::bit_cast`/`memcpy` 做类型双关；避免 `reinterpret_cast` 读写对象
- 保持对齐：使用对齐分配或 `std::assume_aligned` 提示编译器
