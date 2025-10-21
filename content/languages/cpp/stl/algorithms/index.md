---
title: 算法（执行策略/投影/稳定性）
---

执行策略（C++17）
- `std::execution::seq/par/par_unseq/unseq` 控制并行与向量化
- 注意副作用与数据竞争；`par_unseq` 允许重排

投影（projection, C++20）
- 允许在比较/变换前应用投影函数；简化 `std::sort` 对结构体按 key 排序
```cpp
std::sort(v.begin(), v.end(), {}, &Item::key); // by key
```

稳定性
- `stable_sort`/`stable_partition` 保持相对顺序，代价更高

Ranges 算法
- `std::ranges::sort/copy/for_each` 接受 range；与 `views` 组合更优雅

最佳实践
- 对可并行且无共享写的场景使用 `par`；对有向量化机会的使用 `par_unseq`
- 以投影简化比较器；对稳定性有要求选择 stable 版本
