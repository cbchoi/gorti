// Lockfile: RTIambassador support-services signatures per
// IEEE 1516.1-2010 §10.2-§10.44. Locks get*/decode*/normalize*
// methods and the 8 advisory-switch on/off pairs.
//
// Catalogue rows covered: 13.1-13.15.
// FR-DLC requirements: FR-DLC-4 (wstring), FR-DLC-14 (callback re-entry).

#include <RTI/RTIambassador.h>
#include <RTI/Handle.h>
#include <RTI/Typedefs.h>
#include <RTI/VariableLengthData.h>
#include <RTI/RangeBounds.h>
#include <RTI/Enums.h>
#include <RTI/LogicalTimeFactory.h>
#include <RTI/SpecificConfig.h>
#include <RTI/Exception.h>
#include <type_traits>
#include <string>
#include <memory>

namespace {

using rti1516e::RTIambassador;
using rti1516e::FederateHandle;
using rti1516e::ObjectClassHandle;
using rti1516e::ObjectInstanceHandle;
using rti1516e::InteractionClassHandle;
using rti1516e::AttributeHandle;
using rti1516e::ParameterHandle;
using rti1516e::DimensionHandle;
using rti1516e::DimensionHandleSet;
using rti1516e::RegionHandle;
using rti1516e::MessageRetractionHandle;
using rti1516e::OrderType;
using rti1516e::TransportationType;
using rti1516e::ResignAction;
using rti1516e::ServiceGroup;
using rti1516e::VariableLengthData;
using rti1516e::RangeBounds;
using rti1516e::LogicalTimeFactory;

// §10.2 getAutomaticResignDirective() → ResignAction (catalogue 13.1).
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().getAutomaticResignDirective()),
    ResignAction>);

// §10.3 setAutomaticResignDirective(ResignAction) (catalogue 13.2).
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().setAutomaticResignDirective(
        std::declval<ResignAction>())),
    void>);

// §10.4 getFederateHandle(wstring) → FederateHandle (catalogue 13.3).
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().getFederateHandle(
        std::declval<std::wstring const&>())),
    FederateHandle>);

// §10.5 getFederateName(FederateHandle) → wstring.
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().getFederateName(
        std::declval<FederateHandle>())),
    std::wstring>);

// §10.6 getObjectClassHandle(wstring) → ObjectClassHandle (catalogue 13.4).
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().getObjectClassHandle(
        std::declval<std::wstring const&>())),
    ObjectClassHandle>);

// §10.7 getObjectClassName(ObjectClassHandle) → wstring.
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().getObjectClassName(
        std::declval<ObjectClassHandle>())),
    std::wstring>);

// §10.8 getKnownObjectClassHandle(ObjectInstanceHandle) → ObjectClassHandle.
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().getKnownObjectClassHandle(
        std::declval<ObjectInstanceHandle>())),
    ObjectClassHandle>);

// §10.9 getObjectInstanceHandle(wstring) → ObjectInstanceHandle.
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().getObjectInstanceHandle(
        std::declval<std::wstring const&>())),
    ObjectInstanceHandle>);

// §10.10 getObjectInstanceName(ObjectInstanceHandle) → wstring.
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().getObjectInstanceName(
        std::declval<ObjectInstanceHandle>())),
    std::wstring>);

// §10.11 getAttributeHandle(ObjectClassHandle, wstring) → AttributeHandle.
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().getAttributeHandle(
        std::declval<ObjectClassHandle>(),
        std::declval<std::wstring const&>())),
    AttributeHandle>);

// §10.12 getAttributeName(ObjectClassHandle, AttributeHandle) → wstring.
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().getAttributeName(
        std::declval<ObjectClassHandle>(),
        std::declval<AttributeHandle>())),
    std::wstring>);

// §10.13 getUpdateRateValue(wstring) → double (catalogue 13.5).
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().getUpdateRateValue(
        std::declval<std::wstring const&>())),
    double>);

// §10.14 getUpdateRateValueForAttribute(ObjectInstanceHandle, AttributeHandle) → double.
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().getUpdateRateValueForAttribute(
        std::declval<ObjectInstanceHandle>(),
        std::declval<AttributeHandle>())),
    double>);

// §10.15 getInteractionClassHandle(wstring) → InteractionClassHandle (catalogue 13.6).
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().getInteractionClassHandle(
        std::declval<std::wstring const&>())),
    InteractionClassHandle>);

// §10.16 getInteractionClassName(InteractionClassHandle) → wstring.
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().getInteractionClassName(
        std::declval<InteractionClassHandle>())),
    std::wstring>);

// §10.17 getParameterHandle(InteractionClassHandle, wstring) → ParameterHandle.
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().getParameterHandle(
        std::declval<InteractionClassHandle>(),
        std::declval<std::wstring const&>())),
    ParameterHandle>);

// §10.18 getParameterName(InteractionClassHandle, ParameterHandle) → wstring.
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().getParameterName(
        std::declval<InteractionClassHandle>(),
        std::declval<ParameterHandle>())),
    std::wstring>);

