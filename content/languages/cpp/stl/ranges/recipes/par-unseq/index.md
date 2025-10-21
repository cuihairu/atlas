---
title: Ranges × 并行算法（par_unseq 管道）
---

示例：过滤 + 映射 + 并行归约
```cpp
#include <ranges>
#include <execution>
#include <numeric>
int sum = std::transform_reduce(std::execution::par_unseq,
  v.begin(), v.end(), 0,
  std::plus<>{},
  [](int x){ return (x%2==0) ? x*x : 0; });
```

注意
- `par_unseq` 允许重排/向量化：禁止数据竞争/外部副作用
- 对 cache 友好的容器（`vector`）收益更明显；`list` 等不适
- 与 ranges 组合：先用 view 取子序列，再 `ranges::fold`（C++23）或回退 `transform_reduce`
