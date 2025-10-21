---
title: 编译器（GCC / Clang / MSVC）
---

标准与选项
- 语言版本：`-std=c++2b/c++23` 等；MSVC `/std:c++20`
- 诊断：`-Wall -Wextra -Wpedantic`（Clang 还可 `-Wconversion/-Wshadow`）

平台差异
- ABI 与标准库实现：libstdc++/libc++/MSVC STL
- 内联 ASM、属性与内建函数差异；跨平台需封装

链接与工具链
- LTO/PGO 支持差异与参数；跨平台链接器（ld/gold/lld）

最佳实践
- 在 CI 中矩阵测试三大编译器/多个版本；固定 docker 镜像或 toolchain file
- 用统一的工具链层（CMake 工具链文件）隔离差异
