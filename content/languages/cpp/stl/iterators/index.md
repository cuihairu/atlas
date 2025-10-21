---
title: 迭代器（类别/适配器/失效）
---

迭代器类别
- input/output/forward/bidirectional/random_access/contiguous（C++20）
- 算法根据类别选择实现（如双端扫描、二分等）

适配器
- 插入：`back_inserter/front_inserter/inserter`
- 流/istream 迭代器；`move_iterator`（移动迭代）

失效规则
- 容器操作引起的失效见容器章节；算法不应在同一范围内就地插入/删除

最佳实践
- 指定正确的迭代器类别/范围；对自定义容器提供合适的 iterator traits
- 尽可能使用 `ranges` 接口避免“begin/end”样板
