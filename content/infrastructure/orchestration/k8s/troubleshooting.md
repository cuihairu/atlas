---
title: K8s 故障排查
---

症状 → 原因 → 诊断 → 修复：
- Pod Pending：资源不足/调度失败 → kubectl describe/node 资源 → 调整配额/亲和性
- CrashLoopBackOff：探针失败/配置错误 → kubectl logs/events → 修正环境变量/镜像/探针
- 网络异常：CNI/策略 → 检查 CNI 日志/网络策略 → 修复 CNI 配置/策略
