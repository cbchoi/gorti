// Lockfile: IEEE 1516.1-2010 — HLAunicodeString stores std::wstring.
// Catalogue §14 row 14.3.
//
// M31 RED — fails until M32 lands `RTI/encoding/BasicDataElements.h` with the
// HLAunicodeString class that operates on std::wstring. gorti M17 has only
// `encodeHLAunicodeString(u16string_view)` — the BLOCKING divergence.

#include <RTI/encoding/BasicDataElements.h>
#include <RTI/encoding/DataElement.h>
#include <type_traits>
#include <string>

namespace {

// Class derives from DataElement.
static_assert(std::is_class_v<rti1516e::HLAunicodeString>);
static_assert(std::is_base_of_v<rti1516e::DataElement, rti1516e::HLAunicodeString>);

// Default ctor.
static_assert(std::is_default_constructible_v<rti1516e::HLAunicodeString>);

// Ctor takes wstring (NOT u16string).
static_assert(std::is_constructible_v<rti1516e::HLAunicodeString, std::wstring const&>);

// Copy ctor + assignment.
static_assert(std::is_copy_constructible_v<rti1516e::HLAunicodeString>);
static_assert(std::is_copy_assignable_v<rti1516e::HLAunicodeString>);

// op=(wstring const&) — for convenient federate code `s = L"hello";`.
static_assert(std::is_assignable_v<rti1516e::HLAunicodeString&, std::wstring const&>);

// set(wstring const&) and get() returning wstring.
static_assert(std::is_same_v<
    decltype(std::declval<rti1516e::HLAunicodeString&>().set(
        std::declval<std::wstring const&>())),
    void>);
static_assert(std::is_same_v<
    decltype(std::declval<rti1516e::HLAunicodeString const&>().get()),
    std::wstring>);

// Implicit conversion to wstring — federates write `wstring s = u;`.
static_assert(std::is_convertible_v<rti1516e::HLAunicodeString, std::wstring>);

}  // namespace
