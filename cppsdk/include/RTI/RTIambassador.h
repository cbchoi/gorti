// IEEE 1516.1-2010 §10.6 / Annex A — RTI/RTIambassador.h
// gorti M31 forward-declaration stub. Spec text reprinted with permission
// from IEEE 1516.1(TM)-2010.
//
// Pure-abstract federate-facing ambassador. M31 declares the methods the
// lockfile tests assert on; bodies land M32-M35.
//
// Layout follows the spec service-group ordering:
//   §4   Federation Management
//   §5   Declaration Management
//   §6   Object Management
//   §7   Ownership Management
//   §8   Time Management
//   §9   Data Distribution Management
//   §10  Support Services (handle lookups + evoke)
//
// Catalogue-mapped rows: 2.1-2.5 (construction), 3.* (lifecycle),
// 9.* (time), 10.* (DDM), 11.* (object mgmt), 12.* (ownership),
// 13.* (support services).

#ifndef RTI_RTIambassador_h
#define RTI_RTIambassador_h

namespace rti1516e {
class FederateAmbassador;
class LogicalTime;
class LogicalTimeFactory;
class LogicalTimeInterval;
class RangeBounds;
}  // namespace rti1516e

#include <RTI/SpecificConfig.h>
#include <RTI/Typedefs.h>
#include <RTI/Exception.h>
#include <string>
#include <vector>
#include <set>
#include <memory>

namespace rti1516e {

class RTI_EXPORT RTIambassador {
 protected:
  RTIambassador() RTI_NOEXCEPT;

 public:
  virtual ~RTIambassador();

  // ===== §4 Federation Management =====

  // §4.2 connect — central FR-DLC-3 lockfile target.
  virtual void connect(FederateAmbassador& federateAmbassador,
                       CallbackModel theCallbackModel,
                       std::wstring const& localSettingsDesignator = L"") = 0;

  // §4.3 disconnect.
  virtual void disconnect() = 0;

  // §4.5 createFederationExecution — 3 overloads.
  virtual void createFederationExecution(
      std::wstring const& federationExecutionName,
      std::wstring const& fomModule,
      std::wstring const& logicalTimeImplementationName = L"") = 0;

  virtual void createFederationExecution(
      std::wstring const& federationExecutionName,
      std::vector<std::wstring> const& fomModules,
      std::wstring const& logicalTimeImplementationName = L"") = 0;

  virtual void createFederationExecutionWithMIM(
      std::wstring const& federationExecutionName,
      std::vector<std::wstring> const& fomModules,
      std::wstring const& mimModule,
      std::wstring const& logicalTimeImplementationName = L"") = 0;

  // §4.6 destroyFederationExecution.
  virtual void destroyFederationExecution(
      std::wstring const& federationExecutionName) = 0;

  // §4.7 listFederationExecutions — result via callback.
  virtual void listFederationExecutions() = 0;

  // §4.9 joinFederationExecution — 2 overloads.
  virtual FederateHandle joinFederationExecution(
      std::wstring const& federateType,
      std::wstring const& federationExecutionName,
      std::vector<std::wstring> const& additionalFomModules =
          std::vector<std::wstring>()) = 0;

  virtual FederateHandle joinFederationExecution(
      std::wstring const& federateName,
      std::wstring const& federateType,
      std::wstring const& federationExecutionName,
      std::vector<std::wstring> const& additionalFomModules =
          std::vector<std::wstring>()) = 0;

  // §4.10 resignFederationExecution — mandatory ResignAction (FR-DLC-10).
  virtual void resignFederationExecution(ResignAction resignAction) = 0;

  // §4.11-15 sync points.
  virtual void registerFederationSynchronizationPoint(
      std::wstring const& label, VariableLengthData const& theUserSuppliedTag) = 0;

  virtual void registerFederationSynchronizationPoint(
      std::wstring const& label,
      VariableLengthData const& theUserSuppliedTag,
      FederateHandleSet const& syncSet) = 0;

  virtual void synchronizationPointAchieved(std::wstring const& label,
                                            bool successfully = true) = 0;

