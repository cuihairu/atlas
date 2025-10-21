---
title: Ranges（视图/适配器/算法）
---

提要：C++20 Ranges 为“以懒计算为核心”的序列抽象。通过 `views` 组合（`filter/transform/take/drop` 等）构建流水线，与 `ranges::algo`/`ranges::begin/end` 协同；C++23 补充 `views::zip` 等。

核心要素
- 概念：`range`/`view`/`borrowed_range`/`input_range`...；迭代器命名与约束
- 视图（view）：轻量、不拥有、惰性；组合性强（管道 `|`）
- 适配器：`views::filter/transform/take/drop/chunk/slide/join/split` 等
- 算法：`std::ranges::sort/copy/for_each` 等直接接收 range
- 容器化：使用 `ranges::to`（C++23）或 `std::ranges::copy` + `back_inserter`

示例
```cpp
#include <ranges>
#include <vector>
#include <algorithm>
using namespace std;

vector<int> v{1,2,3,4,5,6};
auto pipeline = v | views::filter([](int x){return x%2==0;})
                   | views::transform([](int x){return x*x;});
vector<int> out;
ranges::copy(pipeline, back_inserter(out)); // out = {4,16,36}
```

最佳实践
- 以 view 组合为主、在边界处物化（减少拷贝/分配）
- 捕获生命周期：lambda 捕获引用时谨慎，避免悬垂引用（`borrowed_range` 概念能帮助推断）
- 可读性优先：长流水线适当拆分中间 view 变量
- 与协程/生成器配合：以 `views::transform`/`views::take_while` 等驱动上游事件流

常见陷阱
- `dangling`：对临时构造的 range 取迭代器/引用会悬垂
- 复杂度：某些 view 嵌套组合可能退化为 O(n^2) 访问；注意分析器与基准测试
- 拥有语义：需要拥有序列时，使用 `std::vector` 等容器或 `ranges::to`（C++23）
