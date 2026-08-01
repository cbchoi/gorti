// Lockfile: IEEE 1516.1-2010 RTI/time/HLAfloat64TimeFactory.h.
// Catalogue §9 row 9.4 (concrete float64 factory).
//
// M31 RED — fails until M32 lands the header.
//
// IEEE 1516.1-2010 API reference: RTI/time/HLAfloat64TimeFactory.h

#include <RTI/time/HLAfloat64TimeFactory.h>
#include <RTI/time/HLAfloat64Time.h>
#include <RTI/time/HLAfloat64Interval.h>
#include <RTI/LogicalTimeFactory.h>
#include <RTI/SpecificConfig.h>
#include <type_traits>
#include <string>

namespace {

static_assert(std::is_class_v<rti1516e::HLAfloat64TimeFactory>);
static_assert(std::is_base_of_v<rti1516e::LogicalTimeFactory,
                                rti1516e::HLAfloat64TimeFactory>);
static_assert(std::has_virtual_destructor_v<rti1516e::HLAfloat64TimeFactory>);

// HLAfloat64TimeName: const std::wstring(L"HLAfloat64Time")
// declared at namespace scope in the header.
static_assert(std::is_same_v<
    decltype(rti1516e::HLAfloat64TimeName), std::wstring const>);

// makeLogicalTime(double) returns auto_ptr<HLAfloat64Time> (covariant).
static_assert(std::is_same_v<
    decltype(std::declval<rti1516e::HLAfloat64TimeFactory&>().makeLogicalTime(
        std::declval<double>())),
    rti1516e::auto_ptr<rti1516e::HLAfloat64Time>>);

// makeLogicalTimeInterval(double) returns auto_ptr<HLAfloat64Interval>.
static_assert(std::is_same_v<
    decltype(std::declval<rti1516e::HLAfloat64TimeFactory&>().makeLogicalTimeInterval(
        std::declval<double>())),
    rti1516e::auto_ptr<rti1516e::HLAfloat64Interval>>);

// getName returns L"HLAfloat64Time" — only return-type lockable at compile time.
static_assert(std::is_same_v<
    decltype(std::declval<rti1516e::HLAfloat64TimeFactory const&>().getName()),
    std::wstring>);

}  // namespace
