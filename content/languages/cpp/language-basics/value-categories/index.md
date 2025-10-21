---
title: 值类别与引用
---

值类别
- lvalue：有身份（可取地址）的对象，如具名变量
- prvalue：纯右值，表达式产生的临时值（C++17 起大多“物化”到临时对象）
- xvalue：将亡值，常见于 `std::move(x)` 的结果

引用绑定规则
- `T&` 只能绑定 lvalue；`const T&` 可延长临时寿命
- `T&&` 绑定 xvalue/prvalue；“转发引用”`T&&`（模板推导）可绑定任意值类别
```cpp
int x=0; const int& r=42;  // 临时寿命延长
int&& rx = std::move(x);   // xvalue
```

转发与完美转发
- 保持值类别的转发行：`std::forward<T>(t)`
- 引用折叠配合万能引用，构建“转发构造/工厂”
```cpp
template<class T, class...Args>
T make(Args&&... args){ return T(std::forward<Args>(args)...); }
```

临时物化（C++17）
- 大多数 prvalue 在需要对象处会物化为临时，延长到全表达式结束
- 寿命延长：仅 `const&` 绑定；`&&` 绑定临时不延长寿命

易错点
- 绑定到 `const T&` 的临时延长寿命，但引用到子对象（`.member`）不一定安全
- 返回引用需指向仍然存活的对象；不要返回局部变量引用
- 滥用 `std::move` 使对象过早进入“已移走”状态
