# M17 Cut-1 — cppsdk test suite.
#
# Each test target links against the public `rti1516e` library and
# GoogleTest. add_test() registers the target with CTest so
# `ctest --output-on-failure` from the build dir runs them all.

find_package(GTest CONFIG REQUIRED)

# --- Unit tests (no rtid subprocess) ----------------------------------

add_executable(test_ambassador_unit
  test_ambassador_unit.cpp
)
target_link_libraries(test_ambassador_unit PRIVATE
  rti1516e
  GTest::gtest
  GTest::gtest_main
)
add_test(NAME ambassador_unit COMMAND test_ambassador_unit)

# --- Integration tests (real rtid subprocess) -------------------------
#
# RTID_REPO_ROOT lets the RtidProcess fixture find the rtid binary
# relative to the repository root (REPO_ROOT/bin/rtid). The fixture
# also accepts an RTID_BINARY env var override at run time. The
# repo's `make build` target produces the binary; CI should run
# that before invoking ctest.

add_executable(test_lifecycle_integration
  test_lifecycle_integration.cpp
)
target_compile_definitions(test_lifecycle_integration PRIVATE
  RTID_REPO_ROOT="${CMAKE_SOURCE_DIR}/.."
)
target_include_directories(test_lifecycle_integration PRIVATE
  "${CMAKE_CURRENT_SOURCE_DIR}"
)
target_link_libraries(test_lifecycle_integration PRIVATE
  rti1516e
  GTest::gtest
  GTest::gtest_main
)
add_test(NAME lifecycle_integration COMMAND test_lifecycle_integration)
set_tests_properties(lifecycle_integration PROPERTIES
  LABELS "integration"
  TIMEOUT 60
)
