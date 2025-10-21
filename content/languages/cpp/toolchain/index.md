---
title: 工具链与链接/ABI（编译器/标准库/可见性）
---

概览
- 编译器：GCC/Clang/MSVC；标准库实现：libstdc++/libc++/MSVC STL
- 平台差异影响 ABI/链接；在 CI 中矩阵测试并固定 toolchain

组成
- 编译器与选项：见 `toolchain/compilers`、`toolchain/flags`
- ABI/链接/ODR/可见性：见 `toolchain/abi-linking`
- 标准库选择：见 `toolchain/stdlib`

实践
- 以 CMake toolchain file 统一配置；导出/安装 Config 包，明确可见 API
- 生产构建：Release/RelWithDebInfo；必要时开启 LTO/PGO
