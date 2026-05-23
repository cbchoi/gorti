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

add_executable(test_encoding_unit
  test_encoding_unit.cpp
)
target_link_libraries(test_encoding_unit PRIVATE
  rti1516e
  GTest::gtest
  GTest::gtest_main
)
add_test(NAME encoding_unit COMMAND test_encoding_unit)

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

add_executable(test_handles_integration
  test_handles_integration.cpp
)
target_compile_definitions(test_handles_integration PRIVATE
  RTID_REPO_ROOT="${CMAKE_SOURCE_DIR}/.."
  TEST_FOM_PATH="${CMAKE_CURRENT_SOURCE_DIR}/fixtures/test_fom.xml"
)
target_include_directories(test_handles_integration PRIVATE
  "${CMAKE_CURRENT_SOURCE_DIR}"
)
target_link_libraries(test_handles_integration PRIVATE
  rti1516e
  GTest::gtest
  GTest::gtest_main
)
add_test(NAME handles_integration COMMAND test_handles_integration)
set_tests_properties(handles_integration PROPERTIES
  LABELS "integration"
  TIMEOUT 60
)

add_executable(test_declarations_integration
  test_declarations_integration.cpp
)
target_compile_definitions(test_declarations_integration PRIVATE
  RTID_REPO_ROOT="${CMAKE_SOURCE_DIR}/.."
  TEST_FOM_PATH="${CMAKE_CURRENT_SOURCE_DIR}/fixtures/test_fom.xml"
)
target_include_directories(test_declarations_integration PRIVATE
  "${CMAKE_CURRENT_SOURCE_DIR}"
)
target_link_libraries(test_declarations_integration PRIVATE
  rti1516e
  GTest::gtest
  GTest::gtest_main
)
add_test(NAME declarations_integration COMMAND test_declarations_integration)
set_tests_properties(declarations_integration PROPERTIES
  LABELS "integration"
  TIMEOUT 60
)

add_executable(test_objects_integration
  test_objects_integration.cpp
)
target_compile_definitions(test_objects_integration PRIVATE
  RTID_REPO_ROOT="${CMAKE_SOURCE_DIR}/.."
  TEST_FOM_PATH="${CMAKE_CURRENT_SOURCE_DIR}/fixtures/test_fom.xml"
)
target_include_directories(test_objects_integration PRIVATE
  "${CMAKE_CURRENT_SOURCE_DIR}"
)
target_link_libraries(test_objects_integration PRIVATE
  rti1516e
  GTest::gtest
  GTest::gtest_main
)
add_test(NAME objects_integration COMMAND test_objects_integration)
set_tests_properties(objects_integration PROPERTIES
  LABELS "integration"
  TIMEOUT 60
)

add_executable(test_callbacks_integration
  test_callbacks_integration.cpp
)
target_compile_definitions(test_callbacks_integration PRIVATE
  RTID_REPO_ROOT="${CMAKE_SOURCE_DIR}/.."
  TEST_FOM_PATH="${CMAKE_CURRENT_SOURCE_DIR}/fixtures/test_fom.xml"
)
target_include_directories(test_callbacks_integration PRIVATE
  "${CMAKE_CURRENT_SOURCE_DIR}"
)
target_link_libraries(test_callbacks_integration PRIVATE
  rti1516e
  GTest::gtest
  GTest::gtest_main
)
add_test(NAME callbacks_integration COMMAND test_callbacks_integration)
set_tests_properties(callbacks_integration PROPERTIES
  LABELS "integration"
  TIMEOUT 60
)

add_executable(test_instance_handles_integration
  test_instance_handles_integration.cpp
)
target_compile_definitions(test_instance_handles_integration PRIVATE
  RTID_REPO_ROOT="${CMAKE_SOURCE_DIR}/.."
  TEST_FOM_PATH="${CMAKE_CURRENT_SOURCE_DIR}/fixtures/test_fom.xml"
)
target_include_directories(test_instance_handles_integration PRIVATE
  "${CMAKE_CURRENT_SOURCE_DIR}"
)
target_link_libraries(test_instance_handles_integration PRIVATE
  rti1516e
  GTest::gtest
  GTest::gtest_main
)
add_test(NAME instance_handles_integration COMMAND test_instance_handles_integration)
set_tests_properties(instance_handles_integration PROPERTIES
  LABELS "integration"
  TIMEOUT 60
)

add_executable(test_reservation_integration
  test_reservation_integration.cpp
)
target_compile_definitions(test_reservation_integration PRIVATE
  RTID_REPO_ROOT="${CMAKE_SOURCE_DIR}/.."
  TEST_FOM_PATH="${CMAKE_CURRENT_SOURCE_DIR}/fixtures/test_fom.xml"
)
target_include_directories(test_reservation_integration PRIVATE
  "${CMAKE_CURRENT_SOURCE_DIR}"
)
target_link_libraries(test_reservation_integration PRIVATE
  rti1516e
  GTest::gtest
  GTest::gtest_main
)
add_test(NAME reservation_integration COMMAND test_reservation_integration)
set_tests_properties(reservation_integration PROPERTIES
  LABELS "integration"
  TIMEOUT 60
)
