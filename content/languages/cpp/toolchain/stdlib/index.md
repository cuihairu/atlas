---
title: 标准库实现（libstdc++ / libc++ / MSVC STL）
---

差异与选择
- ABI：不同实现的容器/算法细节不同；跨平台二进制需注意 ABI 稳定策略（PImpl/可见性）
- 特性滞后：新标准特性落地时间不同；可在 CI 中启用多实现测试
- 工具：libc++ 更配合 Clang，libstdc++ 是 GCC 默认，MSVC STL 面向 Windows 生态

建议
- 跨平台库暴露稳定 C 接口或 PImpl 隔离；内部可自由选择实现
- 使用 feature test macros（`__cpp_*`）按特性分支，而非按编译器名称
