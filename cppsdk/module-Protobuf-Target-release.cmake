# Avoid multiple calls to find_package to append duplicated properties to the targets
include_guard()########### VARIABLES #######################################################################
#############################################################################################
set(protobuf_FRAMEWORKS_FOUND_RELEASE "") # Will be filled later
conan_find_apple_frameworks(protobuf_FRAMEWORKS_FOUND_RELEASE "${protobuf_FRAMEWORKS_RELEASE}" "${protobuf_FRAMEWORK_DIRS_RELEASE}")

set(protobuf_LIBRARIES_TARGETS "") # Will be filled later


######## Create an interface target to contain all the dependencies (frameworks, system and conan deps)
if(NOT TARGET protobuf_DEPS_TARGET)
    add_library(protobuf_DEPS_TARGET INTERFACE IMPORTED)
endif()

set_property(TARGET protobuf_DEPS_TARGET
             APPEND PROPERTY INTERFACE_LINK_LIBRARIES
             $<$<CONFIG:Release>:${protobuf_FRAMEWORKS_FOUND_RELEASE}>
             $<$<CONFIG:Release>:${protobuf_SYSTEM_LIBS_RELEASE}>
             $<$<CONFIG:Release>:absl::strings;ZLIB::ZLIB;absl::absl_check;absl::absl_log;absl::algorithm;absl::base;absl::bind_front;absl::bits;absl::btree;absl::cleanup;absl::cord;absl::core_headers;absl::debugging;absl::die_if_null;absl::dynamic_annotations;absl::flags;absl::flat_hash_map;absl::flat_hash_set;absl::function_ref;absl::hash;absl::if_constexpr;absl::layout;absl::log_initialize;absl::log_globals;absl::log_severity;absl::memory;absl::node_hash_map;absl::node_hash_set;absl::optional;absl::random_distributions;absl::random_random;absl::span;absl::status;absl::statusor;absl::synchronization;absl::time;absl::type_traits;absl::utility;absl::variant;utf8_range::utf8_validity;protobuf::libprotobuf>)

####### Find the libraries declared in cpp_info.libs, create an IMPORTED target for each one and link the
####### protobuf_DEPS_TARGET to all of them
conan_package_library_targets("${protobuf_LIBS_RELEASE}"    # libraries
                              "${protobuf_LIB_DIRS_RELEASE}" # package_libdir
                              "${protobuf_BIN_DIRS_RELEASE}" # package_bindir
                              "${protobuf_LIBRARY_TYPE_RELEASE}"
                              "${protobuf_IS_HOST_WINDOWS_RELEASE}"
                              protobuf_DEPS_TARGET
                              protobuf_LIBRARIES_TARGETS  # out_libraries_targets
                              "_RELEASE"
                              "protobuf"    # package_name
                              "${protobuf_NO_SONAME_MODE_RELEASE}")  # soname

# FIXME: What is the result of this for multi-config? All configs adding themselves to path?
set(CMAKE_MODULE_PATH ${protobuf_BUILD_DIRS_RELEASE} ${CMAKE_MODULE_PATH})

