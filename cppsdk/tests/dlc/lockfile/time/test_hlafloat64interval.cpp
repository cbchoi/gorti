// Lockfile: IEEE 1516.1-2010 RTI/time/HLAfloat64Interval.h.
// Catalogue §9 row 9.4 (concrete float64 interval).
//
// M31 RED — fails until M32 lands the header.

#include <RTI/time/HLAfloat64Interval.h>
#include <RTI/LogicalTimeInterval.h>
#include <type_traits>

namespace {

static_assert(std::is_class_v<rti1516e::HLAfloat64Interval>);
static_assert(std::is_base_of_v<rti1516e::LogicalTimeInterval, rti1516e::HLAfloat64Interval>);
static_assert(std::has_virtual_destructor_v<rti1516e::HLAfloat64Interval>);

static_assert(std::is_default_constructible_v<rti1516e::HLAfloat64Interval>);
static_assert(std::is_constructible_v<rti1516e::HLAfloat64Interval, double>);
static_assert(std::is_copy_constructible_v<rti1516e::HLAfloat64Interval>);

// Concrete-specific surface
static_assert(std::is_same_v<
    decltype(std::declval<rti1516e::HLAfloat64Interval const&>().getInterval()),
    double>);
static_assert(std::is_same_v<
    decltype(std::declval<rti1516e::HLAfloat64Interval&>().setInterval(
        std::declval<double>())),
    void>);
static_assert(std::is_convertible_v<rti1516e::HLAfloat64Interval, double>);
static_assert(std::is_assignable_v<rti1516e::HLAfloat64Interval&, double>);

// Inherited surface presence
static_assert(std::is_same_v<
    decltype(std::declval<rti1516e::HLAfloat64Interval const&>().implementationName()),
    std::wstring>);
static_assert(std::is_same_v<
    decltype(std::declval<rti1516e::HLAfloat64Interval const&>().toString()),
    std::wstring>);

}  // namespace
