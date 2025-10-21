---
title: 分配器（Allocator）
---

基础
- 标准容器可注入分配器类型：`std::vector<T, Alloc>`
- 传播特性：`propagate_on_container_{copy,move,swap}_assignment`
- 状态：无状态/有状态；花式指针（fancy pointer）

实践
- 自定义分配器用于嵌入式/共享内存/计量等场景
- 结合 `pmr` 提供运行期选择的内存策略

注意事项
- 分配器类型参与容器类型：不同分配器的 vector 互不兼容
- 需要与容器的异常安全与强保证配合测试
