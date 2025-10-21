---
title: 模块（Modules）
---

概览
- 目的：取代传统“头文件 + 宏防护”的包含模型，解决 ODR/编译时间/可见性污染等问题
- 术语：接口单元（interface unit）、实现单元（implementation unit）、分区（partition）、头单元（header unit）

基本语法
```cpp
// math.ixx — 接口单元
export module math;
export int add(int a, int b);

// math_impl.cxx — 实现单元
module math;            // 实现同一模块
int add(int a, int b){ return a+b; }

// main.cpp — 使用方
import math;
int main(){ return add(1,2); }
```

分区（partition）与可见性
- `export module m:util;` 定义分区；`export import :util;` 在接口单元中再导出
- 实现分区仅在模块内部可见；接口分区可向外暴露

头单元（header unit）
- 将现有头映射为“可导入”的单元：`import <vector>;`（编译器/工具链实现相关）
- 与 `#include` 混用需小心 ODR 与编译顺序

构建与 BMI（编译器实现细节）
- 模块编译产生 BMI（binary module interface），构建系统需管理依赖与顺序
- CMake 支持（逐步完善，依编译器）：`CMAKE_EXPERIMENTAL_CXX_MODULE_CMAKE_API`/`target_sources(... FILE_SET CXX_MODULES ...)`

最佳实践
- 新库优先模块化接口（配合封装命名空间与显式导出），旧库可逐步头单元化
- 不在模块接口里暴露实现细节（私有 helper 放实现单元）
- 过渡期保留 `#include` 版本的头，提供双路径兼容

注意事项
- 编译器支持差异显著（Clang/GCC/MSVC 实装程度与命令行不同）
- 与 PCH/Unity Build/大规模增量构建的配合需要验证
