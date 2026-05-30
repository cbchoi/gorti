########### AGGREGATED COMPONENTS AND DEPENDENCIES FOR THE MULTI CONFIG #####################
#############################################################################################

list(APPEND grpc_COMPONENT_NAMES gRPC::address_sorting gRPC::gpr gRPC::upb_base_lib gRPC::upb_mem_lib gRPC::utf8_range_lib gRPC::grpc_plugin_support gRPC::upb_message_lib gRPC::grpc_unsecure gRPC::upb_json_lib gRPC::upb_textformat_lib gRPC::grpc_authorization_provider gRPC::grpc gRPC::grpc++_unsecure gRPC::grpc++ gRPC::grpc++_alts gRPC::grpc++_error_details gRPC::grpc++_reflection gRPC::grpcpp_channelz)
list(REMOVE_DUPLICATES grpc_COMPONENT_NAMES)
if(DEFINED grpc_FIND_DEPENDENCY_NAMES)
  list(APPEND grpc_FIND_DEPENDENCY_NAMES c-ares OpenSSL re2 protobuf ZLIB absl)
  list(REMOVE_DUPLICATES grpc_FIND_DEPENDENCY_NAMES)
else()
  set(grpc_FIND_DEPENDENCY_NAMES c-ares OpenSSL re2 protobuf ZLIB absl)
endif()
set(c-ares_FIND_MODE "NO_MODULE")
set(OpenSSL_FIND_MODE "NO_MODULE")
set(re2_FIND_MODE "NO_MODULE")
set(protobuf_FIND_MODE "NO_MODULE")
set(ZLIB_FIND_MODE "NO_MODULE")
set(absl_FIND_MODE "NO_MODULE")

########### VARIABLES #######################################################################
#############################################################################################
set(grpc_PACKAGE_FOLDER_RELEASE "/home/claude/.conan2/p/b/grpc3459a1a209da6/p")
set(grpc_BUILD_MODULES_PATHS_RELEASE "${grpc_PACKAGE_FOLDER_RELEASE}/lib/cmake/conan_trick/grpc_cpp_plugin.cmake"
			"${grpc_PACKAGE_FOLDER_RELEASE}/lib/cmake/conan_trick/grpc_csharp_plugin.cmake"
			"${grpc_PACKAGE_FOLDER_RELEASE}/lib/cmake/conan_trick/grpc_node_plugin.cmake"
			"${grpc_PACKAGE_FOLDER_RELEASE}/lib/cmake/conan_trick/grpc_objective_c_plugin.cmake"
			"${grpc_PACKAGE_FOLDER_RELEASE}/lib/cmake/conan_trick/grpc_php_plugin.cmake"
			"${grpc_PACKAGE_FOLDER_RELEASE}/lib/cmake/conan_trick/grpc_python_plugin.cmake"
			"${grpc_PACKAGE_FOLDER_RELEASE}/lib/cmake/conan_trick/grpc_ruby_plugin.cmake")


set(grpc_INCLUDE_DIRS_RELEASE "${grpc_PACKAGE_FOLDER_RELEASE}/include")
set(grpc_RES_DIRS_RELEASE )
set(grpc_DEFINITIONS_RELEASE )
set(grpc_SHARED_LINK_FLAGS_RELEASE )
set(grpc_EXE_LINK_FLAGS_RELEASE )
set(grpc_OBJECTS_RELEASE )
set(grpc_COMPILE_DEFINITIONS_RELEASE )
set(grpc_COMPILE_OPTIONS_C_RELEASE )
set(grpc_COMPILE_OPTIONS_CXX_RELEASE )
set(grpc_LIB_DIRS_RELEASE "${grpc_PACKAGE_FOLDER_RELEASE}/lib")
set(grpc_BIN_DIRS_RELEASE )
set(grpc_LIBRARY_TYPE_RELEASE STATIC)
set(grpc_IS_HOST_WINDOWS_RELEASE 0)
set(grpc_LIBS_RELEASE grpcpp_channelz grpc++_reflection grpc++_error_details grpc++_alts grpc++ grpc++_unsecure grpc grpc_authorization_provider upb_textformat_lib upb_json_lib grpc_unsecure upb_message_lib grpc_plugin_support utf8_range_lib upb_mem_lib upb_base_lib gpr address_sorting)
set(grpc_SYSTEM_LIBS_RELEASE m pthread)
set(grpc_FRAMEWORK_DIRS_RELEASE )
set(grpc_FRAMEWORKS_RELEASE )
set(grpc_BUILD_DIRS_RELEASE )
set(grpc_NO_SONAME_MODE_RELEASE FALSE)


# COMPOUND VARIABLES
set(grpc_COMPILE_OPTIONS_RELEASE
    "$<$<COMPILE_LANGUAGE:CXX>:${grpc_COMPILE_OPTIONS_CXX_RELEASE}>"
    "$<$<COMPILE_LANGUAGE:C>:${grpc_COMPILE_OPTIONS_C_RELEASE}>")
set(grpc_LINKER_FLAGS_RELEASE
    "$<$<STREQUAL:$<TARGET_PROPERTY:TYPE>,SHARED_LIBRARY>:${grpc_SHARED_LINK_FLAGS_RELEASE}>"
    "$<$<STREQUAL:$<TARGET_PROPERTY:TYPE>,MODULE_LIBRARY>:${grpc_SHARED_LINK_FLAGS_RELEASE}>"
    "$<$<STREQUAL:$<TARGET_PROPERTY:TYPE>,EXECUTABLE>:${grpc_EXE_LINK_FLAGS_RELEASE}>")


set(grpc_COMPONENTS_RELEASE gRPC::address_sorting gRPC::gpr gRPC::upb_base_lib gRPC::upb_mem_lib gRPC::utf8_range_lib gRPC::grpc_plugin_support gRPC::upb_message_lib gRPC::grpc_unsecure gRPC::upb_json_lib gRPC::upb_textformat_lib gRPC::grpc_authorization_provider gRPC::grpc gRPC::grpc++_unsecure gRPC::grpc++ gRPC::grpc++_alts gRPC::grpc++_error_details gRPC::grpc++_reflection gRPC::grpcpp_channelz)
########### COMPONENT gRPC::grpcpp_channelz VARIABLES ############################################

set(grpc_gRPC_grpcpp_channelz_INCLUDE_DIRS_RELEASE "${grpc_PACKAGE_FOLDER_RELEASE}/include")
set(grpc_gRPC_grpcpp_channelz_LIB_DIRS_RELEASE "${grpc_PACKAGE_FOLDER_RELEASE}/lib")
set(grpc_gRPC_grpcpp_channelz_BIN_DIRS_RELEASE )
set(grpc_gRPC_grpcpp_channelz_LIBRARY_TYPE_RELEASE STATIC)
set(grpc_gRPC_grpcpp_channelz_IS_HOST_WINDOWS_RELEASE 0)
set(grpc_gRPC_grpcpp_channelz_RES_DIRS_RELEASE )
set(grpc_gRPC_grpcpp_channelz_DEFINITIONS_RELEASE )
set(grpc_gRPC_grpcpp_channelz_OBJECTS_RELEASE )
set(grpc_gRPC_grpcpp_channelz_COMPILE_DEFINITIONS_RELEASE )
set(grpc_gRPC_grpcpp_channelz_COMPILE_OPTIONS_C_RELEASE "")
set(grpc_gRPC_grpcpp_channelz_COMPILE_OPTIONS_CXX_RELEASE "")
set(grpc_gRPC_grpcpp_channelz_LIBS_RELEASE grpcpp_channelz)
set(grpc_gRPC_grpcpp_channelz_SYSTEM_LIBS_RELEASE m pthread)
set(grpc_gRPC_grpcpp_channelz_FRAMEWORK_DIRS_RELEASE )
set(grpc_gRPC_grpcpp_channelz_FRAMEWORKS_RELEASE )
set(grpc_gRPC_grpcpp_channelz_DEPENDENCIES_RELEASE gRPC::grpc++)
set(grpc_gRPC_grpcpp_channelz_SHARED_LINK_FLAGS_RELEASE )
set(grpc_gRPC_grpcpp_channelz_EXE_LINK_FLAGS_RELEASE )
set(grpc_gRPC_grpcpp_channelz_NO_SONAME_MODE_RELEASE FALSE)

