---
title: CMake 实践（Modern CMake）
---

最小示例
```cmake
cmake_minimum_required(VERSION 3.25)
project(demo LANGUAGES CXX)
set(CMAKE_CXX_STANDARD 20)
set(CMAKE_CXX_STANDARD_REQUIRED ON)

add_library(core STATIC src/core.cpp)
target_include_directories(core PUBLIC include)
target_compile_features(core PUBLIC cxx_std_20)

add_executable(app src/main.cpp)
target_link_libraries(app PRIVATE core)
```

安装与导出（Config 包）
```cmake
install(TARGETS core app
  EXPORT demoTargets
  RUNTIME DESTINATION bin
  LIBRARY DESTINATION lib
  ARCHIVE DESTINATION lib
  INCLUDES DESTINATION include)

install(DIRECTORY include/ DESTINATION include)
install(EXPORT demoTargets NAMESPACE demo:: DESTINATION lib/cmake/demo)
include(CMakePackageConfigHelpers)
configure_package_config_file(cmake/demoConfig.cmake.in
  OUTPUT_FILE ${CMAKE_CURRENT_BINARY_DIR}/demoConfig.cmake
  INSTALL_DESTINATION lib/cmake/demo)
install(FILES ${CMAKE_CURRENT_BINARY_DIR}/demoConfig.cmake DESTINATION lib/cmake/demo)
```

最佳实践
- 以“目标（target）为中心”：`target_*` 指令表达接口与依赖（PUBLIC/PRIVATE/INTERFACE）
- 不全局污染：少用 `include_directories/add_definitions`，改用 `target_*`
- 特性驱动：`target_compile_features(... cxx_std_20)`；按目标指定标准
- 按配置与平台：生成器表达式 `$<CONFIG:Release>`、`$<PLATFORM_ID:Linux>`
- 第三方依赖：优先 `find_package`（Config 模式）；其次 `FetchContent`；最后外部构建
- 导出与安装：`install(TARGETS ...)` + `export` + `configure_package_config_file`

常见问题
- Debug/Release 混链（vcpkg 多配置生成器需注意）
- 传递宏/包含路径未区分可见性导致 ODR/ABI 问题
- 使用 `file(GLOB)` 失去增量构建精确性（生成器特性已改善，但仍建议列举源码）
