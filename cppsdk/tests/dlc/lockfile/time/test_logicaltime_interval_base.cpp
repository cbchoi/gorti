// Lockfile: IEEE 1516.1-2010 RTI/LogicalTimeInterval.h — abstract base.
// Catalogue §9 row 9.2.
//
// M31 RED — fails until M32 lands `RTI/LogicalTimeInterval.h`.
//
// Pitch ref: ~/prti1516e/api/cpp/HLA_1516-2010/RTI/LogicalTimeInterval.h:35-139

#include <RTI/LogicalTimeInterval.h>
#include <RTI/VariableLengthData.h>
#include <type_traits>
#include <string>
#include <cstddef>

namespace {

static_assert(std::is_class_v<rti1516e::LogicalTimeInterval>);
static_assert(std::is_abstract_v<rti1516e::LogicalTimeInterval>);
static_assert(std::is_polymorphic_v<rti1516e::LogicalTimeInterval>);
static_assert(std::has_virtual_destructor_v<rti1516e::LogicalTimeInterval>);

// setZero / isZero / setEpsilon / isEpsilon
static_assert(std::is_same_v<
    decltype(static_cast<void (rti1516e::LogicalTimeInterval::*)()>(
        &rti1516e::LogicalTimeInterval::setZero)),
    void (rti1516e::LogicalTimeInterval::*)()>);
static_assert(std::is_same_v<
    decltype(static_cast<bool (rti1516e::LogicalTimeInterval::*)() const>(
        &rti1516e::LogicalTimeInterval::isZero)),
    bool (rti1516e::LogicalTimeInterval::*)() const>);
static_assert(std::is_same_v<
    decltype(static_cast<void (rti1516e::LogicalTimeInterval::*)()>(
        &rti1516e::LogicalTimeInterval::setEpsilon)),
    void (rti1516e::LogicalTimeInterval::*)()>);
static_assert(std::is_same_v<
    decltype(static_cast<bool (rti1516e::LogicalTimeInterval::*)() const>(
        &rti1516e::LogicalTimeInterval::isEpsilon)),
    bool (rti1516e::LogicalTimeInterval::*)() const>);

// op>, op<, op==, op>=, op<= against LogicalTimeInterval const&.
static_assert(std::is_same_v<
    decltype(std::declval<rti1516e::LogicalTimeInterval const&>() >
             std::declval<rti1516e::LogicalTimeInterval const&>()), bool>);
static_assert(std::is_same_v<
    decltype(std::declval<rti1516e::LogicalTimeInterval const&>() <
             std::declval<rti1516e::LogicalTimeInterval const&>()), bool>);
static_assert(std::is_same_v<
    decltype(std::declval<rti1516e::LogicalTimeInterval const&>() ==
             std::declval<rti1516e::LogicalTimeInterval const&>()), bool>);
static_assert(std::is_same_v<
    decltype(std::declval<rti1516e::LogicalTimeInterval const&>() >=
             std::declval<rti1516e::LogicalTimeInterval const&>()), bool>);
static_assert(std::is_same_v<
    decltype(std::declval<rti1516e::LogicalTimeInterval const&>() <=
             std::declval<rti1516e::LogicalTimeInterval const&>()), bool>);

// encode / encodedLength / toString / implementationName
static_assert(std::is_same_v<
    decltype(std::declval<rti1516e::LogicalTimeInterval const&>().encode()),
    rti1516e::VariableLengthData>);
static_assert(std::is_same_v<
    decltype(std::declval<rti1516e::LogicalTimeInterval const&>().encodedLength()),
    size_t>);
static_assert(std::is_same_v<
    decltype(std::declval<rti1516e::LogicalTimeInterval const&>().toString()),
    std::wstring>);
static_assert(std::is_same_v<
    decltype(std::declval<rti1516e::LogicalTimeInterval const&>().implementationName()),
    std::wstring>);

}  // namespace
