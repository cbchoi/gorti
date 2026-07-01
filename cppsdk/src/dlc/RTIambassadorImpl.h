// IEEE 1516.1-2010 §10.6 / Annex A — DLC RTIambassador concrete impl.
//
// gorti M32. The class derives from the pure-abstract
// `rti1516e::RTIambassador` declared in <RTI/RTIambassador.h>. Every
// method currently throws RTIinternalError("M32 stub — impl in M33+")
// to satisfy the vtable so 27 conformance fixtures LINK.
//
// M33+ will progressively fill in real bodies (adapters over gorti's
// M17 gRPC surface with wstring↔string conversion).

#ifndef GORTI_DLC_RTI_AMBASSADOR_IMPL_H
#define GORTI_DLC_RTI_AMBASSADOR_IMPL_H

#include <RTI/RTIambassador.h>

#include <memory>

// M35 Agent BD — forward-decl of the callback bridge (full defn lives in
// dlc/FederateAmbassadorBridge.h, which pulls in the M17 shim). Keeping
// this a forward-decl lets DLCRTIambassadorImpl own the bridge via a
// unique_ptr member without dragging the M17 shim into every TU that
// includes this header.
namespace gorti {
namespace dlc {
class DLCFederateAmbassadorBridge;
}  // namespace dlc
}  // namespace gorti

namespace rti1516e {

// Forward-decl only — full defn in dlc/M17Bridge.h. Keeping this a
// forward-decl (not an #include of the M17 header) is what prevents the
// M17 `rti1516e::RTIambassador` class from colliding with the DLC
// spec-abstract `rti1516e::RTIambassador` in this same header.
class M17Bridge;

class DLCRTIambassadorImpl : public RTIambassador {
 public:
  DLCRTIambassadorImpl();
  ~DLCRTIambassadorImpl() override;

  // ===== §4 Federation Management =====
  void connect(FederateAmbassador& federateAmbassador,
               CallbackModel theCallbackModel,
               std::wstring const& localSettingsDesignator = L"") override;
  void disconnect() override;
  void createFederationExecution(
      std::wstring const& federationExecutionName,
      std::wstring const& fomModule,
      std::wstring const& logicalTimeImplementationName = L"") override;
  void createFederationExecution(
      std::wstring const& federationExecutionName,
      std::vector<std::wstring> const& fomModules,
      std::wstring const& logicalTimeImplementationName = L"") override;
  void createFederationExecutionWithMIM(
      std::wstring const& federationExecutionName,
      std::vector<std::wstring> const& fomModules,
      std::wstring const& mimModule,
      std::wstring const& logicalTimeImplementationName = L"") override;
  void destroyFederationExecution(
      std::wstring const& federationExecutionName) override;
  void listFederationExecutions() override;
  FederateHandle joinFederationExecution(
      std::wstring const& federateType,
      std::wstring const& federationExecutionName,
      std::vector<std::wstring> const& additionalFomModules =
          std::vector<std::wstring>()) override;
  FederateHandle joinFederationExecution(
      std::wstring const& federateName,
      std::wstring const& federateType,
      std::wstring const& federationExecutionName,
      std::vector<std::wstring> const& additionalFomModules =
          std::vector<std::wstring>()) override;
  void resignFederationExecution(ResignAction resignAction) override;
  void registerFederationSynchronizationPoint(
      std::wstring const& label,
      VariableLengthData const& theUserSuppliedTag) override;
  void registerFederationSynchronizationPoint(
      std::wstring const& label,
      VariableLengthData const& theUserSuppliedTag,
      FederateHandleSet const& syncSet) override;
  void synchronizationPointAchieved(std::wstring const& label,
                                    bool successfully = true) override;
  void requestFederationSave(std::wstring const& label) override;
  void requestFederationSave(std::wstring const& label,
                             LogicalTime const& theTime) override;
  void federateSaveBegun() override;
  void federateSaveComplete() override;
  void federateSaveNotComplete() override;
  void abortFederationSave() override;
  void queryFederationSaveStatus() override;
  void requestFederationRestore(std::wstring const& label) override;
  void federateRestoreComplete() override;
  void federateRestoreNotComplete() override;
  void abortFederationRestore() override;
  void queryFederationRestoreStatus() override;

