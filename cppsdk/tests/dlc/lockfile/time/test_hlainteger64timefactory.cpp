// Lockfile: IEEE 1516.1-2010 RTI/time/HLAinteger64TimeFactory.h.
// Catalogue §9 row 9.5 (concrete int64 factory).
//
// M31 RED — fails until M32 lands the header.
//
// Pitch ref: ~/prti1516e/api/cpp/HLA_1516-2010/RTI/time/HLAinteger64TimeFactory.h

#include <RTI/time/HLAinteger64TimeFactory.h>
#include <RTI/time/HLAinteger64Time.h>
#include <RTI/time/HLAinteger64Interval.h>
#include <RTI/LogicalTimeFactory.h>
#include <RTI/SpecificConfig.h>
#include <RTI/encoding/EncodingConfig.h>  // for Integer64
#include <type_traits>
#include <string>

namespace {

static_assert(std::is_class_v<rti1516e::HLAinteger64TimeFactory>);
static_assert(std::is_base_of_v<rti1516e::LogicalTimeFactory,
                                rti1516e::HLAinteger64TimeFactory>);
static_assert(std::has_virtual_destructor_v<rti1516e::HLAinteger64TimeFactory>);

// HLAinteger64TimeName: const std::wstring(L"HLAinteger64Time") at namespace scope.
static_assert(std::is_same_v<
    decltype(rti1516e::HLAinteger64TimeName), std::wstring const>);

// makeLogicalTime(Integer64) returns auto_ptr<HLAinteger64Time> (covariant).
static_assert(std::is_same_v<
    decltype(std::declval<rti1516e::HLAinteger64TimeFactory&>().makeLogicalTime(
        std::declval<rti1516e::Integer64>())),
    rti1516e::auto_ptr<rti1516e::HLAinteger64Time>>);

// makeLogicalTimeInterval(Integer64) returns auto_ptr<HLAinteger64Interval>.
static_assert(std::is_same_v<
    decltype(std::declval<rti1516e::HLAinteger64TimeFactory&>().makeLogicalTimeInterval(
        std::declval<rti1516e::Integer64>())),
    rti1516e::auto_ptr<rti1516e::HLAinteger64Interval>>);

static_assert(std::is_same_v<
    decltype(std::declval<rti1516e::HLAinteger64TimeFactory const&>().getName()),
    std::wstring>);

}  // namespace
