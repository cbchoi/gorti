########### AGGREGATED COMPONENTS AND DEPENDENCIES FOR THE MULTI CONFIG #####################
#############################################################################################

list(APPEND protobuf_COMPONENT_NAMES utf8_range::utf8_range utf8_range::utf8_validity protobuf::libprotobuf protobuf::libprotoc)
list(REMOVE_DUPLICATES protobuf_COMPONENT_NAMES)
if(DEFINED protobuf_FIND_DEPENDENCY_NAMES)
  list(APPEND protobuf_FIND_DEPENDENCY_NAMES ZLIB absl)
  list(REMOVE_DUPLICATES protobuf_FIND_DEPENDENCY_NAMES)
else()
  set(protobuf_FIND_DEPENDENCY_NAMES ZLIB absl)
endif()
set(ZLIB_FIND_MODE "NO_MODULE")
set(absl_FIND_MODE "NO_MODULE")

########### VARIABLES #######################################################################
#############################################################################################
set(protobuf_PACKAGE_FOLDER_RELEASE "/home/claude/.conan2/p/b/protoccdb6907fcd48/p")
set(protobuf_BUILD_MODULES_PATHS_RELEASE "${protobuf_PACKAGE_FOLDER_RELEASE}/lib/cmake/protobuf/protobuf-generate.cmake"
			"${protobuf_PACKAGE_FOLDER_RELEASE}/lib/cmake/protobuf/protobuf-module.cmake"
			"${protobuf_PACKAGE_FOLDER_RELEASE}/lib/cmake/protobuf/protobuf-options.cmake"
			"${protobuf_PACKAGE_FOLDER_RELEASE}/lib/cmake/protobuf/protobuf-conan-protoc-target.cmake")


set(protobuf_INCLUDE_DIRS_RELEASE "${protobuf_PACKAGE_FOLDER_RELEASE}/include")
set(protobuf_RES_DIRS_RELEASE )
set(protobuf_DEFINITIONS_RELEASE )
set(protobuf_SHARED_LINK_FLAGS_RELEASE )
set(protobuf_EXE_LINK_FLAGS_RELEASE )
set(protobuf_OBJECTS_RELEASE )
set(protobuf_COMPILE_DEFINITIONS_RELEASE )
set(protobuf_COMPILE_OPTIONS_C_RELEASE )
set(protobuf_COMPILE_OPTIONS_CXX_RELEASE )
set(protobuf_LIB_DIRS_RELEASE "${protobuf_PACKAGE_FOLDER_RELEASE}/lib")
set(protobuf_BIN_DIRS_RELEASE )
set(protobuf_LIBRARY_TYPE_RELEASE STATIC)
set(protobuf_IS_HOST_WINDOWS_RELEASE 0)
set(protobuf_LIBS_RELEASE protoc protobuf utf8_validity utf8_range)
set(protobuf_SYSTEM_LIBS_RELEASE m pthread)
set(protobuf_FRAMEWORK_DIRS_RELEASE )
set(protobuf_FRAMEWORKS_RELEASE )
set(protobuf_BUILD_DIRS_RELEASE "${protobuf_PACKAGE_FOLDER_RELEASE}/lib/cmake/protobuf")
set(protobuf_NO_SONAME_MODE_RELEASE FALSE)


# COMPOUND VARIABLES
set(protobuf_COMPILE_OPTIONS_RELEASE
    "$<$<COMPILE_LANGUAGE:CXX>:${protobuf_COMPILE_OPTIONS_CXX_RELEASE}>"
    "$<$<COMPILE_LANGUAGE:C>:${protobuf_COMPILE_OPTIONS_C_RELEASE}>")
set(protobuf_LINKER_FLAGS_RELEASE
    "$<$<STREQUAL:$<TARGET_PROPERTY:TYPE>,SHARED_LIBRARY>:${protobuf_SHARED_LINK_FLAGS_RELEASE}>"
    "$<$<STREQUAL:$<TARGET_PROPERTY:TYPE>,MODULE_LIBRARY>:${protobuf_SHARED_LINK_FLAGS_RELEASE}>"
    "$<$<STREQUAL:$<TARGET_PROPERTY:TYPE>,EXECUTABLE>:${protobuf_EXE_LINK_FLAGS_RELEASE}>")


set(protobuf_COMPONENTS_RELEASE utf8_range::utf8_range utf8_range::utf8_validity protobuf::libprotobuf protobuf::libprotoc)
########### COMPONENT protobuf::libprotoc VARIABLES ############################################