# COMPOUND VARIABLES
set(grpc_gRPC_grpcpp_channelz_LINKER_FLAGS_RELEASE
        $<$<STREQUAL:$<TARGET_PROPERTY:TYPE>,SHARED_LIBRARY>:${grpc_gRPC_grpcpp_channelz_SHARED_LINK_FLAGS_RELEASE}>
        $<$<STREQUAL:$<TARGET_PROPERTY:TYPE>,MODULE_LIBRARY>:${grpc_gRPC_grpcpp_channelz_SHARED_LINK_FLAGS_RELEASE}>
        $<$<STREQUAL:$<TARGET_PROPERTY:TYPE>,EXECUTABLE>:${grpc_gRPC_grpcpp_channelz_EXE_LINK_FLAGS_RELEASE}>
)
set(grpc_gRPC_grpcpp_channelz_COMPILE_OPTIONS_RELEASE
    "$<$<COMPILE_LANGUAGE:CXX>:${grpc_gRPC_grpcpp_channelz_COMPILE_OPTIONS_CXX_RELEASE}>"
    "$<$<COMPILE_LANGUAGE:C>:${grpc_gRPC_grpcpp_channelz_COMPILE_OPTIONS_C_RELEASE}>")
########### COMPONENT gRPC::grpc++_reflection VARIABLES ############################################

set(grpc_gRPC_grpc++_reflection_INCLUDE_DIRS_RELEASE "${grpc_PACKAGE_FOLDER_RELEASE}/include")
set(grpc_gRPC_grpc++_reflection_LIB_DIRS_RELEASE "${grpc_PACKAGE_FOLDER_RELEASE}/lib")
set(grpc_gRPC_grpc++_reflection_BIN_DIRS_RELEASE )
set(grpc_gRPC_grpc++_reflection_LIBRARY_TYPE_RELEASE STATIC)
set(grpc_gRPC_grpc++_reflection_IS_HOST_WINDOWS_RELEASE 0)
set(grpc_gRPC_grpc++_reflection_RES_DIRS_RELEASE )
set(grpc_gRPC_grpc++_reflection_DEFINITIONS_RELEASE )
set(grpc_gRPC_grpc++_reflection_OBJECTS_RELEASE )
set(grpc_gRPC_grpc++_reflection_COMPILE_DEFINITIONS_RELEASE )
set(grpc_gRPC_grpc++_reflection_COMPILE_OPTIONS_C_RELEASE "")
set(grpc_gRPC_grpc++_reflection_COMPILE_OPTIONS_CXX_RELEASE "")
set(grpc_gRPC_grpc++_reflection_LIBS_RELEASE grpc++_reflection)
set(grpc_gRPC_grpc++_reflection_SYSTEM_LIBS_RELEASE m pthread)
set(grpc_gRPC_grpc++_reflection_FRAMEWORK_DIRS_RELEASE )
set(grpc_gRPC_grpc++_reflection_FRAMEWORKS_RELEASE )
set(grpc_gRPC_grpc++_reflection_DEPENDENCIES_RELEASE gRPC::grpc++)
set(grpc_gRPC_grpc++_reflection_SHARED_LINK_FLAGS_RELEASE )
set(grpc_gRPC_grpc++_reflection_EXE_LINK_FLAGS_RELEASE )
set(grpc_gRPC_grpc++_reflection_NO_SONAME_MODE_RELEASE FALSE)

# COMPOUND VARIABLES
set(grpc_gRPC_grpc++_reflection_LINKER_FLAGS_RELEASE
        $<$<STREQUAL:$<TARGET_PROPERTY:TYPE>,SHARED_LIBRARY>:${grpc_gRPC_grpc++_reflection_SHARED_LINK_FLAGS_RELEASE}>
        $<$<STREQUAL:$<TARGET_PROPERTY:TYPE>,MODULE_LIBRARY>:${grpc_gRPC_grpc++_reflection_SHARED_LINK_FLAGS_RELEASE}>
        $<$<STREQUAL:$<TARGET_PROPERTY:TYPE>,EXECUTABLE>:${grpc_gRPC_grpc++_reflection_EXE_LINK_FLAGS_RELEASE}>
)
set(grpc_gRPC_grpc++_reflection_COMPILE_OPTIONS_RELEASE
    "$<$<COMPILE_LANGUAGE:CXX>:${grpc_gRPC_grpc++_reflection_COMPILE_OPTIONS_CXX_RELEASE}>"
    "$<$<COMPILE_LANGUAGE:C>:${grpc_gRPC_grpc++_reflection_COMPILE_OPTIONS_C_RELEASE}>")
########### COMPONENT gRPC::grpc++_error_details VARIABLES ############################################

set(grpc_gRPC_grpc++_error_details_INCLUDE_DIRS_RELEASE "${grpc_PACKAGE_FOLDER_RELEASE}/include")
set(grpc_gRPC_grpc++_error_details_LIB_DIRS_RELEASE "${grpc_PACKAGE_FOLDER_RELEASE}/lib")
set(grpc_gRPC_grpc++_error_details_BIN_DIRS_RELEASE )
set(grpc_gRPC_grpc++_error_details_LIBRARY_TYPE_RELEASE STATIC)
set(grpc_gRPC_grpc++_error_details_IS_HOST_WINDOWS_RELEASE 0)
set(grpc_gRPC_grpc++_error_details_RES_DIRS_RELEASE )
set(grpc_gRPC_grpc++_error_details_DEFINITIONS_RELEASE )
set(grpc_gRPC_grpc++_error_details_OBJECTS_RELEASE )
set(grpc_gRPC_grpc++_error_details_COMPILE_DEFINITIONS_RELEASE )
set(grpc_gRPC_grpc++_error_details_COMPILE_OPTIONS_C_RELEASE "")
set(grpc_gRPC_grpc++_error_details_COMPILE_OPTIONS_CXX_RELEASE "")
set(grpc_gRPC_grpc++_error_details_LIBS_RELEASE grpc++_error_details)
set(grpc_gRPC_grpc++_error_details_SYSTEM_LIBS_RELEASE m pthread)
set(grpc_gRPC_grpc++_error_details_FRAMEWORK_DIRS_RELEASE )
set(grpc_gRPC_grpc++_error_details_FRAMEWORKS_RELEASE )
set(grpc_gRPC_grpc++_error_details_DEPENDENCIES_RELEASE gRPC::grpc++)
set(grpc_gRPC_grpc++_error_details_SHARED_LINK_FLAGS_RELEASE )
set(grpc_gRPC_grpc++_error_details_EXE_LINK_FLAGS_RELEASE )
set(grpc_gRPC_grpc++_error_details_NO_SONAME_MODE_RELEASE FALSE)

# COMPOUND VARIABLES
set(grpc_gRPC_grpc++_error_details_LINKER_FLAGS_RELEASE
        $<$<STREQUAL:$<TARGET_PROPERTY:TYPE>,SHARED_LIBRARY>:${grpc_gRPC_grpc++_error_details_SHARED_LINK_FLAGS_RELEASE}>
        $<$<STREQUAL:$<TARGET_PROPERTY:TYPE>,MODULE_LIBRARY>:${grpc_gRPC_grpc++_error_details_SHARED_LINK_FLAGS_RELEASE}>
        $<$<STREQUAL:$<TARGET_PROPERTY:TYPE>,EXECUTABLE>:${grpc_gRPC_grpc++_error_details_EXE_LINK_FLAGS_RELEASE}>
)
set(grpc_gRPC_grpc++_error_details_COMPILE_OPTIONS_RELEASE
    "$<$<COMPILE_LANGUAGE:CXX>:${grpc_gRPC_grpc++_error_details_COMPILE_OPTIONS_CXX_RELEASE}>"
    "$<$<COMPILE_LANGUAGE:C>:${grpc_gRPC_grpc++_error_details_COMPILE_OPTIONS_C_RELEASE}>")
########### COMPONENT gRPC::grpc++_alts VARIABLES ############################################

