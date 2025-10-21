cmake_minimum_required(VERSION 3.25)
project(rpc_coro_proto LANGUAGES CXX)

set(CMAKE_CXX_STANDARD 20)
set(CMAKE_CXX_STANDARD_REQUIRED ON)

find_package(Boost REQUIRED)
find_package(Protobuf REQUIRED)

include_directories(${CMAKE_CURRENT_BINARY_DIR})

protobuf_generate_cpp(PROTO_SRCS PROTO_HDRS ${CMAKE_CURRENT_SOURCE_DIR}/proto/hello.proto)
add_library(proto-hello ${PROTO_SRCS} ${PROTO_HDRS})
target_link_libraries(proto-hello PUBLIC protobuf::libprotobuf)

add_library(rpc_headers INTERFACE)
target_include_directories(rpc_headers INTERFACE ${CMAKE_CURRENT_SOURCE_DIR}/include)
target_link_libraries(rpc_headers INTERFACE Boost::boost)

add_executable(server-proto src/server.cpp)
target_link_libraries(server-proto PRIVATE rpc_headers proto-hello)

add_executable(client-proto src/client.cpp)
target_link_libraries(client-proto PRIVATE rpc_headers proto-hello)

