---
title: C++ Core Guidelines（摘要）
---

要点
- 以类型与不可变性保证正确性（`const` 默认）
- 以 RAII 和智能指针管理资源；避免裸 `new/delete`
- 接口显式、无二义；错误处理有约定（异常/expected）
- 以算法与 Ranges 表达意图；避免复杂宏
