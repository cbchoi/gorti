# cppsdk/tests/dlc/lockfile/CMakeLists.txt — M31 lockfile RED scaffold.
#
# Per docs/M31_DISPATCH_PLAN.md §2.4 + TASK-340 in §5 W1.
#
# Layout:
#   - One translation unit (.cpp) per spec-mandated header / class group.
#   - Each TU is registered as an EXCLUDE_FROM_ALL executable. The default
#     build never touches them, so M17's cppsdk tests stay GREEN.
#   - Each TU also gets an `add_test` whose COMMAND invokes
#     `cmake --build .../<target>`. That nested build is EXPECTED TO FAIL
#     while M32+ implementation work is in flight; `WILL_FAIL TRUE` inverts
#     the result so ctest reports the failure as a passing test.
#
# Aggregate target `dlc_lockfile` depends on every TU so that
#   cmake --build build --target dlc_lockfile
# attempts each in one pass (all expected RED in M31).
#
# To list the suite without running it:
#   ctest --test-dir build -L lockfile -N
#
# Acceptance probe (see TASK-359 #1): count of TUs is written to
#   expected_red_count.txt
# and `scripts/check-milestones.sh check_m31` cross-references it.

# Re-glob whenever a TU is added/removed.
file(GLOB_RECURSE LOCKFILE_SOURCES
  CONFIGURE_DEPENDS
  "${CMAKE_CURRENT_SOURCE_DIR}/*.cpp"
)

# Per-TU compile target + per-TU ctest entry (with WILL_FAIL=TRUE).
set(_lockfile_tu_targets "")
foreach(src ${LOCKFILE_SOURCES})
  # Use the source's parent dir + stem to disambiguate (e.g. exceptions/test_handle
  # would collide with types/test_handle without the prefix; here all stems are
  # unique, but the prefix keeps target names self-explanatory).
  get_filename_component(_name "${src}" NAME_WE)
  get_filename_component(_dir  "${src}" DIRECTORY)
  get_filename_component(_subdir "${_dir}" NAME)
  set(_tgt "dlc_lockfile_${_subdir}_${_name}")

  # EXCLUDE_FROM_ALL — never built by default; CI-on-demand only.
  # Tests register via WILL_FAIL=TRUE, so failed compile == passed test.
  add_executable(${_tgt} EXCLUDE_FROM_ALL "${src}")
  target_include_directories(${_tgt} PRIVATE
    "${CMAKE_SOURCE_DIR}/include"
  )
  target_compile_features(${_tgt} PRIVATE cxx_std_17)

  # The test invokes a nested cmake --build for this one target. If the
  # target fails to compile (the RED-by-design M31 state), ctest treats
  # WILL_FAIL=TRUE as a pass. When M32+ lands the impl and the TU compiles,
  # WILL_FAIL will need flipping per-TU.
  add_test(NAME lockfile_${_subdir}_${_name}
    COMMAND ${CMAKE_COMMAND} --build "${CMAKE_BINARY_DIR}"
                              --target ${_tgt}
  )
  set_tests_properties(lockfile_${_subdir}_${_name} PROPERTIES
    WILL_FAIL TRUE
    LABELS "lockfile;lockfile_${_subdir}"
  )

  list(APPEND _lockfile_tu_targets ${_tgt})
endforeach()

# Aggregate custom target — `cmake --build . --target dlc_lockfile` tries all.
# Refined from the dispatch plan's placeholder which referenced the source
# paths instead of target names.
add_custom_target(dlc_lockfile
  COMMENT "M31 lockfile suite — every TU should fail to compile (RED)"
)
if(_lockfile_tu_targets)
  add_dependencies(dlc_lockfile ${_lockfile_tu_targets})
endif()

# Record the M31 expected RED count for the acceptance probe.
list(LENGTH LOCKFILE_SOURCES LOCKFILE_COUNT)
file(WRITE "${CMAKE_CURRENT_SOURCE_DIR}/expected_red_count.txt"
  "${LOCKFILE_COUNT}\n")

message(STATUS "DLC lockfile: ${LOCKFILE_COUNT} TUs registered (all WILL_FAIL=TRUE).")
