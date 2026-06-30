// Lockfile: RTIambassador time-management signatures per IEEE 1516.1-2010 §8.
// Locks regulation, constraint, advance primitives, queries (queryGALT/LITS/
// LogicalTime/Lookahead with out-param form), modifyLookahead, retract,
// and the TSO overloads of updateAttributeValues / sendInteraction /
// deleteObjectInstance that take LogicalTime.
//
// Catalogue rows covered: 9.1, 9.2, 9.6, 9.7, 9.8, 9.9, 9.10, 9.11, 9.12, 9.13, 9.14.
// FR-DLC requirements: FR-DLC-8, FR-DLC-12.

#include <RTI/RTIambassador.h>
#include <RTI/LogicalTime.h>
#include <RTI/LogicalTimeInterval.h>
#include <RTI/LogicalTimeFactory.h>
#include <RTI/Handle.h>
#include <RTI/Typedefs.h>
#include <RTI/VariableLengthData.h>
#include <RTI/Enums.h>
#include <RTI/SpecificConfig.h>
#include <RTI/Exception.h>
#include <type_traits>
#include <memory>

namespace {

using rti1516e::RTIambassador;
using rti1516e::LogicalTime;
using rti1516e::LogicalTimeInterval;
using rti1516e::LogicalTimeFactory;
using rti1516e::ObjectInstanceHandle;
using rti1516e::InteractionClassHandle;
using rti1516e::AttributeHandleValueMap;
using rti1516e::ParameterHandleValueMap;
using rti1516e::AttributeHandle;
using rti1516e::AttributeHandleSet;
using rti1516e::MessageRetractionHandle;
using rti1516e::VariableLengthData;
using rti1516e::OrderType;
using rti1516e::TransportationType;

// §8.2 enableTimeRegulation(LogicalTimeInterval const&) — async; ack via callback.
//      Catalogue 9.11: gorti M17 took double + sync return.
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().enableTimeRegulation(
        std::declval<LogicalTimeInterval const&>())),
    void>);

// §8.4 disableTimeRegulation()
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().disableTimeRegulation()),
    void>);

// §8.5 enableTimeConstrained()
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().enableTimeConstrained()),
    void>);

// §8.7 disableTimeConstrained()
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().disableTimeConstrained()),
    void>);

// §8.8 timeAdvanceRequest(LogicalTime const&) — catalogue 9.6.
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().timeAdvanceRequest(
        std::declval<LogicalTime const&>())),
    void>);

// §8.9 timeAdvanceRequestAvailable(LogicalTime const&)
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().timeAdvanceRequestAvailable(
        std::declval<LogicalTime const&>())),
    void>);

// §8.10 nextMessageRequest(LogicalTime const&)
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().nextMessageRequest(
        std::declval<LogicalTime const&>())),
    void>);

// §8.11 nextMessageRequestAvailable(LogicalTime const&)
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().nextMessageRequestAvailable(
        std::declval<LogicalTime const&>())),
    void>);

// §8.12 flushQueueRequest(LogicalTime const&)
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().flushQueueRequest(
        std::declval<LogicalTime const&>())),
    void>);

// §8.14 enableAsynchronousDelivery()
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().enableAsynchronousDelivery()),
    void>);

// §8.15 disableAsynchronousDelivery()
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().disableAsynchronousDelivery()),
    void>);

// §8.16 queryGALT — `bool queryGALT(LogicalTime& outTime)` — catalogue 9.7.
//        gorti M17 returned a GALTResult struct; spec is bool + out-param.
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().queryGALT(
        std::declval<LogicalTime&>())),
    bool>);

// §8.17 queryLogicalTime(LogicalTime&) — void return; out-param per catalogue 9.8.
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().queryLogicalTime(
        std::declval<LogicalTime&>())),
    void>);

// §8.18 queryLITS(LogicalTime&) bool — catalogue 9.9.
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().queryLITS(
        std::declval<LogicalTime&>())),
    bool>);

// §8.19 modifyLookahead(LogicalTimeInterval const&) — catalogue 9.12.
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().modifyLookahead(
        std::declval<LogicalTimeInterval const&>())),
    void>);

// §8.20 queryLookahead(LogicalTimeInterval&) — catalogue 9.10.
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().queryLookahead(
        std::declval<LogicalTimeInterval&>())),
    void>);

// §8.21 retract(MessageRetractionHandle) — catalogue 9.13.
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().retract(
        std::declval<MessageRetractionHandle>())),
    void>);

// §8.23 changeAttributeOrderType(object, set, OrderType) — catalogue 9.14.
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().changeAttributeOrderType(
        std::declval<ObjectInstanceHandle>(),
        std::declval<AttributeHandleSet const&>(),
        std::declval<OrderType>())),
    void>);

// §8.24 changeInteractionOrderType(class, OrderType)
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().changeInteractionOrderType(
        std::declval<InteractionClassHandle>(),
        std::declval<OrderType>())),
    void>);

// §6.10 TSO updateAttributeValues — returns MessageRetractionHandle.
//        This is the time-tagged overload that completes §6/§8's surface.
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().updateAttributeValues(
        std::declval<ObjectInstanceHandle>(),
        std::declval<AttributeHandleValueMap const&>(),
        std::declval<VariableLengthData const&>(),
        std::declval<LogicalTime const&>())),
    MessageRetractionHandle>);

// §6.12 TSO sendInteraction — returns MessageRetractionHandle.
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().sendInteraction(
        std::declval<InteractionClassHandle>(),
        std::declval<ParameterHandleValueMap const&>(),
        std::declval<VariableLengthData const&>(),
        std::declval<LogicalTime const&>())),
    MessageRetractionHandle>);

// §6.14 TSO deleteObjectInstance — returns MessageRetractionHandle.
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().deleteObjectInstance(
        std::declval<ObjectInstanceHandle>(),
        std::declval<VariableLengthData const&>(),
        std::declval<LogicalTime const&>())),
    MessageRetractionHandle>);

// LogicalTime / LogicalTimeInterval / LogicalTimeFactory are abstract per §8
// (catalogue 9.1, 9.2, 9.3).
static_assert(std::is_class_v<LogicalTime>);
static_assert(std::is_abstract_v<LogicalTime>);
static_assert(std::is_polymorphic_v<LogicalTime>);
static_assert(std::has_virtual_destructor_v<LogicalTime>);

static_assert(std::is_class_v<LogicalTimeInterval>);
static_assert(std::is_abstract_v<LogicalTimeInterval>);
static_assert(std::is_polymorphic_v<LogicalTimeInterval>);
static_assert(std::has_virtual_destructor_v<LogicalTimeInterval>);

static_assert(std::is_class_v<LogicalTimeFactory>);
static_assert(std::is_abstract_v<LogicalTimeFactory>);
static_assert(std::has_virtual_destructor_v<LogicalTimeFactory>);

// §8 spec-mandated exceptions reachable.
static_assert(std::is_base_of_v<rti1516e::Exception, rti1516e::TimeRegulationIsNotEnabled>);
static_assert(std::is_base_of_v<rti1516e::Exception, rti1516e::TimeConstrainedIsNotEnabled>);
static_assert(std::is_base_of_v<rti1516e::Exception, rti1516e::InTimeAdvancingState>);
static_assert(std::is_base_of_v<rti1516e::Exception, rti1516e::InvalidLogicalTime>);
static_assert(std::is_base_of_v<rti1516e::Exception, rti1516e::InvalidLookahead>);
static_assert(std::is_base_of_v<rti1516e::Exception, rti1516e::MessageCanNoLongerBeRetracted>);

}  // namespace
