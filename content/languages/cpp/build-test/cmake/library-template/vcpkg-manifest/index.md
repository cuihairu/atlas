---
title: vcpkg 清单（manifest）与注册表
---

最小 vcpkg.json
```json
{
  "name": "demo",
  "version-string": "0.1.0",
  "dependencies": ["fmt", {"name":"gtest", "default-features": false}]
}
```

CMake 工具链集成
```cmake
-DCMAKE_TOOLCHAIN_FILE=$VCPKG_ROOT/scripts/buildsystems/vcpkg.cmake
```

版本与注册表
- 版本锁定：`builtin-registries` 或自建 registry，`"overrides"` 指定版本
- 多配置生成器（MSVC）：同一安装根可放 Debug/Release 两套二进制

CI 提示
- 缓存 vcpkg 安装与二进制缓存（`VCPKG_DEFAULT_BINARY_CACHE`）
- 矩阵构建（GCC/Clang/MSVC），以 toolchain file 保持一致
