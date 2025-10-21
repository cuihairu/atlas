---
title: 类型与限定符（cv/ref、constexpr）
---

内建与复合类型
- 标量：`bool`、整数（带符号/无符号、宽度 `char/short/int/long/long long`）、浮点（`float/double/long double`）
- 复合：指针/引用 `T* / T& / T&&`，数组 `T[N]`，函数类型与指针，成员指针 `T C::*`
- 枚举：`enum` / `enum class`（强作用域与强类型）

cv 限定与顶层/底层 const
- 顶层 `const`：限定对象自身，不影响指向对象的可变性
- 底层 `const`：通过指针/引用观察到的 `const`
```cpp
int i=0; const int ci=0;        // 顶层 const
int* p=&i; const int* q=&i;     // q: 指向 const int（底层）
int* const cp=&i;               // cp: const 指针（顶层）
```

引用与引用折叠
- 左值引用 `T&`、右值引用 `T&&`
- 转发引用（万能引用）：模板形参推导时的 `T&&`
- 折叠规则：`& &`/`& &&`/`&& &` → `&`；`&& &&` → `&&`

`constexpr/consteval`
- `constexpr`：在常量上下文可求值；可用于变量/函数/构造
- `consteval`：立即函数，必须在编译期求值
```cpp
constexpr int f(int x){ return x*2; }
consteval int g(int x){ return x*3; }
static_assert(f(2)==4);
```

类型推导
- `auto` 丢顶层 const；`auto&/auto&&` 保留引用
- `decltype(x)` 遵循标识符规则；`decltype((x))` 保留值类别
```cpp
const int x=0; auto a=x;        // a: int
decltype(x) b=x;                // b: const int
int y=0; decltype((y)) r=y;     // r: int&
```

最佳实践
- 默认 `const`，表达只读用 `string_view`/`span`
- 以值/智能指针表达所有权；指针/引用表达借用
- 能 `constexpr` 尽量 `constexpr`，避免宏常量
