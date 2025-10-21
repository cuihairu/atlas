---
title: Python 绑定（pybind11）
---

要点
- 模块定义 `PYBIND11_MODULE`；类/函数/枚举导出；异常映射
- 生命周期：`keep_alive`、智能指针与所有权边界

示例
```cpp
PYBIND11_MODULE(demo, m){ m.def("add", [](int a,int b){return a+b;}); }
```

建议
- 减少跨语言边界的对象共享；大数据传输用内存视图/零拷贝接口
