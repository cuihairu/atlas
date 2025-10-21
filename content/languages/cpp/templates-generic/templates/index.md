---
title: 模板/特化/实例化（两阶段查找）
---

基础
- 类/函数模板、非类型模板参数（NTTP）、默认模板参数
- 特化：全特化与偏特化（函数模板不支持偏特化，可用重载代替）

两阶段查找（two-phase lookup）
- 第一次在模板定义处进行依赖名解析，第二次在实例化处补全
- 依赖名需用 `typename`/`template` 消歧
```cpp
template<class T>
void f(typename T::value_type x){ /* ... */ }
```

实参与推导
- 函数模板可做模板实参推导（CTAD for class template 也可用）
- 默认模板实参与 ARITY 设计，避免复杂歧义

最佳实践
- 优先重载而非函数模板偏特化；类模板用偏特化
- 通过 traits/concepts 对模板进行约束，避免晚期实例化报错