  // §4.16-22 save.
  virtual void requestFederationSave(std::wstring const& label) = 0;
  virtual void requestFederationSave(std::wstring const& label,
                                     LogicalTime const& theTime) = 0;
  virtual void federateSaveBegun() = 0;
  virtual void federateSaveComplete() = 0;
  virtual void federateSaveNotComplete() = 0;
  virtual void abortFederationSave() = 0;
  virtual void queryFederationSaveStatus() = 0;

  // §4.24-31 restore.
  virtual void requestFederationRestore(std::wstring const& label) = 0;
  virtual void federateRestoreComplete() = 0;
  virtual void federateRestoreNotComplete() = 0;
  virtual void abortFederationRestore() = 0;
  virtual void queryFederationRestoreStatus() = 0;

  // ===== §5 Declaration Management =====

  virtual void publishObjectClassAttributes(
      ObjectClassHandle theClass, AttributeHandleSet const& attributeList) = 0;
  virtual void unpublishObjectClass(ObjectClassHandle theClass) = 0;
  virtual void unpublishObjectClassAttributes(
      ObjectClassHandle theClass, AttributeHandleSet const& attributeList) = 0;
  virtual void publishInteractionClass(InteractionClassHandle theInteraction) = 0;
  virtual void unpublishInteractionClass(
      InteractionClassHandle theInteraction) = 0;

  virtual void subscribeObjectClassAttributes(
      ObjectClassHandle theClass,
      AttributeHandleSet const& attributeList,
      bool active = true,
      std::wstring const& updateRateDesignator = L"") = 0;
  virtual void unsubscribeObjectClass(ObjectClassHandle theClass) = 0;
  virtual void unsubscribeObjectClassAttributes(
      ObjectClassHandle theClass, AttributeHandleSet const& attributeList) = 0;
  virtual void subscribeInteractionClass(InteractionClassHandle theClass,
                                         bool active = true) = 0;
  virtual void unsubscribeInteractionClass(InteractionClassHandle theClass) = 0;

  // ===== §6 Object Management =====

  virtual void reserveObjectInstanceName(std::wstring const& theObjectInstanceName) = 0;
  virtual void releaseObjectInstanceName(std::wstring const& theObjectInstanceName) = 0;
  virtual void reserveMultipleObjectInstanceName(
      std::set<std::wstring> const& theObjectInstanceNames) = 0;
  virtual void releaseMultipleObjectInstanceName(
      std::set<std::wstring> const& theObjectInstanceNames) = 0;

  virtual ObjectInstanceHandle registerObjectInstance(
      ObjectClassHandle theClass) = 0;
  virtual ObjectInstanceHandle registerObjectInstance(
      ObjectClassHandle theClass, std::wstring const& theObjectInstanceName) = 0;

  virtual void updateAttributeValues(
      ObjectInstanceHandle theObject,
      AttributeHandleValueMap const& theAttributeValues,
      VariableLengthData const& theUserSuppliedTag) = 0;

  virtual MessageRetractionHandle updateAttributeValues(
      ObjectInstanceHandle theObject,
      AttributeHandleValueMap const& theAttributeValues,
      VariableLengthData const& theUserSuppliedTag,
      LogicalTime const& theTime) = 0;

  virtual void sendInteraction(
      InteractionClassHandle theInteraction,
      ParameterHandleValueMap const& theParameterValues,
      VariableLengthData const& theUserSuppliedTag) = 0;

  virtual MessageRetractionHandle sendInteraction(
      InteractionClassHandle theInteraction,
      ParameterHandleValueMap const& theParameterValues,
      VariableLengthData const& theUserSuppliedTag,
      LogicalTime const& theTime) = 0;

  virtual void deleteObjectInstance(
      ObjectInstanceHandle theObject,
      VariableLengthData const& theUserSuppliedTag) = 0;

  virtual MessageRetractionHandle deleteObjectInstance(
      ObjectInstanceHandle theObject,
      VariableLengthData const& theUserSuppliedTag,
      LogicalTime const& theTime) = 0;

  virtual void localDeleteObjectInstance(ObjectInstanceHandle theObject) = 0;

  virtual void requestAttributeValueUpdate(
      ObjectInstanceHandle theObject,
      AttributeHandleSet const& theAttributes,
      VariableLengthData const& theUserSuppliedTag) = 0;
  virtual void requestAttributeValueUpdate(
      ObjectClassHandle theClass,
      AttributeHandleSet const& theAttributes,
      VariableLengthData const& theUserSuppliedTag) = 0;

