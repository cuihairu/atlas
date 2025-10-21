---
title: RVO/NRVO 与 LTO/PGO
---

RVO/NRVO
- RVO（返回值优化）：省略临时对象构造/析构；C++17 大多数场景强制执行
- NRVO：命名返回值优化（局部具名变量返回）受控制流影响，不一定触发

LTO（链接时优化）
- 跨翻译单元内联/去重；配置 `-flto`（GCC/Clang/MSVC 均支持）
- 注意与工具链（ar/ld/ranlib）的兼容；CMake 以 `INTERPROCEDURAL_OPTIMIZATION` 打开

PGO（性能导向优化）
- 两阶段：训练（生成 profile）+ 使用（根据 profile 做热路径优化）
- Clang：`-fprofile-instr-generate/use`；GCC：`-fprofile-generate/use`

实践建议
- Release 默认开 LTO，热点服务/算法再上 PGO
- 基于代表性负载训练 PGO，避免“过拟合测试”
- 与 Sanitizers/coverage 的相容性需在 CI matrix 中验证
