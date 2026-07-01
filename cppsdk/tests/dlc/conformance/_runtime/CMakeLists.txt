# M33-I — Annex C exception runtime tests.
#
# Unlike the other fixtures under cppsdk/tests/dlc/conformance/ (which build
# federate binaries against the abstract 1516e API and are expected to LINK
# and RUN as green ceremonies land), this fixture is genuinely gorti-only:
# the spec exceptions are plain class objects with no RTI involvement, so
# there is no federate binary — just a gtest driver that constructs, throws,
# and catches every one of the 121 spec-mandated leaf classes.
#
# The parent CMakeLists.txt in cppsdk/tests/dlc/conformance/ already skips
# `_runtime` from its auto-fanout (which is a federate-only pattern) and
# explicitly `add_subdirectory(_runtime)`'s us at the end. WILL_FAIL is
# unset (default OFF) — this test must PASS in M33 GREEN.

find_package(GTest CONFIG REQUIRED)

add_executable(test_conf__runtime_exceptions
  test_exceptions_throw_and_catch.cpp
)
target_include_directories(test_conf__runtime_exceptions PRIVATE
  "${CMAKE_SOURCE_DIR}/include"
)
target_link_libraries(test_conf__runtime_exceptions PRIVATE
  rti1516e_dlc
  GTest::gtest
  GTest::gtest_main
)
target_compile_features(test_conf__runtime_exceptions PRIVATE cxx_std_17)

add_test(NAME conformance__runtime_exceptions
  COMMAND test_conf__runtime_exceptions)
set_tests_properties(conformance__runtime_exceptions PROPERTIES
  LABELS "conformance"
  TIMEOUT 60
)

# Also add this driver to the dlc_conformance meta-target so
# `cmake --build build --target dlc_conformance` picks it up.
if(TARGET dlc_conformance)
  add_dependencies(dlc_conformance test_conf__runtime_exceptions)
endif()

# --- M34 (Agent AF) — §14 encoding round-trip runtime test ---------------
#
# Sits alongside the exception runtime test. Exercises every basic +
# composite encoder in BasicDataElements / HLA{fixed,variable}Array /
# HLA{fixed,variant}Record / HLAopaqueData. Cross-checks byte output
# against gorti's golden vectors in tests/conformance/encoding_vectors.json.
# GREEN test — WILL_FAIL unset, must PASS.

add_executable(test_conf__runtime_encoding
  test_encoding_roundtrip.cpp
)
target_include_directories(test_conf__runtime_encoding PRIVATE
  "${CMAKE_SOURCE_DIR}/include"
)
target_link_libraries(test_conf__runtime_encoding PRIVATE
  rti1516e_dlc
  GTest::gtest
  GTest::gtest_main
)
target_compile_features(test_conf__runtime_encoding PRIVATE cxx_std_17)

add_test(NAME conformance__runtime_encoding
  COMMAND test_conf__runtime_encoding)
set_tests_properties(conformance__runtime_encoding PROPERTIES
  LABELS "conformance"
  TIMEOUT 60
)

if(TARGET dlc_conformance)
  add_dependencies(dlc_conformance test_conf__runtime_encoding)
endif()