set(grpc_gRPC_grpc++_alts_INCLUDE_DIRS_RELEASE "${grpc_PACKAGE_FOLDER_RELEASE}/include")
set(grpc_gRPC_grpc++_alts_LIB_DIRS_RELEASE "${grpc_PACKAGE_FOLDER_RELEASE}/lib")
set(grpc_gRPC_grpc++_alts_BIN_DIRS_RELEASE )
set(grpc_gRPC_grpc++_alts_LIBRARY_TYPE_RELEASE STATIC)
set(grpc_gRPC_grpc++_alts_IS_HOST_WINDOWS_RELEASE 0)
set(grpc_gRPC_grpc++_alts_RES_DIRS_RELEASE )
set(grpc_gRPC_grpc++_alts_DEFINITIONS_RELEASE )
set(grpc_gRPC_grpc++_alts_OBJECTS_RELEASE )
set(grpc_gRPC_grpc++_alts_COMPILE_DEFINITIONS_RELEASE )
set(grpc_gRPC_grpc++_alts_COMPILE_OPTIONS_C_RELEASE "")
set(grpc_gRPC_grpc++_alts_COMPILE_OPTIONS_CXX_RELEASE "")
set(grpc_gRPC_grpc++_alts_LIBS_RELEASE grpc++_alts)
set(grpc_gRPC_grpc++_alts_SYSTEM_LIBS_RELEASE m pthread)
set(grpc_gRPC_grpc++_alts_FRAMEWORK_DIRS_RELEASE )
set(grpc_gRPC_grpc++_alts_FRAMEWORKS_RELEASE )
set(grpc_gRPC_grpc++_alts_DEPENDENCIES_RELEASE gRPC::grpc++)
set(grpc_gRPC_grpc++_alts_SHARED_LINK_FLAGS_RELEASE )
set(grpc_gRPC_grpc++_alts_EXE_LINK_FLAGS_RELEASE )
set(grpc_gRPC_grpc++_alts_NO_SONAME_MODE_RELEASE FALSE)

# COMPOUND VARIABLES
set(grpc_gRPC_grpc++_alts_LINKER_FLAGS_RELEASE
        $<$<STREQUAL:$<TARGET_PROPERTY:TYPE>,SHARED_LIBRARY>:${grpc_gRPC_grpc++_alts_SHARED_LINK_FLAGS_RELEASE}>
        $<$<STREQUAL:$<TARGET_PROPERTY:TYPE>,MODULE_LIBRARY>:${grpc_gRPC_grpc++_alts_SHARED_LINK_FLAGS_RELEASE}>
        $<$<STREQUAL:$<TARGET_PROPERTY:TYPE>,EXECUTABLE>:${grpc_gRPC_grpc++_alts_EXE_LINK_FLAGS_RELEASE}>
)
set(grpc_gRPC_grpc++_alts_COMPILE_OPTIONS_RELEASE
    "$<$<COMPILE_LANGUAGE:CXX>:${grpc_gRPC_grpc++_alts_COMPILE_OPTIONS_CXX_RELEASE}>"
    "$<$<COMPILE_LANGUAGE:C>:${grpc_gRPC_grpc++_alts_COMPILE_OPTIONS_C_RELEASE}>")
########### COMPONENT gRPC::grpc++ VARIABLES ############################################

set(grpc_gRPC_grpc++_INCLUDE_DIRS_RELEASE "${grpc_PACKAGE_FOLDER_RELEASE}/include")
set(grpc_gRPC_grpc++_LIB_DIRS_RELEASE "${grpc_PACKAGE_FOLDER_RELEASE}/lib")
set(grpc_gRPC_grpc++_BIN_DIRS_RELEASE )
set(grpc_gRPC_grpc++_LIBRARY_TYPE_RELEASE STATIC)
set(grpc_gRPC_grpc++_IS_HOST_WINDOWS_RELEASE 0)
set(grpc_gRPC_grpc++_RES_DIRS_RELEASE )
set(grpc_gRPC_grpc++_DEFINITIONS_RELEASE )
set(grpc_gRPC_grpc++_OBJECTS_RELEASE )
set(grpc_gRPC_grpc++_COMPILE_DEFINITIONS_RELEASE )
set(grpc_gRPC_grpc++_COMPILE_OPTIONS_C_RELEASE "")
set(grpc_gRPC_grpc++_COMPILE_OPTIONS_CXX_RELEASE "")
set(grpc_gRPC_grpc++_LIBS_RELEASE grpc++)
set(grpc_gRPC_grpc++_SYSTEM_LIBS_RELEASE m pthread)
set(grpc_gRPC_grpc++_FRAMEWORK_DIRS_RELEASE )
set(grpc_gRPC_grpc++_FRAMEWORKS_RELEASE )
set(grpc_gRPC_grpc++_DEPENDENCIES_RELEASE absl::absl_check absl::absl_log gRPC::grpc protobuf::libprotobuf)
set(grpc_gRPC_grpc++_SHARED_LINK_FLAGS_RELEASE )
set(grpc_gRPC_grpc++_EXE_LINK_FLAGS_RELEASE )
set(grpc_gRPC_grpc++_NO_SONAME_MODE_RELEASE FALSE)

# COMPOUND VARIABLES
set(grpc_gRPC_grpc++_LINKER_FLAGS_RELEASE
        $<$<STREQUAL:$<TARGET_PROPERTY:TYPE>,SHARED_LIBRARY>:${grpc_gRPC_grpc++_SHARED_LINK_FLAGS_RELEASE}>
        $<$<STREQUAL:$<TARGET_PROPERTY:TYPE>,MODULE_LIBRARY>:${grpc_gRPC_grpc++_SHARED_LINK_FLAGS_RELEASE}>
        $<$<STREQUAL:$<TARGET_PROPERTY:TYPE>,EXECUTABLE>:${grpc_gRPC_grpc++_EXE_LINK_FLAGS_RELEASE}>
)
set(grpc_gRPC_grpc++_COMPILE_OPTIONS_RELEASE
    "$<$<COMPILE_LANGUAGE:CXX>:${grpc_gRPC_grpc++_COMPILE_OPTIONS_CXX_RELEASE}>"
    "$<$<COMPILE_LANGUAGE:C>:${grpc_gRPC_grpc++_COMPILE_OPTIONS_C_RELEASE}>")
########### COMPONENT gRPC::grpc++_unsecure VARIABLES ############################################

set(grpc_gRPC_grpc++_unsecure_INCLUDE_DIRS_RELEASE "${grpc_PACKAGE_FOLDER_RELEASE}/include")
set(grpc_gRPC_grpc++_unsecure_LIB_DIRS_RELEASE "${grpc_PACKAGE_FOLDER_RELEASE}/lib")
set(grpc_gRPC_grpc++_unsecure_BIN_DIRS_RELEASE )
set(grpc_gRPC_grpc++_unsecure_LIBRARY_TYPE_RELEASE STATIC)
set(grpc_gRPC_grpc++_unsecure_IS_HOST_WINDOWS_RELEASE 0)
set(grpc_gRPC_grpc++_unsecure_RES_DIRS_RELEASE )
set(grpc_gRPC_grpc++_unsecure_DEFINITIONS_RELEASE )
set(grpc_gRPC_grpc++_unsecure_OBJECTS_RELEASE )
set(grpc_gRPC_grpc++_unsecure_COMPILE_DEFINITIONS_RELEASE )
set(grpc_gRPC_grpc++_unsecure_COMPILE_OPTIONS_C_RELEASE "")
set(grpc_gRPC_grpc++_unsecure_COMPILE_OPTIONS_CXX_RELEASE "")
set(grpc_gRPC_grpc++_unsecure_LIBS_RELEASE grpc++_unsecure)
set(grpc_gRPC_grpc++_unsecure_SYSTEM_LIBS_RELEASE m pthread)
set(grpc_gRPC_grpc++_unsecure_FRAMEWORK_DIRS_RELEASE )
set(grpc_gRPC_grpc++_unsecure_FRAMEWORKS_RELEASE )
set(grpc_gRPC_grpc++_unsecure_DEPENDENCIES_RELEASE absl::absl_check absl::absl_log gRPC::grpc_unsecure protobuf::libprotobuf)
set(grpc_gRPC_grpc++_unsecure_SHARED_LINK_FLAGS_RELEASE )
set(grpc_gRPC_grpc++_unsecure_EXE_LINK_FLAGS_RELEASE )
set(grpc_gRPC_grpc++_unsecure_NO_SONAME_MODE_RELEASE FALSE)

