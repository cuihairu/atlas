---
title: 生命周期与悬垂引用（dangling）
---

要点
- 非拥有视图（`string_view/span`）仅作观察，不延长寿命
- 容器操作可能使迭代器/引用失效：按容器规则表维护
- 返回引用需确保目标仍有效；不要返回局部变量引用

工具
- `gsl::not_null`/`owner<T*>` 明确所有权语义（第三方）
- `std::observer_ptr`（提案）/自定义观察指针

策略
- 明确所有权转移点，以值或 `unique_ptr` 表达
- 接口传参默认按值或 `string_view`/`span`，避免裸指针悬垂
