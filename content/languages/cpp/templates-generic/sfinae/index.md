---
title: SFINAE 与约束（void_t/检测惯用法/Concepts）
---

SFINAE（替换失败非错）
- 在模板实参推导中，替换失败不报错而是丢弃该候选
- `std::void_t`/检测惯用法（detection idiom）用于探测成员/表达式是否存在
```cpp
template<class, class=void>
struct has_reserve:false_type{};

template<class T>
struct has_reserve<T, void_t<decltype(declval<T&>().reserve(0))>>: true_type{};
```

C++20 Concepts/`requires`
- 用约束直接表达需要的语法/语义，优于 SFINAE 报错质量与可读性
```cpp
template<class T>
concept Reservable = requires(T x){ x.reserve(0); };

template<Reservable T>
void grow(T& x){ x.reserve(1024); }
```

排序与可用性
- 约束参与重载决议；更强约束更特化
- 将“基本约束”写在模板形参/函数上；复杂条件写 `requires` 表达式

实践建议
- 新代码优先 Concepts；保留 SFINAE 以兼容旧编译器
- 探测只写在接口边界；内部实现使用静态分派（if constexpr / 重载）