  // ===== §7 Ownership Management =====

  virtual void unconditionalAttributeOwnershipDivestiture(
      ObjectInstanceHandle theObject,
      AttributeHandleSet const& theAttributes) = 0;
  virtual void negotiatedAttributeOwnershipDivestiture(
      ObjectInstanceHandle theObject,
      AttributeHandleSet const& theAttributes,
      VariableLengthData const& theUserSuppliedTag) = 0;
  virtual void confirmDivestiture(
      ObjectInstanceHandle theObject,
      AttributeHandleSet const& theAttributes,
      VariableLengthData const& theUserSuppliedTag) = 0;
  virtual void attributeOwnershipAcquisition(
      ObjectInstanceHandle theObject,
      AttributeHandleSet const& desiredAttributes,
      VariableLengthData const& theUserSuppliedTag) = 0;
  virtual void attributeOwnershipAcquisitionIfAvailable(
      ObjectInstanceHandle theObject,
      AttributeHandleSet const& desiredAttributes) = 0;
  virtual void attributeOwnershipReleaseDenied(
      ObjectInstanceHandle theObject,
      AttributeHandleSet const& theAttributes) = 0;
  virtual void attributeOwnershipDivestitureIfWanted(
      ObjectInstanceHandle theObject,
      AttributeHandleSet const& theAttributes,
      AttributeHandleSet& theDivestedAttributes) = 0;
  virtual void cancelNegotiatedAttributeOwnershipDivestiture(
      ObjectInstanceHandle theObject,
      AttributeHandleSet const& theAttributes) = 0;
  virtual void cancelAttributeOwnershipAcquisition(
      ObjectInstanceHandle theObject,
      AttributeHandleSet const& theAttributes) = 0;
  virtual void queryAttributeOwnership(ObjectInstanceHandle theObject,
                                       AttributeHandle theAttribute) = 0;
  virtual bool isAttributeOwnedByFederate(ObjectInstanceHandle theObject,
                                          AttributeHandle theAttribute) = 0;

  // ===== §8 Time Management =====

  virtual void enableTimeRegulation(LogicalTimeInterval const& theLookahead) = 0;
  virtual void disableTimeRegulation() = 0;
  virtual void enableTimeConstrained() = 0;
  virtual void disableTimeConstrained() = 0;

  virtual void timeAdvanceRequest(LogicalTime const& theTime) = 0;
  virtual void timeAdvanceRequestAvailable(LogicalTime const& theTime) = 0;
  virtual void nextMessageRequest(LogicalTime const& theTime) = 0;
  virtual void nextMessageRequestAvailable(LogicalTime const& theTime) = 0;
  virtual void flushQueueRequest(LogicalTime const& theTime) = 0;

  virtual void enableAsynchronousDelivery() = 0;
  virtual void disableAsynchronousDelivery() = 0;

  virtual bool queryGALT(LogicalTime& theTime) = 0;
  virtual void queryLogicalTime(LogicalTime& theTime) = 0;
  virtual bool queryLITS(LogicalTime& theTime) = 0;
  virtual void modifyLookahead(LogicalTimeInterval const& theLookahead) = 0;
  virtual void queryLookahead(LogicalTimeInterval& interval) = 0;

  virtual void retract(MessageRetractionHandle theHandle) = 0;
  virtual void changeAttributeOrderType(
      ObjectInstanceHandle theObject,
      AttributeHandleSet const& theAttributes,
      OrderType theType) = 0;
  virtual void changeInteractionOrderType(
      InteractionClassHandle theClass, OrderType theType) = 0;

  // ===== §9 Data Distribution Management =====

  virtual RegionHandle createRegion(
      DimensionHandleSet const& theDimensions) = 0;
  virtual void commitRegionModifications(RegionHandleSet const& theRegions) = 0;
  virtual void deleteRegion(RegionHandle theRegion) = 0;

