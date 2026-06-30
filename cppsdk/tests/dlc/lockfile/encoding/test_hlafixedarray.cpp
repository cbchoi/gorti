// Lockfile: IEEE 1516.1-2010 RTI/encoding/HLAfixedArray.h — class form.
// Catalogue §14 row 14.4.
//
// M31 RED — fails until M32 lands `RTI/encoding/HLAfixedArray.h`. gorti M17
// has `encodeHLAfixedArray` as a free template — the BLOCKING divergence.

#include <RTI/encoding/HLAfixedArray.h>
#include <RTI/encoding/DataElement.h>
#include <type_traits>
#include <cstddef>

namespace {

static_assert(std::is_class_v<rti1516e::HLAfixedArray>);
static_assert(std::is_base_of_v<rti1516e::DataElement, rti1516e::HLAfixedArray>);

// Spec ctor: HLAfixedArray(DataElement const& prototype, size_t length).
static_assert(std::is_constructible_v<rti1516e::HLAfixedArray,
                                      rti1516e::DataElement const&,
                                      size_t>);
// Copy ctor.
static_assert(std::is_copy_constructible_v<rti1516e::HLAfixedArray>);

// size() const
static_assert(std::is_same_v<
    decltype(std::declval<rti1516e::HLAfixedArray const&>().size()),
    size_t>);

// set(size_t, DataElement const&)
static_assert(std::is_same_v<
    decltype(std::declval<rti1516e::HLAfixedArray&>().set(
        std::declval<size_t>(),
        std::declval<rti1516e::DataElement const&>())),
    void>);

// get(size_t) const — DataElement const&
static_assert(std::is_same_v<
    decltype(std::declval<rti1516e::HLAfixedArray const&>().get(
        std::declval<size_t>())),
    rti1516e::DataElement const&>);

}  // namespace
