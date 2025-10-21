---
title: 错误码 / 状态（std::error_code / error_category）
---

模型
- `std::error_code`（值 + `error_category`）表达可恢复错误；`std::error_condition` 做跨域映射
- I/O 与系统接口广泛使用 error_code；避免异常成本/跨边界传播

自定义错误类别
```cpp
struct my_category: std::error_category{
  const char* name() const noexcept override { return "my"; }
  std::string message(int ev) const override { return /*...*/; }
};
inline const my_category& my_cat(){ static my_category c; return c; }
inline std::error_code make_error_code(MyErr e){ return {int(e), my_cat()}; }
```

建议
- 库内：定义清晰的枚举错误 + category；提供 `make_error_code`
- 边界：在应用层统一记录/转换；与 `expected<T,E>` 协作更清晰