# COMPOUND VARIABLES
set(grpc_gRPC_grpc++_unsecure_LINKER_FLAGS_RELEASE
        $<$<STREQUAL:$<TARGET_PROPERTY:TYPE>,SHARED_LIBRARY>:${grpc_gRPC_grpc++_unsecure_SHARED_LINK_FLAGS_RELEASE}>
        $<$<STREQUAL:$<TARGET_PROPERTY:TYPE>,MODULE_LIBRARY>:${grpc_gRPC_grpc++_unsecure_SHARED_LINK_FLAGS_RELEASE}>
        $<$<STREQUAL:$<TARGET_PROPERTY:TYPE>,EXECUTABLE>:${grpc_gRPC_grpc++_unsecure_EXE_LINK_FLAGS_RELEASE}>
)
set(grpc_gRPC_grpc++_unsecure_COMPILE_OPTIONS_RELEASE
    "$<$<COMPILE_LANGUAGE:CXX>:${grpc_gRPC_grpc++_unsecure_COMPILE_OPTIONS_CXX_RELEASE}>"
    "$<$<COMPILE_LANGUAGE:C>:${grpc_gRPC_grpc++_unsecure_COMPILE_OPTIONS_C_RELEASE}>")
########### COMPONENT gRPC::grpc VARIABLES ############################################

set(grpc_gRPC_grpc_INCLUDE_DIRS_RELEASE "${grpc_PACKAGE_FOLDER_RELEASE}/include")
set(grpc_gRPC_grpc_LIB_DIRS_RELEASE "${grpc_PACKAGE_FOLDER_RELEASE}/lib")
set(grpc_gRPC_grpc_BIN_DIRS_RELEASE )
set(grpc_gRPC_grpc_LIBRARY_TYPE_RELEASE STATIC)
set(grpc_gRPC_grpc_IS_HOST_WINDOWS_RELEASE 0)
set(grpc_gRPC_grpc_RES_DIRS_RELEASE )
set(grpc_gRPC_grpc_DEFINITIONS_RELEASE )
set(grpc_gRPC_grpc_OBJECTS_RELEASE )
set(grpc_gRPC_grpc_COMPILE_DEFINITIONS_RELEASE )
set(grpc_gRPC_grpc_COMPILE_OPTIONS_C_RELEASE "")
set(grpc_gRPC_grpc_COMPILE_OPTIONS_CXX_RELEASE "")
set(grpc_gRPC_grpc_LIBS_RELEASE grpc)
set(grpc_gRPC_grpc_SYSTEM_LIBS_RELEASE m pthread)
set(grpc_gRPC_grpc_FRAMEWORK_DIRS_RELEASE )
set(grpc_gRPC_grpc_FRAMEWORKS_RELEASE )
set(grpc_gRPC_grpc_DEPENDENCIES_RELEASE gRPC::upb_json_lib gRPC::upb_textformat_lib re2::re2 ZLIB::ZLIB absl::algorithm_container absl::config absl::no_destructor absl::cleanup absl::flat_hash_map absl::flat_hash_set absl::inlined_vector absl::bind_front absl::function_ref absl::hash absl::type_traits absl::random_bit_gen_ref absl::random_distributions absl::statusor absl::span absl::utility c-ares::cares gRPC::gpr OpenSSL::SSL OpenSSL::Crypto gRPC::address_sorting)
set(grpc_gRPC_grpc_SHARED_LINK_FLAGS_RELEASE )
set(grpc_gRPC_grpc_EXE_LINK_FLAGS_RELEASE )
set(grpc_gRPC_grpc_NO_SONAME_MODE_RELEASE FALSE)

# COMPOUND VARIABLES
set(grpc_gRPC_grpc_LINKER_FLAGS_RELEASE
        $<$<STREQUAL:$<TARGET_PROPERTY:TYPE>,SHARED_LIBRARY>:${grpc_gRPC_grpc_SHARED_LINK_FLAGS_RELEASE}>
        $<$<STREQUAL:$<TARGET_PROPERTY:TYPE>,MODULE_LIBRARY>:${grpc_gRPC_grpc_SHARED_LINK_FLAGS_RELEASE}>
        $<$<STREQUAL:$<TARGET_PROPERTY:TYPE>,EXECUTABLE>:${grpc_gRPC_grpc_EXE_LINK_FLAGS_RELEASE}>
)
set(grpc_gRPC_grpc_COMPILE_OPTIONS_RELEASE
    "$<$<COMPILE_LANGUAGE:CXX>:${grpc_gRPC_grpc_COMPILE_OPTIONS_CXX_RELEASE}>"
    "$<$<COMPILE_LANGUAGE:C>:${grpc_gRPC_grpc_COMPILE_OPTIONS_C_RELEASE}>")
########### COMPONENT gRPC::grpc_authorization_provider VARIABLES ############################################

set(grpc_gRPC_grpc_authorization_provider_INCLUDE_DIRS_RELEASE "${grpc_PACKAGE_FOLDER_RELEASE}/include")
set(grpc_gRPC_grpc_authorization_provider_LIB_DIRS_RELEASE "${grpc_PACKAGE_FOLDER_RELEASE}/lib")
set(grpc_gRPC_grpc_authorization_provider_BIN_DIRS_RELEASE )
set(grpc_gRPC_grpc_authorization_provider_LIBRARY_TYPE_RELEASE STATIC)
set(grpc_gRPC_grpc_authorization_provider_IS_HOST_WINDOWS_RELEASE 0)
set(grpc_gRPC_grpc_authorization_provider_RES_DIRS_RELEASE )
set(grpc_gRPC_grpc_authorization_provider_DEFINITIONS_RELEASE )
set(grpc_gRPC_grpc_authorization_provider_OBJECTS_RELEASE )
set(grpc_gRPC_grpc_authorization_provider_COMPILE_DEFINITIONS_RELEASE )
set(grpc_gRPC_grpc_authorization_provider_COMPILE_OPTIONS_C_RELEASE "")
set(grpc_gRPC_grpc_authorization_provider_COMPILE_OPTIONS_CXX_RELEASE "")
set(grpc_gRPC_grpc_authorization_provider_LIBS_RELEASE grpc_authorization_provider)
set(grpc_gRPC_grpc_authorization_provider_SYSTEM_LIBS_RELEASE m pthread)
set(grpc_gRPC_grpc_authorization_provider_FRAMEWORK_DIRS_RELEASE )
set(grpc_gRPC_grpc_authorization_provider_FRAMEWORKS_RELEASE )
set(grpc_gRPC_grpc_authorization_provider_DEPENDENCIES_RELEASE gRPC::utf8_range_lib gRPC::upb_message_lib re2::re2 ZLIB::ZLIB absl::config absl::no_destructor absl::cleanup absl::flat_hash_map absl::flat_hash_set absl::inlined_vector absl::function_ref absl::hash absl::type_traits absl::statusor absl::span absl::utility c-ares::cares gRPC::gpr gRPC::address_sorting)
set(grpc_gRPC_grpc_authorization_provider_SHARED_LINK_FLAGS_RELEASE )
set(grpc_gRPC_grpc_authorization_provider_EXE_LINK_FLAGS_RELEASE )
set(grpc_gRPC_grpc_authorization_provider_NO_SONAME_MODE_RELEASE FALSE)

# COMPOUND VARIABLES
set(grpc_gRPC_grpc_authorization_provider_LINKER_FLAGS_RELEASE
        $<$<STREQUAL:$<TARGET_PROPERTY:TYPE>,SHARED_LIBRARY>:${grpc_gRPC_grpc_authorization_provider_SHARED_LINK_FLAGS_RELEASE}>
        $<$<STREQUAL:$<TARGET_PROPERTY:TYPE>,MODULE_LIBRARY>:${grpc_gRPC_grpc_authorization_provider_SHARED_LINK_FLAGS_RELEASE}>
        $<$<STREQUAL:$<TARGET_PROPERTY:TYPE>,EXECUTABLE>:${grpc_gRPC_grpc_authorization_provider_EXE_LINK_FLAGS_RELEASE}>
)
set(grpc_gRPC_grpc_authorization_provider_COMPILE_OPTIONS_RELEASE
    "$<$<COMPILE_LANGUAGE:CXX>:${grpc_gRPC_grpc_authorization_provider_COMPILE_OPTIONS_CXX_RELEASE}>"
    "$<$<COMPILE_LANGUAGE:C>:${grpc_gRPC_grpc_authorization_provider_COMPILE_OPTIONS_C_RELEASE}>")
