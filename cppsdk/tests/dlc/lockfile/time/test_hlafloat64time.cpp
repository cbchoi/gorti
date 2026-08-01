// Lockfile: IEEE 1516.1-2010 RTI/time/HLAfloat64Time.h.
// Catalogue §9 row 9.4 (concrete float64 time type).
//
// M31 RED — fails until M32 lands `RTI/time/HLAfloat64Time.h`.
// Federates that choose HLAfloat64Time include this and instantiate via factory.
//
// IEEE 1516.1-2010 API reference: RTI/time/HLAfloat64Time.h

#include <RTI/time/HLAfloat64Time.h>
#include <RTI/LogicalTime.h>
#include <type_traits>

namespace {

// ---------- Row 9.4: HLAfloat64Time is a concrete subclass of LogicalTime ----------
static_assert(std::is_class_v<rti1516e::HLAfloat64Time>);
static_assert(std::is_base_of_v<rti1516e::LogicalTime, rti1516e::HLAfloat64Time>);
static_assert(std::has_virtual_destructor_v<rti1516e::HLAfloat64Time>);
// reference_rti's concrete is constructible from a bare double (federate code does
// `HLAfloat64Time t(0.0);` directly), and default-constructible.
static_assert(std::is_default_constructible_v<rti1516e::HLAfloat64Time>);
static_assert(std::is_constructible_v<rti1516e::HLAfloat64Time, double>);
static_assert(std::is_copy_constructible_v<rti1516e::HLAfloat64Time>);

// ---------- Concrete-specific surface: getTime / setTime / op double ----------
static_assert(std::is_same_v<
    decltype(std::declval<rti1516e::HLAfloat64Time const&>().getTime()),
    double>);
static_assert(std::is_same_v<
    decltype(std::declval<rti1516e::HLAfloat64Time&>().setTime(
        std::declval<double>())),
    void>);
// operator double() const  — federate code does `double d = t;` directly.
static_assert(std::is_convertible_v<rti1516e::HLAfloat64Time, double>);

// HLAfloat64Time& operator=(double)  per reference_rti — federate code does `t = 1.0`.
static_assert(std::is_assignable_v<rti1516e::HLAfloat64Time&, double>);

// ---------- Inherited spec interface still has the right shape ----------
// implementationName() returns the literal L"HLAfloat64Time" per spec — we
// can only lock the return-type / virtual-presence at compile time.
static_assert(std::is_same_v<
    decltype(std::declval<rti1516e::HLAfloat64Time const&>().implementationName()),
    std::wstring>);
static_assert(std::is_same_v<
    decltype(std::declval<rti1516e::HLAfloat64Time const&>().toString()),
    std::wstring>);

}  // namespace
