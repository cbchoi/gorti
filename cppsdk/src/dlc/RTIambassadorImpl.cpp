// IEEE 1516.1-2010 §10.6 / Annex A — DLC RTIambassador stub impl.
//
// gorti M32. Every method throws RTIinternalError("M32 stub — impl in M33+").
// M32 GREEN target is LINK-only; runtime bodies land M33+ as wstring-adapters
// over gorti's M17 gRPC surface.

#include "RTIambassadorImpl.h"
#include <RTI/Exception.h>
#include <RTI/RangeBounds.h>
#include <RTI/RTIambassadorFactory.h>

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
void DLCRTIambassadorImpl::publishObjectClassAttributes(
    ObjectClassHandle, AttributeHandleSet const&) {
  m32_stub("publishObjectClassAttributes");
}
void DLCRTIambassadorImpl::unpublishObjectClass(ObjectClassHandle) {
  m32_stub("unpublishObjectClass");
}
void DLCRTIambassadorImpl::unpublishObjectClassAttributes(
    ObjectClassHandle, AttributeHandleSet const&) {
  m32_stub("unpublishObjectClassAttributes");
}
void DLCRTIambassadorImpl::publishInteractionClass(InteractionClassHandle) {
  m32_stub("publishInteractionClass");
}
void DLCRTIambassadorImpl::unpublishInteractionClass(InteractionClassHandle) {
  m32_stub("unpublishInteractionClass");
}
void DLCRTIambassadorImpl::subscribeObjectClassAttributes(
    ObjectClassHandle, AttributeHandleSet const&, bool,
    std::wstring const&) {
  m32_stub("subscribeObjectClassAttributes");
}
void DLCRTIambassadorImpl::unsubscribeObjectClass(ObjectClassHandle) {
  m32_stub("unsubscribeObjectClass");
}
void DLCRTIambassadorImpl::unsubscribeObjectClassAttributes(
    ObjectClassHandle, AttributeHandleSet const&) {
  m32_stub("unsubscribeObjectClassAttributes");
}
void DLCRTIambassadorImpl::subscribeInteractionClass(InteractionClassHandle,
                                                     bool) {
  m32_stub("subscribeInteractionClass");
}
void DLCRTIambassadorImpl::unsubscribeInteractionClass(InteractionClassHandle) {
  m32_stub("unsubscribeInteractionClass");
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
void DLCRTIambassadorImpl::unconditionalAttributeOwnershipDivestiture(
    ObjectInstanceHandle, AttributeHandleSet const&) {
  m32_stub("unconditionalAttributeOwnershipDivestiture");
}
void DLCRTIambassadorImpl::negotiatedAttributeOwnershipDivestiture(
    ObjectInstanceHandle, AttributeHandleSet const&,
    VariableLengthData const&) {
  m32_stub("negotiatedAttributeOwnershipDivestiture");
}
void DLCRTIambassadorImpl::confirmDivestiture(ObjectInstanceHandle,
                                              AttributeHandleSet const&,
                                              VariableLengthData const&) {
  m32_stub("confirmDivestiture");
}
void DLCRTIambassadorImpl::attributeOwnershipAcquisition(
    ObjectInstanceHandle, AttributeHandleSet const&,
    VariableLengthData const&) {
  m32_stub("attributeOwnershipAcquisition");
}
void DLCRTIambassadorImpl::attributeOwnershipAcquisitionIfAvailable(
    ObjectInstanceHandle, AttributeHandleSet const&) {
  m32_stub("attributeOwnershipAcquisitionIfAvailable");
}
void DLCRTIambassadorImpl::attributeOwnershipReleaseDenied(
    ObjectInstanceHandle, AttributeHandleSet const&) {
  m32_stub("attributeOwnershipReleaseDenied");
}
void DLCRTIambassadorImpl::attributeOwnershipDivestitureIfWanted(
    ObjectInstanceHandle, AttributeHandleSet const&, AttributeHandleSet&) {
  m32_stub("attributeOwnershipDivestitureIfWanted");
}
void DLCRTIambassadorImpl::cancelNegotiatedAttributeOwnershipDivestiture(
    ObjectInstanceHandle, AttributeHandleSet const&) {
  m32_stub("cancelNegotiatedAttributeOwnershipDivestiture");
}
void DLCRTIambassadorImpl::cancelAttributeOwnershipAcquisition(
    ObjectInstanceHandle, AttributeHandleSet const&) {
  m32_stub("cancelAttributeOwnershipAcquisition");
}
void DLCRTIambassadorImpl::queryAttributeOwnership(ObjectInstanceHandle,
                                                   AttributeHandle) {
  m32_stub("queryAttributeOwnership");
}
bool DLCRTIambassadorImpl::isAttributeOwnedByFederate(ObjectInstanceHandle,
                                                      AttributeHandle) {
  m32_stub("isAttributeOwnedByFederate");
}

// ===== §8 Time Management =====
void DLCRTIambassadorImpl::enableTimeRegulation(LogicalTimeInterval const&) {
  m32_stub("enableTimeRegulation");
}
void DLCRTIambassadorImpl::disableTimeRegulation() {
  m32_stub("disableTimeRegulation");
}
void DLCRTIambassadorImpl::enableTimeConstrained() {
  m32_stub("enableTimeConstrained");
}
void DLCRTIambassadorImpl::disableTimeConstrained() {
  m32_stub("disableTimeConstrained");
}
void DLCRTIambassadorImpl::timeAdvanceRequest(LogicalTime const&) {
  m32_stub("timeAdvanceRequest");
}
void DLCRTIambassadorImpl::timeAdvanceRequestAvailable(LogicalTime const&) {
  m32_stub("timeAdvanceRequestAvailable");
}
void DLCRTIambassadorImpl::nextMessageRequest(LogicalTime const&) {
  m32_stub("nextMessageRequest");
}
void DLCRTIambassadorImpl::nextMessageRequestAvailable(LogicalTime const&) {
  m32_stub("nextMessageRequestAvailable");
}
void DLCRTIambassadorImpl::flushQueueRequest(LogicalTime const&) {
  m32_stub("flushQueueRequest");
}
void DLCRTIambassadorImpl::enableAsynchronousDelivery() {
  m32_stub("enableAsynchronousDelivery");
}
void DLCRTIambassadorImpl::disableAsynchronousDelivery() {
  m32_stub("disableAsynchronousDelivery");
}
bool DLCRTIambassadorImpl::queryGALT(LogicalTime&) { m32_stub("queryGALT"); }
void DLCRTIambassadorImpl::queryLogicalTime(LogicalTime&) {
  m32_stub("queryLogicalTime");
}
bool DLCRTIambassadorImpl::queryLITS(LogicalTime&) { m32_stub("queryLITS"); }
void DLCRTIambassadorImpl::modifyLookahead(LogicalTimeInterval const&) {
  m32_stub("modifyLookahead");
}
void DLCRTIambassadorImpl::queryLookahead(LogicalTimeInterval&) {
  m32_stub("queryLookahead");
}
void DLCRTIambassadorImpl::retract(MessageRetractionHandle) {
  m32_stub("retract");
}
void DLCRTIambassadorImpl::changeAttributeOrderType(ObjectInstanceHandle,
                                                    AttributeHandleSet const&,
                                                    OrderType) {
  m32_stub("changeAttributeOrderType");
}
void DLCRTIambassadorImpl::changeInteractionOrderType(InteractionClassHandle,
                                                      OrderType) {
  m32_stub("changeInteractionOrderType");
}

// ===== §9 DDM =====
RegionHandle DLCRTIambassadorImpl::createRegion(DimensionHandleSet const&) {
  m32_stub("createRegion");
}
void DLCRTIambassadorImpl::commitRegionModifications(RegionHandleSet const&) {
  m32_stub("commitRegionModifications");
}
void DLCRTIambassadorImpl::deleteRegion(RegionHandle) {
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
ResignAction DLCRTIambassadorImpl::getAutomaticResignDirective() {
  m32_stub("getAutomaticResignDirective");
}
void DLCRTIambassadorImpl::setAutomaticResignDirective(ResignAction) {
  m32_stub("setAutomaticResignDirective");
}
FederateHandle DLCRTIambassadorImpl::getFederateHandle(std::wstring const&) {
  m32_stub("getFederateHandle");
}
std::wstring DLCRTIambassadorImpl::getFederateName(FederateHandle) {
  m32_stub("getFederateName");
}
ObjectClassHandle DLCRTIambassadorImpl::getObjectClassHandle(
    std::wstring const&) {
  m32_stub("getObjectClassHandle");
}
std::wstring DLCRTIambassadorImpl::getObjectClassName(ObjectClassHandle) {
  m32_stub("getObjectClassName");
}
ObjectClassHandle DLCRTIambassadorImpl::getKnownObjectClassHandle(
    ObjectInstanceHandle) {
  m32_stub("getKnownObjectClassHandle");
}
ObjectInstanceHandle DLCRTIambassadorImpl::getObjectInstanceHandle(
    std::wstring const&) {
  m32_stub("getObjectInstanceHandle");
}
std::wstring DLCRTIambassadorImpl::getObjectInstanceName(
    ObjectInstanceHandle) {
  m32_stub("getObjectInstanceName");
}
AttributeHandle DLCRTIambassadorImpl::getAttributeHandle(ObjectClassHandle,
                                                         std::wstring const&) {
  m32_stub("getAttributeHandle");
}
std::wstring DLCRTIambassadorImpl::getAttributeName(ObjectClassHandle,
                                                    AttributeHandle) {
  m32_stub("getAttributeName");
}
double DLCRTIambassadorImpl::getUpdateRateValue(std::wstring const&) {
  m32_stub("getUpdateRateValue");
}
double DLCRTIambassadorImpl::getUpdateRateValueForAttribute(
    ObjectInstanceHandle, AttributeHandle) {
  m32_stub("getUpdateRateValueForAttribute");
}
InteractionClassHandle DLCRTIambassadorImpl::getInteractionClassHandle(
    std::wstring const&) {
  m32_stub("getInteractionClassHandle");
}
std::wstring DLCRTIambassadorImpl::getInteractionClassName(
    InteractionClassHandle) {
  m32_stub("getInteractionClassName");
}
ParameterHandle DLCRTIambassadorImpl::getParameterHandle(
    InteractionClassHandle, std::wstring const&) {
  m32_stub("getParameterHandle");
}
std::wstring DLCRTIambassadorImpl::getParameterName(InteractionClassHandle,
                                                    ParameterHandle) {
  m32_stub("getParameterName");
}
OrderType DLCRTIambassadorImpl::getOrderType(std::wstring const&) {
  m32_stub("getOrderType");
}
std::wstring DLCRTIambassadorImpl::getOrderName(OrderType) {
  m32_stub("getOrderName");
}
TransportationType DLCRTIambassadorImpl::getTransportationType(
    std::wstring const&) {
  m32_stub("getTransportationType");
}
std::wstring DLCRTIambassadorImpl::getTransportationName(TransportationType) {
  m32_stub("getTransportationName");
}
DimensionHandleSet DLCRTIambassadorImpl::getAvailableDimensionsForClassAttribute(
    ObjectClassHandle, AttributeHandle) {
  m32_stub("getAvailableDimensionsForClassAttribute");
}
DimensionHandleSet
DLCRTIambassadorImpl::getAvailableDimensionsForInteractionClass(
    InteractionClassHandle) {
  m32_stub("getAvailableDimensionsForInteractionClass");
}
DimensionHandle DLCRTIambassadorImpl::getDimensionHandle(std::wstring const&) {
  m32_stub("getDimensionHandle");
}
std::wstring DLCRTIambassadorImpl::getDimensionName(DimensionHandle) {
  m32_stub("getDimensionName");
}
unsigned long DLCRTIambassadorImpl::getDimensionUpperBound(DimensionHandle) {
  m32_stub("getDimensionUpperBound");
}
DimensionHandleSet DLCRTIambassadorImpl::getDimensionHandleSet(RegionHandle) {
  m32_stub("getDimensionHandleSet");
}
RangeBounds DLCRTIambassadorImpl::getRangeBounds(RegionHandle,
                                                 DimensionHandle) {
  m32_stub("getRangeBounds");
}
void DLCRTIambassadorImpl::setRangeBounds(RegionHandle, DimensionHandle,
                                          RangeBounds const&) {
  m32_stub("setRangeBounds");
}
unsigned long DLCRTIambassadorImpl::normalizeFederateHandle(FederateHandle) {
  m32_stub("normalizeFederateHandle");
}
unsigned long DLCRTIambassadorImpl::normalizeServiceGroup(ServiceGroup) {
  m32_stub("normalizeServiceGroup");
}
void DLCRTIambassadorImpl::enableObjectClassRelevanceAdvisorySwitch() {
  m32_stub("enableObjectClassRelevanceAdvisorySwitch");
}
void DLCRTIambassadorImpl::disableObjectClassRelevanceAdvisorySwitch() {
  m32_stub("disableObjectClassRelevanceAdvisorySwitch");
}
void DLCRTIambassadorImpl::enableAttributeRelevanceAdvisorySwitch() {
  m32_stub("enableAttributeRelevanceAdvisorySwitch");
}
void DLCRTIambassadorImpl::disableAttributeRelevanceAdvisorySwitch() {
  m32_stub("disableAttributeRelevanceAdvisorySwitch");
}
void DLCRTIambassadorImpl::enableAttributeScopeAdvisorySwitch() {
  m32_stub("enableAttributeScopeAdvisorySwitch");
}
void DLCRTIambassadorImpl::disableAttributeScopeAdvisorySwitch() {
  m32_stub("disableAttributeScopeAdvisorySwitch");
}
void DLCRTIambassadorImpl::enableInteractionRelevanceAdvisorySwitch() {
  m32_stub("enableInteractionRelevanceAdvisorySwitch");
}
void DLCRTIambassadorImpl::disableInteractionRelevanceAdvisorySwitch() {
  m32_stub("disableInteractionRelevanceAdvisorySwitch");
}
bool DLCRTIambassadorImpl::evokeCallback(double) {
  m32_stub("evokeCallback");
}
bool DLCRTIambassadorImpl::evokeMultipleCallbacks(double, double) {
  m32_stub("evokeMultipleCallbacks");
}
void DLCRTIambassadorImpl::enableCallbacks() { m32_stub("enableCallbacks"); }
void DLCRTIambassadorImpl::disableCallbacks() { m32_stub("disableCallbacks"); }
rti1516e::auto_ptr<LogicalTimeFactory> DLCRTIambassadorImpl::getTimeFactory() {
  m32_stub("getTimeFactory");
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

FederateHandle DLCRTIambassadorImpl::decodeFederateHandle(
    VariableLengthData const& v) {
  return FederateHandleFriend::decode(v);
}
ObjectClassHandle DLCRTIambassadorImpl::decodeObjectClassHandle(
    VariableLengthData const& v) {
  return ObjectClassHandleFriend::decode(v);
}
InteractionClassHandle DLCRTIambassadorImpl::decodeInteractionClassHandle(
    VariableLengthData const& v) {
  return InteractionClassHandleFriend::decode(v);
}
ObjectInstanceHandle DLCRTIambassadorImpl::decodeObjectInstanceHandle(
    VariableLengthData const& v) {
  return ObjectInstanceHandleFriend::decode(v);
}
AttributeHandle DLCRTIambassadorImpl::decodeAttributeHandle(
    VariableLengthData const& v) {
  return AttributeHandleFriend::decode(v);
}
ParameterHandle DLCRTIambassadorImpl::decodeParameterHandle(
    VariableLengthData const& v) {
  return ParameterHandleFriend::decode(v);
}
DimensionHandle DLCRTIambassadorImpl::decodeDimensionHandle(
    VariableLengthData const& v) {
  return DimensionHandleFriend::decode(v);
}
MessageRetractionHandle DLCRTIambassadorImpl::decodeMessageRetractionHandle(
    VariableLengthData const& v) {
  return MessageRetractionHandleFriend::decode(v);
}
RegionHandle DLCRTIambassadorImpl::decodeRegionHandle(
    VariableLengthData const& v) {
  return RegionHandleFriend::decode(v);
}

}  // namespace rti1516e
