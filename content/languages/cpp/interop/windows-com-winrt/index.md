---
title: Windows（COM / WinRT）
---

要点
- COM：接口基于 `IUnknown`（QueryInterface/AddRef/Release），引用计数；IDL 定义
- WinRT：现代化的组件模型，语言投影更友好

建议
- 用智能指针（WRL/C++/WinRT）管理引用计数；明确线程模型（STA/MTA）
