// Lockfile: RTIambassadorFactory per IEEE 1516.1-2010 §10 / Annex A.
// Locks the factory's createRTIambassador() returning rti1516e::auto_ptr
// (per §3.1.0 C++17 resolution: alias for std::unique_ptr under C++17,
// optionally aliased back to std::auto_ptr under C++14 with build flag).
//
// Catalogue rows covered: 2.2.
// FR-DLC requirements: FR-DLC-2.

#include <RTI/RTIambassadorFactory.h>
#include <RTI/RTIambassador.h>
#include <RTI/SpecificConfig.h>
#include <RTI/Exception.h>
#include <type_traits>
#include <memory>

namespace {

using rti1516e::RTIambassador;
using rti1516e::RTIambassadorFactory;

// §10 — RTIambassadorFactory is a concrete, default-constructible class.
//        Federates create the factory on the stack:
//          rti1516e::RTIambassadorFactory factory;
//          auto amb = factory.createRTIambassador();
static_assert(std::is_class_v<RTIambassadorFactory>);
static_assert(!std::is_abstract_v<RTIambassadorFactory>);
static_assert(std::is_default_constructible_v<RTIambassadorFactory>);
static_assert(std::has_virtual_destructor_v<RTIambassadorFactory>);

// §10 — createRTIambassador() returns rti1516e::auto_ptr<RTIambassador>.
//        FR-DLC-2: the spec text says `std::auto_ptr`; gorti's C++17 build
//        target (cppsdk/CMakeLists.txt:31) cannot include `std::auto_ptr`
//        because the C++17 standard library REMOVED it. Per §3.1.0 the
//        resolution is `RTI/SpecificConfig.h` providing
//          template <typename T> using auto_ptr = std::unique_ptr<T>;
//        in namespace rti1516e under C++17+, with optional re-aliasing to
//        std::auto_ptr under C++14 via -DGORTI_DLC_USE_REAL_AUTO_PTR.
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassadorFactory&>().createRTIambassador()),
    rti1516e::auto_ptr<RTIambassador>>);

// §3.1.0 — `rti1516e::auto_ptr<T>` is exactly `std::unique_ptr<T>` under
//          the gorti C++17 default. If the optional C++14
//          GORTI_DLC_USE_REAL_AUTO_PTR build flag is set, this assertion
//          flips to `std::auto_ptr<T>`. Default lockfile is C++17.
#if !defined(GORTI_DLC_USE_REAL_AUTO_PTR)
static_assert(std::is_same_v<
    rti1516e::auto_ptr<RTIambassador>,
    std::unique_ptr<RTIambassador>>);
#endif

// §10 — createRTIambassador may throw RTIinternalError; verify it exists.
static_assert(std::is_class_v<rti1516e::RTIinternalError>);
static_assert(std::is_base_of_v<rti1516e::Exception, rti1516e::RTIinternalError>);

// §10 — the factory holds no state observable to federates: copy / move
//        semantics are not load-bearing, but a non-copyable factory would
//        break federates that pass it by value into helper functions.
//        Lock that the factory is copyable.
static_assert(std::is_copy_constructible_v<RTIambassadorFactory>);

}  // namespace
