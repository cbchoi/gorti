// Lockfile: full IEEE 1516.1-2010 Annex C exception hierarchy.
// Catalogue §6 row 6.3 — locks every spec-named exception class.
//
// M31 RED — this TU must fail to compile until M32+ lands the headers.
// Each missing exception class triggers a single static_assert / lookup
// failure tied to its own row, so the test harness can count
// `expected_red_count.txt` against this file's contribution.
//
// Class list extracted verbatim from
// IEEE 1516.1-2010 API reference: RTI/Exception.h
// 121 RTI_EXCEPTION(...) entries -> 121 static_asserts here.

#include <RTI/Exception.h>
#include <RTI/SpecificConfig.h>
#include <type_traits>
#include <stdexcept>  // for std::runtime_error used in static_assert below

namespace {

// Annex C — base contract: Exception is a class, not derived from std::runtime_error.
static_assert(std::is_class_v<rti1516e::Exception>);
static_assert(!std::is_base_of_v<std::runtime_error, rti1516e::Exception>);

// Per RTI_EXCEPTION(name) macro, every named exception must be a class
// publicly derived from rti1516e::Exception. One assertion per class.

#define LOCK_EXCEPTION(Name) \
  static_assert(std::is_class_v<rti1516e::Name>, \
                "spec exception class missing: " #Name); \
  static_assert(std::is_base_of_v<rti1516e::Exception, rti1516e::Name>, \
                "spec exception does not derive from Exception: " #Name)

LOCK_EXCEPTION(AlreadyConnected);
LOCK_EXCEPTION(AsynchronousDeliveryAlreadyDisabled);
LOCK_EXCEPTION(AsynchronousDeliveryAlreadyEnabled);
LOCK_EXCEPTION(AttributeAcquisitionWasNotCanceled);
LOCK_EXCEPTION(AttributeAcquisitionWasNotRequested);
LOCK_EXCEPTION(AttributeAlreadyBeingAcquired);
LOCK_EXCEPTION(AttributeAlreadyBeingChanged);
LOCK_EXCEPTION(AttributeAlreadyBeingDivested);
LOCK_EXCEPTION(AttributeAlreadyOwned);
LOCK_EXCEPTION(AttributeDivestitureWasNotRequested);
LOCK_EXCEPTION(AttributeNotDefined);
LOCK_EXCEPTION(AttributeNotOwned);
LOCK_EXCEPTION(AttributeNotPublished);
LOCK_EXCEPTION(AttributeNotRecognized);
LOCK_EXCEPTION(AttributeNotSubscribed);
LOCK_EXCEPTION(AttributeRelevanceAdvisorySwitchIsOff);
LOCK_EXCEPTION(AttributeRelevanceAdvisorySwitchIsOn);
LOCK_EXCEPTION(AttributeScopeAdvisorySwitchIsOff);
LOCK_EXCEPTION(AttributeScopeAdvisorySwitchIsOn);
LOCK_EXCEPTION(BadInitializationParameter);
LOCK_EXCEPTION(CallNotAllowedFromWithinCallback);
LOCK_EXCEPTION(ConnectionFailed);
LOCK_EXCEPTION(CouldNotCreateLogicalTimeFactory);
LOCK_EXCEPTION(CouldNotDecode);
LOCK_EXCEPTION(CouldNotDiscover);
LOCK_EXCEPTION(CouldNotEncode);
LOCK_EXCEPTION(CouldNotOpenFDD);
LOCK_EXCEPTION(CouldNotOpenMIM);
LOCK_EXCEPTION(CouldNotInitiateRestore);
LOCK_EXCEPTION(DeletePrivilegeNotHeld);
LOCK_EXCEPTION(DesignatorIsHLAstandardMIM);
LOCK_EXCEPTION(RequestForTimeConstrainedPending);
LOCK_EXCEPTION(NoRequestToEnableTimeConstrainedWasPending);
LOCK_EXCEPTION(RequestForTimeRegulationPending);
LOCK_EXCEPTION(NoRequestToEnableTimeRegulationWasPending);
LOCK_EXCEPTION(NoFederateWillingToAcquireAttribute);
LOCK_EXCEPTION(ErrorReadingFDD);
LOCK_EXCEPTION(ErrorReadingMIM);
LOCK_EXCEPTION(FederateAlreadyExecutionMember);
LOCK_EXCEPTION(FederateHandleNotKnown);
LOCK_EXCEPTION(FederateHasNotBegunSave);
LOCK_EXCEPTION(FederateInternalError);
LOCK_EXCEPTION(FederateIsExecutionMember);
LOCK_EXCEPTION(FederateNameAlreadyInUse);
LOCK_EXCEPTION(FederateNotExecutionMember);
LOCK_EXCEPTION(FederateOwnsAttributes);
LOCK_EXCEPTION(FederateServiceInvocationsAreBeingReportedViaMOM);
LOCK_EXCEPTION(FederateUnableToUseTime);
LOCK_EXCEPTION(FederatesCurrentlyJoined);
LOCK_EXCEPTION(FederationExecutionAlreadyExists);
LOCK_EXCEPTION(FederationExecutionDoesNotExist);
LOCK_EXCEPTION(IllegalName);
LOCK_EXCEPTION(IllegalTimeArithmetic);
LOCK_EXCEPTION(InconsistentFDD);
LOCK_EXCEPTION(InteractionClassAlreadyBeingChanged);
LOCK_EXCEPTION(InteractionClassNotDefined);
LOCK_EXCEPTION(InteractionClassNotPublished);
LOCK_EXCEPTION(InteractionClassNotRecognized);
LOCK_EXCEPTION(InteractionClassNotSubscribed);
LOCK_EXCEPTION(InteractionParameterNotDefined);
LOCK_EXCEPTION(InteractionParameterNotRecognized);
LOCK_EXCEPTION(InteractionRelevanceAdvisorySwitchIsOff);
LOCK_EXCEPTION(InteractionRelevanceAdvisorySwitchIsOn);
LOCK_EXCEPTION(InTimeAdvancingState);
LOCK_EXCEPTION(InvalidAttributeHandle);
LOCK_EXCEPTION(InvalidDimensionHandle);
LOCK_EXCEPTION(InvalidFederateHandle);
LOCK_EXCEPTION(InvalidInteractionClassHandle);
LOCK_EXCEPTION(InvalidLocalSettingsDesignator);
LOCK_EXCEPTION(InvalidLogicalTime);
LOCK_EXCEPTION(InvalidLogicalTimeInterval);
LOCK_EXCEPTION(InvalidLookahead);
LOCK_EXCEPTION(InvalidObjectClassHandle);
LOCK_EXCEPTION(InvalidOrderName);
LOCK_EXCEPTION(InvalidOrderType);
LOCK_EXCEPTION(InvalidParameterHandle);
LOCK_EXCEPTION(InvalidRangeBound);
LOCK_EXCEPTION(InvalidRegion);
LOCK_EXCEPTION(InvalidResignAction);
LOCK_EXCEPTION(InvalidRegionContext);
LOCK_EXCEPTION(InvalidMessageRetractionHandle);
LOCK_EXCEPTION(InvalidServiceGroup);
LOCK_EXCEPTION(InvalidTransportationName);
LOCK_EXCEPTION(InvalidTransportationType);
LOCK_EXCEPTION(InvalidUpdateRateDesignator);
LOCK_EXCEPTION(JoinedFederateIsNotInTimeAdvancingState);
LOCK_EXCEPTION(LogicalTimeAlreadyPassed);
LOCK_EXCEPTION(MessageCanNoLongerBeRetracted);
LOCK_EXCEPTION(NameNotFound);
LOCK_EXCEPTION(NameSetWasEmpty);
LOCK_EXCEPTION(NoAcquisitionPending);
LOCK_EXCEPTION(NotConnected);
LOCK_EXCEPTION(ObjectClassNotDefined);
LOCK_EXCEPTION(ObjectClassNotKnown);
LOCK_EXCEPTION(ObjectClassNotPublished);
LOCK_EXCEPTION(ObjectClassRelevanceAdvisorySwitchIsOff);
LOCK_EXCEPTION(ObjectClassRelevanceAdvisorySwitchIsOn);
LOCK_EXCEPTION(ObjectInstanceNameInUse);
LOCK_EXCEPTION(ObjectInstanceNameNotReserved);
LOCK_EXCEPTION(ObjectInstanceNotKnown);
LOCK_EXCEPTION(OwnershipAcquisitionPending);
LOCK_EXCEPTION(RTIinternalError);
LOCK_EXCEPTION(RegionDoesNotContainSpecifiedDimension);
LOCK_EXCEPTION(RegionInUseForUpdateOrSubscription);
LOCK_EXCEPTION(RegionNotCreatedByThisFederate);
LOCK_EXCEPTION(RestoreInProgress);
LOCK_EXCEPTION(RestoreNotInProgress);
LOCK_EXCEPTION(RestoreNotRequested);
LOCK_EXCEPTION(SaveInProgress);
LOCK_EXCEPTION(SaveNotInProgress);
LOCK_EXCEPTION(SaveNotInitiated);
LOCK_EXCEPTION(SpecifiedSaveLabelDoesNotExist);
LOCK_EXCEPTION(SynchronizationPointLabelNotAnnounced);
LOCK_EXCEPTION(TimeConstrainedAlreadyEnabled);
LOCK_EXCEPTION(TimeConstrainedIsNotEnabled);
LOCK_EXCEPTION(TimeRegulationAlreadyEnabled);
LOCK_EXCEPTION(TimeRegulationIsNotEnabled);
LOCK_EXCEPTION(UnableToPerformSave);
LOCK_EXCEPTION(UnknownName);
LOCK_EXCEPTION(UnsupportedCallbackModel);
LOCK_EXCEPTION(InternalError);

// Catalogue §6 row 6.4 — RTIinternalError must be a leaf, NOT the base of every
// other exception. (gorti currently has it as the base — this is the divergence
// that drives this assertion.)
static_assert(!std::is_base_of_v<rti1516e::RTIinternalError, rti1516e::ConnectionFailed>,
              "RTIinternalError must be a leaf per Annex C, not a common base");
static_assert(!std::is_base_of_v<rti1516e::RTIinternalError, rti1516e::ObjectInstanceNotKnown>,
              "RTIinternalError must be a leaf per Annex C, not a common base");

#undef LOCK_EXCEPTION

}  // namespace
