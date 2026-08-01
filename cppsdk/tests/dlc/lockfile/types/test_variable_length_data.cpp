// Lockfile: IEEE 1516.1-2010 RTI/VariableLengthData.h — Annex A class form.
// Catalogue §8 rows 8.1-8.3.
//
// M31 RED — fails until M32 lands `RTI/VariableLengthData.h` with the
// spec class (NOT a std::vector<uint8_t> alias, which is gorti's M17 form).
//
// Three blocking divergences:
//   8.1 — must be a class, not an alias.
//   8.2 — three ownership modes (copy / borrow / take).
//   8.3 — VariableLengthDataDeleteFunction typedef on void(*)(void*).
//
// IEEE 1516.1-2010 API reference: RTI/VariableLengthData.h

#include <RTI/VariableLengthData.h>
#include <type_traits>
#include <cstddef>
#include <vector>
#include <cstdint>

namespace {

// ---------- Row 8.1: VariableLengthData is a class, NOT std::vector ----------
static_assert(std::is_class_v<rti1516e::VariableLengthData>);
static_assert(!std::is_same_v<rti1516e::VariableLengthData, std::vector<uint8_t>>);
static_assert(!std::is_same_v<rti1516e::VariableLengthData, std::vector<std::byte>>);

// ---------- Constructors ----------
// Default ctor.
static_assert(std::is_default_constructible_v<rti1516e::VariableLengthData>);
// ctor(void const*, size_t) — caller-copies path (row 8.2).
static_assert(std::is_constructible_v<rti1516e::VariableLengthData,
                                      void const*, size_t>);
// Copy ctor.
static_assert(std::is_copy_constructible_v<rti1516e::VariableLengthData>);
// Copy assign — reverts to internal storage per reference_rti comment.
static_assert(std::is_copy_assignable_v<rti1516e::VariableLengthData>);
// Destructible.
static_assert(std::is_destructible_v<rti1516e::VariableLengthData>);

// ---------- Member accessors ----------
// void const* data() const
static_assert(std::is_same_v<
    decltype(std::declval<rti1516e::VariableLengthData const&>().data()),
    void const*>);
// size_t size() const
static_assert(std::is_same_v<
    decltype(std::declval<rti1516e::VariableLengthData const&>().size()),
    size_t>);

// ---------- Row 8.2: three ownership modes — setData / setDataPointer / takeDataPointer
// setData(void const*, size_t) — copies.
static_assert(std::is_same_v<
    decltype(std::declval<rti1516e::VariableLengthData&>().setData(
        std::declval<void const*>(), std::declval<size_t>())),
    void>);
// setDataPointer(void*, size_t) — borrows; caller keeps ownership.
static_assert(std::is_same_v<
    decltype(std::declval<rti1516e::VariableLengthData&>().setDataPointer(
        std::declval<void*>(), std::declval<size_t>())),
    void>);
// takeDataPointer(void*, size_t, VariableLengthDataDeleteFunction=0) — takes ownership.
static_assert(std::is_same_v<
    decltype(std::declval<rti1516e::VariableLengthData&>().takeDataPointer(
        std::declval<void*>(), std::declval<size_t>(),
        std::declval<rti1516e::VariableLengthDataDeleteFunction>())),
    void>);

// ---------- Row 8.3: VariableLengthDataDeleteFunction is a free typedef ----------
static_assert(std::is_same_v<rti1516e::VariableLengthDataDeleteFunction,
                             void (*)(void*)>);

// Default-arg path of takeDataPointer (no deleter).
static_assert(std::is_invocable_v<
    decltype(&rti1516e::VariableLengthData::takeDataPointer),
    rti1516e::VariableLengthData&,
    void*, size_t,
    rti1516e::VariableLengthDataDeleteFunction>);

}  // namespace
