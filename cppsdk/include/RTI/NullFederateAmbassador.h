// IEEE 1516.1-2010 §10.42 / Annex A — RTI/NullFederateAmbassador.h
// gorti M33 (Agent J). Spec text reprinted with permission from
// IEEE 1516.1(TM)-2010.
//
// Concrete no-op subclass of FederateAmbassador (catalogue row 4.2).
// Federates inherit from this when they only care about a subset of the
// ~60 spec callbacks. Out-of-line no-op impls live in NullFederateAmbassador.cpp.
//
// Catalogue rows covered: 4.1 (base), 4.2 (Null subclass), 4.3-4.36
// (every callback). Row 4.37 (RTI_THROW) is applied on every override.

#ifndef RTI_NullFederateAmbassador_h
#define RTI_NullFederateAmbassador_h

#include <RTI/FederateAmbassador.h>

namespace rti1516e {

class RTI_EXPORT NullFederateAmbassador : public FederateAmbassador {
 public:
  NullFederateAmbassador() RTI_THROW(FederateInternalError);
  virtual ~NullFederateAmbassador() RTI_NOEXCEPT;

  // §4 Federation Management — overrides.
  virtual void connectionLost(std::wstring const& faultDescription)
      RTI_THROW(FederateInternalError) override;
  virtual void reportFederationExecutions(
      FederationExecutionInformationVector const&)
      RTI_THROW(FederateInternalError) override;
  virtual void synchronizationPointRegistrationSucceeded(
      std::wstring const& label)
      RTI_THROW(FederateInternalError) override;
  virtual void synchronizationPointRegistrationFailed(
      std::wstring const& label,
      SynchronizationPointFailureReason reason)
      RTI_THROW(FederateInternalError) override;
  virtual void announceSynchronizationPoint(
      std::wstring const& label,
      VariableLengthData const& theUserSuppliedTag)
      RTI_THROW(FederateInternalError) override;
  virtual void federationSynchronized(
      std::wstring const& label,
      FederateHandleSet const& failedToSyncSet)
      RTI_THROW(FederateInternalError) override;
  virtual void initiateFederateSave(std::wstring const& label)
      RTI_THROW(FederateInternalError) override;
  virtual void initiateFederateSave(std::wstring const& label,
                                    LogicalTime const& theTime)
      RTI_THROW(FederateInternalError) override;
  virtual void federationSaved()
      RTI_THROW(FederateInternalError) override;
  virtual void federationNotSaved(SaveFailureReason)
      RTI_THROW(FederateInternalError) override;
  virtual void federationSaveStatusResponse(
      FederateHandleSaveStatusPairVector const&)
      RTI_THROW(FederateInternalError) override;
  virtual void requestFederationRestoreSucceeded(std::wstring const& label)
      RTI_THROW(FederateInternalError) override;
  virtual void requestFederationRestoreFailed(std::wstring const& label)
      RTI_THROW(FederateInternalError) override;
  virtual void federationRestoreBegun()
      RTI_THROW(FederateInternalError) override;
  virtual void initiateFederateRestore(std::wstring const& label,
                                       std::wstring const& federateName,
                                       FederateHandle handle)
      RTI_THROW(FederateInternalError) override;
  virtual void federationRestored()
      RTI_THROW(FederateInternalError) override;
  virtual void federationNotRestored(RestoreFailureReason)
      RTI_THROW(FederateInternalError) override;
  virtual void federationRestoreStatusResponse(
      FederateRestoreStatusVector const&)
      RTI_THROW(FederateInternalError) override;

  // §5 Declaration Management — overrides.
  virtual void startRegistrationForObjectClass(ObjectClassHandle)
      RTI_THROW(FederateInternalError) override;
  virtual void stopRegistrationForObjectClass(ObjectClassHandle)
      RTI_THROW(FederateInternalError) override;
  virtual void turnInteractionsOn(InteractionClassHandle)
      RTI_THROW(FederateInternalError) override;
  virtual void turnInteractionsOff(InteractionClassHandle)
      RTI_THROW(FederateInternalError) override;

  // §6 Object Management — overrides (3x reflect/receive/remove).
  virtual void objectInstanceNameReservationSucceeded(std::wstring const&)
      RTI_THROW(FederateInternalError) override;
  virtual void objectInstanceNameReservationFailed(std::wstring const&)
      RTI_THROW(FederateInternalError) override;
  virtual void multipleObjectInstanceNameReservationSucceeded(
      std::set<std::wstring> const&)
      RTI_THROW(FederateInternalError) override;
  virtual void multipleObjectInstanceNameReservationFailed(
      std::set<std::wstring> const&)
      RTI_THROW(FederateInternalError) override;

  virtual void discoverObjectInstance(ObjectInstanceHandle, ObjectClassHandle,
                                      std::wstring const&)
      RTI_THROW(FederateInternalError) override;
  virtual void discoverObjectInstance(ObjectInstanceHandle, ObjectClassHandle,
                                      std::wstring const&,
                                      FederateHandle)
      RTI_THROW(FederateInternalError) override;

  virtual void reflectAttributeValues(
      ObjectInstanceHandle, AttributeHandleValueMap const&,
      VariableLengthData const&, OrderType, TransportationType,
      SupplementalReflectInfo)
      RTI_THROW(FederateInternalError) override;
  virtual void reflectAttributeValues(
      ObjectInstanceHandle, AttributeHandleValueMap const&,
      VariableLengthData const&, OrderType, TransportationType,
      LogicalTime const&, OrderType, SupplementalReflectInfo)
      RTI_THROW(FederateInternalError) override;
  virtual void reflectAttributeValues(
      ObjectInstanceHandle, AttributeHandleValueMap const&,
      VariableLengthData const&, OrderType, TransportationType,
      LogicalTime const&, OrderType, MessageRetractionHandle,
      SupplementalReflectInfo)
      RTI_THROW(FederateInternalError) override;

