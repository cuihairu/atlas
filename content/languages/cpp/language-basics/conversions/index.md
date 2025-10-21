---
title: 隐式/显式转换
---

内建转换序列
- 整数提升：`char/short/bool` → `int/unsigned int`
- 数值转换：浮点↔整数、不同宽度间转换（可能溢出/舍入）
- 指针转换：`T*` → `const T*`（加 cv 合法，去 cv 非法）；派生→基类指针

用户自定义转换
- 转换构造：`X(T)`；转换运算符：`operator T()`
- `explicit`（C++11+）：仅允许显式转换（`static_cast`），不参与隐式
```cpp
struct X{ explicit X(int); explicit operator bool() const; };
X x1 = 1;          // 错：X(int) 为 explicit
X x2(1);           // OK
if(x2){}           // 错：operator bool 是 explicit
if(static_cast<bool>(x2)){} // OK
```

列表初始化与缩窄
- `{}` 禁缩窄：浮点→整数、超范围整数→更窄整数等
- 避免“意外的数据丢失”：优先 `{}`，必要时 `static_cast`

布尔与指针转换
- 布尔：多数类型可转换为 `bool`；避免与重载形成二义性
- 指针空值：用 `nullptr`（而非 `0`/`NULL`）；重载时区分指针/整数

实践建议
- 明确边界：对“昂贵或语义不明显”的转换声明为 `explicit`
- 为用户自定义类型避免“与算术类型的可疑重载”（如 `operator int()`）
- 必要时提供 `from_xxx` 静态工厂，替代隐式构造