  // ===== §5 Declaration Management =====
  void publishObjectClassAttributes(
      ObjectClassHandle theClass,
      AttributeHandleSet const& attributeList) override;
  void unpublishObjectClass(ObjectClassHandle theClass) override;
  void unpublishObjectClassAttributes(
      ObjectClassHandle theClass,
      AttributeHandleSet const& attributeList) override;
  void publishInteractionClass(InteractionClassHandle theInteraction) override;
  void unpublishInteractionClass(
      InteractionClassHandle theInteraction) override;
  void subscribeObjectClassAttributes(
      ObjectClassHandle theClass,
      AttributeHandleSet const& attributeList,
      bool active = true,
      std::wstring const& updateRateDesignator = L"") override;
  void unsubscribeObjectClass(ObjectClassHandle theClass) override;
  void unsubscribeObjectClassAttributes(
      ObjectClassHandle theClass,
      AttributeHandleSet const& attributeList) override;
  void subscribeInteractionClass(InteractionClassHandle theClass,
                                 bool active = true) override;
  void unsubscribeInteractionClass(InteractionClassHandle theClass) override;

  // ===== §6 Object Management =====
  void reserveObjectInstanceName(
      std::wstring const& theObjectInstanceName) override;
  void releaseObjectInstanceName(
      std::wstring const& theObjectInstanceName) override;
  void reserveMultipleObjectInstanceName(
      std::set<std::wstring> const& theObjectInstanceNames) override;
  void releaseMultipleObjectInstanceName(
      std::set<std::wstring> const& theObjectInstanceNames) override;

  ObjectInstanceHandle registerObjectInstance(
      ObjectClassHandle theClass) override;
  ObjectInstanceHandle registerObjectInstance(
      ObjectClassHandle theClass,
      std::wstring const& theObjectInstanceName) override;

  void updateAttributeValues(
      ObjectInstanceHandle theObject,
      AttributeHandleValueMap const& theAttributeValues,
      VariableLengthData const& theUserSuppliedTag) override;
  MessageRetractionHandle updateAttributeValues(
      ObjectInstanceHandle theObject,
      AttributeHandleValueMap const& theAttributeValues,
      VariableLengthData const& theUserSuppliedTag,
      LogicalTime const& theTime) override;

  void sendInteraction(
      InteractionClassHandle theInteraction,
      ParameterHandleValueMap const& theParameterValues,
      VariableLengthData const& theUserSuppliedTag) override;
  MessageRetractionHandle sendInteraction(
      InteractionClassHandle theInteraction,
      ParameterHandleValueMap const& theParameterValues,
      VariableLengthData const& theUserSuppliedTag,
      LogicalTime const& theTime) override;

  void deleteObjectInstance(ObjectInstanceHandle theObject,
                            VariableLengthData const& theUserSuppliedTag) override;
  MessageRetractionHandle deleteObjectInstance(
      ObjectInstanceHandle theObject,
      VariableLengthData const& theUserSuppliedTag,
      LogicalTime const& theTime) override;

  void localDeleteObjectInstance(ObjectInstanceHandle theObject) override;
  void requestAttributeValueUpdate(
      ObjectInstanceHandle theObject,
      AttributeHandleSet const& theAttributes,
      VariableLengthData const& theUserSuppliedTag) override;
  void requestAttributeValueUpdate(
      ObjectClassHandle theClass,
      AttributeHandleSet const& theAttributes,
      VariableLengthData const& theUserSuppliedTag) override;

