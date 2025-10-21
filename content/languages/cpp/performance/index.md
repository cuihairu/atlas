title: 性能（移动语义 / RVO/LTO/PGO / SIMD / 缓存）
---

导航
- 移动语义与拷贝消除：`performance/move-semantics`
- RVO/NRVO 与 LTO/PGO：`performance/rvo-lto-pgo`
- 向量化（SIMD / std::simd）：`performance/simd`
- 缓存/分支预测/局部性：`performance/cache`

建议
- 基于基准与分析器定位，再做针对性优化（移动/RVO/布局/SIMD）
- 与并行算法/执行策略结合（`std::execution`），注意数据竞争与副作用
