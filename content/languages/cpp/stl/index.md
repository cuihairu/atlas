---
title: 标准库（STL）概览
---

导航
- 容器：`stl/containers` · 迭代器：`stl/iterators` · 算法：`stl/algorithms`
- Ranges：`stl/ranges`（views/recipes/par‑unseq）
- 字符串：`stl/strings` · I/O/格式化：`stl/io-format` · 文件系统：`stl/filesystem`
- 时间：`stl/chrono` · 正则：`stl/regex` · 随机：`stl/random`

建议
- 优先 `vector` 与算法/Ranges；避免手写循环与低效容器
- 接口以 `string_view/span` 表达只读视图；异常安全与复杂度保证要在注释中声明
