cmake_minimum_required(VERSION 3.25)
project(rpc_coro LANGUAGES CXX)

set(CMAKE_CXX_STANDARD 20)
set(CMAKE_CXX_STANDARD_REQUIRED ON)

find_package(Boost REQUIRED)
find_package(nlohmann_json CONFIG QUIET)

add_library(rpc_coro_headers INTERFACE)
target_include_directories(rpc_coro_headers INTERFACE ${CMAKE_CURRENT_SOURCE_DIR}/include)
target_link_libraries(rpc_coro_headers INTERFACE Boost::boost)
if (nlohmann_json_FOUND)
  target_link_libraries(rpc_coro_headers INTERFACE nlohmann_json::nlohmann_json)
endif()

add_executable(server src/server.cpp)
target_link_libraries(server PRIVATE rpc_coro_headers)

add_executable(client src/client.cpp)
target_link_libraries(client PRIVATE rpc_coro_headers)