########### COMPONENT gRPC::upb_textformat_lib VARIABLES ############################################

set(grpc_gRPC_upb_textformat_lib_INCLUDE_DIRS_RELEASE "${grpc_PACKAGE_FOLDER_RELEASE}/include")
set(grpc_gRPC_upb_textformat_lib_LIB_DIRS_RELEASE "${grpc_PACKAGE_FOLDER_RELEASE}/lib")
set(grpc_gRPC_upb_textformat_lib_BIN_DIRS_RELEASE )
set(grpc_gRPC_upb_textformat_lib_LIBRARY_TYPE_RELEASE STATIC)
set(grpc_gRPC_upb_textformat_lib_IS_HOST_WINDOWS_RELEASE 0)
set(grpc_gRPC_upb_textformat_lib_RES_DIRS_RELEASE )
set(grpc_gRPC_upb_textformat_lib_DEFINITIONS_RELEASE )
set(grpc_gRPC_upb_textformat_lib_OBJECTS_RELEASE )
set(grpc_gRPC_upb_textformat_lib_COMPILE_DEFINITIONS_RELEASE )
set(grpc_gRPC_upb_textformat_lib_COMPILE_OPTIONS_C_RELEASE "")
set(grpc_gRPC_upb_textformat_lib_COMPILE_OPTIONS_CXX_RELEASE "")
set(grpc_gRPC_upb_textformat_lib_LIBS_RELEASE upb_textformat_lib)
set(grpc_gRPC_upb_textformat_lib_SYSTEM_LIBS_RELEASE m pthread)
set(grpc_gRPC_upb_textformat_lib_FRAMEWORK_DIRS_RELEASE )
set(grpc_gRPC_upb_textformat_lib_FRAMEWORKS_RELEASE )
set(grpc_gRPC_upb_textformat_lib_DEPENDENCIES_RELEASE gRPC::utf8_range_lib gRPC::upb_message_lib)
set(grpc_gRPC_upb_textformat_lib_SHARED_LINK_FLAGS_RELEASE )
set(grpc_gRPC_upb_textformat_lib_EXE_LINK_FLAGS_RELEASE )
set(grpc_gRPC_upb_textformat_lib_NO_SONAME_MODE_RELEASE FALSE)

# COMPOUND VARIABLES
set(grpc_gRPC_upb_textformat_lib_LINKER_FLAGS_RELEASE
        $<$<STREQUAL:$<TARGET_PROPERTY:TYPE>,SHARED_LIBRARY>:${grpc_gRPC_upb_textformat_lib_SHARED_LINK_FLAGS_RELEASE}>
        $<$<STREQUAL:$<TARGET_PROPERTY:TYPE>,MODULE_LIBRARY>:${grpc_gRPC_upb_textformat_lib_SHARED_LINK_FLAGS_RELEASE}>
        $<$<STREQUAL:$<TARGET_PROPERTY:TYPE>,EXECUTABLE>:${grpc_gRPC_upb_textformat_lib_EXE_LINK_FLAGS_RELEASE}>
)
set(grpc_gRPC_upb_textformat_lib_COMPILE_OPTIONS_RELEASE
    "$<$<COMPILE_LANGUAGE:CXX>:${grpc_gRPC_upb_textformat_lib_COMPILE_OPTIONS_CXX_RELEASE}>"
    "$<$<COMPILE_LANGUAGE:C>:${grpc_gRPC_upb_textformat_lib_COMPILE_OPTIONS_C_RELEASE}>")
########### COMPONENT gRPC::upb_json_lib VARIABLES ############################################

set(grpc_gRPC_upb_json_lib_INCLUDE_DIRS_RELEASE "${grpc_PACKAGE_FOLDER_RELEASE}/include")
set(grpc_gRPC_upb_json_lib_LIB_DIRS_RELEASE "${grpc_PACKAGE_FOLDER_RELEASE}/lib")
set(grpc_gRPC_upb_json_lib_BIN_DIRS_RELEASE )
set(grpc_gRPC_upb_json_lib_LIBRARY_TYPE_RELEASE STATIC)
set(grpc_gRPC_upb_json_lib_IS_HOST_WINDOWS_RELEASE 0)
set(grpc_gRPC_upb_json_lib_RES_DIRS_RELEASE )
set(grpc_gRPC_upb_json_lib_DEFINITIONS_RELEASE )
set(grpc_gRPC_upb_json_lib_OBJECTS_RELEASE )
set(grpc_gRPC_upb_json_lib_COMPILE_DEFINITIONS_RELEASE )
set(grpc_gRPC_upb_json_lib_COMPILE_OPTIONS_C_RELEASE "")
set(grpc_gRPC_upb_json_lib_COMPILE_OPTIONS_CXX_RELEASE "")
set(grpc_gRPC_upb_json_lib_LIBS_RELEASE upb_json_lib)
set(grpc_gRPC_upb_json_lib_SYSTEM_LIBS_RELEASE m pthread)
set(grpc_gRPC_upb_json_lib_FRAMEWORK_DIRS_RELEASE )
set(grpc_gRPC_upb_json_lib_FRAMEWORKS_RELEASE )
set(grpc_gRPC_upb_json_lib_DEPENDENCIES_RELEASE gRPC::utf8_range_lib gRPC::upb_message_lib)
set(grpc_gRPC_upb_json_lib_SHARED_LINK_FLAGS_RELEASE )
set(grpc_gRPC_upb_json_lib_EXE_LINK_FLAGS_RELEASE )
set(grpc_gRPC_upb_json_lib_NO_SONAME_MODE_RELEASE FALSE)

# COMPOUND VARIABLES
set(grpc_gRPC_upb_json_lib_LINKER_FLAGS_RELEASE
        $<$<STREQUAL:$<TARGET_PROPERTY:TYPE>,SHARED_LIBRARY>:${grpc_gRPC_upb_json_lib_SHARED_LINK_FLAGS_RELEASE}>
        $<$<STREQUAL:$<TARGET_PROPERTY:TYPE>,MODULE_LIBRARY>:${grpc_gRPC_upb_json_lib_SHARED_LINK_FLAGS_RELEASE}>
        $<$<STREQUAL:$<TARGET_PROPERTY:TYPE>,EXECUTABLE>:${grpc_gRPC_upb_json_lib_EXE_LINK_FLAGS_RELEASE}>
)
set(grpc_gRPC_upb_json_lib_COMPILE_OPTIONS_RELEASE
    "$<$<COMPILE_LANGUAGE:CXX>:${grpc_gRPC_upb_json_lib_COMPILE_OPTIONS_CXX_RELEASE}>"
    "$<$<COMPILE_LANGUAGE:C>:${grpc_gRPC_upb_json_lib_COMPILE_OPTIONS_C_RELEASE}>")
########### COMPONENT gRPC::grpc_unsecure VARIABLES ############################################

set(grpc_gRPC_grpc_unsecure_INCLUDE_DIRS_RELEASE "${grpc_PACKAGE_FOLDER_RELEASE}/include")
set(grpc_gRPC_grpc_unsecure_LIB_DIRS_RELEASE "${grpc_PACKAGE_FOLDER_RELEASE}/lib")
set(grpc_gRPC_grpc_unsecure_BIN_DIRS_RELEASE )
set(grpc_gRPC_grpc_unsecure_LIBRARY_TYPE_RELEASE STATIC)
set(grpc_gRPC_grpc_unsecure_IS_HOST_WINDOWS_RELEASE 0)
set(grpc_gRPC_grpc_unsecure_RES_DIRS_RELEASE )
set(grpc_gRPC_grpc_unsecure_DEFINITIONS_RELEASE )
set(grpc_gRPC_grpc_unsecure_OBJECTS_RELEASE )
set(grpc_gRPC_grpc_unsecure_COMPILE_DEFINITIONS_RELEASE )
set(grpc_gRPC_grpc_unsecure_COMPILE_OPTIONS_C_RELEASE "")
set(grpc_gRPC_grpc_unsecure_COMPILE_OPTIONS_CXX_RELEASE "")
set(grpc_gRPC_grpc_unsecure_LIBS_RELEASE grpc_unsecure)
set(grpc_gRPC_grpc_unsecure_SYSTEM_LIBS_RELEASE m pthread)
set(grpc_gRPC_grpc_unsecure_FRAMEWORK_DIRS_RELEASE )
set(grpc_gRPC_grpc_unsecure_FRAMEWORKS_RELEASE )
set(grpc_gRPC_grpc_unsecure_DEPENDENCIES_RELEASE gRPC::utf8_range_lib gRPC::upb_message_lib ZLIB::ZLIB absl::algorithm_container absl::config absl::no_destructor absl::cleanup absl::flat_hash_map absl::flat_hash_set absl::inlined_vector absl::bind_front absl::function_ref absl::hash absl::type_traits absl::random_bit_gen_ref absl::random_distributions absl::statusor absl::span absl::utility c-ares::cares gRPC::gpr gRPC::address_sorting)
set(grpc_gRPC_grpc_unsecure_SHARED_LINK_FLAGS_RELEASE )
set(grpc_gRPC_grpc_unsecure_EXE_LINK_FLAGS_RELEASE )
set(grpc_gRPC_grpc_unsecure_NO_SONAME_MODE_RELEASE FALSE)

