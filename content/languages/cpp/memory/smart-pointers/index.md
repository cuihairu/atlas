---
title: 智能指针（unique_ptr/shared_ptr/weak_ptr）
---

unique_ptr
- 独占所有权，可移动不可拷贝；`make_unique` 优于 `new`
- 自定义删除器：`unique_ptr<T, Deleter>` 不改变大小（若 Deleter 空间优化）

shared_ptr / weak_ptr
- 引用计数共享；弱引用打破环；启用 `enable_shared_from_this`
- `make_shared` 合并分配，降低碎片

常见陷阱
- 循环引用：`shared_ptr` 成员互相持有 → 使用 `weak_ptr` 断环
- 从 `this` 构造 `shared_ptr` 导致“双控制块” → 用 `enable_shared_from_this`
- 自定义删除器捕获状态过大，导致 `unique_ptr` 变胖

实践建议
- 拥有语义用 `unique_ptr`；共享但不滥用 `shared_ptr`
- 接口参数：只读借用用裸指针/引用；转移所有权用 `unique_ptr&&`
