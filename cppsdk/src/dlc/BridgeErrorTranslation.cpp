// M39 — bridge-error → DLC spec-exception translation (impl).
//
// See BridgeErrorTranslation.h for the pipeline contract. The prefix
// vocabulary began with the eight M17 typed exceptions, grew detail
// matching at M37, and became the full
// Annex C class list at M39 when the server started declaring the spec
// exception via the `rti-spec-exception` gRPC trailer.

#include "BridgeErrorTranslation.h"

#include <RTI/Exception.h>

#include <string>

namespace gorti {
namespace dlc {

namespace {

// narrow → wide, byte-widening (identical policy to RTIambassadorImpl's
// s2ws: error text is ASCII from errors.go; high bytes pass through).
std::wstring widen(std::string const& s) {
  std::wstring w;
  w.reserve(s.size());
  for (char c : s) w.push_back(static_cast<wchar_t>(static_cast<unsigned char>(c)));
  return w;
}

}  // namespace

// GORTI_ANNEXC_EXCEPTIONS — X-list of every RTI_EXCEPTION(...) leaf in
// <RTI/Exception.h> (IEEE 1516.1-2010 Annex C) EXCEPT RTIinternalError.
// That one is deliberately absent: pre-M39 servers' rejections arrive
// folded under the `RTIinternalError: ` prefix with the real cause only
// in the detail text, so matching it here would bypass the DEPRECATED
// detail sniffs below — and the final fallback throws RTIinternalError
// anyway. Regenerate with:
//   grep -E '^RTI_EXCEPTION\(' cppsdk/include/RTI/Exception.h
#define GORTI_ANNEXC_EXCEPTIONS(X) \
  X(AlreadyConnected) \
  X(AsynchronousDeliveryAlreadyDisabled) \
  X(AsynchronousDeliveryAlreadyEnabled) \
  X(AttributeAcquisitionWasNotCanceled) \
  X(AttributeAcquisitionWasNotRequested) \
  X(AttributeAlreadyBeingAcquired) \
  X(AttributeAlreadyBeingChanged) \
  X(AttributeAlreadyBeingDivested) \
  X(AttributeAlreadyOwned) \
  X(AttributeDivestitureWasNotRequested) \
  X(AttributeNotDefined) \
  X(AttributeNotOwned) \
  X(AttributeNotPublished) \
  X(AttributeNotRecognized) \
  X(AttributeNotSubscribed) \
  X(AttributeRelevanceAdvisorySwitchIsOff) \
  X(AttributeRelevanceAdvisorySwitchIsOn) \
  X(AttributeScopeAdvisorySwitchIsOff) \
  X(AttributeScopeAdvisorySwitchIsOn) \
  X(BadInitializationParameter) \
  X(CallNotAllowedFromWithinCallback) \
  X(ConnectionFailed) \
  X(CouldNotCreateLogicalTimeFactory) \
  X(CouldNotDecode) \
  X(CouldNotDiscover) \
  X(CouldNotEncode) \
  X(CouldNotOpenFDD) \
  X(CouldNotOpenMIM) \
  X(CouldNotInitiateRestore) \
  X(DeletePrivilegeNotHeld) \
  X(DesignatorIsHLAstandardMIM) \
  X(RequestForTimeConstrainedPending) \
  X(NoRequestToEnableTimeConstrainedWasPending) \
  X(RequestForTimeRegulationPending) \
  X(NoRequestToEnableTimeRegulationWasPending) \
  X(NoFederateWillingToAcquireAttribute) \
  X(ErrorReadingFDD) \
  X(ErrorReadingMIM) \
  X(FederateAlreadyExecutionMember) \
  X(FederateHandleNotKnown) \
  X(FederateHasNotBegunSave) \
  X(FederateInternalError) \
  X(FederateIsExecutionMember) \
  X(FederateNameAlreadyInUse) \
  X(FederateNotExecutionMember) \
  X(FederateOwnsAttributes) \
  X(FederateServiceInvocationsAreBeingReportedViaMOM) \
  X(FederateUnableToUseTime) \
  X(FederatesCurrentlyJoined) \
  X(FederationExecutionAlreadyExists) \
  X(FederationExecutionDoesNotExist) \
  X(IllegalName) \
  X(IllegalTimeArithmetic) \
  X(InconsistentFDD) \
  X(InteractionClassAlreadyBeingChanged) \
  X(InteractionClassNotDefined) \
  X(InteractionClassNotPublished) \
  X(InteractionClassNotRecognized) \
  X(InteractionClassNotSubscribed) \
  X(InteractionParameterNotDefined) \
  X(InteractionParameterNotRecognized) \
  X(InteractionRelevanceAdvisorySwitchIsOff) \
  X(InteractionRelevanceAdvisorySwitchIsOn) \
  X(InTimeAdvancingState) \
  X(InvalidAttributeHandle) \
  X(InvalidDimensionHandle) \
  X(InvalidFederateHandle) \
  X(InvalidInteractionClassHandle) \
  X(InvalidLocalSettingsDesignator) \
  X(InvalidLogicalTime) \
  X(InvalidLogicalTimeInterval) \
  X(InvalidLookahead) \
  X(InvalidObjectClassHandle) \
  X(InvalidOrderName) \
  X(InvalidOrderType) \
  X(InvalidParameterHandle) \
  X(InvalidRangeBound) \
  X(InvalidRegion) \
  X(InvalidResignAction) \
  X(InvalidRegionContext) \
  X(InvalidMessageRetractionHandle) \
  X(InvalidServiceGroup) \
  X(InvalidTransportationName) \
  X(InvalidTransportationType) \
  X(InvalidUpdateRateDesignator) \
  X(JoinedFederateIsNotInTimeAdvancingState) \
  X(LogicalTimeAlreadyPassed) \
  X(MessageCanNoLongerBeRetracted) \
  X(NameNotFound) \
  X(NameSetWasEmpty) \
  X(NoAcquisitionPending) \
  X(NotConnected) \
  X(ObjectClassNotDefined) \
  X(ObjectClassNotKnown) \
  X(ObjectClassNotPublished) \
  X(ObjectClassRelevanceAdvisorySwitchIsOff) \
  X(ObjectClassRelevanceAdvisorySwitchIsOn) \
  X(ObjectInstanceNameInUse) \
  X(ObjectInstanceNameNotReserved) \
  X(ObjectInstanceNotKnown) \
  X(OwnershipAcquisitionPending) \
  X(RegionDoesNotContainSpecifiedDimension) \
  X(RegionInUseForUpdateOrSubscription) \
  X(RegionNotCreatedByThisFederate) \
  X(RestoreInProgress) \
  X(RestoreNotInProgress) \
  X(RestoreNotRequested) \
  X(SaveInProgress) \
  X(SaveNotInProgress) \
  X(SaveNotInitiated) \
  X(SpecifiedSaveLabelDoesNotExist) \
  X(SynchronizationPointLabelNotAnnounced) \
  X(TimeConstrainedAlreadyEnabled) \
  X(TimeConstrainedIsNotEnabled) \
  X(TimeRegulationAlreadyEnabled) \
  X(TimeRegulationIsNotEnabled) \
  X(UnableToPerformSave) \
  X(UnknownName) \
  X(UnsupportedCallbackModel) \
  X(InternalError)
// end of Annex C X-list (120 names = 121 leaves minus RTIinternalError)

[[noreturn]] void translateBridgeError(std::runtime_error const& e) {
  using namespace rti1516e;  // <RTI/Exception.h> spec types (DLC set).

  std::string what = e.what();
  std::wstring msg = widen(what);

  // ---- 1. Special-cased prefix: FederateNotExecutionMember ---------------
  // M37 EE: pre-M39 M17 clients fold generic FAILED_PRECONDITION
  // rejections under this prefix, so the §5 publication-gate details
  // must be sniffed FIRST — a plain prefix match would misreport
  // ObjectClassNotPublished / InteractionClassNotPublished failures.
  // (An M39 server never hits this: its trailer names the precise
  // class, so the prefix is already ObjectClassNotPublished etc.)
  if (what.rfind("FederateNotExecutionMember:", 0) == 0) {
    if (what.find("interaction class not published") != std::string::npos)
      throw InteractionClassNotPublished(msg);
    if (what.find("not published") != std::string::npos)
      throw ObjectClassNotPublished(msg);
    throw FederateNotExecutionMember(msg);
  }

  // ---- 2. PRIMARY: Annex C prefix table ----------------------------------
  // M39 metadata-first channel: M17Bridge guard() emits
  // `<AnnexCName>: <detail> [op=...]` where the name came from the
  // server's rti-spec-exception trailer (or from an M17 typed class of
  // the same name). One branch per Annex C leaf class.
#define GORTI_TRY_PREFIX(Name) \
  if (what.rfind(#Name ":", 0) == 0) throw Name(msg);
  GORTI_ANNEXC_EXCEPTIONS(GORTI_TRY_PREFIX)
#undef GORTI_TRY_PREFIX

  // ---- 3. DEPRECATED: detail-string sniffs (legacy fallback) -------------
  // Pre-M39 servers only. The M17 client folds most rejections into
  // RTIinternalError with the gRPC status message as detail; these
  // substrings are pinned to rti/internal/core/errors.go (see
  // cppsdk/src/dlc/README.md). Order matters: most specific first. Do
  // NOT extend this table — new exception coverage belongs in the
  // server-side spec-exception table (rti/internal/transport/grpc/
  // spec_exception.go), which feeds the prefix channel above.
  auto contains = [&what](char const* needle) {
    return what.find(needle) != std::string::npos;
  };
  // §5/§6 publication gates.
  if (contains("interaction class not published"))
    throw InteractionClassNotPublished(msg);
  if (contains("not published")) throw ObjectClassNotPublished(msg);
  // §8 time gates.
  if (contains("lookahead must be non-negative"))
    throw InvalidLookahead(msg);
  if (contains("requested time is not greater than current logical time"))
    throw LogicalTimeAlreadyPassed(msg);
  if (contains("invalid logical time") || contains("lookahead"))
    throw InvalidLogicalTime(msg);

  // ---- 4. Fallback --------------------------------------------------------
  // Every other case (including bare RTIinternalError) folds to the
  // spec-legal RTIinternalError catch-all.
  throw RTIinternalError(msg);
}

}  // namespace dlc
}  // namespace gorti