set(protobuf_protobuf_libprotoc_INCLUDE_DIRS_RELEASE "${protobuf_PACKAGE_FOLDER_RELEASE}/include")
set(protobuf_protobuf_libprotoc_LIB_DIRS_RELEASE "${protobuf_PACKAGE_FOLDER_RELEASE}/lib")
set(protobuf_protobuf_libprotoc_BIN_DIRS_RELEASE )
set(protobuf_protobuf_libprotoc_LIBRARY_TYPE_RELEASE STATIC)
set(protobuf_protobuf_libprotoc_IS_HOST_WINDOWS_RELEASE 0)
set(protobuf_protobuf_libprotoc_RES_DIRS_RELEASE )
set(protobuf_protobuf_libprotoc_DEFINITIONS_RELEASE )
set(protobuf_protobuf_libprotoc_OBJECTS_RELEASE )
set(protobuf_protobuf_libprotoc_COMPILE_DEFINITIONS_RELEASE )
set(protobuf_protobuf_libprotoc_COMPILE_OPTIONS_C_RELEASE "")
set(protobuf_protobuf_libprotoc_COMPILE_OPTIONS_CXX_RELEASE "")
set(protobuf_protobuf_libprotoc_LIBS_RELEASE protoc)
set(protobuf_protobuf_libprotoc_SYSTEM_LIBS_RELEASE )
set(protobuf_protobuf_libprotoc_FRAMEWORK_DIRS_RELEASE )
set(protobuf_protobuf_libprotoc_FRAMEWORKS_RELEASE )
set(protobuf_protobuf_libprotoc_DEPENDENCIES_RELEASE protobuf::libprotobuf absl::absl_check absl::absl_log absl::algorithm absl::base absl::bind_front absl::bits absl::btree absl::cleanup absl::cord absl::core_headers absl::debugging absl::die_if_null absl::dynamic_annotations absl::flags absl::flat_hash_map absl::flat_hash_set absl::function_ref absl::hash absl::if_constexpr absl::layout absl::log_initialize absl::log_globals absl::log_severity absl::memory absl::node_hash_map absl::node_hash_set absl::optional absl::random_distributions absl::random_random absl::span absl::status absl::statusor absl::strings absl::synchronization absl::time absl::type_traits absl::utility absl::variant)
set(protobuf_protobuf_libprotoc_SHARED_LINK_FLAGS_RELEASE )
set(protobuf_protobuf_libprotoc_EXE_LINK_FLAGS_RELEASE )
set(protobuf_protobuf_libprotoc_NO_SONAME_MODE_RELEASE FALSE)

# COMPOUND VARIABLES
set(protobuf_protobuf_libprotoc_LINKER_FLAGS_RELEASE
        $<$<STREQUAL:$<TARGET_PROPERTY:TYPE>,SHARED_LIBRARY>:${protobuf_protobuf_libprotoc_SHARED_LINK_FLAGS_RELEASE}>
        $<$<STREQUAL:$<TARGET_PROPERTY:TYPE>,MODULE_LIBRARY>:${protobuf_protobuf_libprotoc_SHARED_LINK_FLAGS_RELEASE}>
        $<$<STREQUAL:$<TARGET_PROPERTY:TYPE>,EXECUTABLE>:${protobuf_protobuf_libprotoc_EXE_LINK_FLAGS_RELEASE}>
)
set(protobuf_protobuf_libprotoc_COMPILE_OPTIONS_RELEASE
    "$<$<COMPILE_LANGUAGE:CXX>:${protobuf_protobuf_libprotoc_COMPILE_OPTIONS_CXX_RELEASE}>"
    "$<$<COMPILE_LANGUAGE:C>:${protobuf_protobuf_libprotoc_COMPILE_OPTIONS_C_RELEASE}>")
########### COMPONENT protobuf::libprotobuf VARIABLES ############################################

