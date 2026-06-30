// Lockfile: RTIambassador declaration-management signatures per
// IEEE 1516.1-2010 §5 (publish/subscribe for object classes + interactions
// with the spec-mandated `active` flag and `updateRate` designator).
//
// Catalogue rows covered: 11.9, 11.10, 11.11.
// FR-DLC requirements: FR-DLC-4 (wstring).

#include <RTI/RTIambassador.h>
#include <RTI/Handle.h>
#include <RTI/Typedefs.h>
#include <RTI/Exception.h>
#include <type_traits>
#include <string>

namespace {

using rti1516e::RTIambassador;
using rti1516e::ObjectClassHandle;
using rti1516e::InteractionClassHandle;
using rti1516e::AttributeHandleSet;

// §5.2 publishObjectClassAttributes(ObjectClassHandle, AttributeHandleSet const&)
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().publishObjectClassAttributes(
        std::declval<ObjectClassHandle>(),
        std::declval<AttributeHandleSet const&>())),
    void>);

// §5.3 unpublishObjectClass(ObjectClassHandle) — whole-class form (catalogue 11.10).
//       gorti M17 only had the attribute-subset form.
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().unpublishObjectClass(
        std::declval<ObjectClassHandle>())),
    void>);

// §5.3 unpublishObjectClassAttributes(ObjectClassHandle, AttributeHandleSet const&)
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().unpublishObjectClassAttributes(
        std::declval<ObjectClassHandle>(),
        std::declval<AttributeHandleSet const&>())),
    void>);

// §5.4 publishInteractionClass(InteractionClassHandle)
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().publishInteractionClass(
        std::declval<InteractionClassHandle>())),
    void>);

// §5.5 unpublishInteractionClass(InteractionClassHandle)
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().unpublishInteractionClass(
        std::declval<InteractionClassHandle>())),
    void>);

// §5.6 subscribeObjectClassAttributes — (cls, set, bool active=true, wstring updateRate=L"")
//       Catalogue 11.9: gorti M17 lacks both the active flag and updateRate.
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().subscribeObjectClassAttributes(
        std::declval<ObjectClassHandle>(),
        std::declval<AttributeHandleSet const&>(),
        std::declval<bool>(),
        std::declval<std::wstring const&>())),
    void>);

// §5.6 — also lock the 3-arg form (relying on default updateRate=L"") so federates
//        written without an updateRate compile.
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().subscribeObjectClassAttributes(
        std::declval<ObjectClassHandle>(),
        std::declval<AttributeHandleSet const&>(),
        std::declval<bool>())),
    void>);

// §5.7 unsubscribeObjectClass(ObjectClassHandle) — whole-class form.
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().unsubscribeObjectClass(
        std::declval<ObjectClassHandle>())),
    void>);

// §5.7 unsubscribeObjectClassAttributes — attribute-subset form.
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().unsubscribeObjectClassAttributes(
        std::declval<ObjectClassHandle>(),
        std::declval<AttributeHandleSet const&>())),
    void>);

// §5.8 subscribeInteractionClass — (cls, bool active=true) per catalogue 11.11.
//       gorti M17 lacked the active flag.
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().subscribeInteractionClass(
        std::declval<InteractionClassHandle>(),
        std::declval<bool>())),
    void>);

// §5.9 unsubscribeInteractionClass(InteractionClassHandle)
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().unsubscribeInteractionClass(
        std::declval<InteractionClassHandle>())),
    void>);

// §5 — spec-mandated exception types reachable.
static_assert(std::is_base_of_v<rti1516e::Exception, rti1516e::AttributeNotDefined>);
static_assert(std::is_base_of_v<rti1516e::Exception, rti1516e::ObjectClassNotDefined>);
static_assert(std::is_base_of_v<rti1516e::Exception, rti1516e::InteractionClassNotDefined>);
static_assert(std::is_base_of_v<rti1516e::Exception, rti1516e::ObjectClassNotPublished>);
static_assert(std::is_base_of_v<rti1516e::Exception, rti1516e::InvalidUpdateRateDesignator>);

}  // namespace
