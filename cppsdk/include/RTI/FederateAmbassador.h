// IEEE 1516.1-2010 §10.42 / Annex A — RTI/FederateAmbassador.h
// gorti M31 forward-declaration stub. Spec text reprinted with permission
// from IEEE 1516.1(TM)-2010.
//
// Pure-abstract callback interface. Federates that derive directly from
// FederateAmbassador must override every callback; most federates derive
// from NullFederateAmbassador instead (default no-op bodies).
//
// Catalogue rows 4.1-4.37. CENTRAL FR-DLC-5 lockfile target: the 3x
// reflect / 3x receive / 3x remove overload set per §6.11 / §6.13 / §6.15.

#ifndef RTI_FederateAmbassador_h
#define RTI_FederateAmbassador_h

namespace rti1516e {
class LogicalTime;
}

#include <RTI/SpecificConfig.h>
#include <RTI/Typedefs.h>
#include <RTI/Exception.h>
#include <string>
#include <set>

namespace rti1516e {

class RTI_EXPORT FederateAmbassador {
 protected:
  FederateAmbassador() RTI_NOEXCEPT;

 public:
  virtual ~FederateAmbassador() RTI_NOEXCEPT;

  // ===== §4 Federation Management callbacks =====

  virtual void connectionLost(std::wstring const& faultDescription) = 0;

  virtual void reportFederationExecutions(
      FederationExecutionInformationVector const&
          theFederationExecutionInformationList) = 0;

  virtual void synchronizationPointRegistrationSucceeded(
      std::wstring const& label) = 0;
  virtual void synchronizationPointRegistrationFailed(
      std::wstring const& label,
      SynchronizationPointFailureReason reason) = 0;

  virtual void announceSynchronizationPoint(
      std::wstring const& label,
      VariableLengthData const& theUserSuppliedTag) = 0;

  virtual void federationSynchronized(
      std::wstring const& label,
      FederateHandleSet const& failedToSyncSet) = 0;

  virtual void initiateFederateSave(std::wstring const& label) = 0;
  virtual void initiateFederateSave(std::wstring const& label,
                                    LogicalTime const& theTime) = 0;

  virtual void federationSaved() = 0;
  virtual void federationNotSaved(SaveFailureReason theSaveFailureReason) = 0;

  virtual void federationSaveStatusResponse(
      FederateHandleSaveStatusPairVector const& theFederateStatusVector) = 0;

  virtual void requestFederationRestoreSucceeded(
      std::wstring const& label) = 0;
  virtual void requestFederationRestoreFailed(std::wstring const& label) = 0;

  virtual void federationRestoreBegun() = 0;

  virtual void initiateFederateRestore(std::wstring const& label,
                                       std::wstring const& federateName,
                                       FederateHandle handle) = 0;

  virtual void federationRestored() = 0;
  virtual void federationNotRestored(
      RestoreFailureReason theRestoreFailureReason) = 0;

  virtual void federationRestoreStatusResponse(
      FederateRestoreStatusVector const& theFederateRestoreStatusVector) = 0;

  // ===== §5 Declaration Management callbacks =====

  virtual void startRegistrationForObjectClass(ObjectClassHandle theClass) = 0;
  virtual void stopRegistrationForObjectClass(ObjectClassHandle theClass) = 0;
  virtual void turnInteractionsOn(InteractionClassHandle theHandle) = 0;
  virtual void turnInteractionsOff(InteractionClassHandle theHandle) = 0;

  // ===== §6 Object Management callbacks =====

  virtual void objectInstanceNameReservationSucceeded(
      std::wstring const& theObjectInstanceName) = 0;
  virtual void objectInstanceNameReservationFailed(
      std::wstring const& theObjectInstanceName) = 0;
  virtual void multipleObjectInstanceNameReservationSucceeded(
      std::set<std::wstring> const& theObjectInstanceNames) = 0;
  virtual void multipleObjectInstanceNameReservationFailed(
      std::set<std::wstring> const& theObjectInstanceNames) = 0;

