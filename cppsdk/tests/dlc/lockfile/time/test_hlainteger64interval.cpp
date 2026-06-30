// Lockfile: IEEE 1516.1-2010 RTI/time/HLAinteger64Interval.h.
// Catalogue §9 row 9.5 (concrete int64 interval).
//
// M31 RED — fails until M32 lands the header.

#include <RTI/time/HLAinteger64Interval.h>
#include <RTI/LogicalTimeInterval.h>
#include <RTI/encoding/EncodingConfig.h>  // for Integer64
#include <type_traits>

namespace {

static_assert(std::is_class_v<rti1516e::HLAinteger64Interval>);
static_assert(std::is_base_of_v<rti1516e::LogicalTimeInterval,
                                rti1516e::HLAinteger64Interval>);
static_assert(std::has_virtual_destructor_v<rti1516e::HLAinteger64Interval>);

static_assert(std::is_default_constructible_v<rti1516e::HLAinteger64Interval>);
static_assert(std::is_constructible_v<rti1516e::HLAinteger64Interval, rti1516e::Integer64>);
static_assert(std::is_copy_constructible_v<rti1516e::HLAinteger64Interval>);

static_assert(std::is_same_v<
    decltype(std::declval<rti1516e::HLAinteger64Interval const&>().getInterval()),
    rti1516e::Integer64>);
static_assert(std::is_same_v<
    decltype(std::declval<rti1516e::HLAinteger64Interval&>().setInterval(
        std::declval<rti1516e::Integer64>())),
    void>);
static_assert(std::is_convertible_v<rti1516e::HLAinteger64Interval, rti1516e::Integer64>);
static_assert(std::is_assignable_v<rti1516e::HLAinteger64Interval&, rti1516e::Integer64>);

static_assert(std::is_same_v<
    decltype(std::declval<rti1516e::HLAinteger64Interval const&>().implementationName()),
    std::wstring>);

}  // namespace
