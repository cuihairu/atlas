---
title: C++23 特性（精选）
---

语言/库精选
- `std::expected`：结果或错误的统一返回
- `std::print`/`std::format` 增强：简化输出
- Ranges 扩展：`views::zip`/`chunk`/`slide` 等
- `mdspan`（多维视图）：数值计算/图像等领域
- `std::move_only_function`：仅移动的类型擦除可调用
- `flat_map/flat_set`（有实现进入提案/TS，实际以库为准）

实践建议
- 逐步引入 `expected` 替代错误码/异常在内部层的组合
- 在性能敏感路径采用 `mdspan`/`zip` 组合 + `par_unseq` 算法
