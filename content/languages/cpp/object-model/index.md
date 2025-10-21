---
title: 对象模型
---

本节覆盖 C++ 对象语义：类的特殊成员函数与可见性、继承体系与访问控制、虚函数与动态多态、RTTI 与类型识别，以及对象的创建/销毁/生命周期规则。

导航
- [[classes|类与成员]]：特殊成员（拷贝/移动/析构）、Rule of Zero/Five、友元/可见性
- [[inheritance|继承与可见性]]：public/protected/private、`using` 暴露、菱形与虚继承
- [[virtual-polymorphism|虚函数与多态]]：`virtual/override/final`、纯虚接口、对象切片
- [[rtti|RTTI 与动态类型]]：`typeid/dynamic_cast`、替代方案（访问者/variant）
- [[object-lifetime|对象生存期]]：存储期、构造与销毁顺序、寿命与别名、`std::launder`
