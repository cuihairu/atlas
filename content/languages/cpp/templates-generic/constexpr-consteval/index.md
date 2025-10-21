---
title: constexpr / consteval
---

constexpr
- 表达式在常量上下文可求值；可用于变量、函数、构造函数
- 运行期也可调用，具备双态

consteval（C++20）
- 立即函数，必须在编译期求值；不能在运行期调用
- 典型用于生成元数据/校验
```cpp
consteval int id(){ return 42; }
constexpr int v = id(); // OK
// int r = id(); // 错：运行期上下文
```

实践建议
- 能编译期计算的均考虑 `constexpr`；性能与可验证性更好
- 谨慎使用 `consteval`，仅用于必须编译期的场景（如生成查表）
