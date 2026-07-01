// IEEE 1516.1-2010 §10.42 / Annex A — NullFederateAmbassador default-no-op impls.
//
// gorti M32. All overrides are `{}` — federate code overrides only the
// callbacks it cares about.

#include <RTI/NullFederateAmbassador.h>

namespace rti1516e {

NullFederateAmbassador::NullFederateAmbassador() = default;
NullFederateAmbassador::~NullFederateAmbassador() RTI_NOEXCEPT = default;

// § 4 Federation Management.
void NullFederateAmbassador::connectionLost(std::wstring const&) {}
void NullFederateAmbassador::reportFederationExecutions(
    FederationExecutionInformationVector const&) {}
void NullFederateAmbassador::synchronizationPointRegistrationSucceeded(
    std::wstring const&) {}
void NullFederateAmbassador::synchronizationPointRegistrationFailed(
    std::wstring const&, SynchronizationPointFailureReason) {}
void NullFederateAmbassador::announceSynchronizationPoint(
    std::wstring const&, VariableLengthData const&) {}
void NullFederateAmbassador::federationSynchronized(
    std::wstring const&, FederateHandleSet const&) {}
void NullFederateAmbassador::initiateFederateSave(std::wstring const&) {}
void NullFederateAmbassador::initiateFederateSave(std::wstring const&,
                                                   LogicalTime const&) {}
void NullFederateAmbassador::federationSaved() {}
void NullFederateAmbassador::federationNotSaved(SaveFailureReason) {}
void NullFederateAmbassador::federationSaveStatusResponse(
    FederateHandleSaveStatusPairVector const&) {}
void NullFederateAmbassador::requestFederationRestoreSucceeded(
    std::wstring const&) {}
void NullFederateAmbassador::requestFederationRestoreFailed(
    std::wstring const&) {}
void NullFederateAmbassador::federationRestoreBegun() {}
void NullFederateAmbassador::initiateFederateRestore(std::wstring const&,
                                                     std::wstring const&,
                                                     FederateHandle) {}
void NullFederateAmbassador::federationRestored() {}
void NullFederateAmbassador::federationNotRestored(RestoreFailureReason) {}
void NullFederateAmbassador::federationRestoreStatusResponse(
    FederateRestoreStatusVector const&) {}

// § 5 Declaration Management.
void NullFederateAmbassador::startRegistrationForObjectClass(
    ObjectClassHandle) {}
void NullFederateAmbassador::stopRegistrationForObjectClass(ObjectClassHandle) {}
void NullFederateAmbassador::turnInteractionsOn(InteractionClassHandle) {}
void NullFederateAmbassador::turnInteractionsOff(InteractionClassHandle) {}

// § 6 Object Management.
void NullFederateAmbassador::objectInstanceNameReservationSucceeded(
    std::wstring const&) {}
void NullFederateAmbassador::objectInstanceNameReservationFailed(
    std::wstring const&) {}
void NullFederateAmbassador::multipleObjectInstanceNameReservationSucceeded(
    std::set<std::wstring> const&) {}
void NullFederateAmbassador::multipleObjectInstanceNameReservationFailed(
    std::set<std::wstring> const&) {}

void NullFederateAmbassador::discoverObjectInstance(ObjectInstanceHandle,
                                                    ObjectClassHandle,
                                                    std::wstring const&) {}
void NullFederateAmbassador::discoverObjectInstance(ObjectInstanceHandle,
                                                    ObjectClassHandle,
                                                    std::wstring const&,
                                                    FederateHandle) {}

void NullFederateAmbassador::reflectAttributeValues(
    ObjectInstanceHandle, AttributeHandleValueMap const&,
    VariableLengthData const&, OrderType, TransportationType,
    SupplementalReflectInfo) {}
void NullFederateAmbassador::reflectAttributeValues(
    ObjectInstanceHandle, AttributeHandleValueMap const&,
    VariableLengthData const&, OrderType, TransportationType,
    LogicalTime const&, OrderType, SupplementalReflectInfo) {}
void NullFederateAmbassador::reflectAttributeValues(
    ObjectInstanceHandle, AttributeHandleValueMap const&,
    VariableLengthData const&, OrderType, TransportationType,
    LogicalTime const&, OrderType, MessageRetractionHandle,
    SupplementalReflectInfo) {}

void NullFederateAmbassador::receiveInteraction(
    InteractionClassHandle, ParameterHandleValueMap const&,
    VariableLengthData const&, OrderType, TransportationType,
    SupplementalReceiveInfo) {}
void NullFederateAmbassador::receiveInteraction(
    InteractionClassHandle, ParameterHandleValueMap const&,
    VariableLengthData const&, OrderType, TransportationType,
    LogicalTime const&, OrderType, SupplementalReceiveInfo) {}
void NullFederateAmbassador::receiveInteraction(
    InteractionClassHandle, ParameterHandleValueMap const&,
    VariableLengthData const&, OrderType, TransportationType,
    LogicalTime const&, OrderType, MessageRetractionHandle,
    SupplementalReceiveInfo) {}

void NullFederateAmbassador::removeObjectInstance(ObjectInstanceHandle,
                                                  VariableLengthData const&,
                                                  OrderType,
                                                  SupplementalRemoveInfo) {}
void NullFederateAmbassador::removeObjectInstance(ObjectInstanceHandle,
                                                  VariableLengthData const&,
                                                  OrderType,
                                                  LogicalTime const&, OrderType,
                                                  SupplementalRemoveInfo) {}
void NullFederateAmbassador::removeObjectInstance(
    ObjectInstanceHandle, VariableLengthData const&, OrderType,
    LogicalTime const&, OrderType, MessageRetractionHandle,
    SupplementalRemoveInfo) {}

void NullFederateAmbassador::attributesInScope(ObjectInstanceHandle,
                                               AttributeHandleSet const&) {}
void NullFederateAmbassador::attributesOutOfScope(ObjectInstanceHandle,
                                                  AttributeHandleSet const&) {}
void NullFederateAmbassador::provideAttributeValueUpdate(
    ObjectInstanceHandle, AttributeHandleSet const&, VariableLengthData const&) {
}
void NullFederateAmbassador::turnUpdatesOnForObjectInstance(
    ObjectInstanceHandle, AttributeHandleSet const&) {}
void NullFederateAmbassador::turnUpdatesOnForObjectInstance(
    ObjectInstanceHandle, AttributeHandleSet const&, std::wstring const&) {}
void NullFederateAmbassador::turnUpdatesOffForObjectInstance(
    ObjectInstanceHandle, AttributeHandleSet const&) {}

void NullFederateAmbassador::confirmAttributeTransportationTypeChange(
    ObjectInstanceHandle, AttributeHandleSet, TransportationType) {}
void NullFederateAmbassador::reportAttributeTransportationType(
    ObjectInstanceHandle, AttributeHandle, TransportationType) {}
void NullFederateAmbassador::confirmInteractionTransportationTypeChange(
    InteractionClassHandle, TransportationType) {}
void NullFederateAmbassador::reportInteractionTransportationType(
    FederateHandle, InteractionClassHandle, TransportationType) {}

// § 7 Ownership Management.
void NullFederateAmbassador::requestAttributeOwnershipAssumption(
    ObjectInstanceHandle, AttributeHandleSet const&,
    VariableLengthData const&) {}
void NullFederateAmbassador::requestDivestitureConfirmation(
    ObjectInstanceHandle, AttributeHandleSet const&) {}
void NullFederateAmbassador::attributeOwnershipAcquisitionNotification(
    ObjectInstanceHandle, AttributeHandleSet const&,
    VariableLengthData const&) {}
void NullFederateAmbassador::attributeOwnershipUnavailable(
    ObjectInstanceHandle, AttributeHandleSet const&) {}
void NullFederateAmbassador::requestAttributeOwnershipRelease(
    ObjectInstanceHandle, AttributeHandleSet const&,
    VariableLengthData const&) {}
void NullFederateAmbassador::confirmAttributeOwnershipAcquisitionCancellation(
    ObjectInstanceHandle, AttributeHandleSet const&) {}
void NullFederateAmbassador::informAttributeOwnership(ObjectInstanceHandle,
                                                      AttributeHandle,
                                                      FederateHandle) {}
void NullFederateAmbassador::attributeIsNotOwned(ObjectInstanceHandle,
                                                 AttributeHandle) {}
void NullFederateAmbassador::attributeIsOwnedByRTI(ObjectInstanceHandle,
                                                   AttributeHandle) {}

// § 8 Time Management.
void NullFederateAmbassador::timeRegulationEnabled(LogicalTime const&) {}
void NullFederateAmbassador::timeConstrainedEnabled(LogicalTime const&) {}
void NullFederateAmbassador::timeAdvanceGrant(LogicalTime const&) {}

void NullFederateAmbassador::requestRetraction(MessageRetractionHandle) {}

}  // namespace rti1516e
