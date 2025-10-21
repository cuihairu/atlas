---
title: 包管理（Conan / vcpkg）
---

Conan 与 vcpkg 对比
- 模式：Conan 面向“工具链 + 二进制包”生态；vcpkg 强调“从源构建”（也支持二进制缓存）
- 清单：Conan `conanfile.(py/txt)`；vcpkg `vcpkg.json`（manifest）
- 版本与锁：Conan lockfile；vcpkg manifest + `versioning`（registries/版本约束）
- 集成：两者均有 CMake 工具链文件（`conan_toolchain.cmake` / `vcpkg.cmake`）

实践建议
- CI 复现：锁定工具链（编译器/标准库）+ 依赖版本（lockfile/registries）
- ABI 稳定性：跨编译器/标准库时注意 ABI 差异（GLIBC++ vs MSVC）
- 多配置生成器：vcpkg 与 MSVC/多配置构建配合良好；Conan 也支持 profiles
- 与 FetchContent：尽量避免混用“包管理”和“抓源码”导致的重复/冲突

入门步骤
- vcpkg：引入 `vcpkg.cmake` 工具链；在 `vcpkg.json` 列出依赖；CMake 配置时 `-DCMAKE_TOOLCHAIN_FILE=`
- Conan：使用 `conan profile detect`；`conan install` 生成工具链与缓存；CMake 配置时包含工具链