  // ===== §7 Ownership Management =====
  void unconditionalAttributeOwnershipDivestiture(
      ObjectInstanceHandle theObject,
      AttributeHandleSet const& theAttributes) override;
  void negotiatedAttributeOwnershipDivestiture(
      ObjectInstanceHandle theObject,
      AttributeHandleSet const& theAttributes,
      VariableLengthData const& theUserSuppliedTag) override;
  void confirmDivestiture(ObjectInstanceHandle theObject,
                          AttributeHandleSet const& theAttributes,
                          VariableLengthData const& theUserSuppliedTag) override;
  void attributeOwnershipAcquisition(
      ObjectInstanceHandle theObject,
      AttributeHandleSet const& desiredAttributes,
      VariableLengthData const& theUserSuppliedTag) override;
  void attributeOwnershipAcquisitionIfAvailable(
      ObjectInstanceHandle theObject,
      AttributeHandleSet const& desiredAttributes) override;
  void attributeOwnershipReleaseDenied(
      ObjectInstanceHandle theObject,
      AttributeHandleSet const& theAttributes) override;
  void attributeOwnershipDivestitureIfWanted(
      ObjectInstanceHandle theObject,
      AttributeHandleSet const& theAttributes,
      AttributeHandleSet& theDivestedAttributes) override;
  void cancelNegotiatedAttributeOwnershipDivestiture(
      ObjectInstanceHandle theObject,
      AttributeHandleSet const& theAttributes) override;
  void cancelAttributeOwnershipAcquisition(
      ObjectInstanceHandle theObject,
      AttributeHandleSet const& theAttributes) override;
  void queryAttributeOwnership(ObjectInstanceHandle theObject,
                               AttributeHandle theAttribute) override;
  bool isAttributeOwnedByFederate(ObjectInstanceHandle theObject,
                                  AttributeHandle theAttribute) override;

  // ===== §8 Time Management =====
  void enableTimeRegulation(LogicalTimeInterval const& theLookahead) override;
  void disableTimeRegulation() override;
  void enableTimeConstrained() override;
  void disableTimeConstrained() override;
  void timeAdvanceRequest(LogicalTime const& theTime) override;
  void timeAdvanceRequestAvailable(LogicalTime const& theTime) override;
  void nextMessageRequest(LogicalTime const& theTime) override;
  void nextMessageRequestAvailable(LogicalTime const& theTime) override;
  void flushQueueRequest(LogicalTime const& theTime) override;
  void enableAsynchronousDelivery() override;
  void disableAsynchronousDelivery() override;
  bool queryGALT(LogicalTime& theTime) override;
  void queryLogicalTime(LogicalTime& theTime) override;
  bool queryLITS(LogicalTime& theTime) override;
  void modifyLookahead(LogicalTimeInterval const& theLookahead) override;
  void queryLookahead(LogicalTimeInterval& interval) override;
  void retract(MessageRetractionHandle theHandle) override;
  void changeAttributeOrderType(ObjectInstanceHandle theObject,
                                AttributeHandleSet const& theAttributes,
                                OrderType theType) override;
  void changeInteractionOrderType(InteractionClassHandle theClass,
                                  OrderType theType) override;

  // ===== §9 Data Distribution Management =====
  RegionHandle createRegion(DimensionHandleSet const& theDimensions) override;
  void commitRegionModifications(RegionHandleSet const& theRegions) override;
  void deleteRegion(RegionHandle const& theRegion) override;

  ObjectInstanceHandle registerObjectInstanceWithRegions(
      ObjectClassHandle theClass,
      AttributeHandleSetRegionHandleSetPairVector const& attributesAndRegions)
      override;
  ObjectInstanceHandle registerObjectInstanceWithRegions(
      ObjectClassHandle theClass,
      AttributeHandleSetRegionHandleSetPairVector const& attributesAndRegions,
      std::wstring const& theObjectInstanceName) override;

  void associateRegionsForUpdates(
      ObjectInstanceHandle theObject,
      AttributeHandleSetRegionHandleSetPairVector const&
          attributesAndRegions) override;
  void unassociateRegionsForUpdates(
      ObjectInstanceHandle theObject,
      AttributeHandleSetRegionHandleSetPairVector const&
          attributesAndRegions) override;

