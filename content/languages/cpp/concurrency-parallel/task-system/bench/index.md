---
title: 线程池基准建议（quick bench）
---

指标
- 提交吞吐：tasks/s；延迟分布：p50/p95/p99
- 窃取次数、队列长度、任务执行/等待时间

基准方法
- 固定任务大小（无共享写）测最大吞吐
- 混合任务（CPU/IO）看调度效果；线程数与 CPU 数映射
- 绑定亲和性：减少跨 NUMA；对比开启/关闭

工具
- Google Benchmark + 自定义线程池 API
- perf/VTune/heaptrack 结合热点定位与分配分析
