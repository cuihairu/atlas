---
title: 随机与数值（random）
---

用法
- 引擎：`std::mt19937`（梅森旋转）、`std::random_device`（熵源，与平台相关）
- 分布：`uniform_int_distribution`、`normal_distribution` 等
```cpp
std::mt19937 rng(std::random_device{}());
std::uniform_int_distribution<int> dist(1,6);
int x = dist(rng);
```

建议
- 不要用 `rand()`；按需求选择分布；明确 seed 策略（可重复 vs 真随机）
