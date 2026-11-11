cmake_minimum_required(VERSION 3.16)
project(atlas-sdk-cpp CXX)

add_library(atlas_sdk src/client.cpp)
target_compile_features(atlas_sdk PUBLIC cxx_std_17)
# Vendored single headers (third_party/) ship with the SDK — no external
# dependencies beyond a C++17 compiler and threading.
target_include_directories(atlas_sdk PUBLIC ${CMAKE_CURRENT_SOURCE_DIR}/include
                                            ${CMAKE_CURRENT_SOURCE_DIR}/third_party)
find_package(Threads REQUIRED)
target_link_libraries(atlas_sdk PUBLIC Threads::Threads)

option(ATLAS_SDK_BUILD_TESTS "Build Atlas C++ SDK tests" OFF)
option(ATLAS_SDK_WARNINGS_AS_ERRORS "Treat compiler warnings as errors" OFF)

if(CMAKE_CXX_COMPILER_ID MATCHES "GNU|Clang")
    target_compile_options(atlas_sdk PRIVATE -Wall -Wextra)
    if(ATLAS_SDK_WARNINGS_AS_ERRORS)
        target_compile_options(atlas_sdk PRIVATE -Werror)
    endif()
endif()

if(ATLAS_SDK_BUILD_TESTS)
    enable_testing()
    add_subdirectory(tests)
endif()
