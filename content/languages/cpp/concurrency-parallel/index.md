---
title: 并发与并行（线程/同步/原子/算法/协程）
---

导航
- 内存模型：`concurrency-parallel/memory-model`
- 线程与取消：`concurrency-parallel/threads`
- 原子操作：`concurrency-parallel/atomics`
- 同步原语：`concurrency-parallel/sync`
- futures/async：`concurrency-parallel/futures-async`
- 并行算法：`concurrency-parallel/parallel-algorithms`
- 协程：`concurrency-parallel/coroutines`
- 任务系统/线程池：`concurrency-parallel/task-system`

建议
- 优先高层原语与结构化并发；以 stop_token 传递取消；在热点路径评估 atomics 与并行算法
