---
title: 枚举与位域
---

枚举
- 非作用域枚举：`enum E {A,B};` 可隐式转整数；易与重载混淆
- 作用域枚举：`enum class Flags : unsigned { A=1, B=2, C=4 };` 强类型，推荐
- 位掩码操作：为 `enum class` 自定义按位运算符
```cpp
enum class F : unsigned { A=1, B=2, C=4 };
inline F operator|(F a, F b){ return F(unsigned(a)|unsigned(b)); }
inline bool any(F f){ return unsigned(f)!=0; }
```

位域（bit-field）
- 在 `struct` 中以固定宽度表示：`unsigned mode:3;`
- 警惕：实现相关的布局/对齐/溢出；跨平台序列化不建议使用位域
```cpp
struct S{
  unsigned a:3; // 0..7
  unsigned b:5; // 0..31
};
```

最佳实践
- 选用 `enum class`，指明底层类型与掩码值
- 运算符定义仅限于需要的按位操作；避免与算术重载混淆
- 二进制协议/持久化用明确的掩码字段，不依赖位域布局
