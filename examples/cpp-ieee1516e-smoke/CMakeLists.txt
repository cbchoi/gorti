# examples/cpp-ieee1516e-smoke — IEEE 1516e ambassador shape C++ publisher binary.
#
# Built as part of the cppsdk build (added from cppsdk/CMakeLists.txt
# via add_subdirectory). The Python interop test runs the binary as
# a subprocess after `make build && cmake --build cppsdk/build`.

cmake_minimum_required(VERSION 3.18)

# Use the cppsdk targets when included as a subdirectory.
add_executable(cpp_ieee1516e_publisher publisher.cpp)
target_link_libraries(cpp_ieee1516e_publisher PRIVATE rti1516e)
set_target_properties(cpp_ieee1516e_publisher PROPERTIES
  RUNTIME_OUTPUT_DIRECTORY "${CMAKE_BINARY_DIR}/examples/cpp-ieee1516e-smoke"
)

# Copy the FOM into the build dir so the publisher's default --fom
# resolves correctly when run from the build output directory.
configure_file(
  "${CMAKE_CURRENT_SOURCE_DIR}/federation.fom.xml"
  "${CMAKE_BINARY_DIR}/examples/cpp-ieee1516e-smoke/federation.fom.xml"
  COPYONLY
)
