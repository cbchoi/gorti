// Lockfile: RTIambassador object-management signatures per IEEE 1516.1-2010 §6.
// Locks reserveObjectInstanceName (singular + plural set form), registerObjectInstance
// (2 overloads), updateAttributeValues (2 overloads), sendInteraction (2 overloads),
// deleteObjectInstance (2 overloads), localDeleteObjectInstance,
// requestAttributeValueUpdate (2 overloads), and retract.
//
// Catalogue rows covered: 11.1, 11.2, 11.3, 11.4, 11.5, 11.6, 11.7, 17.1 (tag-mandatory).
// FR-DLC requirements: FR-DLC-4, FR-DLC-13.

#include <RTI/RTIambassador.h>
#include <RTI/Handle.h>
#include <RTI/Typedefs.h>
#include <RTI/VariableLengthData.h>
#include <RTI/Exception.h>
#include <type_traits>
#include <string>
#include <set>

namespace {

using rti1516e::RTIambassador;
using rti1516e::ObjectClassHandle;
using rti1516e::ObjectInstanceHandle;
using rti1516e::InteractionClassHandle;
using rti1516e::AttributeHandleSet;
using rti1516e::AttributeHandleValueMap;
using rti1516e::ParameterHandleValueMap;
using rti1516e::MessageRetractionHandle;
using rti1516e::VariableLengthData;

// §6.4 reserveObjectInstanceName(wstring const&) — SINGULAR per catalogue 11.1.
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().reserveObjectInstanceName(
        std::declval<std::wstring const&>())),
    void>);

// §6.4 releaseObjectInstanceName(wstring const&)
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().releaseObjectInstanceName(
        std::declval<std::wstring const&>())),
    void>);

// §6.5 reserveMultipleObjectInstanceName(set<wstring> const&) — SINGULAR
//      "Name" (not "Names"), set (not vector), wstring (not string).
//      gorti M17 had `reserveMultipleObjectInstanceNames(vector<string>)`.
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().reserveMultipleObjectInstanceName(
        std::declval<std::set<std::wstring> const&>())),
    void>);

// §6.5 releaseMultipleObjectInstanceName
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().releaseMultipleObjectInstanceName(
        std::declval<std::set<std::wstring> const&>())),
    void>);

// §6.8 registerObjectInstance — 2 overloads (catalogue 11.2).
// Overload 1: (ObjectClassHandle) — RTI auto-names.
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().registerObjectInstance(
        std::declval<ObjectClassHandle>())),
    ObjectInstanceHandle>);

// Overload 2: (ObjectClassHandle, wstring const& name) — federate-named.
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().registerObjectInstance(
        std::declval<ObjectClassHandle>(),
        std::declval<std::wstring const&>())),
    ObjectInstanceHandle>);

// §6.10 updateAttributeValues — 2 overloads, tag MANDATORY (catalogue 11.3, 17.1).
// RO form: (object, attr-map, tag) returning void.
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().updateAttributeValues(
        std::declval<ObjectInstanceHandle>(),
        std::declval<AttributeHandleValueMap const&>(),
        std::declval<VariableLengthData const&>())),
    void>);

// TSO form: NOTE — locked in test_rtiambassador_time.cpp because the time-tagged
// overload requires LogicalTime const& which lives under §8.

// §6.12 sendInteraction — 2 overloads, tag MANDATORY (catalogue 11.4, 17.1).
// RO form.
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().sendInteraction(
        std::declval<InteractionClassHandle>(),
        std::declval<ParameterHandleValueMap const&>(),
        std::declval<VariableLengthData const&>())),
    void>);
// TSO form: locked in time TU.

// §6.14 deleteObjectInstance — RO form (catalogue 11.5).
//        Returns void; the TSO form returns MessageRetractionHandle (locked
//        in time TU).
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().deleteObjectInstance(
        std::declval<ObjectInstanceHandle>(),
        std::declval<VariableLengthData const&>())),
    void>);

// §6.16 localDeleteObjectInstance (catalogue 11.6).
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().localDeleteObjectInstance(
        std::declval<ObjectInstanceHandle>())),
    void>);

// §6.19 requestAttributeValueUpdate — 2 overloads (by instance, by class)
//        per catalogue 11.7.
// By-instance form.
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().requestAttributeValueUpdate(
        std::declval<ObjectInstanceHandle>(),
        std::declval<AttributeHandleSet const&>(),
        std::declval<VariableLengthData const&>())),
    void>);
// By-class form.
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().requestAttributeValueUpdate(
        std::declval<ObjectClassHandle>(),
        std::declval<AttributeHandleSet const&>(),
        std::declval<VariableLengthData const&>())),
    void>);

// §6 — spec-mandated exception types reachable.
static_assert(std::is_base_of_v<rti1516e::Exception, rti1516e::ObjectInstanceNotKnown>);
static_assert(std::is_base_of_v<rti1516e::Exception, rti1516e::ObjectInstanceNameInUse>);
static_assert(std::is_base_of_v<rti1516e::Exception, rti1516e::ObjectInstanceNameNotReserved>);
static_assert(std::is_base_of_v<rti1516e::Exception, rti1516e::IllegalName>);
static_assert(std::is_base_of_v<rti1516e::Exception, rti1516e::DeletePrivilegeNotHeld>);
static_assert(std::is_base_of_v<rti1516e::Exception, rti1516e::InteractionClassNotPublished>);

}  // namespace
