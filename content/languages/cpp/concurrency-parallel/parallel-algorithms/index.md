---
title: 并行算法与执行策略（execution policies）
---

策略
- `std::execution::seq/par/par_unseq/unseq` 控制并行/向量化
- `par_unseq` 允许重排与 SIMD；需避免数据竞争与外部副作用

示例
```cpp
auto sum = std::transform_reduce(std::execution::par,
  v.begin(), v.end(), 0, std::plus<>{}, [](int x){ return x*x; });
```

建议
- 对 CPU 友好容器（vector）收益明显；list 等不适
- 与 Ranges 管道/投影组合，提升可读性
