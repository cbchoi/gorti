// Lockfile: FederateAmbassador callback overload set per IEEE 1516.1-2010
// §6, §7, §8. The central parity-test blocker (catalogue §4 of
// DLC_DIVERGENCE_CATALOGUE.md): 14 callback overloads gorti M17 did not ship.
//
// Locked here:
//   - 2 discoverObjectInstance overloads (§6.9)
//   - 3 reflectAttributeValues overloads (§6.11)            ★ central
//   - 3 receiveInteraction overloads (§6.13)
//   - 3 removeObjectInstance overloads (§6.15)
//   - synchronizationPointAchieved-side callbacks (§4.12-4.15)
//   - timeRegulationEnabled / timeConstrainedEnabled / timeAdvanceGrant (§8.3, 8.6, 8.13)
//
// Catalogue rows covered: 4.1, 4.3-4.36 (selected). FR-DLC-5.

#include <RTI/FederateAmbassador.h>
#include <RTI/LogicalTime.h>
#include <RTI/Handle.h>
#include <RTI/Typedefs.h>
#include <RTI/VariableLengthData.h>
#include <RTI/Enums.h>
#include <RTI/Exception.h>
#include <type_traits>
#include <string>
#include <set>

namespace {

using rti1516e::FederateAmbassador;
using rti1516e::LogicalTime;
using rti1516e::ObjectInstanceHandle;
using rti1516e::ObjectClassHandle;
using rti1516e::InteractionClassHandle;
using rti1516e::AttributeHandle;
using rti1516e::AttributeHandleSet;
using rti1516e::AttributeHandleValueMap;
using rti1516e::ParameterHandleValueMap;
using rti1516e::FederateHandle;
using rti1516e::FederateHandleSet;
using rti1516e::MessageRetractionHandle;
using rti1516e::VariableLengthData;
using rti1516e::OrderType;
using rti1516e::TransportationType;
using rti1516e::SupplementalReflectInfo;
using rti1516e::SupplementalReceiveInfo;
using rti1516e::SupplementalRemoveInfo;

// §4.1 / §10 — FederateAmbassador is pure-abstract (catalogue 4.1).
static_assert(std::is_class_v<FederateAmbassador>);
static_assert(std::is_abstract_v<FederateAmbassador>);
static_assert(std::has_virtual_destructor_v<FederateAmbassador>);

// §4.4 connectionLost(wstring) — catalogue 4.3.
static_assert(std::is_same_v<
    decltype(std::declval<FederateAmbassador&>().connectionLost(
        std::declval<std::wstring const&>())),
    void>);

// §4.13 announceSynchronizationPoint(wstring, VariableLengthData) — catalogue 4.6.
static_assert(std::is_same_v<
    decltype(std::declval<FederateAmbassador&>().announceSynchronizationPoint(
        std::declval<std::wstring const&>(),
        std::declval<VariableLengthData const&>())),
    void>);

// §4.15 federationSynchronized(wstring, FederateHandleSet failedToSyncSet)
//        Catalogue 4.7.
static_assert(std::is_same_v<
    decltype(std::declval<FederateAmbassador&>().federationSynchronized(
        std::declval<std::wstring const&>(),
        std::declval<FederateHandleSet const&>())),
    void>);

// §6.3 objectInstanceNameReservationSucceeded(wstring) — catalogue 4.17.
static_assert(std::is_same_v<
    decltype(std::declval<FederateAmbassador&>().objectInstanceNameReservationSucceeded(
        std::declval<std::wstring const&>())),
    void>);

// §6.3 objectInstanceNameReservationFailed(wstring).
static_assert(std::is_same_v<
    decltype(std::declval<FederateAmbassador&>().objectInstanceNameReservationFailed(
        std::declval<std::wstring const&>())),
    void>);

// §6.6 multipleObjectInstanceNameReservationSucceeded(set<wstring>) — catalogue 4.18.
static_assert(std::is_same_v<
    decltype(std::declval<FederateAmbassador&>().multipleObjectInstanceNameReservationSucceeded(
        std::declval<std::set<std::wstring> const&>())),
    void>);

// §6.6 multipleObjectInstanceNameReservationFailed(set<wstring>) — single-arg form.
static_assert(std::is_same_v<
    decltype(std::declval<FederateAmbassador&>().multipleObjectInstanceNameReservationFailed(
        std::declval<std::set<std::wstring> const&>())),
    void>);

// §6.9 discoverObjectInstance — 2 overloads (catalogue 4.19).
// Overload 1: (object, class, wstring name)
static_assert(std::is_same_v<
    decltype(std::declval<FederateAmbassador&>().discoverObjectInstance(
        std::declval<ObjectInstanceHandle>(),
        std::declval<ObjectClassHandle>(),
        std::declval<std::wstring const&>())),
    void>);

// Overload 2: (object, class, wstring name, FederateHandle producingFederate)
static_assert(std::is_same_v<
    decltype(std::declval<FederateAmbassador&>().discoverObjectInstance(
        std::declval<ObjectInstanceHandle>(),
        std::declval<ObjectClassHandle>(),
        std::declval<std::wstring const&>(),
        std::declval<FederateHandle>())),
    void>);

// §6.11 reflectAttributeValues — 3 overloads. CENTRAL PARITY-TEST BLOCKER
//        (catalogue 4.20).
// Overload A (RO, no time, no retraction):
//   (object, attr-map, tag, sentOrder, theType, SupplementalReflectInfo)
static_assert(std::is_same_v<
    decltype(std::declval<FederateAmbassador&>().reflectAttributeValues(
        std::declval<ObjectInstanceHandle>(),
        std::declval<AttributeHandleValueMap const&>(),
        std::declval<VariableLengthData const&>(),
        std::declval<OrderType>(),
        std::declval<TransportationType>(),
        std::declval<SupplementalReflectInfo>())),
    void>);

// Overload B (TSO, no retraction):
//   (object, attr-map, tag, sentOrder, theType, LogicalTime, receivedOrder, SupplementalReflectInfo)
static_assert(std::is_same_v<
    decltype(std::declval<FederateAmbassador&>().reflectAttributeValues(
        std::declval<ObjectInstanceHandle>(),
        std::declval<AttributeHandleValueMap const&>(),
        std::declval<VariableLengthData const&>(),
        std::declval<OrderType>(),
        std::declval<TransportationType>(),
        std::declval<LogicalTime const&>(),
        std::declval<OrderType>(),
        std::declval<SupplementalReflectInfo>())),
    void>);

// Overload C (TSO + retraction):
//   (object, attr-map, tag, sentOrder, theType, LogicalTime, receivedOrder,
//    MessageRetractionHandle, SupplementalReflectInfo)
static_assert(std::is_same_v<
    decltype(std::declval<FederateAmbassador&>().reflectAttributeValues(
        std::declval<ObjectInstanceHandle>(),
        std::declval<AttributeHandleValueMap const&>(),
        std::declval<VariableLengthData const&>(),
        std::declval<OrderType>(),
        std::declval<TransportationType>(),
        std::declval<LogicalTime const&>(),
        std::declval<OrderType>(),
        std::declval<MessageRetractionHandle>(),
        std::declval<SupplementalReflectInfo>())),
    void>);

// §6.13 receiveInteraction — 3 overloads (catalogue 4.21).
// Overload A (RO):
static_assert(std::is_same_v<
    decltype(std::declval<FederateAmbassador&>().receiveInteraction(
        std::declval<InteractionClassHandle>(),
        std::declval<ParameterHandleValueMap const&>(),
        std::declval<VariableLengthData const&>(),
        std::declval<OrderType>(),
        std::declval<TransportationType>(),
        std::declval<SupplementalReceiveInfo>())),
    void>);

// Overload B (TSO, no retraction):
static_assert(std::is_same_v<
    decltype(std::declval<FederateAmbassador&>().receiveInteraction(
        std::declval<InteractionClassHandle>(),
        std::declval<ParameterHandleValueMap const&>(),
        std::declval<VariableLengthData const&>(),
        std::declval<OrderType>(),
        std::declval<TransportationType>(),
        std::declval<LogicalTime const&>(),
        std::declval<OrderType>(),
        std::declval<SupplementalReceiveInfo>())),
    void>);

// Overload C (TSO + retraction):
static_assert(std::is_same_v<
    decltype(std::declval<FederateAmbassador&>().receiveInteraction(
        std::declval<InteractionClassHandle>(),
        std::declval<ParameterHandleValueMap const&>(),
        std::declval<VariableLengthData const&>(),
        std::declval<OrderType>(),
        std::declval<TransportationType>(),
        std::declval<LogicalTime const&>(),
        std::declval<OrderType>(),
        std::declval<MessageRetractionHandle>(),
        std::declval<SupplementalReceiveInfo>())),
    void>);

// §6.15 removeObjectInstance — 3 overloads (catalogue 4.22).
// Overload A (RO):
static_assert(std::is_same_v<
    decltype(std::declval<FederateAmbassador&>().removeObjectInstance(
        std::declval<ObjectInstanceHandle>(),
        std::declval<VariableLengthData const&>(),
        std::declval<OrderType>(),
        std::declval<SupplementalRemoveInfo>())),
    void>);

// Overload B (TSO, no retraction):
static_assert(std::is_same_v<
    decltype(std::declval<FederateAmbassador&>().removeObjectInstance(
        std::declval<ObjectInstanceHandle>(),
        std::declval<VariableLengthData const&>(),
        std::declval<OrderType>(),
        std::declval<LogicalTime const&>(),
        std::declval<OrderType>(),
        std::declval<SupplementalRemoveInfo>())),
    void>);

// Overload C (TSO + retraction):
static_assert(std::is_same_v<
    decltype(std::declval<FederateAmbassador&>().removeObjectInstance(
        std::declval<ObjectInstanceHandle>(),
        std::declval<VariableLengthData const&>(),
        std::declval<OrderType>(),
        std::declval<LogicalTime const&>(),
        std::declval<OrderType>(),
        std::declval<MessageRetractionHandle>(),
        std::declval<SupplementalRemoveInfo>())),
    void>);

// §6.17-18 attributesInScope / attributesOutOfScope — catalogue 4.23.
static_assert(std::is_same_v<
    decltype(std::declval<FederateAmbassador&>().attributesInScope(
        std::declval<ObjectInstanceHandle>(),
        std::declval<AttributeHandleSet const&>())),
    void>);

static_assert(std::is_same_v<
    decltype(std::declval<FederateAmbassador&>().attributesOutOfScope(
        std::declval<ObjectInstanceHandle>(),
        std::declval<AttributeHandleSet const&>())),
    void>);

// §6.20 provideAttributeValueUpdate — catalogue 4.24.
static_assert(std::is_same_v<
    decltype(std::declval<FederateAmbassador&>().provideAttributeValueUpdate(
        std::declval<ObjectInstanceHandle>(),
        std::declval<AttributeHandleSet const&>(),
        std::declval<VariableLengthData const&>())),
    void>);

// §8.3 timeRegulationEnabled(LogicalTime) — catalogue 4.33. Async ack.
static_assert(std::is_same_v<
    decltype(std::declval<FederateAmbassador&>().timeRegulationEnabled(
        std::declval<LogicalTime const&>())),
    void>);

// §8.6 timeConstrainedEnabled(LogicalTime) — catalogue 4.34.
static_assert(std::is_same_v<
    decltype(std::declval<FederateAmbassador&>().timeConstrainedEnabled(
        std::declval<LogicalTime const&>())),
    void>);

// §8.13 timeAdvanceGrant(LogicalTime) — catalogue 4.35. The most-frequently
//        called callback in any time-managed federate.
static_assert(std::is_same_v<
    decltype(std::declval<FederateAmbassador&>().timeAdvanceGrant(
        std::declval<LogicalTime const&>())),
    void>);

// §8.22 requestRetraction(MessageRetractionHandle) — catalogue 4.36.
static_assert(std::is_same_v<
    decltype(std::declval<FederateAmbassador&>().requestRetraction(
        std::declval<MessageRetractionHandle>())),
    void>);

// §10 — every callback is declared with RTI_THROW(FederateInternalError) per
//        catalogue 4.37 / FR-DLC-9. The macro itself is locked in
//        test_exception_*. Here we ensure FederateInternalError is reachable.
static_assert(std::is_class_v<rti1516e::FederateInternalError>);
static_assert(std::is_base_of_v<rti1516e::Exception, rti1516e::FederateInternalError>);

}  // namespace
