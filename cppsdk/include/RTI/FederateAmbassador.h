// IEEE 1516.1-2010 §10.42 / Annex A — RTI/FederateAmbassador.h
// gorti M33. Spec text reprinted with permission from
// IEEE 1516.1(TM)-2010.
//
// Pure-abstract callback interface. Federates that derive directly from
// FederateAmbassador must override every callback; most federates derive
// from NullFederateAmbassador instead (default no-op bodies).
//
// Catalogue rows 4.1-4.37. CENTRAL FR-DLC-5 lockfile target: the 3x
// reflect / 3x receive / 3x remove overload set per §6.11 / §6.13 / §6.15.
//
// M33 adds: RTI_THROW(FederateInternalError) declaration decoration on every
// callback per catalogue row 4.37 / FR-DLC-9. Under C++17 the macro expands
// to nothing so ABI is unchanged, but source-level spec-parity is restored.

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
  FederateAmbassador() RTI_THROW(FederateInternalError);

 public:
  virtual ~FederateAmbassador() RTI_NOEXCEPT = 0;

  // ===== §4 Federation Management callbacks =====

  // §4.4 — catalogue 4.3.
  virtual void connectionLost(std::wstring const& faultDescription)
      RTI_THROW(FederateInternalError) = 0;

  // §4.8 — catalogue 4.4.
  virtual void reportFederationExecutions(
      FederationExecutionInformationVector const&
          theFederationExecutionInformationList)
      RTI_THROW(FederateInternalError) = 0;

  // §4.12 — catalogue 4.5.
  virtual void synchronizationPointRegistrationSucceeded(
      std::wstring const& label)
      RTI_THROW(FederateInternalError) = 0;
  virtual void synchronizationPointRegistrationFailed(
      std::wstring const& label,
      SynchronizationPointFailureReason reason)
      RTI_THROW(FederateInternalError) = 0;

  // §4.13 — catalogue 4.6.
  virtual void announceSynchronizationPoint(
      std::wstring const& label,
      VariableLengthData const& theUserSuppliedTag)
      RTI_THROW(FederateInternalError) = 0;

  // §4.15 — catalogue 4.7.
  virtual void federationSynchronized(
      std::wstring const& label,
      FederateHandleSet const& failedToSyncSet)
      RTI_THROW(FederateInternalError) = 0;

  // §4.17 — catalogue 4.8. Two overloads (no-time / with-time).
  virtual void initiateFederateSave(std::wstring const& label)
      RTI_THROW(FederateInternalError) = 0;
  virtual void initiateFederateSave(std::wstring const& label,
                                    LogicalTime const& theTime)
      RTI_THROW(FederateInternalError) = 0;

  // §4.20 — catalogue 4.9. No label per spec.
  virtual void federationSaved()
      RTI_THROW(FederateInternalError) = 0;
  virtual void federationNotSaved(SaveFailureReason theSaveFailureReason)
      RTI_THROW(FederateInternalError) = 0;

  // §4.23 — catalogue 4.10.
  virtual void federationSaveStatusResponse(
      FederateHandleSaveStatusPairVector const& theFederateStatusVector)
      RTI_THROW(FederateInternalError) = 0;

  // §4.25 — catalogue 4.11.
  virtual void requestFederationRestoreSucceeded(std::wstring const& label)
      RTI_THROW(FederateInternalError) = 0;
  virtual void requestFederationRestoreFailed(std::wstring const& label)
      RTI_THROW(FederateInternalError) = 0;

  // §4.26 — catalogue 4.12.
  virtual void federationRestoreBegun()
      RTI_THROW(FederateInternalError) = 0;

  // §4.27 — catalogue 4.13.
  virtual void initiateFederateRestore(std::wstring const& label,
                                       std::wstring const& federateName,
                                       FederateHandle handle)
      RTI_THROW(FederateInternalError) = 0;

  // §4.29 — catalogue 4.14.
  virtual void federationRestored()
      RTI_THROW(FederateInternalError) = 0;
  virtual void federationNotRestored(
      RestoreFailureReason theRestoreFailureReason)
      RTI_THROW(FederateInternalError) = 0;

  // §4.32 — catalogue 4.15.
  virtual void federationRestoreStatusResponse(
      FederateRestoreStatusVector const& theFederateRestoreStatusVector)
      RTI_THROW(FederateInternalError) = 0;

  // ===== §5 Declaration Management callbacks — catalogue 4.16 =====

  // §5.10
  virtual void startRegistrationForObjectClass(ObjectClassHandle theClass)
      RTI_THROW(FederateInternalError) = 0;
  // §5.11
  virtual void stopRegistrationForObjectClass(ObjectClassHandle theClass)
      RTI_THROW(FederateInternalError) = 0;
  // §5.12
  virtual void turnInteractionsOn(InteractionClassHandle theHandle)
      RTI_THROW(FederateInternalError) = 0;
  // §5.13
  virtual void turnInteractionsOff(InteractionClassHandle theHandle)
      RTI_THROW(FederateInternalError) = 0;

  // ===== §6 Object Management callbacks =====

  // §6.3 — catalogue 4.17.
  virtual void objectInstanceNameReservationSucceeded(
      std::wstring const& theObjectInstanceName)
      RTI_THROW(FederateInternalError) = 0;
  virtual void objectInstanceNameReservationFailed(
      std::wstring const& theObjectInstanceName)
      RTI_THROW(FederateInternalError) = 0;

  // §6.6 — catalogue 4.18.
  virtual void multipleObjectInstanceNameReservationSucceeded(
      std::set<std::wstring> const& theObjectInstanceNames)
      RTI_THROW(FederateInternalError) = 0;
  virtual void multipleObjectInstanceNameReservationFailed(
      std::set<std::wstring> const& theObjectInstanceNames)
      RTI_THROW(FederateInternalError) = 0;

  // §6.9 discoverObjectInstance — 2 overloads (catalogue 4.19).
  virtual void discoverObjectInstance(
      ObjectInstanceHandle theObject,
      ObjectClassHandle theObjectClass,
      std::wstring const& theObjectInstanceName)
      RTI_THROW(FederateInternalError) = 0;
  virtual void discoverObjectInstance(
      ObjectInstanceHandle theObject,
      ObjectClassHandle theObjectClass,
      std::wstring const& theObjectInstanceName,
      FederateHandle producingFederate)
      RTI_THROW(FederateInternalError) = 0;

  // §6.11 reflectAttributeValues — 3 OVERLOADS (catalogue 4.20).
  // Central parity-test blocker.
  virtual void reflectAttributeValues(
      ObjectInstanceHandle theObject,
      AttributeHandleValueMap const& theAttributeValues,
      VariableLengthData const& theUserSuppliedTag,
      OrderType sentOrder,
      TransportationType theType,
      SupplementalReflectInfo theReflectInfo)
      RTI_THROW(FederateInternalError) = 0;

  virtual void reflectAttributeValues(
      ObjectInstanceHandle theObject,
      AttributeHandleValueMap const& theAttributeValues,
      VariableLengthData const& theUserSuppliedTag,
      OrderType sentOrder,
      TransportationType theType,
      LogicalTime const& theTime,
      OrderType receivedOrder,
      SupplementalReflectInfo theReflectInfo)
      RTI_THROW(FederateInternalError) = 0;

  virtual void reflectAttributeValues(
      ObjectInstanceHandle theObject,
      AttributeHandleValueMap const& theAttributeValues,
      VariableLengthData const& theUserSuppliedTag,
      OrderType sentOrder,
      TransportationType theType,
      LogicalTime const& theTime,
      OrderType receivedOrder,
      MessageRetractionHandle theHandle,
      SupplementalReflectInfo theReflectInfo)
      RTI_THROW(FederateInternalError) = 0;

  // §6.13 receiveInteraction — 3 OVERLOADS (catalogue 4.21).
  virtual void receiveInteraction(
      InteractionClassHandle theInteraction,
      ParameterHandleValueMap const& theParameterValues,
      VariableLengthData const& theUserSuppliedTag,
      OrderType sentOrder,
      TransportationType theType,
      SupplementalReceiveInfo theReceiveInfo)
      RTI_THROW(FederateInternalError) = 0;

  virtual void receiveInteraction(
      InteractionClassHandle theInteraction,
      ParameterHandleValueMap const& theParameterValues,
      VariableLengthData const& theUserSuppliedTag,
      OrderType sentOrder,
      TransportationType theType,
      LogicalTime const& theTime,
      OrderType receivedOrder,
      SupplementalReceiveInfo theReceiveInfo)
      RTI_THROW(FederateInternalError) = 0;

  virtual void receiveInteraction(
      InteractionClassHandle theInteraction,
      ParameterHandleValueMap const& theParameterValues,
      VariableLengthData const& theUserSuppliedTag,
      OrderType sentOrder,
      TransportationType theType,
      LogicalTime const& theTime,
      OrderType receivedOrder,
      MessageRetractionHandle theHandle,
      SupplementalReceiveInfo theReceiveInfo)
      RTI_THROW(FederateInternalError) = 0;

  // §6.15 removeObjectInstance — 3 OVERLOADS (catalogue 4.22).
  virtual void removeObjectInstance(
      ObjectInstanceHandle theObject,
      VariableLengthData const& theUserSuppliedTag,
      OrderType sentOrder,
      SupplementalRemoveInfo theRemoveInfo)
      RTI_THROW(FederateInternalError) = 0;
  virtual void removeObjectInstance(
      ObjectInstanceHandle theObject,
      VariableLengthData const& theUserSuppliedTag,
      OrderType sentOrder,
      LogicalTime const& theTime,
      OrderType receivedOrder,
      SupplementalRemoveInfo theRemoveInfo)
      RTI_THROW(FederateInternalError) = 0;
  virtual void removeObjectInstance(
      ObjectInstanceHandle theObject,
      VariableLengthData const& theUserSuppliedTag,
      OrderType sentOrder,
      LogicalTime const& theTime,
      OrderType receivedOrder,
      MessageRetractionHandle theHandle,
      SupplementalRemoveInfo theRemoveInfo)
      RTI_THROW(FederateInternalError) = 0;

  // §6.17-18 — catalogue 4.23.
  virtual void attributesInScope(ObjectInstanceHandle theObject,
                                 AttributeHandleSet const& theAttributes)
      RTI_THROW(FederateInternalError) = 0;
  virtual void attributesOutOfScope(ObjectInstanceHandle theObject,
                                    AttributeHandleSet const& theAttributes)
      RTI_THROW(FederateInternalError) = 0;

  // §6.20 — catalogue 4.24.
  virtual void provideAttributeValueUpdate(
      ObjectInstanceHandle theObject,
      AttributeHandleSet const& theAttributes,
      VariableLengthData const& theUserSuppliedTag)
      RTI_THROW(FederateInternalError) = 0;

  // §6.21-22 — catalogue 4.25.
  virtual void turnUpdatesOnForObjectInstance(
      ObjectInstanceHandle theObject,
      AttributeHandleSet const& theAttributes)
      RTI_THROW(FederateInternalError) = 0;
  virtual void turnUpdatesOnForObjectInstance(
      ObjectInstanceHandle theObject,
      AttributeHandleSet const& theAttributes,
      std::wstring const& updateRateDesignator)
      RTI_THROW(FederateInternalError) = 0;
  virtual void turnUpdatesOffForObjectInstance(
      ObjectInstanceHandle theObject,
      AttributeHandleSet const& theAttributes)
      RTI_THROW(FederateInternalError) = 0;

  // §6.24-30 transportation — catalogue 4.26.
  virtual void confirmAttributeTransportationTypeChange(
      ObjectInstanceHandle theObject,
      AttributeHandleSet theAttributes,
      TransportationType theTransportation)
      RTI_THROW(FederateInternalError) = 0;
  virtual void reportAttributeTransportationType(
      ObjectInstanceHandle theObject,
      AttributeHandle theAttribute,
      TransportationType theTransportation)
      RTI_THROW(FederateInternalError) = 0;
  virtual void confirmInteractionTransportationTypeChange(
      InteractionClassHandle theInteraction,
      TransportationType theTransportation)
      RTI_THROW(FederateInternalError) = 0;
  virtual void reportInteractionTransportationType(
      FederateHandle theFederate,
      InteractionClassHandle theInteraction,
      TransportationType theTransportation)
      RTI_THROW(FederateInternalError) = 0;

  // ===== §7 Ownership Management callbacks =====

  // §7.4 — catalogue 4.27.
  virtual void requestAttributeOwnershipAssumption(
      ObjectInstanceHandle theObject,
      AttributeHandleSet const& offeredAttributes,
      VariableLengthData const& theUserSuppliedTag)
      RTI_THROW(FederateInternalError) = 0;
  // §7.5
  virtual void requestDivestitureConfirmation(
      ObjectInstanceHandle theObject,
      AttributeHandleSet const& releasedAttributes)
      RTI_THROW(FederateInternalError) = 0;
  // §7.7 — catalogue 4.28.
  virtual void attributeOwnershipAcquisitionNotification(
      ObjectInstanceHandle theObject,
      AttributeHandleSet const& securedAttributes,
      VariableLengthData const& theUserSuppliedTag)
      RTI_THROW(FederateInternalError) = 0;
  // §7.10 — catalogue 4.29.
  virtual void attributeOwnershipUnavailable(
      ObjectInstanceHandle theObject,
      AttributeHandleSet const& theAttributes)
      RTI_THROW(FederateInternalError) = 0;
  // §7.11 — catalogue 4.30.
  virtual void requestAttributeOwnershipRelease(
      ObjectInstanceHandle theObject,
      AttributeHandleSet const& candidateAttributes,
      VariableLengthData const& theUserSuppliedTag)
      RTI_THROW(FederateInternalError) = 0;
  // §7.16 — catalogue 4.31.
  virtual void confirmAttributeOwnershipAcquisitionCancellation(
      ObjectInstanceHandle theObject,
      AttributeHandleSet const& theAttributes)
      RTI_THROW(FederateInternalError) = 0;
  // §7.18 — catalogue 4.32.
  virtual void informAttributeOwnership(
      ObjectInstanceHandle theObject,
      AttributeHandle theAttribute,
      FederateHandle theOwner)
      RTI_THROW(FederateInternalError) = 0;
  virtual void attributeIsNotOwned(ObjectInstanceHandle theObject,
                                   AttributeHandle theAttribute)
      RTI_THROW(FederateInternalError) = 0;
  virtual void attributeIsOwnedByRTI(ObjectInstanceHandle theObject,
                                     AttributeHandle theAttribute)
      RTI_THROW(FederateInternalError) = 0;

  // ===== §8 Time Management callbacks =====

  // §8.3 — catalogue 4.33.
  virtual void timeRegulationEnabled(LogicalTime const& theFederateTime)
      RTI_THROW(FederateInternalError) = 0;
  // §8.6 — catalogue 4.34.
  virtual void timeConstrainedEnabled(LogicalTime const& theFederateTime)
      RTI_THROW(FederateInternalError) = 0;
  // §8.13 — catalogue 4.35.
  virtual void timeAdvanceGrant(LogicalTime const& theTime)
      RTI_THROW(FederateInternalError) = 0;

  // §8.22 — catalogue 4.36.
  virtual void requestRetraction(MessageRetractionHandle theHandle)
      RTI_THROW(FederateInternalError) = 0;
};

}  // namespace rti1516e

#endif  // RTI_FederateAmbassador_h
