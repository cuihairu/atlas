---
title: 现代 C++ 惯用法（PImpl / CRTP / Type Erasure / SBO）
---

简述
- PImpl：隐藏实现，稳定 ABI，减少编译依赖
- CRTP：静态多态（compile-time mixin）
- Type Erasure：以 `std::function`/自定义 erase 实现运行时多态
- SBO：小对象放栈上，减少分配（如 `small_vector`）
