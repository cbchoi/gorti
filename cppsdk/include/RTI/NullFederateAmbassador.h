// IEEE 1516.1-2010 §10.42 / Annex A — RTI/NullFederateAmbassador.h
// gorti M31 forward-declaration stub. Spec text reprinted with permission
// from IEEE 1516.1(TM)-2010.
//
// Concrete no-op subclass of FederateAmbassador (catalogue row 4.2).
// Federates inherit from this when they only care about a subset of the
// ~60 spec callbacks.
//
// M31 STUB form: each override declared but not defined; impl lands M33.

#ifndef RTI_NullFederateAmbassador_h
#define RTI_NullFederateAmbassador_h

#include <RTI/FederateAmbassador.h>

namespace rti1516e {

class RTI_EXPORT NullFederateAmbassador : public FederateAmbassador {
 public:
  NullFederateAmbassador();
  virtual ~NullFederateAmbassador() RTI_NOEXCEPT;

  // §4 Federation Management — overrides.
  virtual void connectionLost(std::wstring const& faultDescription) override;
  virtual void reportFederationExecutions(
      FederationExecutionInformationVector const&) override;
  virtual void synchronizationPointRegistrationSucceeded(
      std::wstring const& label) override;
  virtual void synchronizationPointRegistrationFailed(
      std::wstring const& label,
      SynchronizationPointFailureReason reason) override;
  virtual void announceSynchronizationPoint(
      std::wstring const& label,
      VariableLengthData const& theUserSuppliedTag) override;
  virtual void federationSynchronized(
      std::wstring const& label,
      FederateHandleSet const& failedToSyncSet) override;
  virtual void initiateFederateSave(std::wstring const& label) override;
  virtual void initiateFederateSave(std::wstring const& label,
                                    LogicalTime const& theTime) override;
  virtual void federationSaved() override;
  virtual void federationNotSaved(SaveFailureReason) override;
  virtual void federationSaveStatusResponse(
      FederateHandleSaveStatusPairVector const&) override;
  virtual void requestFederationRestoreSucceeded(
      std::wstring const& label) override;
  virtual void requestFederationRestoreFailed(
      std::wstring const& label) override;
  virtual void federationRestoreBegun() override;
  virtual void initiateFederateRestore(std::wstring const& label,
                                       std::wstring const& federateName,
                                       FederateHandle handle) override;
  virtual void federationRestored() override;
  virtual void federationNotRestored(RestoreFailureReason) override;
  virtual void federationRestoreStatusResponse(
      FederateRestoreStatusVector const&) override;

  // §5 Declaration Management — overrides.
  virtual void startRegistrationForObjectClass(ObjectClassHandle) override;
  virtual void stopRegistrationForObjectClass(ObjectClassHandle) override;
  virtual void turnInteractionsOn(InteractionClassHandle) override;
  virtual void turnInteractionsOff(InteractionClassHandle) override;

  // §6 Object Management — overrides (3x reflect/receive/remove).
  virtual void objectInstanceNameReservationSucceeded(
      std::wstring const&) override;
  virtual void objectInstanceNameReservationFailed(
      std::wstring const&) override;
  virtual void multipleObjectInstanceNameReservationSucceeded(
      std::set<std::wstring> const&) override;
  virtual void multipleObjectInstanceNameReservationFailed(
      std::set<std::wstring> const&) override;

  virtual void discoverObjectInstance(ObjectInstanceHandle, ObjectClassHandle,
                                      std::wstring const&) override;
  virtual void discoverObjectInstance(ObjectInstanceHandle, ObjectClassHandle,
                                      std::wstring const&,
                                      FederateHandle) override;

  virtual void reflectAttributeValues(
      ObjectInstanceHandle, AttributeHandleValueMap const&,
      VariableLengthData const&, OrderType, TransportationType,
      SupplementalReflectInfo) override;
  virtual void reflectAttributeValues(
      ObjectInstanceHandle, AttributeHandleValueMap const&,
      VariableLengthData const&, OrderType, TransportationType,
      LogicalTime const&, OrderType, SupplementalReflectInfo) override;
  virtual void reflectAttributeValues(
      ObjectInstanceHandle, AttributeHandleValueMap const&,
      VariableLengthData const&, OrderType, TransportationType,
      LogicalTime const&, OrderType, MessageRetractionHandle,
      SupplementalReflectInfo) override;

