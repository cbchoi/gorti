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
void DLCRTIambassadorImpl::reserveObjectInstanceName(std::wstring const&) {
  m32_stub("reserveObjectInstanceName");
}
void DLCRTIambassadorImpl::releaseObjectInstanceName(std::wstring const&) {
  m32_stub("releaseObjectInstanceName");
}
void DLCRTIambassadorImpl::reserveMultipleObjectInstanceName(
    std::set<std::wstring> const&) {
  m32_stub("reserveMultipleObjectInstanceName");
}
void DLCRTIambassadorImpl::releaseMultipleObjectInstanceName(
    std::set<std::wstring> const&) {
  m32_stub("releaseMultipleObjectInstanceName");
}

ObjectInstanceHandle DLCRTIambassadorImpl::registerObjectInstance(
    ObjectClassHandle) {
  m32_stub("registerObjectInstance");
}
ObjectInstanceHandle DLCRTIambassadorImpl::registerObjectInstance(
    ObjectClassHandle, std::wstring const&) {
  m32_stub("registerObjectInstance");
}

void DLCRTIambassadorImpl::updateAttributeValues(
    ObjectInstanceHandle, AttributeHandleValueMap const&,
    VariableLengthData const&) {
  m32_stub("updateAttributeValues");
}
MessageRetractionHandle DLCRTIambassadorImpl::updateAttributeValues(
    ObjectInstanceHandle, AttributeHandleValueMap const&,
    VariableLengthData const&, LogicalTime const&) {
  m32_stub("updateAttributeValues");
}

void DLCRTIambassadorImpl::sendInteraction(InteractionClassHandle,
                                           ParameterHandleValueMap const&,
                                           VariableLengthData const&) {
  m32_stub("sendInteraction");
}
MessageRetractionHandle DLCRTIambassadorImpl::sendInteraction(
    InteractionClassHandle, ParameterHandleValueMap const&,
    VariableLengthData const&, LogicalTime const&) {
  m32_stub("sendInteraction");
}

void DLCRTIambassadorImpl::deleteObjectInstance(ObjectInstanceHandle,
                                                VariableLengthData const&) {
  m32_stub("deleteObjectInstance");
}
MessageRetractionHandle DLCRTIambassadorImpl::deleteObjectInstance(
    ObjectInstanceHandle, VariableLengthData const&, LogicalTime const&) {
  m32_stub("deleteObjectInstance");
}

void DLCRTIambassadorImpl::localDeleteObjectInstance(ObjectInstanceHandle) {
  m32_stub("localDeleteObjectInstance");
}
void DLCRTIambassadorImpl::requestAttributeValueUpdate(
    ObjectInstanceHandle, AttributeHandleSet const&,
    VariableLengthData const&) {
  m32_stub("requestAttributeValueUpdate");
}
void DLCRTIambassadorImpl::requestAttributeValueUpdate(
    ObjectClassHandle, AttributeHandleSet const&, VariableLengthData const&) {
  m32_stub("requestAttributeValueUpdate");
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