  virtual ObjectInstanceHandle registerObjectInstanceWithRegions(
      ObjectClassHandle theClass,
      AttributeHandleSetRegionHandleSetPairVector const&
          attributesAndRegions) = 0;
  virtual ObjectInstanceHandle registerObjectInstanceWithRegions(
      ObjectClassHandle theClass,
      AttributeHandleSetRegionHandleSetPairVector const& attributesAndRegions,
      std::wstring const& theObjectInstanceName) = 0;

  virtual void associateRegionsForUpdates(
      ObjectInstanceHandle theObject,
      AttributeHandleSetRegionHandleSetPairVector const& attributesAndRegions) = 0;
  virtual void unassociateRegionsForUpdates(
      ObjectInstanceHandle theObject,
      AttributeHandleSetRegionHandleSetPairVector const& attributesAndRegions) = 0;

  virtual void subscribeObjectClassAttributesWithRegions(
      ObjectClassHandle theClass,
      AttributeHandleSetRegionHandleSetPairVector const& attributesAndRegions,
      bool active = true,
      std::wstring const& updateRateDesignator = L"") = 0;
  virtual void unsubscribeObjectClassAttributesWithRegions(
      ObjectClassHandle theClass,
      AttributeHandleSetRegionHandleSetPairVector const& attributesAndRegions) = 0;

  virtual void subscribeInteractionClassWithRegions(
      InteractionClassHandle theClass,
      RegionHandleSet const& theRegions,
      bool active = true) = 0;
  virtual void unsubscribeInteractionClassWithRegions(
      InteractionClassHandle theClass, RegionHandleSet const& theRegions) = 0;

  virtual void sendInteractionWithRegions(
      InteractionClassHandle theInteraction,
      ParameterHandleValueMap const& theParameterValues,
      RegionHandleSet const& theRegions,
      VariableLengthData const& theUserSuppliedTag) = 0;
  virtual MessageRetractionHandle sendInteractionWithRegions(
      InteractionClassHandle theInteraction,
      ParameterHandleValueMap const& theParameterValues,
      RegionHandleSet const& theRegions,
      VariableLengthData const& theUserSuppliedTag,
      LogicalTime const& theTime) = 0;
  virtual void requestAttributeValueUpdateWithRegions(
      ObjectClassHandle theClass,
      AttributeHandleSetRegionHandleSetPairVector const& attributesAndRegions,
      VariableLengthData const& theUserSuppliedTag) = 0;

  // ===== §10 Support Services =====

  // §10.2-3 automatic resign directive.
  virtual ResignAction getAutomaticResignDirective() = 0;
  virtual void setAutomaticResignDirective(ResignAction resignAction) = 0;

  // §10.4-5 federate handle/name.
  virtual FederateHandle getFederateHandle(std::wstring const& theName) = 0;
  virtual std::wstring getFederateName(FederateHandle theHandle) = 0;

  // §10.6-12 object class / attribute name<->handle.
  virtual ObjectClassHandle getObjectClassHandle(std::wstring const& theName) = 0;
  virtual std::wstring getObjectClassName(ObjectClassHandle theHandle) = 0;
  virtual ObjectClassHandle getKnownObjectClassHandle(
      ObjectInstanceHandle theObject) = 0;
  virtual ObjectInstanceHandle getObjectInstanceHandle(
      std::wstring const& theName) = 0;
  virtual std::wstring getObjectInstanceName(ObjectInstanceHandle theHandle) = 0;
  virtual AttributeHandle getAttributeHandle(ObjectClassHandle theClass,
                                             std::wstring const& theName) = 0;
  virtual std::wstring getAttributeName(ObjectClassHandle theClass,
                                        AttributeHandle theHandle) = 0;

  // §10.13-14 update-rate accessors.
  virtual double getUpdateRateValue(
      std::wstring const& updateRateDesignator) = 0;
  virtual double getUpdateRateValueForAttribute(ObjectInstanceHandle theObject,
                                                AttributeHandle theAttribute) = 0;

  // §10.15-18 interaction class / parameter name<->handle.
  virtual InteractionClassHandle getInteractionClassHandle(
      std::wstring const& theName) = 0;
  virtual std::wstring getInteractionClassName(
      InteractionClassHandle theHandle) = 0;
  virtual ParameterHandle getParameterHandle(InteractionClassHandle theClass,
                                             std::wstring const& theName) = 0;
  virtual std::wstring getParameterName(InteractionClassHandle theClass,
                                        ParameterHandle theHandle) = 0;

