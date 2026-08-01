// Lockfile: IEEE 1516.1-2010 RTI/RangeBounds.h — DDM dimension range.
// Catalogue §10 row 10.2 (the class half).
//
// M31 RED — fails until M32 lands `RTI/RangeBounds.h` with the spec class
// (gorti's M17 has `struct DimensionRange { uint64 lower, upper; }` which is
// the BLOCKING divergence).
//
// IEEE 1516.1-2010 API reference: RTI/RangeBounds.h

#include <RTI/RangeBounds.h>
#include <type_traits>

namespace {

// ---------- Row 10.2: RangeBounds is a class with getter/setter pairs ----------
static_assert(std::is_class_v<rti1516e::RangeBounds>);

// Default ctor.
static_assert(std::is_default_constructible_v<rti1516e::RangeBounds>);
// (lower, upper) ctor — both unsigned long per spec.
static_assert(std::is_constructible_v<rti1516e::RangeBounds,
                                      unsigned long, unsigned long>);
// Copy ctor + assignment.
static_assert(std::is_copy_constructible_v<rti1516e::RangeBounds>);
static_assert(std::is_copy_assignable_v<rti1516e::RangeBounds>);
static_assert(std::is_destructible_v<rti1516e::RangeBounds>);

// Getters return unsigned long.
static_assert(std::is_same_v<
    decltype(std::declval<rti1516e::RangeBounds const&>().getLowerBound()),
    unsigned long>);
static_assert(std::is_same_v<
    decltype(std::declval<rti1516e::RangeBounds const&>().getUpperBound()),
    unsigned long>);

// Setters take unsigned long, return void.
static_assert(std::is_same_v<
    decltype(std::declval<rti1516e::RangeBounds&>().setLowerBound(
        std::declval<unsigned long>())),
    void>);
static_assert(std::is_same_v<
    decltype(std::declval<rti1516e::RangeBounds&>().setUpperBound(
        std::declval<unsigned long>())),
    void>);

// ---------- Negative lock: NOT a POD struct with raw uint64 fields ----------
// gorti's M17 `DimensionRange` has public `lower`/`upper` members; the spec
// hides them behind getters/setters. So a public `lower` member must NOT exist.
// We can't easily lock the absence of a member; the getter contract above is
// the positive lock that already forces the redesign.

}  // namespace