  virtual void receiveInteraction(
      InteractionClassHandle, ParameterHandleValueMap const&,
      VariableLengthData const&, OrderType, TransportationType,
      SupplementalReceiveInfo)
      RTI_THROW(FederateInternalError) override;
  virtual void receiveInteraction(
      InteractionClassHandle, ParameterHandleValueMap const&,
      VariableLengthData const&, OrderType, TransportationType,
      LogicalTime const&, OrderType, SupplementalReceiveInfo)
      RTI_THROW(FederateInternalError) override;
  virtual void receiveInteraction(
      InteractionClassHandle, ParameterHandleValueMap const&,
      VariableLengthData const&, OrderType, TransportationType,
      LogicalTime const&, OrderType, MessageRetractionHandle,
      SupplementalReceiveInfo)
      RTI_THROW(FederateInternalError) override;

  virtual void removeObjectInstance(ObjectInstanceHandle,
                                    VariableLengthData const&, OrderType,
                                    SupplementalRemoveInfo)
      RTI_THROW(FederateInternalError) override;
  virtual void removeObjectInstance(ObjectInstanceHandle,
                                    VariableLengthData const&, OrderType,
                                    LogicalTime const&, OrderType,
                                    SupplementalRemoveInfo)
      RTI_THROW(FederateInternalError) override;
  virtual void removeObjectInstance(ObjectInstanceHandle,
                                    VariableLengthData const&, OrderType,
                                    LogicalTime const&, OrderType,
                                    MessageRetractionHandle,
                                    SupplementalRemoveInfo)
      RTI_THROW(FederateInternalError) override;

  virtual void attributesInScope(ObjectInstanceHandle,
                                 AttributeHandleSet const&)
      RTI_THROW(FederateInternalError) override;
  virtual void attributesOutOfScope(ObjectInstanceHandle,
                                    AttributeHandleSet const&)
      RTI_THROW(FederateInternalError) override;
  virtual void provideAttributeValueUpdate(
      ObjectInstanceHandle, AttributeHandleSet const&,
      VariableLengthData const&)
      RTI_THROW(FederateInternalError) override;
  virtual void turnUpdatesOnForObjectInstance(
      ObjectInstanceHandle, AttributeHandleSet const&)
      RTI_THROW(FederateInternalError) override;
  virtual void turnUpdatesOnForObjectInstance(
      ObjectInstanceHandle, AttributeHandleSet const&,
      std::wstring const&)
      RTI_THROW(FederateInternalError) override;
  virtual void turnUpdatesOffForObjectInstance(
      ObjectInstanceHandle, AttributeHandleSet const&)
      RTI_THROW(FederateInternalError) override;

  virtual void confirmAttributeTransportationTypeChange(
      ObjectInstanceHandle, AttributeHandleSet, TransportationType)
      RTI_THROW(FederateInternalError) override;
  virtual void reportAttributeTransportationType(
      ObjectInstanceHandle, AttributeHandle, TransportationType)
      RTI_THROW(FederateInternalError) override;
  virtual void confirmInteractionTransportationTypeChange(
      InteractionClassHandle, TransportationType)
      RTI_THROW(FederateInternalError) override;
  virtual void reportInteractionTransportationType(
      FederateHandle, InteractionClassHandle, TransportationType)
      RTI_THROW(FederateInternalError) override;

  // §7 Ownership Management — overrides.
  virtual void requestAttributeOwnershipAssumption(
      ObjectInstanceHandle, AttributeHandleSet const&,
      VariableLengthData const&)
      RTI_THROW(FederateInternalError) override;
  virtual void requestDivestitureConfirmation(
      ObjectInstanceHandle, AttributeHandleSet const&)
      RTI_THROW(FederateInternalError) override;
  virtual void attributeOwnershipAcquisitionNotification(
      ObjectInstanceHandle, AttributeHandleSet const&,
      VariableLengthData const&)
      RTI_THROW(FederateInternalError) override;
  virtual void attributeOwnershipUnavailable(
      ObjectInstanceHandle, AttributeHandleSet const&)
      RTI_THROW(FederateInternalError) override;
  virtual void requestAttributeOwnershipRelease(
      ObjectInstanceHandle, AttributeHandleSet const&,
      VariableLengthData const&)
      RTI_THROW(FederateInternalError) override;
  virtual void confirmAttributeOwnershipAcquisitionCancellation(
      ObjectInstanceHandle, AttributeHandleSet const&)
      RTI_THROW(FederateInternalError) override;
  virtual void informAttributeOwnership(ObjectInstanceHandle, AttributeHandle,
                                        FederateHandle)
      RTI_THROW(FederateInternalError) override;
  virtual void attributeIsNotOwned(ObjectInstanceHandle, AttributeHandle)
      RTI_THROW(FederateInternalError) override;
  virtual void attributeIsOwnedByRTI(ObjectInstanceHandle, AttributeHandle)
      RTI_THROW(FederateInternalError) override;

  // §8 Time Management — overrides.
  virtual void timeRegulationEnabled(LogicalTime const&)
      RTI_THROW(FederateInternalError) override;
  virtual void timeConstrainedEnabled(LogicalTime const&)
      RTI_THROW(FederateInternalError) override;
  virtual void timeAdvanceGrant(LogicalTime const&)
      RTI_THROW(FederateInternalError) override;

  virtual void requestRetraction(MessageRetractionHandle)
      RTI_THROW(FederateInternalError) override;
};

}  // namespace rti1516e

#endif  // RTI_NullFederateAmbassador_h
