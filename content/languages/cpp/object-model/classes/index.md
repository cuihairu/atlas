---
title: 类与成员（特殊成员/Rule of Zero/Five）
---

特殊成员函数
- 默认构造、拷贝构造/赋值、移动构造/赋值、析构
- 生成/禁用规则：声明任何拷贝/移动会影响其他默认生成；`=default`/`=delete` 显式指定
```cpp
struct X{
  X() = default;               // 默认构造
  X(X const&) = delete;        // 禁拷贝
  X(X&&) noexcept = default;   // 移动构造
  ~X() = default;              // 析构
};
```

Rule of Zero / Five
- Zero：资源交由成员（如 `unique_ptr`）管理，不自定义析构/拷贝/移动
- Five：确需自管资源，成组定义拷贝/移动/析构，确保异常安全与强保证

显式构造与转换
- `explicit` 构造避免隐式转换；`explicit(true)`（C++20）可按条件显式
- 转换运算符 `operator T()` 建议慎用，优先 `explicit` 或命名函数

访问控制与友元
- `public/protected/private`；友元函数/类仅授予有限访问，不改变封装边界
- “隐藏友元”模式与 ADL 配合，避免全局污染

成员函数限定
- `const`/`&`/`&&` ref-限定区分只读与消耗语义；`noexcept` 对 ABI/优化有帮助

最佳实践
- 优先 Rule of Zero；必须自管资源时，先写拷贝，再写移动，并做自赋值与异常路径测试
- 提供 `swap`（非成员）与强异常安全；考虑 PImpl 降低构建依赖
- 面向接口编程：对外以最小表面 API 暴露必要能力
