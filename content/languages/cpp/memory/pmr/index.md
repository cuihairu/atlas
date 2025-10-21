---
title: 多态分配器（PMR, polymorphic_allocator）
---

核心概念
- `std::pmr::memory_resource`：运行期选择的内存资源接口
- `std::pmr::polymorphic_allocator<T>`：以资源驱动的分配器，可注入到容器
- 常用资源：`monotonic_buffer_resource`、`unsynchronized_pool_resource`

示例
```cpp
#include <memory_resource>
std::byte buf[4096];
std::pmr::monotonic_buffer_resource pool(buf, sizeof buf);
std::pmr::vector<int> v{&pool};
```

最佳实践
- 用于短生命周期、批量分配释放的场景（arena 模式）
- 注意资源生命周期 ≥ 所有使用它的容器/对象
- 与 `string/std::pmr::string` 等配套使用，避免混用不同分配域
