---
title: CSV 数据管线（views + transform + filter）
---

场景
- 读取 CSV 文本，过滤无效行，提取列并聚合。

示例（简化，伪解析）
```cpp
#include <ranges>
#include <string>
#include <vector>
auto lines = read_all_lines("data.csv"); // std::vector<std::string>
auto pipeline = lines | std::views::filter([](auto const& s){ return !s.empty() && s[0] != '#'; })
                     | std::views::transform([](auto const& s){ return split(s, ','); })
                     | std::views::filter([](auto const& cols){ return cols.size()>=3; })
                     | std::views::transform([](auto const& cols){ return std::stod(cols[2]); });
double sum = 0; for(double v : pipeline) sum += v;
```

注意
- 视图不拥有数据：底层容器需保持存活
- 解析正确性：实际工程使用成熟 CSV 库；这里仅示意视图组合
- 性能：按需物化；与并行算法结合（`transform_reduce`）