// §10.19 getOrderType(wstring) → OrderType (catalogue 13.7).
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().getOrderType(
        std::declval<std::wstring const&>())),
    OrderType>);

// §10.20 getOrderName(OrderType) → wstring.
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().getOrderName(
        std::declval<OrderType>())),
    std::wstring>);

// §10.21 getTransportationType(wstring) → TransportationType.
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().getTransportationType(
        std::declval<std::wstring const&>())),
    TransportationType>);

// §10.22 getTransportationName(TransportationType) → wstring.
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().getTransportationName(
        std::declval<TransportationType>())),
    std::wstring>);

// §10.23-10.30 dimension lookup + region/dim → bounds (catalogue 13.8).
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().getDimensionHandle(
        std::declval<std::wstring const&>())),
    DimensionHandle>);
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().getDimensionName(
        std::declval<DimensionHandle>())),
    std::wstring>);
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().getDimensionUpperBound(
        std::declval<DimensionHandle>())),
    unsigned long>);
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().getDimensionHandleSet(
        std::declval<RegionHandle>())),
    DimensionHandleSet>);
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().getRangeBounds(
        std::declval<RegionHandle>(),
        std::declval<DimensionHandle>())),
    RangeBounds>);
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().setRangeBounds(
        std::declval<RegionHandle>(),
        std::declval<DimensionHandle>(),
        std::declval<RangeBounds const&>())),
    void>);

// §10.31 normalizeFederateHandle(FederateHandle) → unsigned long (catalogue 13.9).
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().normalizeFederateHandle(
        std::declval<FederateHandle>())),
    unsigned long>);

// §10.32 normalizeServiceGroup(ServiceGroup) → unsigned long.
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().normalizeServiceGroup(
        std::declval<ServiceGroup>())),
    unsigned long>);

// §10.33-10.40 — 8 advisory-switch on/off pairs (catalogue 13.10).
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().enableObjectClassRelevanceAdvisorySwitch()), void>);
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().disableObjectClassRelevanceAdvisorySwitch()), void>);
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().enableAttributeRelevanceAdvisorySwitch()), void>);
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().disableAttributeRelevanceAdvisorySwitch()), void>);
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().enableAttributeScopeAdvisorySwitch()), void>);
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().disableAttributeScopeAdvisorySwitch()), void>);
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().enableInteractionRelevanceAdvisorySwitch()), void>);
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().disableInteractionRelevanceAdvisorySwitch()), void>);

// §10.41 evokeCallback(double) → bool (catalogue 13.11): SINGLE arg.
//        gorti M17 had 2 args + defaults; spec is single arg.
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().evokeCallback(
        std::declval<double>())),
    bool>);

// §10.42 evokeMultipleCallbacks(double, double) → bool (catalogue 13.12):
//        2 args, NO defaults.
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().evokeMultipleCallbacks(
        std::declval<double>(),
        std::declval<double>())),
    bool>);

// §10.43-10.44 enable/disableCallbacks (catalogue 13.13).
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().enableCallbacks()), void>);
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().disableCallbacks()), void>);

// getTimeFactory() returns rti1516e::auto_ptr<LogicalTimeFactory> per
// catalogue 13.14 and the §3.1.0 C++17 resolution. We lock the spec-named
// `rti1516e::auto_ptr` alias rather than literal `std::auto_ptr` (removed
// in C++17). The const-ness of the method is also locked.
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador const&>().getTimeFactory()),
    rti1516e::auto_ptr<LogicalTimeFactory>>);

// §10 decode*Handle — 9 methods (catalogue 13.15). All const.
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador const&>().decodeFederateHandle(
        std::declval<VariableLengthData const&>())),
    FederateHandle>);
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador const&>().decodeObjectClassHandle(
        std::declval<VariableLengthData const&>())),
    ObjectClassHandle>);
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador const&>().decodeInteractionClassHandle(
        std::declval<VariableLengthData const&>())),
    InteractionClassHandle>);
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador const&>().decodeObjectInstanceHandle(
        std::declval<VariableLengthData const&>())),
    ObjectInstanceHandle>);
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador const&>().decodeAttributeHandle(
        std::declval<VariableLengthData const&>())),
    AttributeHandle>);
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador const&>().decodeParameterHandle(
        std::declval<VariableLengthData const&>())),
    ParameterHandle>);
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador const&>().decodeDimensionHandle(
        std::declval<VariableLengthData const&>())),
    DimensionHandle>);
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador const&>().decodeMessageRetractionHandle(
        std::declval<VariableLengthData const&>())),
    MessageRetractionHandle>);
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador const&>().decodeRegionHandle(
        std::declval<VariableLengthData const&>())),
    RegionHandle>);

// §10 exceptions.
static_assert(std::is_base_of_v<rti1516e::Exception, rti1516e::CouldNotDecode>);
static_assert(std::is_base_of_v<rti1516e::Exception, rti1516e::InvalidServiceGroup>);
static_assert(std::is_base_of_v<rti1516e::Exception, rti1516e::NameNotFound>);

}  // namespace
