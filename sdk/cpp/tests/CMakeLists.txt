add_executable(atlas_sdk_test client_test.cpp)
target_link_libraries(atlas_sdk_test PRIVATE atlas_sdk)
add_test(NAME client_test COMMAND atlas_sdk_test)
