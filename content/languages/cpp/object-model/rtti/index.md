---
title: RTTI 与动态类型（typeid/dynamic_cast）
---

RTTI 工具
- `typeid(expr)`：获取动态类型 `type_info`（需有虚函数的多态类型）
- `dynamic_cast<T*>(p)`：安全向下转型，失败返回 `nullptr`；引用失败抛 `bad_cast`

何时使用/避免
- 业务分派：优先虚函数/访问者/`std::variant` 模式，减少 RTTI 分支
- 反序列化/插件：在边界处可使用 RTTI 校验类型一致性

替代方案
- `std::variant` + `std::visit`
- 自定义标签/枚举 + 显式分派（更可控）

最佳实践
- 在需要的边界使用 RTTI，不将其渗透到核心路径
- 保持接口稳定：面向抽象编程，避免对具体派生类进行 `dynamic_cast`
