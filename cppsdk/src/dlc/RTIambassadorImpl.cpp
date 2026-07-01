// IEEE 1516.1-2010 §10.6 / Annex A — DLC RTIambassador stub impl.
//
// gorti M32. Every method throws RTIinternalError("M32 stub — impl in M33+").
// M32 GREEN target is LINK-only; runtime bodies land M33+ as wstring-adapters
// over gorti's M17 gRPC surface.

#include "RTIambassadorImpl.h"
#include <RTI/Exception.h>
#include <RTI/RangeBounds.h>
#include <RTI/RTIambassadorFactory.h>
#include <RTI/time/HLAfloat64Time.h>
#include <RTI/time/HLAfloat64Interval.h>

namespace rti1516e {

namespace {
[[noreturn]] void m32_stub(char const* method) {
  std::wstring msg = L"M32 stub — RTIambassador::";
  // narrow → wide append (ASCII-only method names).
  for (char const* p = method; *p; ++p)
    msg.push_back(static_cast<wchar_t>(static_cast<unsigned char>(*p)));
  msg += L"() impl deferred to M33+.";
  throw RTIinternalError(msg);
}
}  // namespace

// Base class ctor/dtor — must have out-of-line definitions for the vtable.
RTIambassador::RTIambassador() RTI_NOEXCEPT {}
RTIambassador::~RTIambassador() {}

DLCRTIambassadorImpl::DLCRTIambassadorImpl() = default;
DLCRTIambassadorImpl::~DLCRTIambassadorImpl() = default;

// ===== §4 Federation Management =====
void DLCRTIambassadorImpl::connect(FederateAmbassador&, CallbackModel,
                                   std::wstring const&) {
  m32_stub("connect");
}
void DLCRTIambassadorImpl::disconnect() { m32_stub("disconnect"); }
void DLCRTIambassadorImpl::createFederationExecution(std::wstring const&,
                                                     std::wstring const&,
                                                     std::wstring const&) {
  m32_stub("createFederationExecution");
}
void DLCRTIambassadorImpl::createFederationExecution(
    std::wstring const&, std::vector<std::wstring> const&,
    std::wstring const&) {
  m32_stub("createFederationExecution");
}
void DLCRTIambassadorImpl::createFederationExecutionWithMIM(
    std::wstring const&, std::vector<std::wstring> const&,
    std::wstring const&, std::wstring const&) {
  m32_stub("createFederationExecutionWithMIM");
}
void DLCRTIambassadorImpl::destroyFederationExecution(std::wstring const&) {
  m32_stub("destroyFederationExecution");
}
void DLCRTIambassadorImpl::listFederationExecutions() {
  m32_stub("listFederationExecutions");
}
FederateHandle DLCRTIambassadorImpl::joinFederationExecution(
    std::wstring const&, std::wstring const&,
    std::vector<std::wstring> const&) {
  m32_stub("joinFederationExecution");
}
FederateHandle DLCRTIambassadorImpl::joinFederationExecution(
    std::wstring const&, std::wstring const&, std::wstring const&,
    std::vector<std::wstring> const&) {
  m32_stub("joinFederationExecution");
}
void DLCRTIambassadorImpl::resignFederationExecution(ResignAction) {
  m32_stub("resignFederationExecution");
}
void DLCRTIambassadorImpl::registerFederationSynchronizationPoint(
    std::wstring const&, VariableLengthData const&) {
  m32_stub("registerFederationSynchronizationPoint");
}
void DLCRTIambassadorImpl::registerFederationSynchronizationPoint(
    std::wstring const&, VariableLengthData const&,
    FederateHandleSet const&) {
  m32_stub("registerFederationSynchronizationPoint");
}
void DLCRTIambassadorImpl::synchronizationPointAchieved(std::wstring const&,
                                                        bool) {
  m32_stub("synchronizationPointAchieved");
}
void DLCRTIambassadorImpl::requestFederationSave(std::wstring const&) {
  m32_stub("requestFederationSave");
}
void DLCRTIambassadorImpl::requestFederationSave(std::wstring const&,
                                                 LogicalTime const&) {
  m32_stub("requestFederationSave");
}
void DLCRTIambassadorImpl::federateSaveBegun() { m32_stub("federateSaveBegun"); }
void DLCRTIambassadorImpl::federateSaveComplete() {
  m32_stub("federateSaveComplete");
}
void DLCRTIambassadorImpl::federateSaveNotComplete() {
  m32_stub("federateSaveNotComplete");
}
void DLCRTIambassadorImpl::abortFederationSave() {
  m32_stub("abortFederationSave");
}
void DLCRTIambassadorImpl::queryFederationSaveStatus() {
  m32_stub("queryFederationSaveStatus");
}
void DLCRTIambassadorImpl::requestFederationRestore(std::wstring const&) {
  m32_stub("requestFederationRestore");
}
void DLCRTIambassadorImpl::federateRestoreComplete() {
  m32_stub("federateRestoreComplete");
}
void DLCRTIambassadorImpl::federateRestoreNotComplete() {
  m32_stub("federateRestoreNotComplete");
}
void DLCRTIambassadorImpl::abortFederationRestore() {
  m32_stub("abortFederationRestore");
}
void DLCRTIambassadorImpl::queryFederationRestoreStatus() {
  m32_stub("queryFederationRestoreStatus");
}

// ===== §5 Declaration Management =====
//
// M34 Agent AC: §5 shim over gorti's M17 declaration Manager surface
// (rti/internal/declaration/manager.go — Publish/Unpublish/Subscribe/
// Unsubscribe {ObjectClassAttributes,InteractionClass}).
//
// Design (parity with §6 shim below):
//   - Spec §5 signatures per catalogue rows 11.9-11.11:
//       * subscribeObjectClassAttributes gets `bool active=true` +
//         `wstring updateRateDesignator=L""` (row 11.9).
//       * unpublishObjectClass whole-class form + subset form both
//         present (row 11.10).
//       * subscribeInteractionClass gets `bool active=true` (row 11.11).
//     The header (RTIambassadorImpl.h lines 79-101) already declares
//     these; this file supplies the bodies.
//   - The DLC handle types (ObjectClassHandle / InteractionClassHandle /
//     AttributeHandleSet) do not map 1:1 to M17's core.ObjectClassHandle
//     etc. (M17 uses raw uint16). A federate-side translation table
//     would live on the pImpl once RTIambassadorImpl.h grows an M17
//     member — same follow-up as §6 ("M33 header PIMPL"). Until then
//     every §5 method throws NotConnected — the spec-legal reply for
//     "no CRC bound" declared in every §5 method's RTI_THROW clause
//     (Pitch RTIambassador.h lines 313-437).
//   - The M17-only extras (`active` flag, `updateRateDesignator`) are
//     accepted at the DLC surface but ignored for now; M35+ wires them
//     into a real request. This matches catalogue rows 11.9/11.11 —
//     the *signature* must accept them (BLOCKING/MAJOR at the ABI
//     boundary) even though the wire semantics land later.

namespace {
[[noreturn]] void m17NotWiredDM_(char const* method) {
  std::wstring msg = L"gorti DLC §5 ";
  for (char const* p = method; *p; ++p)
    msg.push_back(static_cast<wchar_t>(static_cast<unsigned char>(*p)));
  msg += L": M17 declaration.Manager pImpl not yet wired into "
         L"DLCRTIambassadorImpl (M33 follow-up — same header PIMPL as §6). "
         L"Federate is not connected to any CRC.";
  throw NotConnected(msg);
}
}  // namespace

void DLCRTIambassadorImpl::publishObjectClassAttributes(
    ObjectClassHandle theClass, AttributeHandleSet const& attributeList) {
  // §5.2 → M17 declaration.Manager.PublishObjectClassAttributes.
  (void)theClass;
  (void)attributeList;
  m17NotWiredDM_("publishObjectClassAttributes");
}
void DLCRTIambassadorImpl::unpublishObjectClass(ObjectClassHandle theClass) {
  // §5.3 whole-class form (catalogue row 11.10 — was missing). Drops
  // publication of every attribute of `theClass`. M17 side: iterate the
  // class's attribute set and call UnpublishObjectClassAttributes(all).
  (void)theClass;
  m17NotWiredDM_("unpublishObjectClass");
}
void DLCRTIambassadorImpl::unpublishObjectClassAttributes(
    ObjectClassHandle theClass, AttributeHandleSet const& attributeList) {
  // §5.3 subset form → M17 declaration.Manager.UnpublishObjectClassAttributes.
  (void)theClass;
  (void)attributeList;
  m17NotWiredDM_("unpublishObjectClassAttributes");
}
void DLCRTIambassadorImpl::publishInteractionClass(
    InteractionClassHandle theInteraction) {
  // §5.4 → M17 declaration.Manager.PublishInteractionClass.
  (void)theInteraction;
  m17NotWiredDM_("publishInteractionClass");
}
void DLCRTIambassadorImpl::unpublishInteractionClass(
    InteractionClassHandle theInteraction) {
  // §5.5 → M17 declaration.Manager.UnpublishInteractionClass.
  (void)theInteraction;
  m17NotWiredDM_("unpublishInteractionClass");
}
void DLCRTIambassadorImpl::subscribeObjectClassAttributes(
    ObjectClassHandle theClass, AttributeHandleSet const& attributeList,
    bool active, std::wstring const& updateRateDesignator) {
  // §5.6 (catalogue row 11.9). `active` gates §5.10 startRegistration
  // callbacks (passive=false → subscription without registration
  // advisories). `updateRateDesignator` names an update-rate policy
  // from the FOM. Both are DLC-only extras M17 doesn't model yet —
  // M35+ wires them; for now the bodies fall through to NotConnected.
  (void)theClass;
  (void)attributeList;
  (void)active;
  (void)updateRateDesignator;
  m17NotWiredDM_("subscribeObjectClassAttributes");
}
void DLCRTIambassadorImpl::unsubscribeObjectClass(ObjectClassHandle theClass) {
  // §5.7 whole-class form. M17 side: iterate the class's attribute set
  // and call UnsubscribeObjectClassAttributes(all).
  (void)theClass;
  m17NotWiredDM_("unsubscribeObjectClass");
}
void DLCRTIambassadorImpl::unsubscribeObjectClassAttributes(
    ObjectClassHandle theClass, AttributeHandleSet const& attributeList) {
  // §5.7 subset form → M17 declaration.Manager.UnsubscribeObjectClassAttributes.
  (void)theClass;
  (void)attributeList;
  m17NotWiredDM_("unsubscribeObjectClassAttributes");
}
void DLCRTIambassadorImpl::subscribeInteractionClass(
    InteractionClassHandle theClass, bool active) {
  // §5.8 (catalogue row 11.11). `active` gates §5.12 turnInteractionsOn/
  // Off callbacks; DLC-only extra ignored for now, wired M35+.
  (void)theClass;
  (void)active;
  m17NotWiredDM_("subscribeInteractionClass");
}
void DLCRTIambassadorImpl::unsubscribeInteractionClass(
    InteractionClassHandle theClass) {
  // §5.9 → M17 declaration.Manager.UnsubscribeInteractionClass.
  (void)theClass;
  m17NotWiredDM_("unsubscribeInteractionClass");
}

// ===== §6 Object Management =====
//
// M33 Agent L: §6 shim over gorti's M17 RtiAmbassador gRPC surface.
//
// Design:
//   - Spec (§6) uses std::wstring for object/interaction names; M17 uses
//     std::string. Each shim narrows wstring→string via ws2s_() before
//     calling M17. The DLC catalogue §17.1 promotes `theUserSuppliedTag`
//     to MANDATORY on every §6 call; the shim binds it into a
//     std::vector<uint8_t> (m17_tag_) so the M17 wire request always
//     carries the federate-supplied bytes.
//   - The DLCRTIambassadorImpl class currently owns no M17 state member;
//     wiring an M17 RTIambassador into a `pImpl` requires editing
//     RTIambassadorImpl.h, which is outside M33 Agent L's file scope
//     ("Only touch RTIambassadorImpl.cpp lines 159-227"). Until that
//     PIMPL lands (tracked as an M33-follow-up), each shim throws
//     rti1516e::NotConnected — the spec-legal reply for "no CRC bound"
//     that all §6 methods declare in their RTI_THROW clause. This keeps
//     the symbols present (so the 7 om_* conformance fixtures LINK) and
//     is the correct runtime signal for a not-yet-wired federate.
//   - The multi-name variants (§6.5) sort into a std::vector<std::string>
//     preserving the atomic semantics: reserveMultipleObjectInstanceName
//     is one gRPC call (M17: ReserveMultipleObjectInstanceNames);
//     releaseMultipleObjectInstanceName loops the singular M17 release
//     (M17 has no batch release RPC yet).
//   - Async callback shape (§6.5 reservation → objectInstanceNameReservation
//     Succeeded/Failed) is delivered on the federate's FederateAmbassador,
//     wired via the M17 event-stream. That plumbing is unchanged; the
//     spec-facing DLC method returns void and the callback fires when
//     the M17 stream lands the reservation reply.

namespace {

// wstring → narrow string. Object instance names, interaction/object
// class names and attribute names are ASCII per FOM XML convention;
// FR-DLC-17 requires the shim to be lossy-tolerant. When high-Unicode
// characters land in a name the M17 layer will fail on gRPC validation
// — the DLC contract is "spec-legal wide input; narrow-string M17 gate".
std::string ws2s_(std::wstring const& w) {
  return std::string(w.begin(), w.end());
}

// VariableLengthData → byte vector for the M17 tag field. §17.1 makes
// the tag mandatory; passing an empty VLD yields an empty vector, which
// M17 wire-encodes as a zero-length bytes field (spec-legal).
std::vector<uint8_t> tag2bytes_(VariableLengthData const& tag) {
  auto const* p = static_cast<uint8_t const*>(tag.data());
  return std::vector<uint8_t>(p, p + tag.size());
}

// Central point for the "M17 not yet wired" reply. All §6 methods
// declare NotConnected in their RTI_THROW clause (see Pitch spec header
// §6.5-6.19); this is the spec-legal way to signal "no CRC bound".
[[noreturn]] void m17NotWired_(char const* method) {
  std::wstring msg = L"gorti DLC §6 ";
  for (char const* p = method; *p; ++p)
    msg.push_back(static_cast<wchar_t>(static_cast<unsigned char>(*p)));
  msg += L": M17 RtiAmbassador pImpl not yet wired into "
         L"DLCRTIambassadorImpl (M33 follow-up — needs a private member on "
         L"RTIambassadorImpl.h; tracked as \"M33 header PIMPL\" in the "
         L"dispatch plan). Federate is not connected to any CRC.";
  throw NotConnected(msg);
}

}  // namespace

void DLCRTIambassadorImpl::reserveObjectInstanceName(
    std::wstring const& theObjectInstanceName) {
  // §6.5 — async. Result delivered via objectInstanceNameReservation
  //         {Succeeded,Failed}(name) on the bound FederateAmbassador.
  std::string const name = ws2s_(theObjectInstanceName);
  (void)name;  // Used once M17 pImpl lands.
  m17NotWired_("reserveObjectInstanceName");
}

void DLCRTIambassadorImpl::releaseObjectInstanceName(
    std::wstring const& theObjectInstanceName) {
  // §6.6 — synchronous release of a previously reserved name.
  std::string const name = ws2s_(theObjectInstanceName);
  (void)name;
  m17NotWired_("releaseObjectInstanceName");
}

void DLCRTIambassadorImpl::reserveMultipleObjectInstanceName(
    std::set<std::wstring> const& theObjectInstanceNames) {
  // §6.5 (multi) — atomic batch. Delivered via
  //   multipleObjectInstanceNameReservation{Succeeded,Failed}(names).
  // M17 layer: single ReserveMultipleObjectInstanceNames RPC.
  std::vector<std::string> names;
  names.reserve(theObjectInstanceNames.size());
  for (auto const& w : theObjectInstanceNames) {
    names.push_back(ws2s_(w));
  }
  (void)names;
  m17NotWired_("reserveMultipleObjectInstanceName");
}

void DLCRTIambassadorImpl::releaseMultipleObjectInstanceName(
    std::set<std::wstring> const& theObjectInstanceNames) {
  // §6.7 (multi) — spec allows non-atomic release. M17 has no batch
  // release RPC, so the shim will loop the singular release once
  // pImpl lands.
  std::vector<std::string> names;
  names.reserve(theObjectInstanceNames.size());
  for (auto const& w : theObjectInstanceNames) {
    names.push_back(ws2s_(w));
  }
  (void)names;
  m17NotWired_("releaseMultipleObjectInstanceName");
}

ObjectInstanceHandle DLCRTIambassadorImpl::registerObjectInstance(
    ObjectClassHandle theClass) {
  // §6.8 overload 1 — RTI-generated name. Distinct from the 2-arg
  // overload per catalogue row 11.2 (2 spec overloads, not one with
  // default-arg — the vtable slot must be distinct).
  (void)theClass;
  m17NotWired_("registerObjectInstance");
}

ObjectInstanceHandle DLCRTIambassadorImpl::registerObjectInstance(
    ObjectClassHandle theClass,
    std::wstring const& theObjectInstanceName) {
  // §6.8 overload 2 — federate-supplied name. Name must have been
  // reserved via §6.5 first; otherwise ObjectInstanceNameNotReserved.
  std::string const name = ws2s_(theObjectInstanceName);
  (void)theClass;
  (void)name;
  m17NotWired_("registerObjectInstance");
}

void DLCRTIambassadorImpl::updateAttributeValues(
    ObjectInstanceHandle theObject,
    AttributeHandleValueMap const& theAttributeValues,
    VariableLengthData const& theUserSuppliedTag) {
  // §6.10 overload 1 (RO). Tag is MANDATORY per catalogue §17.1.
  auto const m17_tag_ = tag2bytes_(theUserSuppliedTag);
  (void)theObject;
  (void)theAttributeValues;
  (void)m17_tag_;
  m17NotWired_("updateAttributeValues");
}

MessageRetractionHandle DLCRTIambassadorImpl::updateAttributeValues(
    ObjectInstanceHandle theObject,
    AttributeHandleValueMap const& theAttributeValues,
    VariableLengthData const& theUserSuppliedTag,
    LogicalTime const& theTime) {
  // §6.10 overload 2 (TSO). Returns MessageRetractionHandle for
  // §8.21 retract(). Tag mandatory; theTime binds to a §8 logical
  // time from the federation's HLAlogicalTimeFactory.
  auto const m17_tag_ = tag2bytes_(theUserSuppliedTag);
  (void)theObject;
  (void)theAttributeValues;
  (void)m17_tag_;
  (void)theTime;
  m17NotWired_("updateAttributeValues");
}

void DLCRTIambassadorImpl::sendInteraction(
    InteractionClassHandle theInteraction,
    ParameterHandleValueMap const& theParameterValues,
    VariableLengthData const& theUserSuppliedTag) {
  // §6.12 overload 1 (RO). Tag mandatory.
  auto const m17_tag_ = tag2bytes_(theUserSuppliedTag);
  (void)theInteraction;
  (void)theParameterValues;
  (void)m17_tag_;
  m17NotWired_("sendInteraction");
}

MessageRetractionHandle DLCRTIambassadorImpl::sendInteraction(
    InteractionClassHandle theInteraction,
    ParameterHandleValueMap const& theParameterValues,
    VariableLengthData const& theUserSuppliedTag,
    LogicalTime const& theTime) {
  // §6.12 overload 2 (TSO). Returns MessageRetractionHandle. Used by
  // om_message_retraction/federate_publisher.cpp.
  auto const m17_tag_ = tag2bytes_(theUserSuppliedTag);
  (void)theInteraction;
  (void)theParameterValues;
  (void)m17_tag_;
  (void)theTime;
  m17NotWired_("sendInteraction");
}

void DLCRTIambassadorImpl::deleteObjectInstance(
    ObjectInstanceHandle theObject,
    VariableLengthData const& theUserSuppliedTag) {
  // §6.14 overload 1 (RO). Tag mandatory. Federate must own at least
  // the privilegeToDelete attribute of the target instance.
  auto const m17_tag_ = tag2bytes_(theUserSuppliedTag);
  (void)theObject;
  (void)m17_tag_;
  m17NotWired_("deleteObjectInstance");
}

MessageRetractionHandle DLCRTIambassadorImpl::deleteObjectInstance(
    ObjectInstanceHandle theObject,
    VariableLengthData const& theUserSuppliedTag,
    LogicalTime const& theTime) {
  // §6.14 overload 2 (TSO). Returns MessageRetractionHandle.
  auto const m17_tag_ = tag2bytes_(theUserSuppliedTag);
  (void)theObject;
  (void)m17_tag_;
  (void)theTime;
  m17NotWired_("deleteObjectInstance");
}

void DLCRTIambassadorImpl::localDeleteObjectInstance(
    ObjectInstanceHandle theObject) {
  // §6.16 — remove the local reflection only; no wire traffic. Used
  // when the federate no longer needs to reflect updates for the
  // instance. Fires no callbacks.
  (void)theObject;
  m17NotWired_("localDeleteObjectInstance");
}

void DLCRTIambassadorImpl::requestAttributeValueUpdate(
    ObjectInstanceHandle theObject,
    AttributeHandleSet const& theAttributes,
    VariableLengthData const& theUserSuppliedTag) {
  // §6.19 overload 1 — by object instance. Fires provideAttribute-
  // ValueUpdate callback on publisher(s) with matching attributes.
  // Tag mandatory.
  auto const m17_tag_ = tag2bytes_(theUserSuppliedTag);
  (void)theObject;
  (void)theAttributes;
  (void)m17_tag_;
  m17NotWired_("requestAttributeValueUpdate");
}

void DLCRTIambassadorImpl::requestAttributeValueUpdate(
    ObjectClassHandle theClass,
    AttributeHandleSet const& theAttributes,
    VariableLengthData const& theUserSuppliedTag) {
  // §6.19 overload 2 — by object class. Fires provideAttribute-
  // ValueUpdate for every instance of the class the federate knows
  // about that publishes any of the requested attributes.
  auto const m17_tag_ = tag2bytes_(theUserSuppliedTag);
  (void)theClass;
  (void)theAttributes;
  (void)m17_tag_;
  m17NotWired_("requestAttributeValueUpdate");
}

// ===== §7 Ownership Management =====
//
// M33-K: signature-parity impls per IEEE 1516.1-2010 §7.2-7.19 and
// docs/DLC_DIVERGENCE_CATALOGUE.md §12 (rows 12.1-12.7). Bodies here
// are deliberately minimal (no persistent ownership state; no
// FederateAmbassador is bound on this impl until §4 connect() is
// implemented in M34+). They satisfy the vtable + spec signature +
// out-param contracts so §7 conformance fixtures LINK cleanly.
//
// Runtime callback delivery (requestDivestitureConfirmation,
// attributeOwnershipAcquisitionNotification, informAttributeOwnership,
// attributeIsNotOwned, attributeIsOwnedByRTI, attributeOwnershipUnavailable)
// is deferred to M34+ once §4 `connect()` stores the bound
// FederateAmbassador reference on the impl.

// §7.2 — unconditional divest. Void return; no side-effects modeled.
void DLCRTIambassadorImpl::unconditionalAttributeOwnershipDivestiture(
    ObjectInstanceHandle, AttributeHandleSet const&) {
  // no-op: real ownership bookkeeping deferred to M34+.
}

// §7.3 — negotiated divest (offer to subscribers). Callback delivery of
// requestAttributeOwnershipAssumption on subscribers is deferred to M34+.
void DLCRTIambassadorImpl::negotiatedAttributeOwnershipDivestiture(
    ObjectInstanceHandle, AttributeHandleSet const&,
    VariableLengthData const&) {
  // no-op.
}

// §7.6 — confirm divest after §7.5 requestDivestitureConfirmation.
// Catalogue row 12.1: M17 was absent; DLC adds it.
void DLCRTIambassadorImpl::confirmDivestiture(ObjectInstanceHandle,
                                              AttributeHandleSet const&,
                                              VariableLengthData const&) {
  // no-op.
}

// §7.8 — request ownership acquisition with a spec-mandatory tag.
// Catalogue row 12.2: M17 lacked the tag; DLC adds it (BLOCKING).
void DLCRTIambassadorImpl::attributeOwnershipAcquisition(
    ObjectInstanceHandle, AttributeHandleSet const&,
    VariableLengthData const&) {
  // no-op.
}

// §7.9 — acquire-if-available. Per spec this is (object, attributes)
// with NO tag. Catalogue row 12.3: M17 absent; DLC adds it (MAJOR).
void DLCRTIambassadorImpl::attributeOwnershipAcquisitionIfAvailable(
    ObjectInstanceHandle, AttributeHandleSet const&) {
  // no-op.
}

// §7.12 — release-denied. Catalogue row 12.4: M17 absent; DLC adds
// it (MAJOR).
void DLCRTIambassadorImpl::attributeOwnershipReleaseDenied(
    ObjectInstanceHandle, AttributeHandleSet const&) {
  // no-op.
}

// §7.13 — divest-if-wanted. Spec has an out-param filled with the
// attributes the RTI actually divested. Catalogue row 12.5: M17 had no
// out-param; DLC adds it (BLOCKING). We clear the out-set — no owner
// state means nothing was divested this call.
void DLCRTIambassadorImpl::attributeOwnershipDivestitureIfWanted(
    ObjectInstanceHandle, AttributeHandleSet const&,
    AttributeHandleSet& theDivestedAttributes) {
  theDivestedAttributes.clear();
}

// §7.14 — cancel a pending negotiated divest.
void DLCRTIambassadorImpl::cancelNegotiatedAttributeOwnershipDivestiture(
    ObjectInstanceHandle, AttributeHandleSet const&) {
  // no-op.
}

// §7.15 — cancel a pending acquisition.
void DLCRTIambassadorImpl::cancelAttributeOwnershipAcquisition(
    ObjectInstanceHandle, AttributeHandleSet const&) {
  // no-op.
}

// §7.17 — query ownership. Void return per spec; the answer arrives on
// a §7.18 callback (informAttributeOwnership / attributeIsNotOwned /
// attributeIsOwnedByRTI). Catalogue row 12.6 (BLOCKING): M17 returned a
// synchronous OwnershipQueryResult; DLC drops the return + defers the
// answer to the FederateAmbassador. M34+ will dispatch the callback on
// the FederateAmbassador stored by connect().
void DLCRTIambassadorImpl::queryAttributeOwnership(ObjectInstanceHandle,
                                                   AttributeHandle) {
  // no-op: callback delivery deferred to M34+.
}

// §7.19 — is this attribute owned by THIS federate? Direct bool per
// spec. Catalogue row 12.7 (COSMETIC): DLC matches M17 shape. Without
// stored ownership state we return false conservatively — the fixture
// won't observe true until M34+ wires real state.
bool DLCRTIambassadorImpl::isAttributeOwnedByFederate(ObjectInstanceHandle,
                                                      AttributeHandle) {
  return false;
}

// ===== §8 Time Management =====
//
// M34 Agent AB: §8 shim over gorti's M17 time surface.
//
// Design:
//   - Spec (§8) parameters LogicalTime const& and LogicalTimeInterval const&
//     are pure-abstract; gorti M17 speaks double. Each shim narrows to
//     double via `dynamic_cast<HLAfloat64Time const*>(&lt)->getTime()`
//     (resp. HLAfloat64Interval::getInterval()). Federation-scope logical-
//     time bindings other than HLAfloat64Time are not supported (catalogue
//     §9 rows 9.1/9.4). When the concrete type is not HLAfloat64Time the
//     dynamic_cast returns nullptr and we fall through to the M17-not-wired
//     branch; a future pImpl wiring will replace the guard with a proper
//     InvalidLogicalTime throw.
//   - Like §6, DLCRTIambassadorImpl currently owns no M17 client member;
//     wiring one requires editing RTIambassadorImpl.h, which is outside
//     M34 Agent AB's file scope ("Only touch RTIambassadorImpl.cpp §8").
//     Until that pImpl lands each shim throws rti1516e::NotConnected via
//     `m17NotWiredTime_` — spec-legal per every §8 method's RTI_THROW
//     clause (see Pitch RTIambassador.h §8.2-8.24). This keeps the 13
//     catalogue §9 rows LINKed for the tm_* conformance fixtures.
//   - Async ack shape (enableTimeRegulation → timeRegulationEnabled;
//     enableTimeConstrained → timeConstrainedEnabled; TAR/NER/FQR →
//     timeAdvanceGrant) is delivered on the federate's FederateAmbassador
//     via the M17 event-stream once the pImpl lands.
//
// Catalogue §9 rows 9.1-9.14 (FR-DLC-8).

namespace {

// Central point for the "M17 §8 not yet wired" reply. Every §8 method
// declares NotConnected in its RTI_THROW clause; NotConnected is the
// spec-legal signal for "no CRC bound" and correctly reflects the M17-
// pImpl-not-installed state.
[[noreturn]] void m17NotWiredTime_(char const* method) {
  std::wstring msg = L"gorti DLC §8 ";
  for (char const* p = method; *p; ++p)
    msg.push_back(static_cast<wchar_t>(static_cast<unsigned char>(*p)));
  msg += L": M17 time surface not yet wired into DLCRTIambassadorImpl "
         L"(M34 follow-up — needs a private M17 client member on "
         L"RTIambassadorImpl.h; tracked as \"M34 header pImpl\" in the "
         L"dispatch plan). Federate is not connected to any CRC.";
  throw NotConnected(msg);
}

}  // namespace

void DLCRTIambassadorImpl::enableTimeRegulation(
    LogicalTimeInterval const& theLookahead) {
  // §8.2 — async. Federate must be joined; ack arrives asynchronously via
  //        §8.3 timeRegulationEnabled(federateTime) on FederateAmbassador.
  //        gorti M17 uses double; narrow via HLAfloat64Interval concrete.
  auto const* p = dynamic_cast<HLAfloat64Interval const*>(&theLookahead);
  double const m17_lookahead_ = p ? p->getInterval() : 0.0;
  (void)m17_lookahead_;
  m17NotWiredTime_("enableTimeRegulation");
}

void DLCRTIambassadorImpl::disableTimeRegulation() {
  // §8.4 — synchronous per spec (no async ack).
  m17NotWiredTime_("disableTimeRegulation");
}

void DLCRTIambassadorImpl::enableTimeConstrained() {
  // §8.5 — async. Ack via §8.6 timeConstrainedEnabled(federateTime).
  m17NotWiredTime_("enableTimeConstrained");
}

void DLCRTIambassadorImpl::disableTimeConstrained() {
  // §8.7 — synchronous.
  m17NotWiredTime_("disableTimeConstrained");
}

void DLCRTIambassadorImpl::timeAdvanceRequest(LogicalTime const& theTime) {
  // §8.8 — async; grant via §8.13 timeAdvanceGrant(theTime).
  auto const* p = dynamic_cast<HLAfloat64Time const*>(&theTime);
  double const m17_time_ = p ? p->getTime() : 0.0;
  (void)m17_time_;
  m17NotWiredTime_("timeAdvanceRequest");
}

void DLCRTIambassadorImpl::timeAdvanceRequestAvailable(
    LogicalTime const& theTime) {
  // §8.9 — async; grant via §8.13.
  auto const* p = dynamic_cast<HLAfloat64Time const*>(&theTime);
  double const m17_time_ = p ? p->getTime() : 0.0;
  (void)m17_time_;
  m17NotWiredTime_("timeAdvanceRequestAvailable");
}

void DLCRTIambassadorImpl::nextMessageRequest(LogicalTime const& theTime) {
  // §8.10 — async; grant via §8.13. NER anchors the tm_ner_pair fixture.
  auto const* p = dynamic_cast<HLAfloat64Time const*>(&theTime);
  double const m17_time_ = p ? p->getTime() : 0.0;
  (void)m17_time_;
  m17NotWiredTime_("nextMessageRequest");
}

void DLCRTIambassadorImpl::nextMessageRequestAvailable(
    LogicalTime const& theTime) {
  // §8.11 — async; grant via §8.13.
  auto const* p = dynamic_cast<HLAfloat64Time const*>(&theTime);
  double const m17_time_ = p ? p->getTime() : 0.0;
  (void)m17_time_;
  m17NotWiredTime_("nextMessageRequestAvailable");
}

void DLCRTIambassadorImpl::flushQueueRequest(LogicalTime const& theTime) {
  // §8.12 — async; grant via §8.13.
  auto const* p = dynamic_cast<HLAfloat64Time const*>(&theTime);
  double const m17_time_ = p ? p->getTime() : 0.0;
  (void)m17_time_;
  m17NotWiredTime_("flushQueueRequest");
}

void DLCRTIambassadorImpl::enableAsynchronousDelivery() {
  // §8.14 — synchronous toggle.
  m17NotWiredTime_("enableAsynchronousDelivery");
}

void DLCRTIambassadorImpl::disableAsynchronousDelivery() {
  // §8.15 — synchronous toggle.
  m17NotWiredTime_("disableAsynchronousDelivery");
}

bool DLCRTIambassadorImpl::queryGALT(LogicalTime& theTime) {
  // §8.16 — out-param + bool. Returns false if GALT is undefined (no
  //         regulating federates); otherwise assigns theTime = GALT.
  (void)theTime;
  m17NotWiredTime_("queryGALT");
}

void DLCRTIambassadorImpl::queryLogicalTime(LogicalTime& theTime) {
  // §8.17 — assigns federate's current logical time.
  (void)theTime;
  m17NotWiredTime_("queryLogicalTime");
}

bool DLCRTIambassadorImpl::queryLITS(LogicalTime& theTime) {
  // §8.18 — out-param + bool. Least incoming time stamp; false if none.
  (void)theTime;
  m17NotWiredTime_("queryLITS");
}

void DLCRTIambassadorImpl::modifyLookahead(
    LogicalTimeInterval const& theLookahead) {
  // §8.19 — synchronous. Requires TimeRegulation enabled.
  auto const* p = dynamic_cast<HLAfloat64Interval const*>(&theLookahead);
  double const m17_lookahead_ = p ? p->getInterval() : 0.0;
  (void)m17_lookahead_;
  m17NotWiredTime_("modifyLookahead");
}

void DLCRTIambassadorImpl::queryLookahead(LogicalTimeInterval& interval) {
  // §8.20 — out-param. Requires TimeRegulation enabled.
  (void)interval;
  m17NotWiredTime_("queryLookahead");
}

void DLCRTIambassadorImpl::retract(MessageRetractionHandle theHandle) {
  // §8.21 — retract a previously-sent TSO message by handle from
  //         updateAttributeValues/sendInteraction/deleteObjectInstance
  //         TSO overloads.
  (void)theHandle;
  m17NotWiredTime_("retract");
}

void DLCRTIambassadorImpl::changeAttributeOrderType(
    ObjectInstanceHandle theObject,
    AttributeHandleSet const& theAttributes,
    OrderType theType) {
  // §8.23 — per-attribute order change (TIMESTAMP ↔ RECEIVE).
  (void)theObject;
  (void)theAttributes;
  (void)theType;
  m17NotWiredTime_("changeAttributeOrderType");
}

void DLCRTIambassadorImpl::changeInteractionOrderType(
    InteractionClassHandle theClass,
    OrderType theType) {
  // §8.24 — per-interaction-class order change (TIMESTAMP ↔ RECEIVE).
  (void)theClass;
  (void)theType;
  m17NotWiredTime_("changeInteractionOrderType");
}

// ===== §9 DDM =====
RegionHandle DLCRTIambassadorImpl::createRegion(DimensionHandleSet const&) {
  m32_stub("createRegion");
}
void DLCRTIambassadorImpl::commitRegionModifications(RegionHandleSet const&) {
  m32_stub("commitRegionModifications");
}
void DLCRTIambassadorImpl::deleteRegion(RegionHandle const&) {
  m32_stub("deleteRegion");
}

ObjectInstanceHandle DLCRTIambassadorImpl::registerObjectInstanceWithRegions(
    ObjectClassHandle, AttributeHandleSetRegionHandleSetPairVector const&) {
  m32_stub("registerObjectInstanceWithRegions");
}
ObjectInstanceHandle DLCRTIambassadorImpl::registerObjectInstanceWithRegions(
    ObjectClassHandle, AttributeHandleSetRegionHandleSetPairVector const&,
    std::wstring const&) {
  m32_stub("registerObjectInstanceWithRegions");
}

void DLCRTIambassadorImpl::associateRegionsForUpdates(
    ObjectInstanceHandle, AttributeHandleSetRegionHandleSetPairVector const&) {
  m32_stub("associateRegionsForUpdates");
}
void DLCRTIambassadorImpl::unassociateRegionsForUpdates(
    ObjectInstanceHandle, AttributeHandleSetRegionHandleSetPairVector const&) {
  m32_stub("unassociateRegionsForUpdates");
}

void DLCRTIambassadorImpl::subscribeObjectClassAttributesWithRegions(
    ObjectClassHandle, AttributeHandleSetRegionHandleSetPairVector const&,
    bool, std::wstring const&) {
  m32_stub("subscribeObjectClassAttributesWithRegions");
}
void DLCRTIambassadorImpl::unsubscribeObjectClassAttributesWithRegions(
    ObjectClassHandle, AttributeHandleSetRegionHandleSetPairVector const&) {
  m32_stub("unsubscribeObjectClassAttributesWithRegions");
}

void DLCRTIambassadorImpl::subscribeInteractionClassWithRegions(
    InteractionClassHandle, RegionHandleSet const&, bool) {
  m32_stub("subscribeInteractionClassWithRegions");
}
void DLCRTIambassadorImpl::unsubscribeInteractionClassWithRegions(
    InteractionClassHandle, RegionHandleSet const&) {
  m32_stub("unsubscribeInteractionClassWithRegions");
}

void DLCRTIambassadorImpl::sendInteractionWithRegions(
    InteractionClassHandle, ParameterHandleValueMap const&,
    RegionHandleSet const&, VariableLengthData const&) {
  m32_stub("sendInteractionWithRegions");
}
MessageRetractionHandle DLCRTIambassadorImpl::sendInteractionWithRegions(
    InteractionClassHandle, ParameterHandleValueMap const&,
    RegionHandleSet const&, VariableLengthData const&, LogicalTime const&) {
  m32_stub("sendInteractionWithRegions");
}
void DLCRTIambassadorImpl::requestAttributeValueUpdateWithRegions(
    ObjectClassHandle, AttributeHandleSetRegionHandleSetPairVector const&,
    VariableLengthData const&) {
  m32_stub("requestAttributeValueUpdateWithRegions");
}

// ===== §10 Support Services =====
//
// M33 Agent M impl. Design: the DLC RTIambassador has no gRPC binding yet —
// it's the front-end for a future M35 dispatch layer. So handle-lookup
// methods that need federation state throw NotConnected (spec-legal error),
// while stateless conversions (order/transport enum ↔ name, normalize) are
// fully implemented. Advisory-switch on/off pairs no-op (silent success —
// there's no advisory infrastructure to gate yet; the DLC contract only
// requires void return, no throw). evokeCallback / evokeMultipleCallbacks
// no-op returning false (no queued events). getTimeFactory returns a
// null-throwing placeholder (Task M-4: factory-factory chain deferred).
// The 9 decode*Handle methods use per-handle Friend shims (defined below).
//
// Catalogue rows 13.1-13.15 all touched. FR-DLC-4 (wstring), FR-DLC-14
// (callback re-entry) exercised by test_rtiambassador_handle_services.cpp
// static_asserts.

ResignAction DLCRTIambassadorImpl::getAutomaticResignDirective() {
  // §10.2 — default per Pitch is NO_ACTION; return that until a real
  // federation binding lands.
  return NO_ACTION;
}
void DLCRTIambassadorImpl::setAutomaticResignDirective(ResignAction) {
  // §10.3 — no federation binding; silently accept. Spec-legal (returns
  // void, no throw required in the not-connected case for this setter).
}
FederateHandle DLCRTIambassadorImpl::getFederateHandle(std::wstring const&) {
  // §10.4 — need federation state to resolve name → handle.
  throw NotConnected(L"DLC RTIambassador: getFederateHandle requires "
                     L"federation connection (M35+).");
}
std::wstring DLCRTIambassadorImpl::getFederateName(FederateHandle theHandle) {
  // §10.5 — need federation state.
  if (!theHandle.isValid()) throw InvalidFederateHandle(L"getFederateName");
  throw NotConnected(L"DLC RTIambassador: getFederateName requires "
                     L"federation connection (M35+).");
}
ObjectClassHandle DLCRTIambassadorImpl::getObjectClassHandle(
    std::wstring const&) {
  // §10.6 — need FOM state to resolve class name.
  throw NotConnected(L"DLC RTIambassador: getObjectClassHandle requires "
                     L"federation connection (M35+).");
}
std::wstring DLCRTIambassadorImpl::getObjectClassName(
    ObjectClassHandle theHandle) {
  // §10.7 — need FOM state.
  if (!theHandle.isValid())
    throw InvalidObjectClassHandle(L"getObjectClassName");
  throw NotConnected(L"DLC RTIambassador: getObjectClassName requires "
                     L"federation connection (M35+).");
}
ObjectClassHandle DLCRTIambassadorImpl::getKnownObjectClassHandle(
    ObjectInstanceHandle) {
  // §10.8 — need federation state to look up instance's known class.
  throw NotConnected(L"DLC RTIambassador: getKnownObjectClassHandle requires "
                     L"federation connection (M35+).");
}
ObjectInstanceHandle DLCRTIambassadorImpl::getObjectInstanceHandle(
    std::wstring const&) {
  // §10.9 — need federation state to resolve instance name.
  throw NotConnected(L"DLC RTIambassador: getObjectInstanceHandle requires "
                     L"federation connection (M35+).");
}
std::wstring DLCRTIambassadorImpl::getObjectInstanceName(
    ObjectInstanceHandle) {
  // §10.10 — need federation state.
  throw NotConnected(L"DLC RTIambassador: getObjectInstanceName requires "
                     L"federation connection (M35+).");
}
AttributeHandle DLCRTIambassadorImpl::getAttributeHandle(ObjectClassHandle,
                                                         std::wstring const&) {
  // §10.11 — need FOM state.
  throw NotConnected(L"DLC RTIambassador: getAttributeHandle requires "
                     L"federation connection (M35+).");
}
std::wstring DLCRTIambassadorImpl::getAttributeName(ObjectClassHandle,
                                                    AttributeHandle) {
  // §10.12 — need FOM state.
  throw NotConnected(L"DLC RTIambassador: getAttributeName requires "
                     L"federation connection (M35+).");
}
double DLCRTIambassadorImpl::getUpdateRateValue(std::wstring const&) {
  // §10.13 — need FOM state (update-rate designators are FOM-declared).
  throw InvalidUpdateRateDesignator(L"DLC RTIambassador: no FOM loaded "
                                    L"(federation connection deferred M35+).");
}
double DLCRTIambassadorImpl::getUpdateRateValueForAttribute(
    ObjectInstanceHandle, AttributeHandle) {
  // §10.14 — need federation state.
  throw NotConnected(L"DLC RTIambassador: getUpdateRateValueForAttribute "
                     L"requires federation connection (M35+).");
}
InteractionClassHandle DLCRTIambassadorImpl::getInteractionClassHandle(
    std::wstring const&) {
  // §10.15 — need FOM state.
  throw NotConnected(L"DLC RTIambassador: getInteractionClassHandle requires "
                     L"federation connection (M35+).");
}
std::wstring DLCRTIambassadorImpl::getInteractionClassName(
    InteractionClassHandle) {
  // §10.16 — need FOM state.
  throw NotConnected(L"DLC RTIambassador: getInteractionClassName requires "
                     L"federation connection (M35+).");
}
ParameterHandle DLCRTIambassadorImpl::getParameterHandle(
    InteractionClassHandle, std::wstring const&) {
  // §10.17 — need FOM state.
  throw NotConnected(L"DLC RTIambassador: getParameterHandle requires "
                     L"federation connection (M35+).");
}
std::wstring DLCRTIambassadorImpl::getParameterName(InteractionClassHandle,
                                                    ParameterHandle) {
  // §10.18 — need FOM state.
  throw NotConnected(L"DLC RTIambassador: getParameterName requires "
                     L"federation connection (M35+).");
}

// §10.19 getOrderType — spec-defined name ↔ enum mapping. Names per
// IEEE 1516.1-2010 Table 10-1: L"Receive", L"TimeStamp".
OrderType DLCRTIambassadorImpl::getOrderType(std::wstring const& orderName) {
  if (orderName == L"Receive") return RECEIVE;
  if (orderName == L"TimeStamp") return TIMESTAMP;
  throw InvalidOrderName(L"DLC RTIambassador: unknown OrderType name '" +
                         orderName + L"'.");
}
std::wstring DLCRTIambassadorImpl::getOrderName(OrderType theType) {
  switch (theType) {
    case RECEIVE:   return L"Receive";
    case TIMESTAMP: return L"TimeStamp";
  }
  throw InvalidOrderType(L"DLC RTIambassador: OrderType out of range.");
}

// §10.21 getTransportationType — spec-defined name ↔ enum mapping. Names per
// IEEE 1516.1-2010 Table 10-1: L"HLAreliable", L"HLAbestEffort".
TransportationType DLCRTIambassadorImpl::getTransportationType(
    std::wstring const& transportationName) {
  if (transportationName == L"HLAreliable")   return RELIABLE;
  if (transportationName == L"HLAbestEffort") return BEST_EFFORT;
  throw InvalidTransportationName(
      L"DLC RTIambassador: unknown TransportationType name '" +
      transportationName + L"'.");
}
std::wstring DLCRTIambassadorImpl::getTransportationName(
    TransportationType theType) {
  switch (theType) {
    case RELIABLE:    return L"HLAreliable";
    case BEST_EFFORT: return L"HLAbestEffort";
  }
  throw InvalidTransportationType(
      L"DLC RTIambassador: TransportationType out of range.");
}

DimensionHandleSet DLCRTIambassadorImpl::getAvailableDimensionsForClassAttribute(
    ObjectClassHandle, AttributeHandle) {
  // §10.23 — need FOM state (dimensions are FOM-declared).
  throw NotConnected(L"DLC RTIambassador: getAvailableDimensionsForClass"
                     L"Attribute requires federation connection (M35+).");
}
DimensionHandleSet
DLCRTIambassadorImpl::getAvailableDimensionsForInteractionClass(
    InteractionClassHandle) {
  // §10.24 — need FOM state.
  throw NotConnected(L"DLC RTIambassador: getAvailableDimensionsForInteraction"
                     L"Class requires federation connection (M35+).");
}
DimensionHandle DLCRTIambassadorImpl::getDimensionHandle(std::wstring const&) {
  // §10.25 — need FOM state.
  throw NotConnected(L"DLC RTIambassador: getDimensionHandle requires "
                     L"federation connection (M35+).");
}
std::wstring DLCRTIambassadorImpl::getDimensionName(DimensionHandle theHandle) {
  // §10.26 — need FOM state.
  if (!theHandle.isValid())
    throw InvalidDimensionHandle(L"getDimensionName");
  throw NotConnected(L"DLC RTIambassador: getDimensionName requires "
                     L"federation connection (M35+).");
}
unsigned long DLCRTIambassadorImpl::getDimensionUpperBound(
    DimensionHandle theHandle) {
  // §10.27 — need FOM state.
  if (!theHandle.isValid())
    throw InvalidDimensionHandle(L"getDimensionUpperBound");
  throw NotConnected(L"DLC RTIambassador: getDimensionUpperBound requires "
                     L"federation connection (M35+).");
}
DimensionHandleSet DLCRTIambassadorImpl::getDimensionHandleSet(
    RegionHandle theRegion) {
  // §10.28 — need region-registry state.
  if (!theRegion.isValid()) throw InvalidRegion(L"getDimensionHandleSet");
  throw NotConnected(L"DLC RTIambassador: getDimensionHandleSet requires "
                     L"federation connection (M35+).");
}
RangeBounds DLCRTIambassadorImpl::getRangeBounds(RegionHandle theRegion,
                                                 DimensionHandle theDim) {
  // §10.29 — need region-registry state.
  if (!theRegion.isValid()) throw InvalidRegion(L"getRangeBounds");
  if (!theDim.isValid()) throw InvalidDimensionHandle(L"getRangeBounds");
  throw NotConnected(L"DLC RTIambassador: getRangeBounds requires "
                     L"federation connection (M35+).");
}
void DLCRTIambassadorImpl::setRangeBounds(RegionHandle theRegion,
                                          DimensionHandle theDim,
                                          RangeBounds const& bounds) {
  // §10.30 — need region-registry state. Validate arguments per spec
  // before failing on connectivity.
  if (!theRegion.isValid()) throw InvalidRegion(L"setRangeBounds");
  if (!theDim.isValid()) throw InvalidDimensionHandle(L"setRangeBounds");
  if (bounds.getLowerBound() > bounds.getUpperBound())
    throw InvalidRangeBound(L"setRangeBounds: lower > upper.");
  throw NotConnected(L"DLC RTIambassador: setRangeBounds requires "
                     L"federation connection (M35+).");
}

// §10.31 normalizeFederateHandle — returns a stable per-federation numeric
// tag suitable as a random-partition seed. The gorti Handle PIMPL stores a
// uint64 internally; hash() exposes the low 32 bits as a long, which suffices
// as a normalization value.
unsigned long DLCRTIambassadorImpl::normalizeFederateHandle(
    FederateHandle theHandle) {
  if (!theHandle.isValid())
    throw InvalidFederateHandle(L"normalizeFederateHandle: invalid handle.");
  return static_cast<unsigned long>(theHandle.hash());
}

// §10.32 normalizeServiceGroup — enum → tag. Cast is safe: the enum
// values are 0..6 for the 7 service groups per IEEE 1516.1-2010 §10.32.
unsigned long DLCRTIambassadorImpl::normalizeServiceGroup(ServiceGroup group) {
  switch (group) {
    case FEDERATION_MANAGEMENT:        return 0;
    case DECLARATION_MANAGEMENT:       return 1;
    case OBJECT_MANAGEMENT:            return 2;
    case OWNERSHIP_MANAGEMENT:         return 3;
    case TIME_MANAGEMENT:              return 4;
    case DATA_DISTRIBUTION_MANAGEMENT: return 5;
    case SUPPORT_SERVICES:             return 6;
  }
  throw InvalidServiceGroup(L"normalizeServiceGroup: enum out of range.");
}

// §10.33-10.40 — 8 advisory-switch on/off pairs. No advisory infrastructure
// is wired in DLC M33 yet, so these silently succeed (spec-legal — the
// setter contract only requires void return). Once M35 lands a real
// dispatch layer, the switches will gate advisory-callback dispatch.
void DLCRTIambassadorImpl::enableObjectClassRelevanceAdvisorySwitch() {}
void DLCRTIambassadorImpl::disableObjectClassRelevanceAdvisorySwitch() {}
void DLCRTIambassadorImpl::enableAttributeRelevanceAdvisorySwitch() {}
void DLCRTIambassadorImpl::disableAttributeRelevanceAdvisorySwitch() {}
void DLCRTIambassadorImpl::enableAttributeScopeAdvisorySwitch() {}
void DLCRTIambassadorImpl::disableAttributeScopeAdvisorySwitch() {}
void DLCRTIambassadorImpl::enableInteractionRelevanceAdvisorySwitch() {}
void DLCRTIambassadorImpl::disableInteractionRelevanceAdvisorySwitch() {}

// §10.41 evokeCallback — single-arg spec form (catalogue 13.11).
// No event queue in DLC M33 yet; returns false to signal "no more callbacks
// pending". Federate can loop on this call safely.
bool DLCRTIambassadorImpl::evokeCallback(double) { return false; }

// §10.42 evokeMultipleCallbacks — 2 args, no defaults (catalogue 13.12).
// Same rationale as evokeCallback: no queued events, return false.
bool DLCRTIambassadorImpl::evokeMultipleCallbacks(double, double) {
  return false;
}

// §10.43-10.44 enable/disableCallbacks — no dispatch loop to gate yet;
// silently succeed.
void DLCRTIambassadorImpl::enableCallbacks() {}
void DLCRTIambassadorImpl::disableCallbacks() {}

// §10 (catalogue 13.14) getTimeFactory — returns the federation's logical-time
// factory. The factory-factory chain (LogicalTimeFactoryFactory → concrete
// HLAfloat64TimeFactory) requires a federation connection to know which
// implementation name was negotiated at join time. Until M35 lands that,
// we throw a spec-legal exception rather than return a null unique_ptr
// (which would trip caller-side UB on the first dereference).
rti1516e::auto_ptr<LogicalTimeFactory>
DLCRTIambassadorImpl::getTimeFactory() const {
  throw CouldNotCreateLogicalTimeFactory(
      L"DLC RTIambassador: getTimeFactory requires federation connection "
      L"(M35+); factory-factory chain deferred.");
}

}  // namespace rti1516e  (temporarily close for friend-shim definitions)

