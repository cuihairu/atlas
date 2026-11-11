cmake_minimum_required(VERSION 3.16)
project(atlas-example-cpp CXX)

set(ATLAS_SDK_BUILD_TESTS OFF CACHE BOOL "" FORCE)
add_subdirectory(${CMAKE_CURRENT_SOURCE_DIR}/../../sdk/cpp atlas_sdk_build)

add_executable(atlas_example_cpp main.cpp)
target_compile_features(atlas_example_cpp PRIVATE cxx_std_17)
target_link_libraries(atlas_example_cpp PRIVATE atlas_sdk)