  // §6.9 discoverObjectInstance — 2 overloads (catalogue 4.19).
  virtual void discoverObjectInstance(
      ObjectInstanceHandle theObject,
      ObjectClassHandle theObjectClass,
      std::wstring const& theObjectInstanceName) = 0;
  virtual void discoverObjectInstance(
      ObjectInstanceHandle theObject,
      ObjectClassHandle theObjectClass,
      std::wstring const& theObjectInstanceName,
      FederateHandle producingFederate) = 0;

  // §6.11 reflectAttributeValues — 3 OVERLOADS (catalogue 4.20).
  // Central parity-test blocker.
  virtual void reflectAttributeValues(
      ObjectInstanceHandle theObject,
      AttributeHandleValueMap const& theAttributeValues,
      VariableLengthData const& theUserSuppliedTag,
      OrderType sentOrder,
      TransportationType theType,
      SupplementalReflectInfo theReflectInfo) = 0;

  virtual void reflectAttributeValues(
      ObjectInstanceHandle theObject,
      AttributeHandleValueMap const& theAttributeValues,
      VariableLengthData const& theUserSuppliedTag,
      OrderType sentOrder,
      TransportationType theType,
      LogicalTime const& theTime,
      OrderType receivedOrder,
      SupplementalReflectInfo theReflectInfo) = 0;

  virtual void reflectAttributeValues(
      ObjectInstanceHandle theObject,
      AttributeHandleValueMap const& theAttributeValues,
      VariableLengthData const& theUserSuppliedTag,
      OrderType sentOrder,
      TransportationType theType,
      LogicalTime const& theTime,
      OrderType receivedOrder,
      MessageRetractionHandle theHandle,
      SupplementalReflectInfo theReflectInfo) = 0;

  // §6.13 receiveInteraction — 3 OVERLOADS (catalogue 4.21).
  virtual void receiveInteraction(
      InteractionClassHandle theInteraction,
      ParameterHandleValueMap const& theParameterValues,
      VariableLengthData const& theUserSuppliedTag,
      OrderType sentOrder,
      TransportationType theType,
      SupplementalReceiveInfo theReceiveInfo) = 0;

  virtual void receiveInteraction(
      InteractionClassHandle theInteraction,
      ParameterHandleValueMap const& theParameterValues,
      VariableLengthData const& theUserSuppliedTag,
      OrderType sentOrder,
      TransportationType theType,
      LogicalTime const& theTime,
      OrderType receivedOrder,
      SupplementalReceiveInfo theReceiveInfo) = 0;

  virtual void receiveInteraction(
      InteractionClassHandle theInteraction,
      ParameterHandleValueMap const& theParameterValues,
      VariableLengthData const& theUserSuppliedTag,
      OrderType sentOrder,
      TransportationType theType,
      LogicalTime const& theTime,
      OrderType receivedOrder,
      MessageRetractionHandle theHandle,
      SupplementalReceiveInfo theReceiveInfo) = 0;

  // §6.15 removeObjectInstance — 3 OVERLOADS (catalogue 4.22).
  virtual void removeObjectInstance(
      ObjectInstanceHandle theObject,
      VariableLengthData const& theUserSuppliedTag,
      OrderType sentOrder,
      SupplementalRemoveInfo theRemoveInfo) = 0;
  virtual void removeObjectInstance(
      ObjectInstanceHandle theObject,
      VariableLengthData const& theUserSuppliedTag,
      OrderType sentOrder,
      LogicalTime const& theTime,
      OrderType receivedOrder,
      SupplementalRemoveInfo theRemoveInfo) = 0;
  virtual void removeObjectInstance(
      ObjectInstanceHandle theObject,
      VariableLengthData const& theUserSuppliedTag,
      OrderType sentOrder,
      LogicalTime const& theTime,
      OrderType receivedOrder,
      MessageRetractionHandle theHandle,
      SupplementalRemoveInfo theRemoveInfo) = 0;

