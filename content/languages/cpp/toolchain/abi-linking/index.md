---
title: ABI / 链接 / ODR（接口稳定与可见性）
---

ODR（One Definition Rule）
- 违反 ODR 会导致未定义行为（不同 TU 定义不一致）
- inline/模板在多个 TU 合成，必须一致

符号可见性
- 默认隐藏：`-fvisibility=hidden`；对外 API 加 `__attribute__((visibility("default")))` 或宏
- Windows 上用 `__declspec(dllexport/dllimport)`；建议封装成跨平台宏

ABI 稳定策略
- PImpl 隔离实现细节；避免在公开头暴露 STL 容器/模板细节（编译器/库差异）
- 语义兼容：新增字段放尾部、提供兼容构造/默认值
- 版本脚本（ELF）：控制导出符号集合

链接器与优化
- 选择合适链接器（lld/gold/ld）；LTO/ThinLTO 与跨 TU 优化
- 延迟加载与分离调试符号（.dSYM/.pdb/.debug）
