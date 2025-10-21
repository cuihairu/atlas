---
title: 内存与资源管理（RAII/智能指针/分配器/PMR）
---

导航
- 所有权与 RAII：`memory/ownership-raii`
- 智能指针：`memory/smart-pointers`
- 分配器：`memory/allocators` · PMR：`memory/pmr`
- 生命周期与悬垂：`memory/lifetime`
- 对齐与别名：`memory/alignment-aliasing`

建议
- 值/RAII 优先；拥有语义用 `unique_ptr`，共享慎用 `shared_ptr`
- Arena/PMR 优化短生命周期场景；遵守 strict aliasing 与对齐规则
