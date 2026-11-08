cmake_minimum_required(VERSION 3.22)
project(kilnfile_worker LANGUAGES CXX)

set(CMAKE_CXX_STANDARD 20)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
set(CMAKE_CXX_EXTENSIONS OFF)
if(NOT CMAKE_BUILD_TYPE AND NOT CMAKE_CONFIGURATION_TYPES)
  set(CMAKE_BUILD_TYPE RelWithDebInfo)
endif()

add_library(kilnfile_worker_lib STATIC protocol.cc engine.cc sandbox.cc)
target_include_directories(kilnfile_worker_lib PUBLIC ${CMAKE_CURRENT_SOURCE_DIR})
target_compile_options(kilnfile_worker_lib PRIVATE
  $<$<NOT:$<CXX_COMPILER_ID:MSVC>>:-Wall -Wextra>)

add_executable(kilnfile-worker main.cc)
target_link_libraries(kilnfile-worker PRIVATE kilnfile_worker_lib)

option(KILNFILE_WORKER_BUILD_TESTS "Build worker tests" ON)
if(KILNFILE_WORKER_BUILD_TESTS)
  include(CTest)
  include(FetchContent)
  set(INSTALL_GTEST OFF CACHE BOOL "" FORCE)
  FetchContent_Declare(googletest
    URL https://github.com/google/googletest/archive/refs/tags/v1.15.2.tar.gz)
  FetchContent_MakeAvailable(googletest)

  add_executable(worker_tests
    tests/protocol_test.cc tests/sandbox_test.cc tests/e2e_test.cc)
  target_link_libraries(worker_tests PRIVATE kilnfile_worker_lib GTest::gtest_main)
  target_compile_definitions(worker_tests PRIVATE
    KILNFILE_WORKER_PATH="$<TARGET_FILE:kilnfile-worker>")
  add_dependencies(worker_tests kilnfile-worker)

  include(GoogleTest)
  gtest_discover_tests(worker_tests DISCOVERY_MODE PRE_TEST)
endif()
