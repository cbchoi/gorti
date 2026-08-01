// Lockfile: IEEE 1516.1-2010 RTI/encoding/HLAopaqueData.h.
// Catalogue §14 row 14.8.
//
// M31 RED — fails until M32 lands `RTI/encoding/HLAopaqueData.h`. gorti M17 uses
// raw `vector<uint8_t>` — the MAJOR divergence.

#include <RTI/encoding/HLAopaqueData.h>
#include <RTI/encoding/DataElement.h>
#include <RTI/encoding/EncodingConfig.h>
#include <type_traits>
#include <cstddef>

namespace {

static_assert(std::is_class_v<rti1516e::HLAopaqueData>);
static_assert(std::is_base_of_v<rti1516e::DataElement, rti1516e::HLAopaqueData>);

static_assert(std::is_default_constructible_v<rti1516e::HLAopaqueData>);
static_assert(std::is_copy_constructible_v<rti1516e::HLAopaqueData>);

// bufferLength() const — total backing buffer size.
static_assert(std::is_same_v<
    decltype(std::declval<rti1516e::HLAopaqueData const&>().bufferLength()),
    size_t>);

// dataLength() const — bytes actually used.
static_assert(std::is_same_v<
    decltype(std::declval<rti1516e::HLAopaqueData const&>().dataLength()),
    size_t>);

// setDataPointer(Octet** data, size_t bufferSize, size_t dataSize) per reference_rti.
static_assert(std::is_invocable_v<
    decltype(&rti1516e::HLAopaqueData::setDataPointer),
    rti1516e::HLAopaqueData&,
    rti1516e::Octet**, size_t, size_t>);

// get() const — reference_rti: const Octet* get() const  (read-only data pointer).
static_assert(std::is_same_v<
    decltype(std::declval<rti1516e::HLAopaqueData const&>().get()),
    rti1516e::Octet const*>);

}  // namespace
