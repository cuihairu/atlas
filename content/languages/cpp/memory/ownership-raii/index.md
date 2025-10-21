---
title: 所有权与 RAII（Resource Acquisition Is Initialization）
---

原则
- 资源（内存、文件句柄、互斥量等）应绑定到对象生命周期（构造获取、析构释放）
- 以值语义组合资源包装（`unique_ptr`/自定义 guard），Rule of Zero

示例：文件句柄封装
```cpp
class File{
  FILE* f{};
public:
  explicit File(const char* path, const char* mode){ f = fopen(path, mode); }
  ~File(){ if(f) fclose(f); }
  File(File&& o) noexcept: f(std::exchange(o.f, nullptr)){}
  File& operator=(File&& o) noexcept{ if(this!=&o){ if(f) fclose(f); f=std::exchange(o.f,nullptr);} return *this; }
  File(File const&) = delete; File& operator=(File const&) = delete;
};
```

最佳实践
- 构造总是建立有效不变式（或抛异常）；析构不抛
- 以组合代替继承重用资源；提供 `swap` 与移动语义
- 临界区使用 `lock_guard`/`unique_lock` 保证异常安全