  virtual void attributesInScope(ObjectInstanceHandle theObject,
                                 AttributeHandleSet const& theAttributes) = 0;
  virtual void attributesOutOfScope(ObjectInstanceHandle theObject,
                                    AttributeHandleSet const& theAttributes) = 0;

  virtual void provideAttributeValueUpdate(
      ObjectInstanceHandle theObject,
      AttributeHandleSet const& theAttributes,
      VariableLengthData const& theUserSuppliedTag) = 0;

  virtual void turnUpdatesOnForObjectInstance(
      ObjectInstanceHandle theObject,
      AttributeHandleSet const& theAttributes) = 0;
  virtual void turnUpdatesOnForObjectInstance(
      ObjectInstanceHandle theObject,
      AttributeHandleSet const& theAttributes,
      std::wstring const& updateRateDesignator) = 0;
  virtual void turnUpdatesOffForObjectInstance(
      ObjectInstanceHandle theObject,
      AttributeHandleSet const& theAttributes) = 0;

  virtual void confirmAttributeTransportationTypeChange(
      ObjectInstanceHandle theObject,
      AttributeHandleSet theAttributes,
      TransportationType theTransportation) = 0;
  virtual void reportAttributeTransportationType(
      ObjectInstanceHandle theObject,
      AttributeHandle theAttribute,
      TransportationType theTransportation) = 0;
  virtual void confirmInteractionTransportationTypeChange(
      InteractionClassHandle theInteraction,
      TransportationType theTransportation) = 0;
  virtual void reportInteractionTransportationType(
      FederateHandle theFederate,
      InteractionClassHandle theInteraction,
      TransportationType theTransportation) = 0;

  // ===== §7 Ownership Management callbacks =====

  virtual void requestAttributeOwnershipAssumption(
      ObjectInstanceHandle theObject,
      AttributeHandleSet const& offeredAttributes,
      VariableLengthData const& theUserSuppliedTag) = 0;
  virtual void requestDivestitureConfirmation(
      ObjectInstanceHandle theObject,
      AttributeHandleSet const& releasedAttributes) = 0;
  virtual void attributeOwnershipAcquisitionNotification(
      ObjectInstanceHandle theObject,
      AttributeHandleSet const& securedAttributes,
      VariableLengthData const& theUserSuppliedTag) = 0;
  virtual void attributeOwnershipUnavailable(
      ObjectInstanceHandle theObject,
      AttributeHandleSet const& theAttributes) = 0;
  virtual void requestAttributeOwnershipRelease(
      ObjectInstanceHandle theObject,
      AttributeHandleSet const& candidateAttributes,
      VariableLengthData const& theUserSuppliedTag) = 0;
  virtual void confirmAttributeOwnershipAcquisitionCancellation(
      ObjectInstanceHandle theObject,
      AttributeHandleSet const& theAttributes) = 0;
  virtual void informAttributeOwnership(
      ObjectInstanceHandle theObject,
      AttributeHandle theAttribute,
      FederateHandle theOwner) = 0;
  virtual void attributeIsNotOwned(ObjectInstanceHandle theObject,
                                   AttributeHandle theAttribute) = 0;
  virtual void attributeIsOwnedByRTI(ObjectInstanceHandle theObject,
                                     AttributeHandle theAttribute) = 0;

  // ===== §8 Time Management callbacks =====

  virtual void timeRegulationEnabled(LogicalTime const& theFederateTime) = 0;
  virtual void timeConstrainedEnabled(LogicalTime const& theFederateTime) = 0;
  virtual void timeAdvanceGrant(LogicalTime const& theTime) = 0;

  virtual void requestRetraction(MessageRetractionHandle theHandle) = 0;
};

}  // namespace rti1516e

#endif  // RTI_FederateAmbassador_h
