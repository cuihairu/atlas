---
title: Conan 配方与 profile（toolchain 整合）
---

最小 conanfile.txt
```
[requires]
fmt/10.2.1

[generators]
CMakeToolchain
CMakeDeps

[layout]
cmake_layout
```

命令
```
conan profile detect
conan install . --build=missing
cmake --preset conan-default
cmake --build --preset conan-default
```

建议
- 锁定 profile（编译器/标准库）与版本，确保 CI 可重现
- 结合 `CMakeDeps` 与 `find_package(fmt CONFIG REQUIRED)` 消费依赖
