---
title: Sanitizers（ASan/TSan/UBSan/MSan）
---

启用方式（GCC/Clang）
- AddressSanitizer：`-fsanitize=address -fno-omit-frame-pointer -O1~O2`
- ThreadSanitizer：`-fsanitize=thread`（禁与 ASan 同时）
- UndefinedBehavior：`-fsanitize=undefined`
- MemorySanitizer：`-fsanitize=memory`（需全量仪表、较重）

CMake 集成
```cmake
target_compile_options(tgt PRIVATE -fsanitize=address)
target_link_options(tgt PRIVATE -fsanitize=address)
```

最佳实践
- Dev/CI 默认开 ASan+UBSan；竞态相关回归用 TSan 专项跑
- 与 `-fno-omit-frame-pointer` 配合更好的栈回溯
- 生产场景可用 “ASan Lite”（Shadow 模式/低开多关）或仅在金丝雀上启用
- 动态库混用：所有目标需一致的 sanitizer 设置，避免链接期/运行期冲突

常见陷阱
- ODR/ABI 不一致导致 sanitizer 拦截器崩溃
- TSan 报告中的假阳性：注意原子/屏障与注解（`ANNOTATE_HAPPENS_BEFORE` 风格）
