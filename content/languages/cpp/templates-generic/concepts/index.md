---
title: Concepts 约束（requires/标准概念）
---

概念定义与用法
```cpp
#include <concepts>

template<class T>
concept Hashable = requires(T a){ { std::hash<T>{}(a) } -> std::convertible_to<size_t>; };

template<Hashable T>
size_t h(T const& x){ return std::hash<T>{}(x); }
```

标准概念
- `same_as/derived_from/convertible_to`
- 迭代器：`input_iterator/random_access_iterator`
- 范围：`range/view/sized_range`

约束排序与可见性
- 更强的概念更特化；当候选同价时仍遵循常规重载规则
- 概念需在使用点前可见；分层概念有助于复用

最佳实践
- 用概念约束模板，提高诊断质量并缩短错误链
- 在库边界定义自有概念，内聚语义；避免滥用过宽概念导致二义
