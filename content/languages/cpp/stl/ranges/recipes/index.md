---
title: Ranges 组合示例（zip/chunk/slide/join）
---

zip：并行遍历
```cpp
for(auto [a,b] : std::views::zip(v1, v2)){}
```

chunk：分块处理
```cpp
for(auto block : v | std::views::chunk(1024)){
  // block 是一个子 range
}
```

slide：滑动窗口
```cpp
for(auto win : v | std::views::slide(3)){
  // win[0], win[1], win[2]
}
```

join：扁平化
```cpp
auto flat = vec_of_vec | std::views::join;
```

注意
- 悬垂：组合的 view 不拥有数据；只在底层容器存活期间使用
- 复杂度：确保组合不会意外退化；必要时物化 `ranges::to<std::vector>`（C++23）
