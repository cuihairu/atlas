---
title: 性能分析（perf / VTune / heaptrack）
---

采样分析
- Linux `perf`：火焰图（FlameGraph）定位热点；需要调试符号
- VTune：CPU/内存/并发分析；Windows/Linux 皆可

内存分析
- heaptrack/massif：分配热点、峰值内存
- Address/Leak Sanitizer 在 Dev 阶段发现越界/泄漏

建议
- 先用采样找到 20% 热点，再针对性 micro-benchmark/优化