set(protobuf_protobuf_libprotobuf_INCLUDE_DIRS_RELEASE "${protobuf_PACKAGE_FOLDER_RELEASE}/include")
set(protobuf_protobuf_libprotobuf_LIB_DIRS_RELEASE "${protobuf_PACKAGE_FOLDER_RELEASE}/lib")
set(protobuf_protobuf_libprotobuf_BIN_DIRS_RELEASE )
set(protobuf_protobuf_libprotobuf_LIBRARY_TYPE_RELEASE STATIC)
set(protobuf_protobuf_libprotobuf_IS_HOST_WINDOWS_RELEASE 0)
set(protobuf_protobuf_libprotobuf_RES_DIRS_RELEASE )
set(protobuf_protobuf_libprotobuf_DEFINITIONS_RELEASE )
set(protobuf_protobuf_libprotobuf_OBJECTS_RELEASE )
set(protobuf_protobuf_libprotobuf_COMPILE_DEFINITIONS_RELEASE )
set(protobuf_protobuf_libprotobuf_COMPILE_OPTIONS_C_RELEASE "")
set(protobuf_protobuf_libprotobuf_COMPILE_OPTIONS_CXX_RELEASE "")
set(protobuf_protobuf_libprotobuf_LIBS_RELEASE protobuf)
set(protobuf_protobuf_libprotobuf_SYSTEM_LIBS_RELEASE m pthread)
set(protobuf_protobuf_libprotobuf_FRAMEWORK_DIRS_RELEASE )
set(protobuf_protobuf_libprotobuf_FRAMEWORKS_RELEASE )
set(protobuf_protobuf_libprotobuf_DEPENDENCIES_RELEASE ZLIB::ZLIB absl::absl_check absl::absl_log absl::algorithm absl::base absl::bind_front absl::bits absl::btree absl::cleanup absl::cord absl::core_headers absl::debugging absl::die_if_null absl::dynamic_annotations absl::flags absl::flat_hash_map absl::flat_hash_set absl::function_ref absl::hash absl::if_constexpr absl::layout absl::log_initialize absl::log_globals absl::log_severity absl::memory absl::node_hash_map absl::node_hash_set absl::optional absl::random_distributions absl::random_random absl::span absl::status absl::statusor absl::strings absl::synchronization absl::time absl::type_traits absl::utility absl::variant utf8_range::utf8_validity)
set(protobuf_protobuf_libprotobuf_SHARED_LINK_FLAGS_RELEASE )
set(protobuf_protobuf_libprotobuf_EXE_LINK_FLAGS_RELEASE )
set(protobuf_protobuf_libprotobuf_NO_SONAME_MODE_RELEASE FALSE)

# COMPOUND VARIABLES
set(protobuf_protobuf_libprotobuf_LINKER_FLAGS_RELEASE
        $<$<STREQUAL:$<TARGET_PROPERTY:TYPE>,SHARED_LIBRARY>:${protobuf_protobuf_libprotobuf_SHARED_LINK_FLAGS_RELEASE}>
        $<$<STREQUAL:$<TARGET_PROPERTY:TYPE>,MODULE_LIBRARY>:${protobuf_protobuf_libprotobuf_SHARED_LINK_FLAGS_RELEASE}>
        $<$<STREQUAL:$<TARGET_PROPERTY:TYPE>,EXECUTABLE>:${protobuf_protobuf_libprotobuf_EXE_LINK_FLAGS_RELEASE}>
)
set(protobuf_protobuf_libprotobuf_COMPILE_OPTIONS_RELEASE
    "$<$<COMPILE_LANGUAGE:CXX>:${protobuf_protobuf_libprotobuf_COMPILE_OPTIONS_CXX_RELEASE}>"
    "$<$<COMPILE_LANGUAGE:C>:${protobuf_protobuf_libprotobuf_COMPILE_OPTIONS_C_RELEASE}>")
########### COMPONENT utf8_range::utf8_validity VARIABLES ############################################

set(protobuf_utf8_range_utf8_validity_INCLUDE_DIRS_RELEASE "${protobuf_PACKAGE_FOLDER_RELEASE}/include")
set(protobuf_utf8_range_utf8_validity_LIB_DIRS_RELEASE "${protobuf_PACKAGE_FOLDER_RELEASE}/lib")
set(protobuf_utf8_range_utf8_validity_BIN_DIRS_RELEASE )
set(protobuf_utf8_range_utf8_validity_LIBRARY_TYPE_RELEASE STATIC)
set(protobuf_utf8_range_utf8_validity_IS_HOST_WINDOWS_RELEASE 0)
set(protobuf_utf8_range_utf8_validity_RES_DIRS_RELEASE )
set(protobuf_utf8_range_utf8_validity_DEFINITIONS_RELEASE )
set(protobuf_utf8_range_utf8_validity_OBJECTS_RELEASE )
set(protobuf_utf8_range_utf8_validity_COMPILE_DEFINITIONS_RELEASE )
set(protobuf_utf8_range_utf8_validity_COMPILE_OPTIONS_C_RELEASE "")
set(protobuf_utf8_range_utf8_validity_COMPILE_OPTIONS_CXX_RELEASE "")
set(protobuf_utf8_range_utf8_validity_LIBS_RELEASE utf8_validity)
set(protobuf_utf8_range_utf8_validity_SYSTEM_LIBS_RELEASE )
set(protobuf_utf8_range_utf8_validity_FRAMEWORK_DIRS_RELEASE )
set(protobuf_utf8_range_utf8_validity_FRAMEWORKS_RELEASE )
set(protobuf_utf8_range_utf8_validity_DEPENDENCIES_RELEASE absl::strings)
set(protobuf_utf8_range_utf8_validity_SHARED_LINK_FLAGS_RELEASE )
set(protobuf_utf8_range_utf8_validity_EXE_LINK_FLAGS_RELEASE )
set(protobuf_utf8_range_utf8_validity_NO_SONAME_MODE_RELEASE FALSE)