  void subscribeObjectClassAttributesWithRegions(
      ObjectClassHandle theClass,
      AttributeHandleSetRegionHandleSetPairVector const& attributesAndRegions,
      bool active = true,
      std::wstring const& updateRateDesignator = L"") override;
  void unsubscribeObjectClassAttributesWithRegions(
      ObjectClassHandle theClass,
      AttributeHandleSetRegionHandleSetPairVector const&
          attributesAndRegions) override;

  void subscribeInteractionClassWithRegions(
      InteractionClassHandle theClass,
      RegionHandleSet const& theRegions,
      bool active = true) override;
  void unsubscribeInteractionClassWithRegions(
      InteractionClassHandle theClass,
      RegionHandleSet const& theRegions) override;

  void sendInteractionWithRegions(
      InteractionClassHandle theInteraction,
      ParameterHandleValueMap const& theParameterValues,
      RegionHandleSet const& theRegions,
      VariableLengthData const& theUserSuppliedTag) override;
  MessageRetractionHandle sendInteractionWithRegions(
      InteractionClassHandle theInteraction,
      ParameterHandleValueMap const& theParameterValues,
      RegionHandleSet const& theRegions,
      VariableLengthData const& theUserSuppliedTag,
      LogicalTime const& theTime) override;
  void requestAttributeValueUpdateWithRegions(
      ObjectClassHandle theClass,
      AttributeHandleSetRegionHandleSetPairVector const& attributesAndRegions,
      VariableLengthData const& theUserSuppliedTag) override;

  // ===== §10 Support Services =====
  ResignAction getAutomaticResignDirective() override;
  void setAutomaticResignDirective(ResignAction resignAction) override;
  FederateHandle getFederateHandle(std::wstring const& theName) override;
  std::wstring getFederateName(FederateHandle theHandle) override;
  ObjectClassHandle getObjectClassHandle(std::wstring const& theName) override;
  std::wstring getObjectClassName(ObjectClassHandle theHandle) override;
  ObjectClassHandle getKnownObjectClassHandle(
      ObjectInstanceHandle theObject) override;
  ObjectInstanceHandle getObjectInstanceHandle(
      std::wstring const& theName) override;
  std::wstring getObjectInstanceName(ObjectInstanceHandle theHandle) override;
  AttributeHandle getAttributeHandle(ObjectClassHandle theClass,
                                     std::wstring const& theName) override;
  std::wstring getAttributeName(ObjectClassHandle theClass,
                                AttributeHandle theHandle) override;
  double getUpdateRateValue(
      std::wstring const& updateRateDesignator) override;
  double getUpdateRateValueForAttribute(ObjectInstanceHandle theObject,
                                        AttributeHandle theAttribute) override;
  InteractionClassHandle getInteractionClassHandle(
      std::wstring const& theName) override;
  std::wstring getInteractionClassName(
      InteractionClassHandle theHandle) override;
  ParameterHandle getParameterHandle(InteractionClassHandle theClass,
                                     std::wstring const& theName) override;
  std::wstring getParameterName(InteractionClassHandle theClass,
                                ParameterHandle theHandle) override;
  OrderType getOrderType(std::wstring const& orderName) override;
  std::wstring getOrderName(OrderType theType) override;
  TransportationType getTransportationType(
      std::wstring const& transportationName) override;
  std::wstring getTransportationName(TransportationType theType) override;

  DimensionHandleSet getAvailableDimensionsForClassAttribute(
      ObjectClassHandle theClass, AttributeHandle theHandle) override;
  DimensionHandleSet getAvailableDimensionsForInteractionClass(
      InteractionClassHandle theClass) override;
  DimensionHandle getDimensionHandle(std::wstring const& theName) override;
  std::wstring getDimensionName(DimensionHandle theHandle) override;
  unsigned long getDimensionUpperBound(DimensionHandle theHandle) override;
  DimensionHandleSet getDimensionHandleSet(RegionHandle theRegion) override;
  RangeBounds getRangeBounds(RegionHandle theRegion,
                             DimensionHandle theDimension) override;
  void setRangeBounds(RegionHandle theRegion,
                      DimensionHandle theDimension,
                      RangeBounds const& bounds) override;

