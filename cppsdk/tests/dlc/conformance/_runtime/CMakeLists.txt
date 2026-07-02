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

if(TARGET dlc_conformance)
  add_dependencies(dlc_conformance test_conf__runtime_exceptions)
endif()

# --- M34 (Agent AD) — DLCFederateAmbassadorBridge delivery tests ----------
#
# Verifies the M17->DLC callback conversion layer that makes M34 conformance
# fixtures verify events instead of just link. Uses mock DLC ambassador that
# tracks method invocations; no RTI subprocess, no gRPC. Must PASS.
# See cppsdk/src/dlc/FederateAmbassadorBridge.{h,cpp}.

add_executable(test_conf__runtime_callback_bridge
  test_callback_bridge.cpp
)
target_include_directories(test_conf__runtime_callback_bridge PRIVATE
  "${CMAKE_SOURCE_DIR}/include"
  "${CMAKE_SOURCE_DIR}"   # so `#include "src/dlc/FederateAmbassadorBridge.h"` resolves
)
target_link_libraries(test_conf__runtime_callback_bridge PRIVATE
  rti1516e_dlc
  GTest::gtest
  GTest::gtest_main
)
target_compile_features(test_conf__runtime_callback_bridge PRIVATE cxx_std_17)

add_test(NAME conformance__runtime_callback_bridge
  COMMAND test_conf__runtime_callback_bridge)
set_tests_properties(conformance__runtime_callback_bridge PROPERTIES
  LABELS "conformance"
  TIMEOUT 60
)

if(TARGET dlc_conformance)
  add_dependencies(dlc_conformance test_conf__runtime_callback_bridge)
endif()

# --- M34 (Agent AF) — §14 encoding round-trip runtime test ---------------
#
# Sits alongside the exception + bridge runtime tests. Exercises every basic
# + composite encoder in BasicDataElements / HLA{fixed,variable}Array /
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

# --- M35 (Agent BF) — MOM surface lockfile (catalogue row 16.1) ----------
#
# Compile-only lockfile-style shape assertion that the DLC RTIambassador
# discovers `HLAobjectRoot.HLAmanager.HLAfederation` via the standard
# `getObjectClassHandle(std::wstring const&)` call, with no bespoke MOM
# helper on the DLC surface. See test_mom_surface.cpp for the
# static_assert contract. GREEN test — WILL_FAIL unset, must PASS.

add_executable(test_conf__runtime_mom_surface
  test_mom_surface.cpp
)
target_include_directories(test_conf__runtime_mom_surface PRIVATE
  "${CMAKE_SOURCE_DIR}/include"
)
target_link_libraries(test_conf__runtime_mom_surface PRIVATE
  rti1516e_dlc
  GTest::gtest
  GTest::gtest_main
)
target_compile_features(test_conf__runtime_mom_surface PRIVATE cxx_std_17)

add_test(NAME conformance__runtime_mom_surface
  COMMAND test_conf__runtime_mom_surface)
set_tests_properties(conformance__runtime_mom_surface PROPERTIES
  LABELS "conformance"
  TIMEOUT 60
)

if(TARGET dlc_conformance)
  add_dependencies(dlc_conformance test_conf__runtime_mom_surface)
endif()

# --- M39 (Agent HB) — translateBridgeError prefix-table tests --------------
#
# Unit tests for the metadata-first exception pipeline: full Annex C
# prefix table (fed by the server's rti-spec-exception trailer via
# M17Bridge guard()), the M37 EE FederateNotExecutionMember special
# case, the DEPRECATED detail sniffs, and the RTIinternalError
# fallback. No RTI subprocess, no gRPC. Must PASS.
# See cppsdk/src/dlc/BridgeErrorTranslation.{h,cpp}.

add_executable(test_conf__runtime_bridge_error
  test_bridge_error_translation.cpp
)
target_include_directories(test_conf__runtime_bridge_error PRIVATE
  "${CMAKE_SOURCE_DIR}/include"
  "${CMAKE_SOURCE_DIR}"   # so `#include "src/dlc/BridgeErrorTranslation.h"` resolves
)
target_link_libraries(test_conf__runtime_bridge_error PRIVATE
  rti1516e_dlc
  GTest::gtest
  GTest::gtest_main
)
target_compile_features(test_conf__runtime_bridge_error PRIVATE cxx_std_17)

add_test(NAME conformance__runtime_bridge_error
  COMMAND test_conf__runtime_bridge_error)
set_tests_properties(conformance__runtime_bridge_error PROPERTIES
  LABELS "conformance"
  TIMEOUT 60
)

if(TARGET dlc_conformance)
  add_dependencies(dlc_conformance test_conf__runtime_bridge_error)
endif()