# COMPOUND VARIABLES
set(grpc_gRPC_grpc_unsecure_LINKER_FLAGS_RELEASE
        $<$<STREQUAL:$<TARGET_PROPERTY:TYPE>,SHARED_LIBRARY>:${grpc_gRPC_grpc_unsecure_SHARED_LINK_FLAGS_RELEASE}>
        $<$<STREQUAL:$<TARGET_PROPERTY:TYPE>,MODULE_LIBRARY>:${grpc_gRPC_grpc_unsecure_SHARED_LINK_FLAGS_RELEASE}>
        $<$<STREQUAL:$<TARGET_PROPERTY:TYPE>,EXECUTABLE>:${grpc_gRPC_grpc_unsecure_EXE_LINK_FLAGS_RELEASE}>
)
set(grpc_gRPC_grpc_unsecure_COMPILE_OPTIONS_RELEASE
    "$<$<COMPILE_LANGUAGE:CXX>:${grpc_gRPC_grpc_unsecure_COMPILE_OPTIONS_CXX_RELEASE}>"
    "$<$<COMPILE_LANGUAGE:C>:${grpc_gRPC_grpc_unsecure_COMPILE_OPTIONS_C_RELEASE}>")
########### COMPONENT gRPC::upb_message_lib VARIABLES ############################################

set(grpc_gRPC_upb_message_lib_INCLUDE_DIRS_RELEASE "${grpc_PACKAGE_FOLDER_RELEASE}/include")
set(grpc_gRPC_upb_message_lib_LIB_DIRS_RELEASE "${grpc_PACKAGE_FOLDER_RELEASE}/lib")
set(grpc_gRPC_upb_message_lib_BIN_DIRS_RELEASE )
set(grpc_gRPC_upb_message_lib_LIBRARY_TYPE_RELEASE STATIC)
set(grpc_gRPC_upb_message_lib_IS_HOST_WINDOWS_RELEASE 0)
set(grpc_gRPC_upb_message_lib_RES_DIRS_RELEASE )
set(grpc_gRPC_upb_message_lib_DEFINITIONS_RELEASE )
set(grpc_gRPC_upb_message_lib_OBJECTS_RELEASE )
set(grpc_gRPC_upb_message_lib_COMPILE_DEFINITIONS_RELEASE )
set(grpc_gRPC_upb_message_lib_COMPILE_OPTIONS_C_RELEASE "")
set(grpc_gRPC_upb_message_lib_COMPILE_OPTIONS_CXX_RELEASE "")
set(grpc_gRPC_upb_message_lib_LIBS_RELEASE upb_message_lib)
set(grpc_gRPC_upb_message_lib_SYSTEM_LIBS_RELEASE m pthread)
set(grpc_gRPC_upb_message_lib_FRAMEWORK_DIRS_RELEASE )
set(grpc_gRPC_upb_message_lib_FRAMEWORKS_RELEASE )
set(grpc_gRPC_upb_message_lib_DEPENDENCIES_RELEASE gRPC::upb_base_lib gRPC::upb_mem_lib)
set(grpc_gRPC_upb_message_lib_SHARED_LINK_FLAGS_RELEASE )
set(grpc_gRPC_upb_message_lib_EXE_LINK_FLAGS_RELEASE )
set(grpc_gRPC_upb_message_lib_NO_SONAME_MODE_RELEASE FALSE)

# COMPOUND VARIABLES
set(grpc_gRPC_upb_message_lib_LINKER_FLAGS_RELEASE
        $<$<STREQUAL:$<TARGET_PROPERTY:TYPE>,SHARED_LIBRARY>:${grpc_gRPC_upb_message_lib_SHARED_LINK_FLAGS_RELEASE}>
        $<$<STREQUAL:$<TARGET_PROPERTY:TYPE>,MODULE_LIBRARY>:${grpc_gRPC_upb_message_lib_SHARED_LINK_FLAGS_RELEASE}>
        $<$<STREQUAL:$<TARGET_PROPERTY:TYPE>,EXECUTABLE>:${grpc_gRPC_upb_message_lib_EXE_LINK_FLAGS_RELEASE}>
)
set(grpc_gRPC_upb_message_lib_COMPILE_OPTIONS_RELEASE
    "$<$<COMPILE_LANGUAGE:CXX>:${grpc_gRPC_upb_message_lib_COMPILE_OPTIONS_CXX_RELEASE}>"
    "$<$<COMPILE_LANGUAGE:C>:${grpc_gRPC_upb_message_lib_COMPILE_OPTIONS_C_RELEASE}>")
########### COMPONENT gRPC::grpc_plugin_support VARIABLES ############################################

set(grpc_gRPC_grpc_plugin_support_INCLUDE_DIRS_RELEASE "${grpc_PACKAGE_FOLDER_RELEASE}/include")
set(grpc_gRPC_grpc_plugin_support_LIB_DIRS_RELEASE "${grpc_PACKAGE_FOLDER_RELEASE}/lib")
set(grpc_gRPC_grpc_plugin_support_BIN_DIRS_RELEASE )
set(grpc_gRPC_grpc_plugin_support_LIBRARY_TYPE_RELEASE STATIC)
set(grpc_gRPC_grpc_plugin_support_IS_HOST_WINDOWS_RELEASE 0)
set(grpc_gRPC_grpc_plugin_support_RES_DIRS_RELEASE )
set(grpc_gRPC_grpc_plugin_support_DEFINITIONS_RELEASE )
set(grpc_gRPC_grpc_plugin_support_OBJECTS_RELEASE )
set(grpc_gRPC_grpc_plugin_support_COMPILE_DEFINITIONS_RELEASE )
set(grpc_gRPC_grpc_plugin_support_COMPILE_OPTIONS_C_RELEASE "")
set(grpc_gRPC_grpc_plugin_support_COMPILE_OPTIONS_CXX_RELEASE "")
set(grpc_gRPC_grpc_plugin_support_LIBS_RELEASE grpc_plugin_support)
set(grpc_gRPC_grpc_plugin_support_SYSTEM_LIBS_RELEASE m pthread)
set(grpc_gRPC_grpc_plugin_support_FRAMEWORK_DIRS_RELEASE )
set(grpc_gRPC_grpc_plugin_support_FRAMEWORKS_RELEASE )
set(grpc_gRPC_grpc_plugin_support_DEPENDENCIES_RELEASE protobuf::libprotobuf protobuf::libprotoc)
set(grpc_gRPC_grpc_plugin_support_SHARED_LINK_FLAGS_RELEASE )
set(grpc_gRPC_grpc_plugin_support_EXE_LINK_FLAGS_RELEASE )
set(grpc_gRPC_grpc_plugin_support_NO_SONAME_MODE_RELEASE FALSE)

# COMPOUND VARIABLES
set(grpc_gRPC_grpc_plugin_support_LINKER_FLAGS_RELEASE
        $<$<STREQUAL:$<TARGET_PROPERTY:TYPE>,SHARED_LIBRARY>:${grpc_gRPC_grpc_plugin_support_SHARED_LINK_FLAGS_RELEASE}>
        $<$<STREQUAL:$<TARGET_PROPERTY:TYPE>,MODULE_LIBRARY>:${grpc_gRPC_grpc_plugin_support_SHARED_LINK_FLAGS_RELEASE}>
        $<$<STREQUAL:$<TARGET_PROPERTY:TYPE>,EXECUTABLE>:${grpc_gRPC_grpc_plugin_support_EXE_LINK_FLAGS_RELEASE}>
)
set(grpc_gRPC_grpc_plugin_support_COMPILE_OPTIONS_RELEASE
    "$<$<COMPILE_LANGUAGE:CXX>:${grpc_gRPC_grpc_plugin_support_COMPILE_OPTIONS_CXX_RELEASE}>"
    "$<$<COMPILE_LANGUAGE:C>:${grpc_gRPC_grpc_plugin_support_COMPILE_OPTIONS_C_RELEASE}>")
