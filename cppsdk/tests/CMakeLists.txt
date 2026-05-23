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

# Integration tests are added in later milestones as the
# corresponding ambassador surface lands. Each will spawn a real
# rtid subprocess via the helper in fixtures/rtid_fixture.h.
