---
title: 移动语义与拷贝消除
---

要点
- 移动构造/赋值（`T(T&&)`/`operator=(T&&)`）转移资源所有权，避免深拷贝
- 返回值优化（RVO/NRVO）：编译器可省略临时构造；C++17 保证大多数场景的 RVO（强保证）
- 完美转发：`std::forward` 与万能引用，避免不必要拷贝

实践建议
- 接口以值返回（启用 RVO），必要时 `[[nodiscard]]`
- 容器原地构造：`emplace_*` 优于 `push_*`
- 自定义类型：遵循 Rule of Five/Zero；移动后对象保持可析构、可赋值的有效但未指定状态
- 避免滥用 `std::move` 破坏后续使用；结合 `std::exchange` 清晰表达

验证与基准
- 使用 `-fno-elide-constructors` 验证移动路径（仅限实验）
- 以 micro-benchmark（Google Benchmark）对照移动/拷贝成本