########### COMPONENT gRPC::utf8_range_lib VARIABLES ############################################

set(grpc_gRPC_utf8_range_lib_INCLUDE_DIRS_RELEASE "${grpc_PACKAGE_FOLDER_RELEASE}/include")
set(grpc_gRPC_utf8_range_lib_LIB_DIRS_RELEASE "${grpc_PACKAGE_FOLDER_RELEASE}/lib")
set(grpc_gRPC_utf8_range_lib_BIN_DIRS_RELEASE )
set(grpc_gRPC_utf8_range_lib_LIBRARY_TYPE_RELEASE STATIC)
set(grpc_gRPC_utf8_range_lib_IS_HOST_WINDOWS_RELEASE 0)
set(grpc_gRPC_utf8_range_lib_RES_DIRS_RELEASE )
set(grpc_gRPC_utf8_range_lib_DEFINITIONS_RELEASE )
set(grpc_gRPC_utf8_range_lib_OBJECTS_RELEASE )
set(grpc_gRPC_utf8_range_lib_COMPILE_DEFINITIONS_RELEASE )
set(grpc_gRPC_utf8_range_lib_COMPILE_OPTIONS_C_RELEASE "")
set(grpc_gRPC_utf8_range_lib_COMPILE_OPTIONS_CXX_RELEASE "")
set(grpc_gRPC_utf8_range_lib_LIBS_RELEASE utf8_range_lib)
set(grpc_gRPC_utf8_range_lib_SYSTEM_LIBS_RELEASE m pthread)
set(grpc_gRPC_utf8_range_lib_FRAMEWORK_DIRS_RELEASE )
set(grpc_gRPC_utf8_range_lib_FRAMEWORKS_RELEASE )
set(grpc_gRPC_utf8_range_lib_DEPENDENCIES_RELEASE )
set(grpc_gRPC_utf8_range_lib_SHARED_LINK_FLAGS_RELEASE )
set(grpc_gRPC_utf8_range_lib_EXE_LINK_FLAGS_RELEASE )
set(grpc_gRPC_utf8_range_lib_NO_SONAME_MODE_RELEASE FALSE)

# COMPOUND VARIABLES
set(grpc_gRPC_utf8_range_lib_LINKER_FLAGS_RELEASE
        $<$<STREQUAL:$<TARGET_PROPERTY:TYPE>,SHARED_LIBRARY>:${grpc_gRPC_utf8_range_lib_SHARED_LINK_FLAGS_RELEASE}>
        $<$<STREQUAL:$<TARGET_PROPERTY:TYPE>,MODULE_LIBRARY>:${grpc_gRPC_utf8_range_lib_SHARED_LINK_FLAGS_RELEASE}>
        $<$<STREQUAL:$<TARGET_PROPERTY:TYPE>,EXECUTABLE>:${grpc_gRPC_utf8_range_lib_EXE_LINK_FLAGS_RELEASE}>
)
set(grpc_gRPC_utf8_range_lib_COMPILE_OPTIONS_RELEASE
    "$<$<COMPILE_LANGUAGE:CXX>:${grpc_gRPC_utf8_range_lib_COMPILE_OPTIONS_CXX_RELEASE}>"
    "$<$<COMPILE_LANGUAGE:C>:${grpc_gRPC_utf8_range_lib_COMPILE_OPTIONS_C_RELEASE}>")
########### COMPONENT gRPC::upb_mem_lib VARIABLES ############################################

set(grpc_gRPC_upb_mem_lib_INCLUDE_DIRS_RELEASE "${grpc_PACKAGE_FOLDER_RELEASE}/include")
set(grpc_gRPC_upb_mem_lib_LIB_DIRS_RELEASE "${grpc_PACKAGE_FOLDER_RELEASE}/lib")
set(grpc_gRPC_upb_mem_lib_BIN_DIRS_RELEASE )
set(grpc_gRPC_upb_mem_lib_LIBRARY_TYPE_RELEASE STATIC)
set(grpc_gRPC_upb_mem_lib_IS_HOST_WINDOWS_RELEASE 0)
set(grpc_gRPC_upb_mem_lib_RES_DIRS_RELEASE )
set(grpc_gRPC_upb_mem_lib_DEFINITIONS_RELEASE )
set(grpc_gRPC_upb_mem_lib_OBJECTS_RELEASE )
set(grpc_gRPC_upb_mem_lib_COMPILE_DEFINITIONS_RELEASE )
set(grpc_gRPC_upb_mem_lib_COMPILE_OPTIONS_C_RELEASE "")
set(grpc_gRPC_upb_mem_lib_COMPILE_OPTIONS_CXX_RELEASE "")
set(grpc_gRPC_upb_mem_lib_LIBS_RELEASE upb_mem_lib)
set(grpc_gRPC_upb_mem_lib_SYSTEM_LIBS_RELEASE m pthread)
set(grpc_gRPC_upb_mem_lib_FRAMEWORK_DIRS_RELEASE )
set(grpc_gRPC_upb_mem_lib_FRAMEWORKS_RELEASE )
set(grpc_gRPC_upb_mem_lib_DEPENDENCIES_RELEASE )
set(grpc_gRPC_upb_mem_lib_SHARED_LINK_FLAGS_RELEASE )
set(grpc_gRPC_upb_mem_lib_EXE_LINK_FLAGS_RELEASE )
set(grpc_gRPC_upb_mem_lib_NO_SONAME_MODE_RELEASE FALSE)

# COMPOUND VARIABLES
set(grpc_gRPC_upb_mem_lib_LINKER_FLAGS_RELEASE
        $<$<STREQUAL:$<TARGET_PROPERTY:TYPE>,SHARED_LIBRARY>:${grpc_gRPC_upb_mem_lib_SHARED_LINK_FLAGS_RELEASE}>
        $<$<STREQUAL:$<TARGET_PROPERTY:TYPE>,MODULE_LIBRARY>:${grpc_gRPC_upb_mem_lib_SHARED_LINK_FLAGS_RELEASE}>
        $<$<STREQUAL:$<TARGET_PROPERTY:TYPE>,EXECUTABLE>:${grpc_gRPC_upb_mem_lib_EXE_LINK_FLAGS_RELEASE}>
)
set(grpc_gRPC_upb_mem_lib_COMPILE_OPTIONS_RELEASE
    "$<$<COMPILE_LANGUAGE:CXX>:${grpc_gRPC_upb_mem_lib_COMPILE_OPTIONS_CXX_RELEASE}>"
    "$<$<COMPILE_LANGUAGE:C>:${grpc_gRPC_upb_mem_lib_COMPILE_OPTIONS_C_RELEASE}>")
########### COMPONENT gRPC::upb_base_lib VARIABLES ############################################

set(grpc_gRPC_upb_base_lib_INCLUDE_DIRS_RELEASE "${grpc_PACKAGE_FOLDER_RELEASE}/include")
set(grpc_gRPC_upb_base_lib_LIB_DIRS_RELEASE "${grpc_PACKAGE_FOLDER_RELEASE}/lib")
set(grpc_gRPC_upb_base_lib_BIN_DIRS_RELEASE )
set(grpc_gRPC_upb_base_lib_LIBRARY_TYPE_RELEASE STATIC)
set(grpc_gRPC_upb_base_lib_IS_HOST_WINDOWS_RELEASE 0)
set(grpc_gRPC_upb_base_lib_RES_DIRS_RELEASE )
set(grpc_gRPC_upb_base_lib_DEFINITIONS_RELEASE )
set(grpc_gRPC_upb_base_lib_OBJECTS_RELEASE )
set(grpc_gRPC_upb_base_lib_COMPILE_DEFINITIONS_RELEASE )
set(grpc_gRPC_upb_base_lib_COMPILE_OPTIONS_C_RELEASE "")
set(grpc_gRPC_upb_base_lib_COMPILE_OPTIONS_CXX_RELEASE "")
set(grpc_gRPC_upb_base_lib_LIBS_RELEASE upb_base_lib)
set(grpc_gRPC_upb_base_lib_SYSTEM_LIBS_RELEASE m pthread)
set(grpc_gRPC_upb_base_lib_FRAMEWORK_DIRS_RELEASE )
set(grpc_gRPC_upb_base_lib_FRAMEWORKS_RELEASE )
set(grpc_gRPC_upb_base_lib_DEPENDENCIES_RELEASE )
set(grpc_gRPC_upb_base_lib_SHARED_LINK_FLAGS_RELEASE )
set(grpc_gRPC_upb_base_lib_EXE_LINK_FLAGS_RELEASE )
set(grpc_gRPC_upb_base_lib_NO_SONAME_MODE_RELEASE FALSE)

