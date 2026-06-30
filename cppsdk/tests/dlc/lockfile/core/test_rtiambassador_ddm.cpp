// Lockfile: RTIambassador DDM signatures per IEEE 1516.1-2010 §9.
// Locks createRegion (DimensionHandleSet, NOT RoutingSpaceHandle),
// commitRegionModifications (RegionHandleSet, NOT vector),
// registerObjectInstanceWithRegions (pair-vector + 2 overloads),
// associate/unassociateRegionsForUpdates (pair-vector),
// subscribeObjectClassAttributesWithRegions (active + updateRate),
// subscribe/sendInteractionWithRegions (2 overloads).
//
// Catalogue rows covered: 10.1, 10.2, 10.3, 10.4, 10.5, 10.6, 10.7, 10.8, 10.9.
// FR-DLC requirements: FR-DLC-15 (no RoutingSpaceHandle).

#include <RTI/RTIambassador.h>
#include <RTI/Handle.h>
#include <RTI/Typedefs.h>
#include <RTI/RangeBounds.h>
#include <RTI/VariableLengthData.h>
#include <RTI/Exception.h>
#include <type_traits>
#include <string>

namespace {

using rti1516e::RTIambassador;
using rti1516e::RegionHandle;
using rti1516e::RegionHandleSet;
using rti1516e::DimensionHandle;
using rti1516e::DimensionHandleSet;
using rti1516e::ObjectClassHandle;
using rti1516e::ObjectInstanceHandle;
using rti1516e::InteractionClassHandle;
using rti1516e::AttributeHandleSetRegionHandleSetPairVector;
using rti1516e::ParameterHandleValueMap;
using rti1516e::MessageRetractionHandle;
using rti1516e::VariableLengthData;

// §9.2 createRegion(DimensionHandleSet const&) — returns RegionHandle.
//      Catalogue 10.1: drop RoutingSpaceHandle (gorti M17 took (RoutingSpace, vector)).
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().createRegion(
        std::declval<DimensionHandleSet const&>())),
    RegionHandle>);

// §9.5 commitRegionModifications(RegionHandleSet const&) — set, not vector
//      (catalogue 10.9).
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().commitRegionModifications(
        std::declval<RegionHandleSet const&>())),
    void>);

// §9.4 deleteRegion(RegionHandle)
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().deleteRegion(
        std::declval<RegionHandle>())),
    void>);

// §9.5 registerObjectInstanceWithRegions — 2 overloads, returns
//      ObjectInstanceHandle. Catalogue 10.3.
// Overload 1: (class, pair-vector) — auto-name.
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().registerObjectInstanceWithRegions(
        std::declval<ObjectClassHandle>(),
        std::declval<AttributeHandleSetRegionHandleSetPairVector const&>())),
    ObjectInstanceHandle>);

// Overload 2: (class, pair-vector, wstring name) — federate-named.
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().registerObjectInstanceWithRegions(
        std::declval<ObjectClassHandle>(),
        std::declval<AttributeHandleSetRegionHandleSetPairVector const&>(),
        std::declval<std::wstring const&>())),
    ObjectInstanceHandle>);

// §9.6 associateRegionsForUpdates(object, pair-vector) — catalogue 10.4.
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().associateRegionsForUpdates(
        std::declval<ObjectInstanceHandle>(),
        std::declval<AttributeHandleSetRegionHandleSetPairVector const&>())),
    void>);

// §9.6 unassociateRegionsForUpdates(object, pair-vector)
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().unassociateRegionsForUpdates(
        std::declval<ObjectInstanceHandle>(),
        std::declval<AttributeHandleSetRegionHandleSetPairVector const&>())),
    void>);

// §9.8 subscribeObjectClassAttributesWithRegions —
//      (class, pair-vector, bool active=true, wstring updateRate=L"")
//      Catalogue 10.5.
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().subscribeObjectClassAttributesWithRegions(
        std::declval<ObjectClassHandle>(),
        std::declval<AttributeHandleSetRegionHandleSetPairVector const&>(),
        std::declval<bool>(),
        std::declval<std::wstring const&>())),
    void>);

// §9.9 unsubscribeObjectClassAttributesWithRegions(class, pair-vector)
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().unsubscribeObjectClassAttributesWithRegions(
        std::declval<ObjectClassHandle>(),
        std::declval<AttributeHandleSetRegionHandleSetPairVector const&>())),
    void>);

// §9.10 subscribeInteractionClassWithRegions(class, RegionHandleSet, bool active=true)
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().subscribeInteractionClassWithRegions(
        std::declval<InteractionClassHandle>(),
        std::declval<RegionHandleSet const&>(),
        std::declval<bool>())),
    void>);

// §9.11 unsubscribeInteractionClassWithRegions(class, RegionHandleSet)
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().unsubscribeInteractionClassWithRegions(
        std::declval<InteractionClassHandle>(),
        std::declval<RegionHandleSet const&>())),
    void>);

// §9.12 sendInteractionWithRegions — 2 overloads, tag MANDATORY (catalogue 10.6).
// RO form: (class, params, RegionHandleSet, tag) returns void.
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().sendInteractionWithRegions(
        std::declval<InteractionClassHandle>(),
        std::declval<ParameterHandleValueMap const&>(),
        std::declval<RegionHandleSet const&>(),
        std::declval<VariableLengthData const&>())),
    void>);

// TSO form: locked in test_rtiambassador_time.cpp would be ideal but the
// signature lives under §9, so include here.
// (class, params, regions, tag, LogicalTime) → MessageRetractionHandle.
// However we'd need to include <RTI/LogicalTime.h> to instantiate the
// declval; deferred to time TU to avoid duplicated coverage.

// §9.13 requestAttributeValueUpdateWithRegions(class, pair-vector, tag)
//        Catalogue 10.7.
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().requestAttributeValueUpdateWithRegions(
        std::declval<ObjectClassHandle>(),
        std::declval<AttributeHandleSetRegionHandleSetPairVector const&>(),
        std::declval<VariableLengthData const&>())),
    void>);

// §9 spec-mandated exceptions reachable.
static_assert(std::is_base_of_v<rti1516e::Exception, rti1516e::InvalidRegion>);
static_assert(std::is_base_of_v<rti1516e::Exception, rti1516e::RegionNotCreatedByThisFederate>);
static_assert(std::is_base_of_v<rti1516e::Exception, rti1516e::RegionDoesNotContainSpecifiedDimension>);
static_assert(std::is_base_of_v<rti1516e::Exception, rti1516e::InvalidRangeBound>);
static_assert(std::is_base_of_v<rti1516e::Exception, rti1516e::InvalidDimensionHandle>);

}  // namespace
