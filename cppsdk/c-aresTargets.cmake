# Load the debug and release variables
file(GLOB DATA_FILES "${CMAKE_CURRENT_LIST_DIR}/c-ares-*-data.cmake")

foreach(f ${DATA_FILES})
    include(${f})
endforeach()

# Create the targets for all the components
foreach(_COMPONENT ${c-ares_COMPONENT_NAMES} )
    if(NOT TARGET ${_COMPONENT})
        add_library(${_COMPONENT} INTERFACE IMPORTED)
        message(${c-ares_MESSAGE_MODE} "Conan: Component target declared '${_COMPONENT}'")
    endif()
endforeach()

if(NOT TARGET c-ares::cares)
    add_library(c-ares::cares INTERFACE IMPORTED)
    message(${c-ares_MESSAGE_MODE} "Conan: Target declared 'c-ares::cares'")
endif()
# Load the debug and release library finders
file(GLOB CONFIG_FILES "${CMAKE_CURRENT_LIST_DIR}/c-ares-Target-*.cmake")

foreach(f ${CONFIG_FILES})
    include(${f})
endforeach()