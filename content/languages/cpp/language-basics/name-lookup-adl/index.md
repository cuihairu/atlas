---
title: 名字查找与 ADL
---

非限定查找（unqualified lookup）
- 在当前作用域、外层作用域、类与基类作用域中按规则查找
- `using` 声明/指令影响可见性；命名空间别名辅助组织

实参相关查找（ADL）
- 对函数调用 `f(a,b)`，将参与类型 `A,B` 的关联命名空间纳入查找
- 常用于运算符与对称自由函数（如 `operator<<`）
```cpp
namespace lib {
  struct W{};
  void print(W const&);          // 与 W 同一命名空间 → 可被 ADL 找到
}
void g(){ lib::W w; print(w); }  // 无需 lib:: 前缀
```

隐藏友元（hidden friend）
- 在类内声明为友元的自由函数，其可见性仅通过 ADL 暴露，避免全局污染
```cpp
struct V{
  friend bool operator==(V const&, V const&); // 仅通过 ADL 可见
};
```

最佳实践
- 扩展点放入参与类型命名空间；或使用“隐藏友元”
- 避免在 `std` 命名空间添加重载/特化（除标准允许的特化点）
- `using std::swap;` 后调用非限定 `swap(a,b)`，交给 ADL 选择自定义或 std 版本

常见问题
- 非限定查找 + 重载集合下的二义性；使用命名空间限定/`using` 进行消歧
- 把扩展函数放错命名空间导致 ADL 不生效
