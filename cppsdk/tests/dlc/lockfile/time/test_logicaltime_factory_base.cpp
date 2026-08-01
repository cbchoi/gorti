// Lockfile: IEEE 1516.1-2010 RTI/LogicalTimeFactory.h — abstract factory.
// Catalogue §9 row 9.3.
//
// M31 RED — fails until M32 lands `RTI/LogicalTimeFactory.h`. The factory
// returns spec-typed smart pointers (`std::auto_ptr` in C++03; remapped to
// `rti1516e::auto_ptr` via `RTI/SpecificConfig.h` under C++17 per the
// compliance program §3.1.0 — alias to `std::unique_ptr`).
//
// IEEE 1516.1-2010 API reference: RTI/LogicalTimeFactory.h

#include <RTI/LogicalTimeFactory.h>
#include <RTI/LogicalTime.h>
#include <RTI/LogicalTimeInterval.h>
#include <RTI/SpecificConfig.h>
#include <RTI/VariableLengthData.h>
#include <type_traits>
#include <string>

namespace {

// ---------- Row 9.3: LogicalTimeFactory is abstract / polymorphic ----------
static_assert(std::is_class_v<rti1516e::LogicalTimeFactory>);
static_assert(std::is_abstract_v<rti1516e::LogicalTimeFactory>);
static_assert(std::has_virtual_destructor_v<rti1516e::LogicalTimeFactory>);

// ---------- Returns rti1516e::auto_ptr<...> per §3.1.0 C++17 resolution ----------
// makeInitial() / makeFinal() return auto_ptr<LogicalTime>.
static_assert(std::is_same_v<
    decltype(std::declval<rti1516e::LogicalTimeFactory&>().makeInitial()),
    rti1516e::auto_ptr<rti1516e::LogicalTime>>);
static_assert(std::is_same_v<
    decltype(std::declval<rti1516e::LogicalTimeFactory&>().makeFinal()),
    rti1516e::auto_ptr<rti1516e::LogicalTime>>);

// makeZero() / makeEpsilon() return auto_ptr<LogicalTimeInterval>.
static_assert(std::is_same_v<
    decltype(std::declval<rti1516e::LogicalTimeFactory&>().makeZero()),
    rti1516e::auto_ptr<rti1516e::LogicalTimeInterval>>);
static_assert(std::is_same_v<
    decltype(std::declval<rti1516e::LogicalTimeFactory&>().makeEpsilon()),
    rti1516e::auto_ptr<rti1516e::LogicalTimeInterval>>);

// decodeLogicalTime — both overloads return auto_ptr<LogicalTime>.
static_assert(std::is_same_v<
    decltype(std::declval<rti1516e::LogicalTimeFactory&>().decodeLogicalTime(
        std::declval<rti1516e::VariableLengthData const&>())),
    rti1516e::auto_ptr<rti1516e::LogicalTime>>);
static_assert(std::is_same_v<
    decltype(std::declval<rti1516e::LogicalTimeFactory&>().decodeLogicalTime(
        std::declval<void*>(), std::declval<size_t>())),
    rti1516e::auto_ptr<rti1516e::LogicalTime>>);

// decodeLogicalTimeInterval — both overloads return auto_ptr<LogicalTimeInterval>.
static_assert(std::is_same_v<
    decltype(std::declval<rti1516e::LogicalTimeFactory&>().decodeLogicalTimeInterval(
        std::declval<rti1516e::VariableLengthData const&>())),
    rti1516e::auto_ptr<rti1516e::LogicalTimeInterval>>);
static_assert(std::is_same_v<
    decltype(std::declval<rti1516e::LogicalTimeFactory&>().decodeLogicalTimeInterval(
        std::declval<void*>(), std::declval<size_t>())),
    rti1516e::auto_ptr<rti1516e::LogicalTimeInterval>>);

// getName() returns wstring.
static_assert(std::is_same_v<
    decltype(std::declval<rti1516e::LogicalTimeFactory const&>().getName()),
    std::wstring>);

// ---------- LogicalTimeFactoryFactory static maker ----------
// HLAlogicalTimeFactoryFactory::makeLogicalTimeFactory(wstring const&) returns auto_ptr.
static_assert(std::is_class_v<rti1516e::HLAlogicalTimeFactoryFactory>);
static_assert(std::is_same_v<
    decltype(rti1516e::HLAlogicalTimeFactoryFactory::makeLogicalTimeFactory(
        std::declval<std::wstring const&>())),
    rti1516e::auto_ptr<rti1516e::LogicalTimeFactory>>);

// LogicalTimeFactoryFactory (federate-time API factory factory, EXPORT_FEDTIME).
static_assert(std::is_class_v<rti1516e::LogicalTimeFactoryFactory>);
static_assert(std::is_same_v<
    decltype(rti1516e::LogicalTimeFactoryFactory::makeLogicalTimeFactory(
        std::declval<std::wstring const&>())),
    rti1516e::auto_ptr<rti1516e::LogicalTimeFactory>>);

}  // namespace
