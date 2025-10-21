---
title: 最小可复用库模板（安装导出 + Package Config）
---

目录结构
```
demo/
  include/demo/core.hpp
  src/core.cpp
  CMakeLists.txt
  cmake/demoConfig.cmake.in
```

CMakeLists.txt 关键片段
```cmake
cmake_minimum_required(VERSION 3.25)
project(demo LANGUAGES CXX)
add_library(demo-core src/core.cpp)
target_include_directories(demo-core PUBLIC $<BUILD_INTERFACE:${CMAKE_CURRENT_SOURCE_DIR}/include>
                                        $<INSTALL_INTERFACE:include>)
target_compile_features(demo-core PUBLIC cxx_std_20)

install(TARGETS demo-core EXPORT demoTargets
        RUNTIME DESTINATION bin LIBRARY DESTINATION lib ARCHIVE DESTINATION lib
        INCLUDES DESTINATION include)
install(DIRECTORY include/ DESTINATION include)
install(EXPORT demoTargets NAMESPACE demo:: DESTINATION lib/cmake/demo)

include(CMakePackageConfigHelpers)
configure_package_config_file(cmake/demoConfig.cmake.in
  OUTPUT_FILE ${CMAKE_CURRENT_BINARY_DIR}/demoConfig.cmake
  INSTALL_DESTINATION lib/cmake/demo)
install(FILES ${CMAKE_CURRENT_BINARY_DIR}/demoConfig.cmake DESTINATION lib/cmake/demo)
```

demoConfig.cmake.in（最小）
```cmake
@PACKAGE_INIT@
include("${CMAKE_CURRENT_LIST_DIR}/demoTargets.cmake")
```

消费方示例
```cmake
find_package(demo CONFIG REQUIRED)
add_executable(app main.cpp)
target_link_libraries(app PRIVATE demo::demo-core)
```

建议
- 配合 vcpkg/Conan 生成二进制包或注册表；在 CI 中 matrix 构建三大编译器
- 导出可见 API，`-fvisibility=hidden` + 显式可见符号，保持 ABI 稳定
