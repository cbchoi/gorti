// Lockfile: IEEE 1516.1-2010 RTI/encoding/HLAfixedRecord.h — class form.
// Catalogue §14 row 14.6.
//
// M31 RED — fails until M32 lands `RTI/encoding/HLAfixedRecord.h`. gorti M17
// has `encodeHLAfixedRecord` as a free template — the BLOCKING divergence.
//
// IEEE 1516.1-2010 API reference: RTI/encoding/HLAfixedRecord.h

#include <RTI/encoding/HLAfixedRecord.h>
#include <RTI/encoding/DataElement.h>
#include <type_traits>
#include <cstddef>

namespace {

static_assert(std::is_class_v<rti1516e::HLAfixedRecord>);
static_assert(std::is_base_of_v<rti1516e::DataElement, rti1516e::HLAfixedRecord>);

static_assert(std::is_default_constructible_v<rti1516e::HLAfixedRecord>);
static_assert(std::is_copy_constructible_v<rti1516e::HLAfixedRecord>);

// appendElement(DataElement const&) — value-copy append.
static_assert(std::is_same_v<
    decltype(std::declval<rti1516e::HLAfixedRecord&>().appendElement(
        std::declval<rti1516e::DataElement const&>())),
    void>);

// set(size_t, DataElement const&) — replace element at index.
static_assert(std::is_same_v<
    decltype(std::declval<rti1516e::HLAfixedRecord&>().set(
        std::declval<size_t>(),
        std::declval<rti1516e::DataElement const&>())),
    void>);

// get(size_t) const — returns DataElement const&.
static_assert(std::is_same_v<
    decltype(std::declval<rti1516e::HLAfixedRecord const&>().get(
        std::declval<size_t>())),
    rti1516e::DataElement const&>);

// size() const.
static_assert(std::is_same_v<
    decltype(std::declval<rti1516e::HLAfixedRecord const&>().size()),
    size_t>);

}  // namespace
