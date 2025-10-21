---
title: 语言基础
---

本节聚焦“语言本体”的核心规则：类型系统、初始化、值类别与引用、函数与重载、名字查找/ADL、转换规则、枚举与位域、Lambda。建议按顺序阅读，配合章节内的“易错点”和“最佳实践”。

导航
- [[types-and-qualifiers|类型与限定符]]：内建类型、cv/ref 限定、`constexpr/consteval`
- [[initialization|初始化]]：`= () {}` 以及零/值/默认/聚合，缩窄规则
- [[value-categories|值类别与引用]]：lvalue/xvalue/prvalue、引用绑定与寿命延长
- [[functions-overload|函数与重载]]：重载决议、默认实参、成员函数 ref-限定
- [[name-lookup-adl|名字查找与 ADL]]：非限定查找、实参相关查找、`using`、隐藏友元
- [[conversions|转换规则]]：内建/用户自定义转换、`explicit`、列表初始化与缩窄
- [[enum-bitfields|枚举与位域]]：`enum class`、底层类型、位掩码与位域声明
- [[lambdas|Lambda]]：捕获、泛型 Lambda、init-capture、`mutable`、`[*this]`

实践建议（总览）
- 缩小可变范围：`const` 默认、`constexpr` 能常量就常量；接口以值传递 + RVO
- 优先列表初始化 `{}`，可避免隐式缩窄；需要显式时再用 `()`
- 避免悬垂：理解 prvalue 物化、引用绑定寿命延长的边界
- 明确重载：为隐式转换成本设置护栏（`explicit`、避免易混重载如 `bool`/`int`）
- 设计扩展点：基于 ADL 的自由函数放在参与类型同一命名空间；必要时“隐藏友元”
