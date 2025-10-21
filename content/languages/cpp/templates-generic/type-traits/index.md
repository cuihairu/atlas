---
title: type_traits 与萃取（派发/重载控制）
---

编译期常量与型别
- `integral_constant<bool, v>` / `true_type/false_type`
- 常用萃取：`is_same/is_void/is_integral/is_trivially_copyable` 等
- 变换：`remove_cvref/add_pointer/make_signed/decay` 等

派发与重载控制
- tag dispatch：以 `std::true_type/false_type` 选择实现
- `enable_if`（C++11）与 `if constexpr`（C++17）简化条件编译
```cpp
template<class T>
auto size_bytes(T const& x){
  if constexpr (is_trivially_copyable_v<T>) return sizeof(T);
  else return x.size()*sizeof(typename T::value_type);
}
```

与 Concepts 协作
- traits 用于抽象“性质”；concepts 用于“接口能力”；二者可互相构建

最佳实践
- 组合 `type_traits` + `if constexpr` 表达分支；避免宏
- 保持 traits 的“语义稳定”，不要用易变实现细节
