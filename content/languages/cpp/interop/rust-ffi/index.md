---
title: 与 Rust 互操作（C ABI / 所有权）
---

要点
- 以 C ABI 作为桥接：Rust `extern "C"` 与 C++ `extern "C"`
- 明确所有权：谁分配谁释放；传递裸指针/句柄而非复杂对象

建议
- 用 FFI-safe 结构；在边界处转换为各自类型
