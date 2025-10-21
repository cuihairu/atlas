---
title: 元编程（模板 / 常量计算 / 类型计算）
---

层次
- 模板元编程：类型层（traits/SFINAE）、值层（`std::integer_sequence`）
- `constexpr/consteval`：编译期计算与立即求值
- Concepts：以约束替代传统 SFINAE，提升可读性

工具
- 标准库：`type_traits`、`integer_sequence`、`tuple` 反射式操作
- 第三方：Boost.Mp11 等更现代的元函数集合

建议
- 以 Concepts 与 `if constexpr` 简化分支；限制元编程范围，避免过度复杂
