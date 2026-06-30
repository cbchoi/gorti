// Lockfile: IEEE 1516.1-2010 RTI/encoding/HLAvariableArray.h — class form.
// Catalogue §14 row 14.5.
//
// M31 RED — fails until M32 lands `RTI/encoding/HLAvariableArray.h`.

#include <RTI/encoding/HLAvariableArray.h>
#include <RTI/encoding/DataElement.h>
#include <type_traits>
#include <cstddef>

namespace {

static_assert(std::is_class_v<rti1516e::HLAvariableArray>);
static_assert(std::is_base_of_v<rti1516e::DataElement, rti1516e::HLAvariableArray>);

// Spec ctor: HLAvariableArray(DataElement const& prototype).
static_assert(std::is_constructible_v<rti1516e::HLAvariableArray,
                                      rti1516e::DataElement const&>);
static_assert(std::is_copy_constructible_v<rti1516e::HLAvariableArray>);

// addElement(DataElement const&)
static_assert(std::is_same_v<
    decltype(std::declval<rti1516e::HLAvariableArray&>().addElement(
        std::declval<rti1516e::DataElement const&>())),
    void>);

// set(size_t, DataElement const&)
static_assert(std::is_same_v<
    decltype(std::declval<rti1516e::HLAvariableArray&>().set(
        std::declval<size_t>(),
        std::declval<rti1516e::DataElement const&>())),
    void>);

// get(size_t) const
static_assert(std::is_same_v<
    decltype(std::declval<rti1516e::HLAvariableArray const&>().get(
        std::declval<size_t>())),
    rti1516e::DataElement const&>);

// size() const
static_assert(std::is_same_v<
    decltype(std::declval<rti1516e::HLAvariableArray const&>().size()),
    size_t>);

}  // namespace
