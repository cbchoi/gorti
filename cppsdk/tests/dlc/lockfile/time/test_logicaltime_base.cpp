// Lockfile: IEEE 1516.1-2010 RTI/LogicalTime.h — abstract base.
// Catalogue §9 row 9.1.
//
// M31 RED — fails until M32 lands `RTI/LogicalTime.h`. gorti M17 passes
// `double` everywhere, so this base class is entirely absent today.
//
// Pitch ref: ~/prti1516e/api/cpp/HLA_1516-2010/RTI/LogicalTime.h:35-131

#include <RTI/LogicalTime.h>
#include <RTI/VariableLengthData.h>
#include <type_traits>
#include <string>
#include <cstddef>

namespace {

// ---------- Row 9.1: LogicalTime is a polymorphic abstract class ----------
static_assert(std::is_class_v<rti1516e::LogicalTime>);
static_assert(std::is_abstract_v<rti1516e::LogicalTime>);
static_assert(std::is_polymorphic_v<rti1516e::LogicalTime>);
static_assert(std::has_virtual_destructor_v<rti1516e::LogicalTime>);

// ---------- Required pure-virtual surface ----------
// We lock pointer-to-member types; for a pure-virtual decltype works fine.

// setInitial / isInitial / setFinal / isFinal
static_assert(std::is_same_v<
    decltype(static_cast<void (rti1516e::LogicalTime::*)()>(
        &rti1516e::LogicalTime::setInitial)),
    void (rti1516e::LogicalTime::*)()>);
static_assert(std::is_same_v<
    decltype(static_cast<bool (rti1516e::LogicalTime::*)() const>(
        &rti1516e::LogicalTime::isInitial)),
    bool (rti1516e::LogicalTime::*)() const>);
static_assert(std::is_same_v<
    decltype(static_cast<void (rti1516e::LogicalTime::*)()>(
        &rti1516e::LogicalTime::setFinal)),
    void (rti1516e::LogicalTime::*)()>);
static_assert(std::is_same_v<
    decltype(static_cast<bool (rti1516e::LogicalTime::*)() const>(
        &rti1516e::LogicalTime::isFinal)),
    bool (rti1516e::LogicalTime::*)() const>);

// Arithmetic & comparison ops — all take LogicalTimeInterval const& or
// LogicalTime const& and return the spec ref / bool.
static_assert(std::is_same_v<
    decltype(std::declval<rti1516e::LogicalTime&>() = std::declval<rti1516e::LogicalTime const&>()),
    rti1516e::LogicalTime&>);

// op>, op<, op==, op>=, op<= — all bool against LogicalTime const&.
static_assert(std::is_same_v<
    decltype(std::declval<rti1516e::LogicalTime const&>() >
             std::declval<rti1516e::LogicalTime const&>()), bool>);
static_assert(std::is_same_v<
    decltype(std::declval<rti1516e::LogicalTime const&>() <
             std::declval<rti1516e::LogicalTime const&>()), bool>);
static_assert(std::is_same_v<
    decltype(std::declval<rti1516e::LogicalTime const&>() ==
             std::declval<rti1516e::LogicalTime const&>()), bool>);
static_assert(std::is_same_v<
    decltype(std::declval<rti1516e::LogicalTime const&>() >=
             std::declval<rti1516e::LogicalTime const&>()), bool>);
static_assert(std::is_same_v<
    decltype(std::declval<rti1516e::LogicalTime const&>() <=
             std::declval<rti1516e::LogicalTime const&>()), bool>);

// encode() returns VariableLengthData; encodedLength returns size_t.
static_assert(std::is_same_v<
    decltype(std::declval<rti1516e::LogicalTime const&>().encode()),
    rti1516e::VariableLengthData>);
static_assert(std::is_same_v<
    decltype(std::declval<rti1516e::LogicalTime const&>().encodedLength()),
    size_t>);

// toString / implementationName return wstring.
static_assert(std::is_same_v<
    decltype(std::declval<rti1516e::LogicalTime const&>().toString()),
    std::wstring>);
static_assert(std::is_same_v<
    decltype(std::declval<rti1516e::LogicalTime const&>().implementationName()),
    std::wstring>);

}  // namespace