  unsigned long normalizeFederateHandle(FederateHandle theHandle) override;
  unsigned long normalizeServiceGroup(ServiceGroup theGroup) override;

  void enableObjectClassRelevanceAdvisorySwitch() override;
  void disableObjectClassRelevanceAdvisorySwitch() override;
  void enableAttributeRelevanceAdvisorySwitch() override;
  void disableAttributeRelevanceAdvisorySwitch() override;
  void enableAttributeScopeAdvisorySwitch() override;
  void disableAttributeScopeAdvisorySwitch() override;
  void enableInteractionRelevanceAdvisorySwitch() override;
  void disableInteractionRelevanceAdvisorySwitch() override;

  bool evokeCallback(double approximateMinimumTimeInSeconds) override;
  bool evokeMultipleCallbacks(double approximateMinimumTimeInSeconds,
                              double approximateMaximumTimeInSeconds) override;
  void enableCallbacks() override;
  void disableCallbacks() override;

  // §10 — Per Pitch RTIambassador.h:1769 the accessor is `const`.
  rti1516e::auto_ptr<LogicalTimeFactory> getTimeFactory() const override;

  // §10 — 9 decode*Handle methods. All `const` per Pitch RTIambassador.h:1776-1846.
  FederateHandle decodeFederateHandle(
      VariableLengthData const& encodedValue) const override;
  ObjectClassHandle decodeObjectClassHandle(
      VariableLengthData const& encodedValue) const override;
  InteractionClassHandle decodeInteractionClassHandle(
      VariableLengthData const& encodedValue) const override;
  ObjectInstanceHandle decodeObjectInstanceHandle(
      VariableLengthData const& encodedValue) const override;
  AttributeHandle decodeAttributeHandle(
      VariableLengthData const& encodedValue) const override;
  ParameterHandle decodeParameterHandle(
      VariableLengthData const& encodedValue) const override;
  DimensionHandle decodeDimensionHandle(
      VariableLengthData const& encodedValue) const override;
  MessageRetractionHandle decodeMessageRetractionHandle(
      VariableLengthData const& encodedValue) const override;
  RegionHandle decodeRegionHandle(
      VariableLengthData const& encodedValue) const override;

 private:
  // ===== M34 pImpl for M17 delegation =====
  //
  // Owns the M17 concrete rti1516e::RTIambassador via an opaque bridge so
  // the M17 header stays out of TUs that also see <RTI/RTIambassador.h>.
  // Constructed unconditionally in the ctor; connect()/disconnect() drive
  // the M17 wire lifecycle.
  std::unique_ptr<M17Bridge> m17_;

  // Bound DLC-side FederateAmbassador reference from §4.2 connect(). Agent
  // AD's callback dispatch bridge reads this to route M17 callbacks through
  // the DLC callback surface. Null before connect() and after disconnect().
  FederateAmbassador* fed_amb_{nullptr};

  // Selected CallbackModel from §4.2 connect(). Recorded so AD's dispatch
  // bridge and future §10 evokeCallback/evokeMultipleCallbacks impls know
  // whether the federate opted into HLA_IMMEDIATE (dispatch on stream
  // thread) or HLA_EVOKED (dispatch only on evoke). Defaults to HLA_EVOKED
  // to match the Pitch pre-connect state (safe: no callbacks fire).
  CallbackModel callback_model_{HLA_EVOKED};

  // M35 Agent BD — DLC-side callback dispatch bridge. Owns an
  // rti1516e_m17::FederateAmbassador subclass (Agent AD's bridge) that
  // converts M17 callback deliveries into DLC callback invocations on
  // `fed_amb_`. Constructed by connect() and installed as the M17 callback
  // sink via m17_->bind_federate_ambassador. Destroyed by disconnect() so
  // late M17 deliveries have nowhere to land (M17 unbind happens first).
  std::unique_ptr<gorti::dlc::DLCFederateAmbassadorBridge> callback_bridge_;
};

}  // namespace rti1516e

#endif  // GORTI_DLC_RTI_AMBASSADOR_IMPL_H
