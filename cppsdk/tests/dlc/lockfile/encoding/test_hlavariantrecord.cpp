// Lockfile: IEEE 1516.1-2010 RTI/encoding/HLAvariantRecord.h — class form.
// Catalogue §14 row 14.7.
//
// M31 RED — fails until M32 lands `RTI/encoding/HLAvariantRecord.h`. Absent
// from gorti M17 entirely (catalogue marks MAJOR, fix=Add).

#include <RTI/encoding/HLAvariantRecord.h>
#include <RTI/encoding/DataElement.h>
#include <type_traits>

namespace {

static_assert(std::is_class_v<rti1516e::HLAvariantRecord>);
static_assert(std::is_base_of_v<rti1516e::DataElement, rti1516e::HLAvariantRecord>);

// Spec ctor: HLAvariantRecord(DataElement const& discriminantPrototype).
static_assert(std::is_constructible_v<rti1516e::HLAvariantRecord,
                                      rti1516e::DataElement const&>);
static_assert(std::is_copy_constructible_v<rti1516e::HLAvariantRecord>);

// addVariant(DataElement const& discriminant, DataElement const& valuePrototype)
static_assert(std::is_same_v<
    decltype(std::declval<rti1516e::HLAvariantRecord&>().addVariant(
        std::declval<rti1516e::DataElement const&>(),
        std::declval<rti1516e::DataElement const&>())),
    void>);

// setDiscriminant(DataElement const&)
static_assert(std::is_same_v<
    decltype(std::declval<rti1516e::HLAvariantRecord&>().setDiscriminant(
        std::declval<rti1516e::DataElement const&>())),
    void>);

// setVariant(DataElement const&, DataElement const&)
static_assert(std::is_same_v<
    decltype(std::declval<rti1516e::HLAvariantRecord&>().setVariant(
        std::declval<rti1516e::DataElement const&>(),
        std::declval<rti1516e::DataElement const&>())),
    void>);

// getDiscriminant() const — DataElement const&
static_assert(std::is_same_v<
    decltype(std::declval<rti1516e::HLAvariantRecord const&>().getDiscriminant()),
    rti1516e::DataElement const&>);

// getVariant() const — DataElement const&
static_assert(std::is_same_v<
    decltype(std::declval<rti1516e::HLAvariantRecord const&>().getVariant()),
    rti1516e::DataElement const&>);

}  // namespace
