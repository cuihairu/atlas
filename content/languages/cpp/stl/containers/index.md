---
title: 容器（vector/map/unordered_map 等）
---

分类与选择
- 顺序：`vector/deque/list/forward_list`；字符串：`string/string_view`；
- 关联：`set/map/multiset/multimap`（红黑树，有序）；无序：`unordered_*`（哈希）
- 容器适配：`queue/stack/priority_queue`

迭代器/引用失效（简表）
- `vector`：增长引起扩容，全部失效；非 erase 的尾部 push 不使已有引用失效（除重分配）
- `deque`：两端插入不使现有引用失效（块结构），中间插入可能失效
- `list`：稳定迭代器/引用；splice 常数复杂度
- `map/set`：稳定迭代器/引用；`erase` 使被删元素失效

复杂度与内存
- `vector` 连续存储、良好局部性；`list` 指针开销大、差局部性
- 有序关联容器对数时间；无序容器均摊常数（负载因子/哈希质量影响）

最佳实践
- 缓存友好路径优先 `vector`；需要稳定迭代器才考虑 `list`
- `reserve` 预留容量降低重分配；
- 键大型时用 `unordered_map` + 预留；可自定义哈希/等价关系
- C++17 `try_emplace`/`insert_or_assign`，减少拷贝
