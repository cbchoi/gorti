// Lockfile: IEEE 1516.1-2010 RTI/time/HLAinteger64Time.h.
// Catalogue §9 row 9.5 (concrete int64 time).
//
// M31 RED — fails until M32 lands the header.

#include <RTI/time/HLAinteger64Time.h>
#include <RTI/LogicalTime.h>
#include <RTI/encoding/EncodingConfig.h>  // for Integer64
#include <type_traits>

namespace {

static_assert(std::is_class_v<rti1516e::HLAinteger64Time>);
static_assert(std::is_base_of_v<rti1516e::LogicalTime, rti1516e::HLAinteger64Time>);
static_assert(std::has_virtual_destructor_v<rti1516e::HLAinteger64Time>);

static_assert(std::is_default_constructible_v<rti1516e::HLAinteger64Time>);
static_assert(std::is_constructible_v<rti1516e::HLAinteger64Time, rti1516e::Integer64>);
static_assert(std::is_copy_constructible_v<rti1516e::HLAinteger64Time>);

static_assert(std::is_same_v<
    decltype(std::declval<rti1516e::HLAinteger64Time const&>().getTime()),
    rti1516e::Integer64>);
static_assert(std::is_same_v<
    decltype(std::declval<rti1516e::HLAinteger64Time&>().setTime(
        std::declval<rti1516e::Integer64>())),
    void>);
static_assert(std::is_convertible_v<rti1516e::HLAinteger64Time, rti1516e::Integer64>);
static_assert(std::is_assignable_v<rti1516e::HLAinteger64Time&, rti1516e::Integer64>);

static_assert(std::is_same_v<
    decltype(std::declval<rti1516e::HLAinteger64Time const&>().implementationName()),
    std::wstring>);

}  // namespace
