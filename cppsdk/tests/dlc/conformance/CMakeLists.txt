# M31 conformance fixture wiring — TASK-349.
#
# Each subdirectory of cppsdk/tests/dlc/conformance/ (other than
# _harness/) is a fixture containing:
#   * one or more federate*.cpp source files
#   * federation.fom.xml
#   * expected.*.log (skeletons in M31; real reference RTI capture lands TASK-350)
#   * test_<name>.cpp gtest driver
#   * README.md
#
# This CMakeLists fans out via file(GLOB) so adding a fixture does not
# require a CMakeLists edit. Each fixture becomes:
#   * one federate executable per federate*.cpp (EXCLUDE_FROM_ALL)
#   * one gtest driver executable test_conf_<fixture>
#   * one ctest entry named conformance_<fixture> with WILL_FAIL=TRUE
#     and LABELS=conformance.
#
# WILL_FAIL TRUE is the M31 RED contract: federates fail to link with
# `undefined reference to rti1516e::*` (no impl symbols), and gtest
# drivers fail because the federate binaries don't exist. M32+ flips
# WILL_FAIL OFF per fixture as the impl lands.

find_package(GTest CONFIG REQUIRED)

# Where the gtest driver finds its fixture-local files (FOM, goldens).
# Each driver gets a FIXTURE_DIR define pointing at its source dir.

file(GLOB FIXTURE_DIRS LIST_DIRECTORIES true RELATIVE
  "${CMAKE_CURRENT_SOURCE_DIR}" "${CMAKE_CURRENT_SOURCE_DIR}/*")

foreach(dir ${FIXTURE_DIRS})
  set(_fixpath "${CMAKE_CURRENT_SOURCE_DIR}/${dir}")
  # _harness holds shared helpers, not a fixture.
  # _runtime is M33-I's gorti-only runtime-exception fixture — it has its
  # own CMakeLists.txt (no federate*.cpp, links rti1516e_dlc) and is
  # add_subdirectory'd explicitly at the bottom of this file.
  if(IS_DIRECTORY "${_fixpath}"
     AND NOT dir STREQUAL "_harness"
     AND NOT dir STREQUAL "_runtime")
    file(GLOB FED_SOURCES "${_fixpath}/federate*.cpp")
    file(GLOB TEST_SOURCES "${_fixpath}/test_*.cpp")

    if(FED_SOURCES)
      foreach(fed ${FED_SOURCES})
        get_filename_component(fed_name "${fed}" NAME_WE)
        set(_tgt "conf_${dir}_${fed_name}")
        add_executable(${_tgt} EXCLUDE_FROM_ALL "${fed}")
        target_include_directories(${_tgt} PRIVATE
          "${CMAKE_SOURCE_DIR}/include"
          "${CMAKE_CURRENT_SOURCE_DIR}/_harness")
        # M34: link the DLC library (which transitively pulls M17 for the
        # bridged delegate). Prior to M34 this was PRIVATE rti1516e; that
        # linked only against the M17 concrete class and left every
        # <RTI/*> DLC symbol undefined, so the fixtures were WILL_FAIL=TRUE
        # by construction. See docs/M31_DISPATCH_PLAN.md.
        target_link_libraries(${_tgt} PRIVATE rti1516e_dlc)
        target_compile_features(${_tgt} PRIVATE cxx_std_17)
      endforeach()

      if(TEST_SOURCES)
        set(_drv "test_conf_${dir}")
        add_executable(${_drv} EXCLUDE_FROM_ALL ${TEST_SOURCES})
        target_include_directories(${_drv} PRIVATE
          "${CMAKE_SOURCE_DIR}/include"
          "${CMAKE_CURRENT_SOURCE_DIR}/_harness")
        target_compile_definitions(${_drv} PRIVATE
          FIXTURE_DIR="${_fixpath}"
          RTID_REPO_ROOT="${CMAKE_SOURCE_DIR}/..")
        target_link_libraries(${_drv} PRIVATE
          rti1516e
          GTest::gtest
          GTest::gtest_main)
        target_compile_features(${_drv} PRIVATE cxx_std_17)
        add_test(NAME conformance_${dir} COMMAND ${_drv})
        set_tests_properties(conformance_${dir} PROPERTIES
          WILL_FAIL TRUE
          LABELS "conformance"
          TIMEOUT 120
        )
      endif()
    endif()
  endif()
endforeach()

# Convenience: a meta-target that builds every fixture binary at once
# without polluting `ALL`. Useful in CI: `cmake --build build --target dlc_conformance`.
file(GLOB_RECURSE _ALL_FED_SOURCES "${CMAKE_CURRENT_SOURCE_DIR}/*/federate*.cpp")
file(GLOB_RECURSE _ALL_TEST_SOURCES "${CMAKE_CURRENT_SOURCE_DIR}/*/test_*.cpp")
add_custom_target(dlc_conformance)
foreach(_src ${_ALL_FED_SOURCES} ${_ALL_TEST_SOURCES})
  get_filename_component(_fname "${_src}" NAME_WE)
  get_filename_component(_fdir "${_src}" DIRECTORY)
  get_filename_component(_fixname "${_fdir}" NAME)
  if(_fixname STREQUAL "_harness")
    continue()
  endif()
  # _runtime has a custom target name (test_conf__runtime_exceptions) that
  # doesn't fit the auto-fanout naming; it's wired to dlc_conformance from
  # inside its own CMakeLists.txt.
  if(_fixname STREQUAL "_runtime")
    continue()
  endif()
  if(_fname MATCHES "^test_")
    set(_tgt "test_conf_${_fixname}")
  else()
    set(_tgt "conf_${_fixname}_${_fname}")
  endif()
  if(TARGET ${_tgt})
    add_dependencies(dlc_conformance ${_tgt})
  endif()
endforeach()

# M33-I — gorti-only runtime exception fixture (has its own CMakeLists.txt,
# links rti1516e_dlc, WILL_FAIL=OFF).
add_subdirectory(_runtime)
