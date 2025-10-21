---
title: CI 要点（矩阵/缓存/消毒器）
---

矩阵
- 操作系统：Ubuntu/macOS/Windows；编译器：GCC/Clang/MSVC；C++ 标准：20/23
- 拉取 vcpkg/Conan 缓存；或自带 toolchain 容器

缓存
- vcpkg：`VCPKG_DEFAULT_BINARY_CACHE` 指向缓存目录
- Conan：`conan cache`；保留 `~/.conan2` 目录

消毒器（Sanitizers）
- ASan/UBSan：Dev/CI 默认开启；TSan 单独作业避免冲突
- artifact：保留崩溃日志/stacktrace/覆盖率报告

示例步骤（GitHub Actions 概要）
- checkout → 安装依赖 → 配置（toolchain）→ 构建 → 测试（ctest）→ 上传报告