  // §10.19-22 order/transport name<->type.
  virtual OrderType getOrderType(std::wstring const& orderName) = 0;
  virtual std::wstring getOrderName(OrderType theType) = 0;
  virtual TransportationType getTransportationType(
      std::wstring const& transportationName) = 0;
  virtual std::wstring getTransportationName(TransportationType theType) = 0;

  // §10.23-30 dimension lookup + range bounds.
  virtual DimensionHandleSet getAvailableDimensionsForClassAttribute(
      ObjectClassHandle theClass, AttributeHandle theHandle) = 0;
  virtual DimensionHandleSet getAvailableDimensionsForInteractionClass(
      InteractionClassHandle theClass) = 0;
  virtual DimensionHandle getDimensionHandle(std::wstring const& theName) = 0;
  virtual std::wstring getDimensionName(DimensionHandle theHandle) = 0;
  virtual unsigned long getDimensionUpperBound(DimensionHandle theHandle) = 0;
  virtual DimensionHandleSet getDimensionHandleSet(
      RegionHandle theRegion) = 0;
  virtual RangeBounds getRangeBounds(RegionHandle theRegion,
                                     DimensionHandle theDimension) = 0;
  virtual void setRangeBounds(RegionHandle theRegion,
                              DimensionHandle theDimension,
                              RangeBounds const& bounds) = 0;

  // §10.31-32 normalize.
  virtual unsigned long normalizeFederateHandle(FederateHandle theHandle) = 0;
  virtual unsigned long normalizeServiceGroup(ServiceGroup theGroup) = 0;

  // §10.33-40 advisory switches.
  virtual void enableObjectClassRelevanceAdvisorySwitch() = 0;
  virtual void disableObjectClassRelevanceAdvisorySwitch() = 0;
  virtual void enableAttributeRelevanceAdvisorySwitch() = 0;
  virtual void disableAttributeRelevanceAdvisorySwitch() = 0;
  virtual void enableAttributeScopeAdvisorySwitch() = 0;
  virtual void disableAttributeScopeAdvisorySwitch() = 0;
  virtual void enableInteractionRelevanceAdvisorySwitch() = 0;
  virtual void disableInteractionRelevanceAdvisorySwitch() = 0;

  // §10.41-44 callback dispatch.
  virtual bool evokeCallback(double approximateMinimumTimeInSeconds) = 0;
  virtual bool evokeMultipleCallbacks(double approximateMinimumTimeInSeconds,
                                      double approximateMaximumTimeInSeconds) = 0;
  virtual void enableCallbacks() = 0;
  virtual void disableCallbacks() = 0;

  // §10 — factory for the federation's logical-time impl.
  virtual rti1516e::auto_ptr<LogicalTimeFactory> getTimeFactory() = 0;

  // §10 — 9 decode*Handle methods.
  virtual FederateHandle decodeFederateHandle(
      VariableLengthData const& encodedValue) = 0;
  virtual ObjectClassHandle decodeObjectClassHandle(
      VariableLengthData const& encodedValue) = 0;
  virtual InteractionClassHandle decodeInteractionClassHandle(
      VariableLengthData const& encodedValue) = 0;
  virtual ObjectInstanceHandle decodeObjectInstanceHandle(
      VariableLengthData const& encodedValue) = 0;
  virtual AttributeHandle decodeAttributeHandle(
      VariableLengthData const& encodedValue) = 0;
  virtual ParameterHandle decodeParameterHandle(
      VariableLengthData const& encodedValue) = 0;
  virtual DimensionHandle decodeDimensionHandle(
      VariableLengthData const& encodedValue) = 0;
  virtual MessageRetractionHandle decodeMessageRetractionHandle(
      VariableLengthData const& encodedValue) = 0;
  virtual RegionHandle decodeRegionHandle(
      VariableLengthData const& encodedValue) = 0;

 private:
  RTIambassador(RTIambassador const&) = delete;
  RTIambassador& operator=(RTIambassador const&) = delete;
};

}  // namespace rti1516e

#endif  // RTI_RTIambassador_h
