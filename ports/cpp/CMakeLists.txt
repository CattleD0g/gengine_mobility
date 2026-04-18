# Minimal CMake project for the C++ port of gengine.
#
# Build + test:
#   cmake -S . -B build
#   cmake --build build -j
#   ctest --test-dir build --output-on-failure
#
# No external dependencies: lexer, parser, and test harness are hand-
# rolled so the port can vendor cleanly into other trees.

cmake_minimum_required(VERSION 3.14)
project(gengine_cpp CXX)

set(CMAKE_CXX_STANDARD 17)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
set(CMAKE_CXX_EXTENSIONS OFF)

if(NOT CMAKE_BUILD_TYPE)
    set(CMAKE_BUILD_TYPE Release)
endif()

add_compile_options(-Wall -Wextra -Wpedantic)

# Static library so the tests share one compile of the engine.
add_library(gengine_cpp
    src/parser.cpp
    src/engine.cpp
)
target_include_directories(gengine_cpp PUBLIC include)

find_package(Threads REQUIRED)
target_link_libraries(gengine_cpp PUBLIC Threads::Threads)

enable_testing()
add_executable(test_smoke test/test_smoke.cpp)
target_link_libraries(test_smoke PRIVATE gengine_cpp)
add_test(NAME smoke COMMAND test_smoke)
