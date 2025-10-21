---
title: 常用编译 / 链接选项
---

编译选项
- 标准：`-std=c++20/23`；优化：`-O2/-O3/-Ofast`（谨慎）
- 调试：`-g`；符号裁剪：`-g1/-gline-tables-only`（Clang）
- 警告：`-Wall -Wextra -Werror -Wpedantic`；再加 `-Wconversion -Wshadow`（Clang）
- LTO：`-flto`；PGO：`-fprofile-generate/-fprofile-use`

链接选项
- 链接器：`-fuse-ld=lld/gold`；`-Wl,--as-needed` 减少无用依赖
- 导出控制：`-fvisibility=hidden` + 显式可见性

运行时
- 异常：`-fno-exceptions`（仅当架构允许）；RTTI：`-fno-rtti`（小心多态）

实践
- 区分 Release/RelWithDebInfo/Debug 配置；为生产保留必要符号