  virtual void receiveInteraction(
      InteractionClassHandle, ParameterHandleValueMap const&,
      VariableLengthData const&, OrderType, TransportationType,
      SupplementalReceiveInfo) override;
  virtual void receiveInteraction(
      InteractionClassHandle, ParameterHandleValueMap const&,
      VariableLengthData const&, OrderType, TransportationType,
      LogicalTime const&, OrderType, SupplementalReceiveInfo) override;
  virtual void receiveInteraction(
      InteractionClassHandle, ParameterHandleValueMap const&,
      VariableLengthData const&, OrderType, TransportationType,
      LogicalTime const&, OrderType, MessageRetractionHandle,
      SupplementalReceiveInfo) override;

  virtual void removeObjectInstance(ObjectInstanceHandle,
                                    VariableLengthData const&, OrderType,
                                    SupplementalRemoveInfo) override;
  virtual void removeObjectInstance(ObjectInstanceHandle,
                                    VariableLengthData const&, OrderType,
                                    LogicalTime const&, OrderType,
                                    SupplementalRemoveInfo) override;
  virtual void removeObjectInstance(ObjectInstanceHandle,
                                    VariableLengthData const&, OrderType,
                                    LogicalTime const&, OrderType,
                                    MessageRetractionHandle,
                                    SupplementalRemoveInfo) override;

  virtual void attributesInScope(ObjectInstanceHandle,
                                 AttributeHandleSet const&) override;
  virtual void attributesOutOfScope(ObjectInstanceHandle,
                                    AttributeHandleSet const&) override;
  virtual void provideAttributeValueUpdate(
      ObjectInstanceHandle, AttributeHandleSet const&,
      VariableLengthData const&) override;
  virtual void turnUpdatesOnForObjectInstance(
      ObjectInstanceHandle, AttributeHandleSet const&) override;
  virtual void turnUpdatesOnForObjectInstance(
      ObjectInstanceHandle, AttributeHandleSet const&,
      std::wstring const&) override;
  virtual void turnUpdatesOffForObjectInstance(
      ObjectInstanceHandle, AttributeHandleSet const&) override;

  virtual void confirmAttributeTransportationTypeChange(
      ObjectInstanceHandle, AttributeHandleSet, TransportationType) override;
  virtual void reportAttributeTransportationType(
      ObjectInstanceHandle, AttributeHandle, TransportationType) override;
  virtual void confirmInteractionTransportationTypeChange(
      InteractionClassHandle, TransportationType) override;
  virtual void reportInteractionTransportationType(
      FederateHandle, InteractionClassHandle, TransportationType) override;

  // §7 Ownership Management — overrides.
  virtual void requestAttributeOwnershipAssumption(
      ObjectInstanceHandle, AttributeHandleSet const&,
      VariableLengthData const&) override;
  virtual void requestDivestitureConfirmation(
      ObjectInstanceHandle, AttributeHandleSet const&) override;
  virtual void attributeOwnershipAcquisitionNotification(
      ObjectInstanceHandle, AttributeHandleSet const&,
      VariableLengthData const&) override;
  virtual void attributeOwnershipUnavailable(
      ObjectInstanceHandle, AttributeHandleSet const&) override;
  virtual void requestAttributeOwnershipRelease(
      ObjectInstanceHandle, AttributeHandleSet const&,
      VariableLengthData const&) override;
  virtual void confirmAttributeOwnershipAcquisitionCancellation(
      ObjectInstanceHandle, AttributeHandleSet const&) override;
  virtual void informAttributeOwnership(ObjectInstanceHandle, AttributeHandle,
                                        FederateHandle) override;
  virtual void attributeIsNotOwned(ObjectInstanceHandle,
                                   AttributeHandle) override;
  virtual void attributeIsOwnedByRTI(ObjectInstanceHandle,
                                     AttributeHandle) override;

  // §8 Time Management — overrides.
  virtual void timeRegulationEnabled(LogicalTime const&) override;
  virtual void timeConstrainedEnabled(LogicalTime const&) override;
  virtual void timeAdvanceGrant(LogicalTime const&) override;

  virtual void requestRetraction(MessageRetractionHandle) override;
};

}  // namespace rti1516e

#endif  // RTI_NullFederateAmbassador_h
