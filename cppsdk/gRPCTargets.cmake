# Load the debug and release variables
file(GLOB DATA_FILES "${CMAKE_CURRENT_LIST_DIR}/gRPC-*-data.cmake")

foreach(f ${DATA_FILES})
    include(${f})
endforeach()

# Create the targets for all the components
foreach(_COMPONENT ${grpc_COMPONENT_NAMES} )
    if(NOT TARGET ${_COMPONENT})
        add_library(${_COMPONENT} INTERFACE IMPORTED)
        message(${gRPC_MESSAGE_MODE} "Conan: Component target declared '${_COMPONENT}'")
    endif()
endforeach()

if(NOT TARGET grpc::grpc)
    add_library(grpc::grpc INTERFACE IMPORTED)
    message(${gRPC_MESSAGE_MODE} "Conan: Target declared 'grpc::grpc'")
endif()
# Load the debug and release library finders
file(GLOB CONFIG_FILES "${CMAKE_CURRENT_LIST_DIR}/gRPC-Target-*.cmake")

foreach(f ${CONFIG_FILES})
    include(${f})
endforeach()