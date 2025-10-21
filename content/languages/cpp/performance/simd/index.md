---
title: 向量化（SIMD / std::simd）
---

概览
- 自动向量化：编译器基于循环分析自动生成 SIMD 指令（需可分析、数据对齐友好）
- 显式向量化：`std::simd`（并行 TS / C++23 迈进中，具体以库实现为准）、编译器内建/平台 intrinsic

最佳实践
- 数据布局：AoS→SoA，减少分支与别名冲突（strict aliasing）
- 对齐与加载：`std::assume_aligned`/`std::align`，避免跨 cacheline 访问
- 与并行算法/执行策略结合：`std::transform(std::execution::par_unseq, ...)`
- 基准验证：不同架构（AVX2/AVX-512/NEON）性能差异显著

注意事项
- ABI 与回退路径：在不支持指令集的机器上提供标量实现
- 浮点与舍入：向量化后数值结果可能与标量略有不同（结合 ULP 校验）
