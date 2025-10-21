---
title: 函数/重载/默认参数
---

重载决议（overview）
- 候选集：同名、可见且参数个数匹配的函数形成重载集
- 转换序列排序：精确匹配 > 提升（promotion）> 标准转换 > 用户自定义 > 省略号
- 模板与非模板：若都可行，优先非模板；模板之间按偏序更特化优先

成员函数 const/ref 限定
```cpp
struct S{
  int get() const;             // 只读对象可调用
  int get();                   // 非 const 对象优先匹配
  std::string str() &&;        // 仅右值（将亡）对象可调用
  std::string str() &;         // 仅左值对象可调用
};
```

默认实参与重载
- 默认实参在声明处给出；参与重载决议的是“是否提供实参”，而不是默认值本身
- 跨声明重复默认值会 ODR 冲突；将默认值仅放在头文件的一个声明上

抑制不期望的重载
- `explicit` 构造/转换运算符，避免隐式转换走错重载
- 删除函数：`void f(int)=delete;` 阻止错误调用路径
- SFINAE/Constraints（概述）：用概念/启用条件限定候选

示例：避免 `bool` 与整数重载混淆
```cpp
struct X{
  explicit X(int);     // 避免从 bool 隐式转入 X
};
void f(X); void f(bool);
// f(0) 走 f(bool)；f(1) 亦如此，若 X(int) 非 explicit 容易产生二义或混淆
```

实践建议
- 成员函数配合 `const`/ref-限定区分“读取 vs 消耗（move）”语义
- 避免“仅差一个整型宽度”的重载集合；倾向使用模板 + `std::enable_if`/概念
- 接口设计优先“显式、无歧义”的参数类型