# COMPOUND VARIABLES
set(protobuf_utf8_range_utf8_validity_LINKER_FLAGS_RELEASE
        $<$<STREQUAL:$<TARGET_PROPERTY:TYPE>,SHARED_LIBRARY>:${protobuf_utf8_range_utf8_validity_SHARED_LINK_FLAGS_RELEASE}>
        $<$<STREQUAL:$<TARGET_PROPERTY:TYPE>,MODULE_LIBRARY>:${protobuf_utf8_range_utf8_validity_SHARED_LINK_FLAGS_RELEASE}>
        $<$<STREQUAL:$<TARGET_PROPERTY:TYPE>,EXECUTABLE>:${protobuf_utf8_range_utf8_validity_EXE_LINK_FLAGS_RELEASE}>
)
set(protobuf_utf8_range_utf8_validity_COMPILE_OPTIONS_RELEASE
    "$<$<COMPILE_LANGUAGE:CXX>:${protobuf_utf8_range_utf8_validity_COMPILE_OPTIONS_CXX_RELEASE}>"
    "$<$<COMPILE_LANGUAGE:C>:${protobuf_utf8_range_utf8_validity_COMPILE_OPTIONS_C_RELEASE}>")
########### COMPONENT utf8_range::utf8_range VARIABLES ############################################

set(protobuf_utf8_range_utf8_range_INCLUDE_DIRS_RELEASE "${protobuf_PACKAGE_FOLDER_RELEASE}/include")
set(protobuf_utf8_range_utf8_range_LIB_DIRS_RELEASE "${protobuf_PACKAGE_FOLDER_RELEASE}/lib")
set(protobuf_utf8_range_utf8_range_BIN_DIRS_RELEASE )
set(protobuf_utf8_range_utf8_range_LIBRARY_TYPE_RELEASE STATIC)
set(protobuf_utf8_range_utf8_range_IS_HOST_WINDOWS_RELEASE 0)
set(protobuf_utf8_range_utf8_range_RES_DIRS_RELEASE )
set(protobuf_utf8_range_utf8_range_DEFINITIONS_RELEASE )
set(protobuf_utf8_range_utf8_range_OBJECTS_RELEASE )
set(protobuf_utf8_range_utf8_range_COMPILE_DEFINITIONS_RELEASE )
set(protobuf_utf8_range_utf8_range_COMPILE_OPTIONS_C_RELEASE "")
set(protobuf_utf8_range_utf8_range_COMPILE_OPTIONS_CXX_RELEASE "")
set(protobuf_utf8_range_utf8_range_LIBS_RELEASE utf8_range)
set(protobuf_utf8_range_utf8_range_SYSTEM_LIBS_RELEASE )
set(protobuf_utf8_range_utf8_range_FRAMEWORK_DIRS_RELEASE )
set(protobuf_utf8_range_utf8_range_FRAMEWORKS_RELEASE )
set(protobuf_utf8_range_utf8_range_DEPENDENCIES_RELEASE )
set(protobuf_utf8_range_utf8_range_SHARED_LINK_FLAGS_RELEASE )
set(protobuf_utf8_range_utf8_range_EXE_LINK_FLAGS_RELEASE )
set(protobuf_utf8_range_utf8_range_NO_SONAME_MODE_RELEASE FALSE)

# COMPOUND VARIABLES
set(protobuf_utf8_range_utf8_range_LINKER_FLAGS_RELEASE
        $<$<STREQUAL:$<TARGET_PROPERTY:TYPE>,SHARED_LIBRARY>:${protobuf_utf8_range_utf8_range_SHARED_LINK_FLAGS_RELEASE}>
        $<$<STREQUAL:$<TARGET_PROPERTY:TYPE>,MODULE_LIBRARY>:${protobuf_utf8_range_utf8_range_SHARED_LINK_FLAGS_RELEASE}>
        $<$<STREQUAL:$<TARGET_PROPERTY:TYPE>,EXECUTABLE>:${protobuf_utf8_range_utf8_range_EXE_LINK_FLAGS_RELEASE}>
)
set(protobuf_utf8_range_utf8_range_COMPILE_OPTIONS_RELEASE
    "$<$<COMPILE_LANGUAGE:CXX>:${protobuf_utf8_range_utf8_range_COMPILE_OPTIONS_CXX_RELEASE}>"
    "$<$<COMPILE_LANGUAGE:C>:${protobuf_utf8_range_utf8_range_COMPILE_OPTIONS_C_RELEASE}>")