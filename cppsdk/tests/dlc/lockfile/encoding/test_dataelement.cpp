// Lockfile: IEEE 1516.1-2010 RTI/encoding/DataElement.h — abstract base.
// Catalogue §14 row 14.1.
//
// M31 RED — fails until M32 lands `RTI/encoding/DataElement.h`. gorti M17 has
// free encode_* functions, not a polymorphic class — the BLOCKING divergence.
//
// IEEE 1516.1-2010 API reference: RTI/encoding/DataElement.h

#include <RTI/encoding/DataElement.h>
#include <RTI/encoding/EncodingConfig.h>
#include <RTI/SpecificConfig.h>
#include <RTI/VariableLengthData.h>
#include <type_traits>
#include <cstddef>
#include <vector>

namespace {

// ---------- Row 14.1: DataElement is abstract / polymorphic ----------
static_assert(std::is_class_v<rti1516e::DataElement>);
static_assert(std::is_abstract_v<rti1516e::DataElement>);
static_assert(std::is_polymorphic_v<rti1516e::DataElement>);
static_assert(std::has_virtual_destructor_v<rti1516e::DataElement>);

// ---------- Row 14.10: clone() returns auto_ptr<DataElement> ----------
// Under C++17 this is rti1516e::auto_ptr alias to std::unique_ptr.
static_assert(std::is_same_v<
    decltype(std::declval<rti1516e::DataElement const&>().clone()),
    rti1516e::auto_ptr<rti1516e::DataElement>>);

// ---------- Pure-virtual surface lock ----------
// encode() / encode(VariableLengthData&) / encodeInto(vector<Octet>&)
static_assert(std::is_same_v<
    decltype(std::declval<rti1516e::DataElement const&>().encode()),
    rti1516e::VariableLengthData>);
static_assert(std::is_same_v<
    decltype(std::declval<rti1516e::DataElement const&>().encode(
        std::declval<rti1516e::VariableLengthData&>())),
    void>);
static_assert(std::is_same_v<
    decltype(std::declval<rti1516e::DataElement const&>().encodeInto(
        std::declval<std::vector<rti1516e::Octet>&>())),
    void>);

// decode(VariableLengthData const&) / decodeFrom(vector<Octet> const&, size_t)
static_assert(std::is_same_v<
    decltype(std::declval<rti1516e::DataElement&>().decode(
        std::declval<rti1516e::VariableLengthData const&>())),
    void>);
static_assert(std::is_same_v<
    decltype(std::declval<rti1516e::DataElement&>().decodeFrom(
        std::declval<std::vector<rti1516e::Octet> const&>(),
        std::declval<size_t>())),
    size_t>);

// getEncodedLength / getOctetBoundary
static_assert(std::is_same_v<
    decltype(std::declval<rti1516e::DataElement const&>().getEncodedLength()),
    size_t>);
static_assert(std::is_same_v<
    decltype(std::declval<rti1516e::DataElement const&>().getOctetBoundary()),
    unsigned int>);

// isSameTypeAs / hash — virtual but with default impls per spec.
static_assert(std::is_same_v<
    decltype(std::declval<rti1516e::DataElement const&>().isSameTypeAs(
        std::declval<rti1516e::DataElement const&>())),
    bool>);
static_assert(std::is_same_v<
    decltype(std::declval<rti1516e::DataElement const&>().hash()),
    rti1516e::Integer64>);

}  // namespace
