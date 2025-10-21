---
title: Lambda 表达式
---

捕获与生命周期
- 按值 `[=]` / 按引用 `[&]` / 指定捕获 `[x, &y]` / 初始化捕获 `[ptr = std::move(p)]`
- 捕获 `this`：`[this]` 捕获指针；`[*this]`（C++17）按值拷贝对象
- 移动捕获：配合 `std::unique_ptr` 等移动语义

泛型 Lambda 与模板参数（C++14/20）
- C++14：`[](auto x){...}`
- C++20：`<typename T>` 参数列表 `[]<class T>(T x){...}`，支持约束

可变与返回类型
- `mutable` 允许修改按值捕获的副本
- 指定返回类型：`auto(x)->Ret { ... }` 或依赖推导

可转换与存储
- Lambda 可隐式转换为函数指针（无捕获时）
- 有捕获的 Lambda 是闭包对象，可存储于 `std::function` 或模板参数

示例
```cpp
auto gen = [i=0]() mutable { return i++; };
int a=gen(); // 0
int b=gen(); // 1
```

最佳实践
- 捕获最小化，避免悬垂引用；在异步/协程场景优先按值或 `[*this]`
- 复杂 Lambda 拆出具名函数对象，便于测试与复用
- 与 Ranges 管道组合，提升可读性