# COMPOUND VARIABLES
set(grpc_gRPC_upb_base_lib_LINKER_FLAGS_RELEASE
        $<$<STREQUAL:$<TARGET_PROPERTY:TYPE>,SHARED_LIBRARY>:${grpc_gRPC_upb_base_lib_SHARED_LINK_FLAGS_RELEASE}>
        $<$<STREQUAL:$<TARGET_PROPERTY:TYPE>,MODULE_LIBRARY>:${grpc_gRPC_upb_base_lib_SHARED_LINK_FLAGS_RELEASE}>
        $<$<STREQUAL:$<TARGET_PROPERTY:TYPE>,EXECUTABLE>:${grpc_gRPC_upb_base_lib_EXE_LINK_FLAGS_RELEASE}>
)
set(grpc_gRPC_upb_base_lib_COMPILE_OPTIONS_RELEASE
    "$<$<COMPILE_LANGUAGE:CXX>:${grpc_gRPC_upb_base_lib_COMPILE_OPTIONS_CXX_RELEASE}>"
    "$<$<COMPILE_LANGUAGE:C>:${grpc_gRPC_upb_base_lib_COMPILE_OPTIONS_C_RELEASE}>")
########### COMPONENT gRPC::gpr VARIABLES ############################################

set(grpc_gRPC_gpr_INCLUDE_DIRS_RELEASE "${grpc_PACKAGE_FOLDER_RELEASE}/include")
set(grpc_gRPC_gpr_LIB_DIRS_RELEASE "${grpc_PACKAGE_FOLDER_RELEASE}/lib")
set(grpc_gRPC_gpr_BIN_DIRS_RELEASE )
set(grpc_gRPC_gpr_LIBRARY_TYPE_RELEASE STATIC)
set(grpc_gRPC_gpr_IS_HOST_WINDOWS_RELEASE 0)
set(grpc_gRPC_gpr_RES_DIRS_RELEASE )
set(grpc_gRPC_gpr_DEFINITIONS_RELEASE )
set(grpc_gRPC_gpr_OBJECTS_RELEASE )
set(grpc_gRPC_gpr_COMPILE_DEFINITIONS_RELEASE )
set(grpc_gRPC_gpr_COMPILE_OPTIONS_C_RELEASE "")
set(grpc_gRPC_gpr_COMPILE_OPTIONS_CXX_RELEASE "")
set(grpc_gRPC_gpr_LIBS_RELEASE gpr)
set(grpc_gRPC_gpr_SYSTEM_LIBS_RELEASE m pthread)
set(grpc_gRPC_gpr_FRAMEWORK_DIRS_RELEASE )
set(grpc_gRPC_gpr_FRAMEWORKS_RELEASE )
set(grpc_gRPC_gpr_DEPENDENCIES_RELEASE absl::base absl::core_headers absl::log_severity absl::flags absl::flags_marshalling absl::any_invocable absl::check absl::log_globals absl::log absl::memory absl::random_random absl::status absl::cord absl::str_format absl::strings absl::synchronization absl::time absl::optional absl::variant)
set(grpc_gRPC_gpr_SHARED_LINK_FLAGS_RELEASE )
set(grpc_gRPC_gpr_EXE_LINK_FLAGS_RELEASE )
set(grpc_gRPC_gpr_NO_SONAME_MODE_RELEASE FALSE)

# COMPOUND VARIABLES
set(grpc_gRPC_gpr_LINKER_FLAGS_RELEASE
        $<$<STREQUAL:$<TARGET_PROPERTY:TYPE>,SHARED_LIBRARY>:${grpc_gRPC_gpr_SHARED_LINK_FLAGS_RELEASE}>
        $<$<STREQUAL:$<TARGET_PROPERTY:TYPE>,MODULE_LIBRARY>:${grpc_gRPC_gpr_SHARED_LINK_FLAGS_RELEASE}>
        $<$<STREQUAL:$<TARGET_PROPERTY:TYPE>,EXECUTABLE>:${grpc_gRPC_gpr_EXE_LINK_FLAGS_RELEASE}>
)
set(grpc_gRPC_gpr_COMPILE_OPTIONS_RELEASE
    "$<$<COMPILE_LANGUAGE:CXX>:${grpc_gRPC_gpr_COMPILE_OPTIONS_CXX_RELEASE}>"
    "$<$<COMPILE_LANGUAGE:C>:${grpc_gRPC_gpr_COMPILE_OPTIONS_C_RELEASE}>")
########### COMPONENT gRPC::address_sorting VARIABLES ############################################

set(grpc_gRPC_address_sorting_INCLUDE_DIRS_RELEASE "${grpc_PACKAGE_FOLDER_RELEASE}/include")
set(grpc_gRPC_address_sorting_LIB_DIRS_RELEASE "${grpc_PACKAGE_FOLDER_RELEASE}/lib")
set(grpc_gRPC_address_sorting_BIN_DIRS_RELEASE )
set(grpc_gRPC_address_sorting_LIBRARY_TYPE_RELEASE STATIC)
set(grpc_gRPC_address_sorting_IS_HOST_WINDOWS_RELEASE 0)
set(grpc_gRPC_address_sorting_RES_DIRS_RELEASE )
set(grpc_gRPC_address_sorting_DEFINITIONS_RELEASE )
set(grpc_gRPC_address_sorting_OBJECTS_RELEASE )
set(grpc_gRPC_address_sorting_COMPILE_DEFINITIONS_RELEASE )
set(grpc_gRPC_address_sorting_COMPILE_OPTIONS_C_RELEASE "")
set(grpc_gRPC_address_sorting_COMPILE_OPTIONS_CXX_RELEASE "")
set(grpc_gRPC_address_sorting_LIBS_RELEASE address_sorting)
set(grpc_gRPC_address_sorting_SYSTEM_LIBS_RELEASE m pthread)
set(grpc_gRPC_address_sorting_FRAMEWORK_DIRS_RELEASE )
set(grpc_gRPC_address_sorting_FRAMEWORKS_RELEASE )
set(grpc_gRPC_address_sorting_DEPENDENCIES_RELEASE )
set(grpc_gRPC_address_sorting_SHARED_LINK_FLAGS_RELEASE )
set(grpc_gRPC_address_sorting_EXE_LINK_FLAGS_RELEASE )
set(grpc_gRPC_address_sorting_NO_SONAME_MODE_RELEASE FALSE)

# COMPOUND VARIABLES
set(grpc_gRPC_address_sorting_LINKER_FLAGS_RELEASE
        $<$<STREQUAL:$<TARGET_PROPERTY:TYPE>,SHARED_LIBRARY>:${grpc_gRPC_address_sorting_SHARED_LINK_FLAGS_RELEASE}>
        $<$<STREQUAL:$<TARGET_PROPERTY:TYPE>,MODULE_LIBRARY>:${grpc_gRPC_address_sorting_SHARED_LINK_FLAGS_RELEASE}>
        $<$<STREQUAL:$<TARGET_PROPERTY:TYPE>,EXECUTABLE>:${grpc_gRPC_address_sorting_EXE_LINK_FLAGS_RELEASE}>
)
set(grpc_gRPC_address_sorting_COMPILE_OPTIONS_RELEASE
    "$<$<COMPILE_LANGUAGE:CXX>:${grpc_gRPC_address_sorting_COMPILE_OPTIONS_CXX_RELEASE}>"
    "$<$<COMPILE_LANGUAGE:C>:${grpc_gRPC_address_sorting_COMPILE_OPTIONS_C_RELEASE}>")