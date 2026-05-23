# gorti C++ SDK — M17 Cut-1.
#
# Implements IEEE 1516.1-2010 RTIambassador surface against gorti's
# gRPC contracts. Mirrors the Python pysdk/rti1516e/standard.py
# Layer-2 ambassador in C++ for federate code ported from Pitch /
# Portico / MAK.
#
# Dependencies (system or via conan):
#   - gRPC++       (libgrpc++-dev)
#   - protobuf     (libprotobuf-dev)
#   - GoogleTest   (libgtest-dev) — test target only
#
# Build:
#   cd cppsdk
#   conan install . --build=missing -of build
#   cmake -B build -DCMAKE_TOOLCHAIN_FILE=build/conan_toolchain.cmake
#   cmake --build build -j
#
# Tests:
#   cd build && ctest --output-on-failure

cmake_minimum_required(VERSION 3.18)
project(gorti_cppsdk
  VERSION 0.1.0
  DESCRIPTION "gorti C++ SDK — IEEE 1516.1-2010 Pitch-style ambassador"
  LANGUAGES CXX
)

set(CMAKE_CXX_STANDARD 17)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
set(CMAKE_CXX_EXTENSIONS OFF)

if(NOT CMAKE_BUILD_TYPE)
  set(CMAKE_BUILD_TYPE Release CACHE STRING "Build type" FORCE)
endif()

# Standard CMake hygiene.
add_compile_options(-Wall -Wextra -Wpedantic)

# --- Dependencies ---------------------------------------------------------

find_package(Protobuf CONFIG REQUIRED)
find_package(gRPC CONFIG REQUIRED)

# --- Generated stubs ------------------------------------------------------
#
# Stubs are emitted by `buf generate` (run from repo root) into
# cppsdk/_generated/rti/v1/. They are gitignored to match the Go /
# Python conventions. The build picks up whatever is present.

file(GLOB_RECURSE GENERATED_SOURCES
  CONFIGURE_DEPENDS
  "${CMAKE_CURRENT_SOURCE_DIR}/_generated/rti/v1/*.cc"
)

if(NOT GENERATED_SOURCES)
  message(FATAL_ERROR
    "No generated protobuf sources found in cppsdk/_generated/rti/v1/.\n"
    "Run `buf generate` from the repository root first."
  )
endif()

add_library(rti1516e_generated STATIC ${GENERATED_SOURCES})
target_include_directories(rti1516e_generated PUBLIC
  "${CMAKE_CURRENT_SOURCE_DIR}/_generated"
)
target_link_libraries(rti1516e_generated PUBLIC
  protobuf::libprotobuf
  gRPC::grpc++
)

# --- Public SDK library ---------------------------------------------------

add_library(rti1516e
  src/RtiAmbassador.cpp
)
target_include_directories(rti1516e PUBLIC
  "${CMAKE_CURRENT_SOURCE_DIR}/include"
)
target_link_libraries(rti1516e PUBLIC
  rti1516e_generated
)

# --- Tests ----------------------------------------------------------------

enable_testing()
add_subdirectory(tests)

# --- Examples -------------------------------------------------------------
# Built into ${CMAKE_BINARY_DIR}/examples/cpp-pitch-smoke/cpp_pitch_publisher
# so the Python interop test (pysdk/tests/spec/m17/) can spawn it.

add_subdirectory("${CMAKE_SOURCE_DIR}/../examples/cpp-pitch-smoke" cpp-pitch-smoke)