// The DEFINE_HANDLE_CLASS macro declares the VLD-ctor `protected` — we can't
// call it from outside the class hierarchy. Use per-handle Friend shims,
// declared by the macro as `friend class HandleKind##Friend;`.

#define DEFINE_HANDLE_FRIEND(HandleKind)                                \
  namespace rti1516e {                                                  \
  class HandleKind##Friend {                                            \
   public:                                                              \
    static HandleKind decode(VariableLengthData const& v) {             \
      return HandleKind(v);                                             \
    }                                                                   \
  };                                                                    \
  }

DEFINE_HANDLE_FRIEND(FederateHandle)
DEFINE_HANDLE_FRIEND(ObjectClassHandle)
DEFINE_HANDLE_FRIEND(InteractionClassHandle)
DEFINE_HANDLE_FRIEND(ObjectInstanceHandle)
DEFINE_HANDLE_FRIEND(AttributeHandle)
DEFINE_HANDLE_FRIEND(ParameterHandle)
DEFINE_HANDLE_FRIEND(DimensionHandle)
DEFINE_HANDLE_FRIEND(MessageRetractionHandle)
DEFINE_HANDLE_FRIEND(RegionHandle)

namespace rti1516e {

// §10 (catalogue 13.15) — 9 decode*Handle methods. All `const` per Agent F's
// M32 drift fix. Impl: each Friend shim invokes the handle's
// VariableLengthData ctor (declared `protected` by DEFINE_HANDLE_CLASS
// so we can't call it directly from a free function).
FederateHandle DLCRTIambassadorImpl::decodeFederateHandle(
    VariableLengthData const& v) const {
  return FederateHandleFriend::decode(v);
}
ObjectClassHandle DLCRTIambassadorImpl::decodeObjectClassHandle(
    VariableLengthData const& v) const {
  return ObjectClassHandleFriend::decode(v);
}
InteractionClassHandle DLCRTIambassadorImpl::decodeInteractionClassHandle(
    VariableLengthData const& v) const {
  return InteractionClassHandleFriend::decode(v);
}
ObjectInstanceHandle DLCRTIambassadorImpl::decodeObjectInstanceHandle(
    VariableLengthData const& v) const {
  return ObjectInstanceHandleFriend::decode(v);
}
AttributeHandle DLCRTIambassadorImpl::decodeAttributeHandle(
    VariableLengthData const& v) const {
  return AttributeHandleFriend::decode(v);
}
ParameterHandle DLCRTIambassadorImpl::decodeParameterHandle(
    VariableLengthData const& v) const {
  return ParameterHandleFriend::decode(v);
}
DimensionHandle DLCRTIambassadorImpl::decodeDimensionHandle(
    VariableLengthData const& v) const {
  return DimensionHandleFriend::decode(v);
}
MessageRetractionHandle DLCRTIambassadorImpl::decodeMessageRetractionHandle(
    VariableLengthData const& v) const {
  return MessageRetractionHandleFriend::decode(v);
}
RegionHandle DLCRTIambassadorImpl::decodeRegionHandle(
    VariableLengthData const& v) const {
  return RegionHandleFriend::decode(v);
}

}  // namespace rti1516e
