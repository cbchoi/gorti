// Lockfile: RTIambassador ownership-management signatures per
// IEEE 1516.1-2010 §7. Locks divest / acquire / release / confirm /
// queryAttributeOwnership / isAttributeOwnedByFederate.
//
// Catalogue rows covered: 12.1, 12.2, 12.3, 12.4, 12.5, 12.6, 12.7.

#include <RTI/RTIambassador.h>
#include <RTI/Handle.h>
#include <RTI/Typedefs.h>
#include <RTI/VariableLengthData.h>
#include <RTI/Exception.h>
#include <type_traits>

namespace {

using rti1516e::RTIambassador;
using rti1516e::ObjectInstanceHandle;
using rti1516e::AttributeHandle;
using rti1516e::AttributeHandleSet;
using rti1516e::FederateHandle;
using rti1516e::VariableLengthData;

// §7.2 unconditionalAttributeOwnershipDivestiture(object, set)
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().unconditionalAttributeOwnershipDivestiture(
        std::declval<ObjectInstanceHandle>(),
        std::declval<AttributeHandleSet const&>())),
    void>);

// §7.3 negotiatedAttributeOwnershipDivestiture(object, set, tag)
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().negotiatedAttributeOwnershipDivestiture(
        std::declval<ObjectInstanceHandle>(),
        std::declval<AttributeHandleSet const&>(),
        std::declval<VariableLengthData const&>())),
    void>);

// §7.6 confirmDivestiture(object, set, tag) — catalogue 12.1.
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().confirmDivestiture(
        std::declval<ObjectInstanceHandle>(),
        std::declval<AttributeHandleSet const&>(),
        std::declval<VariableLengthData const&>())),
    void>);

// §7.8 attributeOwnershipAcquisition(object, set, tag) — catalogue 12.2: TAG MANDATORY.
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().attributeOwnershipAcquisition(
        std::declval<ObjectInstanceHandle>(),
        std::declval<AttributeHandleSet const&>(),
        std::declval<VariableLengthData const&>())),
    void>);

// §7.9 attributeOwnershipAcquisitionIfAvailable(object, set) — catalogue 12.3.
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().attributeOwnershipAcquisitionIfAvailable(
        std::declval<ObjectInstanceHandle>(),
        std::declval<AttributeHandleSet const&>())),
    void>);

// §7.12 attributeOwnershipReleaseDenied(object, set) — catalogue 12.4.
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().attributeOwnershipReleaseDenied(
        std::declval<ObjectInstanceHandle>(),
        std::declval<AttributeHandleSet const&>())),
    void>);

// §7.13 attributeOwnershipDivestitureIfWanted(object, set, AttributeHandleSet& out)
//        Out-parameter form per catalogue 12.5.
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().attributeOwnershipDivestitureIfWanted(
        std::declval<ObjectInstanceHandle>(),
        std::declval<AttributeHandleSet const&>(),
        std::declval<AttributeHandleSet&>())),
    void>);

// §7.14 cancelNegotiatedAttributeOwnershipDivestiture(object, set)
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().cancelNegotiatedAttributeOwnershipDivestiture(
        std::declval<ObjectInstanceHandle>(),
        std::declval<AttributeHandleSet const&>())),
    void>);

// §7.15 cancelAttributeOwnershipAcquisition(object, set)
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().cancelAttributeOwnershipAcquisition(
        std::declval<ObjectInstanceHandle>(),
        std::declval<AttributeHandleSet const&>())),
    void>);

// §7.17 queryAttributeOwnership(object, attr) returns VOID — result is async,
//        delivered via informAttributeOwnership / attributeIsNotOwned /
//        attributeIsOwnedByRTI callbacks. Catalogue 12.6: gorti M17 returned
//        a sync struct; the spec is callback-driven.
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().queryAttributeOwnership(
        std::declval<ObjectInstanceHandle>(),
        std::declval<AttributeHandle>())),
    void>);

// §7.19 isAttributeOwnedByFederate(object, attr) returns bool — catalogue 12.7.
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().isAttributeOwnedByFederate(
        std::declval<ObjectInstanceHandle>(),
        std::declval<AttributeHandle>())),
    bool>);

// §7 — spec-mandated exception types reachable.
static_assert(std::is_base_of_v<rti1516e::Exception, rti1516e::AttributeNotOwned>);
static_assert(std::is_base_of_v<rti1516e::Exception, rti1516e::AttributeAlreadyBeingDivested>);
static_assert(std::is_base_of_v<rti1516e::Exception, rti1516e::AttributeAlreadyBeingAcquired>);
static_assert(std::is_base_of_v<rti1516e::Exception, rti1516e::AttributeAcquisitionWasNotRequested>);
static_assert(std::is_base_of_v<rti1516e::Exception, rti1516e::AttributeDivestitureWasNotRequested>);
static_assert(std::is_base_of_v<rti1516e::Exception, rti1516e::FederateOwnsAttributes>);

}  // namespace
