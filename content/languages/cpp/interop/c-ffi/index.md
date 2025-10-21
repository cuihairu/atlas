---
title: C 互操作（extern "C" / ABI / 名称修饰）
---

要点
- `extern "C"` 关闭名称修饰，按 C ABI 导出/导入函数
- 结构体与内存布局保持兼容；避免在 C 接口中暴露 C++ 对象/异常

示例
```cpp
extern "C" int add(int a, int b);
```

建议
- 对外提供稳定 C API + 版本化；内部以 C++ 封装实现
