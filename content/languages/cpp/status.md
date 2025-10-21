---
title: 完成度清单（基础部分）
---

概览：本页跟踪 C++ 基础内容的完成度与后续计划。

已完成（可用）
- 语言基础 / 对象模型 / 模板与泛型 / 内存与资源 / 错误与异常 / STL / 并发与并行 / I/O 与网络 / 构建与测试 / 诊断与分析 / 互操作 / 规范与惯用法 / 版本索引
- RPC：demo / envelope / router / client / payload / full‑stack / metrics‑limits / lib‑skeleton / examples/rpc‑coro
- 线程池：固定池骨架、work‑stealing 设计与可运行最小例、bench 指标建议

待深化（建议）
- 协程 ↔ future 桥接（已提供示例，待接入通用执行器与取消）
- STL 专题：regex/format/random 增加性能对比与典型坑示例
- Proto demo：生成脚本与 examples/ 一键构建
- CI 模板：vcpkg/Conan 缓存与 Sanitizers/TSan 矩阵示例工作流

备注：如有你希望优先补充的章节，可在此勾选或留言，我将按优先级推进。