########## COMPONENTS TARGET PROPERTIES Release ########################################

    ########## COMPONENT protobuf::libprotoc #############

        set(protobuf_protobuf_libprotoc_FRAMEWORKS_FOUND_RELEASE "")
        conan_find_apple_frameworks(protobuf_protobuf_libprotoc_FRAMEWORKS_FOUND_RELEASE "${protobuf_protobuf_libprotoc_FRAMEWORKS_RELEASE}" "${protobuf_protobuf_libprotoc_FRAMEWORK_DIRS_RELEASE}")

        set(protobuf_protobuf_libprotoc_LIBRARIES_TARGETS "")

        ######## Create an interface target to contain all the dependencies (frameworks, system and conan deps)
        if(NOT TARGET protobuf_protobuf_libprotoc_DEPS_TARGET)
            add_library(protobuf_protobuf_libprotoc_DEPS_TARGET INTERFACE IMPORTED)
        endif()

        set_property(TARGET protobuf_protobuf_libprotoc_DEPS_TARGET
                     APPEND PROPERTY INTERFACE_LINK_LIBRARIES
                     $<$<CONFIG:Release>:${protobuf_protobuf_libprotoc_FRAMEWORKS_FOUND_RELEASE}>
                     $<$<CONFIG:Release>:${protobuf_protobuf_libprotoc_SYSTEM_LIBS_RELEASE}>
                     $<$<CONFIG:Release>:${protobuf_protobuf_libprotoc_DEPENDENCIES_RELEASE}>
                     )

        ####### Find the libraries declared in cpp_info.component["xxx"].libs,
        ####### create an IMPORTED target for each one and link the 'protobuf_protobuf_libprotoc_DEPS_TARGET' to all of them
        conan_package_library_targets("${protobuf_protobuf_libprotoc_LIBS_RELEASE}"
                              "${protobuf_protobuf_libprotoc_LIB_DIRS_RELEASE}"
                              "${protobuf_protobuf_libprotoc_BIN_DIRS_RELEASE}" # package_bindir
                              "${protobuf_protobuf_libprotoc_LIBRARY_TYPE_RELEASE}"
                              "${protobuf_protobuf_libprotoc_IS_HOST_WINDOWS_RELEASE}"
                              protobuf_protobuf_libprotoc_DEPS_TARGET
                              protobuf_protobuf_libprotoc_LIBRARIES_TARGETS
                              "_RELEASE"
                              "protobuf_protobuf_libprotoc"
                              "${protobuf_protobuf_libprotoc_NO_SONAME_MODE_RELEASE}")


        ########## TARGET PROPERTIES #####################################
        set_property(TARGET protobuf::libprotoc
                     APPEND PROPERTY INTERFACE_LINK_LIBRARIES
                     $<$<CONFIG:Release>:${protobuf_protobuf_libprotoc_OBJECTS_RELEASE}>
                     $<$<CONFIG:Release>:${protobuf_protobuf_libprotoc_LIBRARIES_TARGETS}>
                     )

        if("${protobuf_protobuf_libprotoc_LIBS_RELEASE}" STREQUAL "")
            # If the component is not declaring any "cpp_info.components['foo'].libs" the system, frameworks etc are not
            # linked to the imported targets and we need to do it to the global target
            set_property(TARGET protobuf::libprotoc
                         APPEND PROPERTY INTERFACE_LINK_LIBRARIES
                         protobuf_protobuf_libprotoc_DEPS_TARGET)
        endif()

        set_property(TARGET protobuf::libprotoc APPEND PROPERTY INTERFACE_LINK_OPTIONS
                     $<$<CONFIG:Release>:${protobuf_protobuf_libprotoc_LINKER_FLAGS_RELEASE}>)
        set_property(TARGET protobuf::libprotoc APPEND PROPERTY INTERFACE_INCLUDE_DIRECTORIES
                     $<$<CONFIG:Release>:${protobuf_protobuf_libprotoc_INCLUDE_DIRS_RELEASE}>)
        set_property(TARGET protobuf::libprotoc APPEND PROPERTY INTERFACE_LINK_DIRECTORIES
                     $<$<CONFIG:Release>:${protobuf_protobuf_libprotoc_LIB_DIRS_RELEASE}>)
        set_property(TARGET protobuf::libprotoc APPEND PROPERTY INTERFACE_COMPILE_DEFINITIONS
                     $<$<CONFIG:Release>:${protobuf_protobuf_libprotoc_COMPILE_DEFINITIONS_RELEASE}>)
        set_property(TARGET protobuf::libprotoc APPEND PROPERTY INTERFACE_COMPILE_OPTIONS
                     $<$<CONFIG:Release>:${protobuf_protobuf_libprotoc_COMPILE_OPTIONS_RELEASE}>)


    ########## COMPONENT protobuf::libprotobuf #############

        set(protobuf_protobuf_libprotobuf_FRAMEWORKS_FOUND_RELEASE "")
        conan_find_apple_frameworks(protobuf_protobuf_libprotobuf_FRAMEWORKS_FOUND_RELEASE "${protobuf_protobuf_libprotobuf_FRAMEWORKS_RELEASE}" "${protobuf_protobuf_libprotobuf_FRAMEWORK_DIRS_RELEASE}")

        set(protobuf_protobuf_libprotobuf_LIBRARIES_TARGETS "")

        ######## Create an interface target to contain all the dependencies (frameworks, system and conan deps)
        if(NOT TARGET protobuf_protobuf_libprotobuf_DEPS_TARGET)
            add_library(protobuf_protobuf_libprotobuf_DEPS_TARGET INTERFACE IMPORTED)
        endif()

        set_property(TARGET protobuf_protobuf_libprotobuf_DEPS_TARGET
                     APPEND PROPERTY INTERFACE_LINK_LIBRARIES
                     $<$<CONFIG:Release>:${protobuf_protobuf_libprotobuf_FRAMEWORKS_FOUND_RELEASE}>
                     $<$<CONFIG:Release>:${protobuf_protobuf_libprotobuf_SYSTEM_LIBS_RELEASE}>
                     $<$<CONFIG:Release>:${protobuf_protobuf_libprotobuf_DEPENDENCIES_RELEASE}>
                     )

        ####### Find the libraries declared in cpp_info.component["xxx"].libs,
        ####### create an IMPORTED target for each one and link the 'protobuf_protobuf_libprotobuf_DEPS_TARGET' to all of them
        conan_package_library_targets("${protobuf_protobuf_libprotobuf_LIBS_RELEASE}"
                              "${protobuf_protobuf_libprotobuf_LIB_DIRS_RELEASE}"
                              "${protobuf_protobuf_libprotobuf_BIN_DIRS_RELEASE}" # package_bindir
                              "${protobuf_protobuf_libprotobuf_LIBRARY_TYPE_RELEASE}"
                              "${protobuf_protobuf_libprotobuf_IS_HOST_WINDOWS_RELEASE}"
                              protobuf_protobuf_libprotobuf_DEPS_TARGET
                              protobuf_protobuf_libprotobuf_LIBRARIES_TARGETS
                              "_RELEASE"
                              "protobuf_protobuf_libprotobuf"
                              "${protobuf_protobuf_libprotobuf_NO_SONAME_MODE_RELEASE}")


        ########## TARGET PROPERTIES #####################################
        set_property(TARGET protobuf::libprotobuf
                     APPEND PROPERTY INTERFACE_LINK_LIBRARIES
                     $<$<CONFIG:Release>:${protobuf_protobuf_libprotobuf_OBJECTS_RELEASE}>
                     $<$<CONFIG:Release>:${protobuf_protobuf_libprotobuf_LIBRARIES_TARGETS}>
                     )

        if("${protobuf_protobuf_libprotobuf_LIBS_RELEASE}" STREQUAL "")
            # If the component is not declaring any "cpp_info.components['foo'].libs" the system, frameworks etc are not
            # linked to the imported targets and we need to do it to the global target
            set_property(TARGET protobuf::libprotobuf
                         APPEND PROPERTY INTERFACE_LINK_LIBRARIES
                         protobuf_protobuf_libprotobuf_DEPS_TARGET)
        endif()

        set_property(TARGET protobuf::libprotobuf APPEND PROPERTY INTERFACE_LINK_OPTIONS
                     $<$<CONFIG:Release>:${protobuf_protobuf_libprotobuf_LINKER_FLAGS_RELEASE}>)
        set_property(TARGET protobuf::libprotobuf APPEND PROPERTY INTERFACE_INCLUDE_DIRECTORIES
                     $<$<CONFIG:Release>:${protobuf_protobuf_libprotobuf_INCLUDE_DIRS_RELEASE}>)
        set_property(TARGET protobuf::libprotobuf APPEND PROPERTY INTERFACE_LINK_DIRECTORIES
                     $<$<CONFIG:Release>:${protobuf_protobuf_libprotobuf_LIB_DIRS_RELEASE}>)
        set_property(TARGET protobuf::libprotobuf APPEND PROPERTY INTERFACE_COMPILE_DEFINITIONS
                     $<$<CONFIG:Release>:${protobuf_protobuf_libprotobuf_COMPILE_DEFINITIONS_RELEASE}>)
        set_property(TARGET protobuf::libprotobuf APPEND PROPERTY INTERFACE_COMPILE_OPTIONS
                     $<$<CONFIG:Release>:${protobuf_protobuf_libprotobuf_COMPILE_OPTIONS_RELEASE}>)


    ########## COMPONENT utf8_range::utf8_validity #############

        set(protobuf_utf8_range_utf8_validity_FRAMEWORKS_FOUND_RELEASE "")
        conan_find_apple_frameworks(protobuf_utf8_range_utf8_validity_FRAMEWORKS_FOUND_RELEASE "${protobuf_utf8_range_utf8_validity_FRAMEWORKS_RELEASE}" "${protobuf_utf8_range_utf8_validity_FRAMEWORK_DIRS_RELEASE}")

        set(protobuf_utf8_range_utf8_validity_LIBRARIES_TARGETS "")

        ######## Create an interface target to contain all the dependencies (frameworks, system and conan deps)
        if(NOT TARGET protobuf_utf8_range_utf8_validity_DEPS_TARGET)
            add_library(protobuf_utf8_range_utf8_validity_DEPS_TARGET INTERFACE IMPORTED)
        endif()

        set_property(TARGET protobuf_utf8_range_utf8_validity_DEPS_TARGET
                     APPEND PROPERTY INTERFACE_LINK_LIBRARIES
                     $<$<CONFIG:Release>:${protobuf_utf8_range_utf8_validity_FRAMEWORKS_FOUND_RELEASE}>
                     $<$<CONFIG:Release>:${protobuf_utf8_range_utf8_validity_SYSTEM_LIBS_RELEASE}>
                     $<$<CONFIG:Release>:${protobuf_utf8_range_utf8_validity_DEPENDENCIES_RELEASE}>
                     )

        ####### Find the libraries declared in cpp_info.component["xxx"].libs,
        ####### create an IMPORTED target for each one and link the 'protobuf_utf8_range_utf8_validity_DEPS_TARGET' to all of them
        conan_package_library_targets("${protobuf_utf8_range_utf8_validity_LIBS_RELEASE}"
                              "${protobuf_utf8_range_utf8_validity_LIB_DIRS_RELEASE}"
                              "${protobuf_utf8_range_utf8_validity_BIN_DIRS_RELEASE}" # package_bindir
                              "${protobuf_utf8_range_utf8_validity_LIBRARY_TYPE_RELEASE}"
                              "${protobuf_utf8_range_utf8_validity_IS_HOST_WINDOWS_RELEASE}"
                              protobuf_utf8_range_utf8_validity_DEPS_TARGET
                              protobuf_utf8_range_utf8_validity_LIBRARIES_TARGETS
                              "_RELEASE"
                              "protobuf_utf8_range_utf8_validity"
                              "${protobuf_utf8_range_utf8_validity_NO_SONAME_MODE_RELEASE}")


        ########## TARGET PROPERTIES #####################################
        set_property(TARGET utf8_range::utf8_validity
                     APPEND PROPERTY INTERFACE_LINK_LIBRARIES
                     $<$<CONFIG:Release>:${protobuf_utf8_range_utf8_validity_OBJECTS_RELEASE}>
                     $<$<CONFIG:Release>:${protobuf_utf8_range_utf8_validity_LIBRARIES_TARGETS}>
                     )

        if("${protobuf_utf8_range_utf8_validity_LIBS_RELEASE}" STREQUAL "")
            # If the component is not declaring any "cpp_info.components['foo'].libs" the system, frameworks etc are not
            # linked to the imported targets and we need to do it to the global target
            set_property(TARGET utf8_range::utf8_validity
                         APPEND PROPERTY INTERFACE_LINK_LIBRARIES
                         protobuf_utf8_range_utf8_validity_DEPS_TARGET)
        endif()

        set_property(TARGET utf8_range::utf8_validity APPEND PROPERTY INTERFACE_LINK_OPTIONS
                     $<$<CONFIG:Release>:${protobuf_utf8_range_utf8_validity_LINKER_FLAGS_RELEASE}>)
        set_property(TARGET utf8_range::utf8_validity APPEND PROPERTY INTERFACE_INCLUDE_DIRECTORIES
                     $<$<CONFIG:Release>:${protobuf_utf8_range_utf8_validity_INCLUDE_DIRS_RELEASE}>)
        set_property(TARGET utf8_range::utf8_validity APPEND PROPERTY INTERFACE_LINK_DIRECTORIES
                     $<$<CONFIG:Release>:${protobuf_utf8_range_utf8_validity_LIB_DIRS_RELEASE}>)
        set_property(TARGET utf8_range::utf8_validity APPEND PROPERTY INTERFACE_COMPILE_DEFINITIONS
                     $<$<CONFIG:Release>:${protobuf_utf8_range_utf8_validity_COMPILE_DEFINITIONS_RELEASE}>)
        set_property(TARGET utf8_range::utf8_validity APPEND PROPERTY INTERFACE_COMPILE_OPTIONS
                     $<$<CONFIG:Release>:${protobuf_utf8_range_utf8_validity_COMPILE_OPTIONS_RELEASE}>)


    ########## COMPONENT utf8_range::utf8_range #############

        set(protobuf_utf8_range_utf8_range_FRAMEWORKS_FOUND_RELEASE "")
        conan_find_apple_frameworks(protobuf_utf8_range_utf8_range_FRAMEWORKS_FOUND_RELEASE "${protobuf_utf8_range_utf8_range_FRAMEWORKS_RELEASE}" "${protobuf_utf8_range_utf8_range_FRAMEWORK_DIRS_RELEASE}")

        set(protobuf_utf8_range_utf8_range_LIBRARIES_TARGETS "")

        ######## Create an interface target to contain all the dependencies (frameworks, system and conan deps)
        if(NOT TARGET protobuf_utf8_range_utf8_range_DEPS_TARGET)
            add_library(protobuf_utf8_range_utf8_range_DEPS_TARGET INTERFACE IMPORTED)
        endif()

        set_property(TARGET protobuf_utf8_range_utf8_range_DEPS_TARGET
                     APPEND PROPERTY INTERFACE_LINK_LIBRARIES
                     $<$<CONFIG:Release>:${protobuf_utf8_range_utf8_range_FRAMEWORKS_FOUND_RELEASE}>
                     $<$<CONFIG:Release>:${protobuf_utf8_range_utf8_range_SYSTEM_LIBS_RELEASE}>
                     $<$<CONFIG:Release>:${protobuf_utf8_range_utf8_range_DEPENDENCIES_RELEASE}>
                     )

        ####### Find the libraries declared in cpp_info.component["xxx"].libs,
        ####### create an IMPORTED target for each one and link the 'protobuf_utf8_range_utf8_range_DEPS_TARGET' to all of them
        conan_package_library_targets("${protobuf_utf8_range_utf8_range_LIBS_RELEASE}"
                              "${protobuf_utf8_range_utf8_range_LIB_DIRS_RELEASE}"
                              "${protobuf_utf8_range_utf8_range_BIN_DIRS_RELEASE}" # package_bindir
                              "${protobuf_utf8_range_utf8_range_LIBRARY_TYPE_RELEASE}"
                              "${protobuf_utf8_range_utf8_range_IS_HOST_WINDOWS_RELEASE}"
                              protobuf_utf8_range_utf8_range_DEPS_TARGET
                              protobuf_utf8_range_utf8_range_LIBRARIES_TARGETS
                              "_RELEASE"
                              "protobuf_utf8_range_utf8_range"
                              "${protobuf_utf8_range_utf8_range_NO_SONAME_MODE_RELEASE}")


        ########## TARGET PROPERTIES #####################################
        set_property(TARGET utf8_range::utf8_range
                     APPEND PROPERTY INTERFACE_LINK_LIBRARIES
                     $<$<CONFIG:Release>:${protobuf_utf8_range_utf8_range_OBJECTS_RELEASE}>
                     $<$<CONFIG:Release>:${protobuf_utf8_range_utf8_range_LIBRARIES_TARGETS}>
                     )

        if("${protobuf_utf8_range_utf8_range_LIBS_RELEASE}" STREQUAL "")
            # If the component is not declaring any "cpp_info.components['foo'].libs" the system, frameworks etc are not
            # linked to the imported targets and we need to do it to the global target
            set_property(TARGET utf8_range::utf8_range
                         APPEND PROPERTY INTERFACE_LINK_LIBRARIES
                         protobuf_utf8_range_utf8_range_DEPS_TARGET)
        endif()

        set_property(TARGET utf8_range::utf8_range APPEND PROPERTY INTERFACE_LINK_OPTIONS
                     $<$<CONFIG:Release>:${protobuf_utf8_range_utf8_range_LINKER_FLAGS_RELEASE}>)
        set_property(TARGET utf8_range::utf8_range APPEND PROPERTY INTERFACE_INCLUDE_DIRECTORIES
                     $<$<CONFIG:Release>:${protobuf_utf8_range_utf8_range_INCLUDE_DIRS_RELEASE}>)
        set_property(TARGET utf8_range::utf8_range APPEND PROPERTY INTERFACE_LINK_DIRECTORIES
                     $<$<CONFIG:Release>:${protobuf_utf8_range_utf8_range_LIB_DIRS_RELEASE}>)
        set_property(TARGET utf8_range::utf8_range APPEND PROPERTY INTERFACE_COMPILE_DEFINITIONS
                     $<$<CONFIG:Release>:${protobuf_utf8_range_utf8_range_COMPILE_DEFINITIONS_RELEASE}>)
        set_property(TARGET utf8_range::utf8_range APPEND PROPERTY INTERFACE_COMPILE_OPTIONS
                     $<$<CONFIG:Release>:${protobuf_utf8_range_utf8_range_COMPILE_OPTIONS_RELEASE}>)


    ########## AGGREGATED GLOBAL TARGET WITH THE COMPONENTS #####################
    set_property(TARGET protobuf::protobuf APPEND PROPERTY INTERFACE_LINK_LIBRARIES protobuf::libprotoc)
    set_property(TARGET protobuf::protobuf APPEND PROPERTY INTERFACE_LINK_LIBRARIES protobuf::libprotobuf)
    set_property(TARGET protobuf::protobuf APPEND PROPERTY INTERFACE_LINK_LIBRARIES utf8_range::utf8_validity)
    set_property(TARGET protobuf::protobuf APPEND PROPERTY INTERFACE_LINK_LIBRARIES utf8_range::utf8_range)

########## For the modules (FindXXX)
set(protobuf_LIBRARIES_RELEASE protobuf::protobuf)
