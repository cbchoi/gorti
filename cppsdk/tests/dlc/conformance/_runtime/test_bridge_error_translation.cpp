// M39 Agent HB — translateBridgeError prefix-table unit tests.
//
// Verifies the metadata-first exception pipeline at the DLC boundary:
// M17Bridge guard() emits `<AnnexCName>: <detail> [op=...]` (name taken
// from the server's rti-spec-exception trailer), and
// gorti::dlc::translateBridgeError must throw the precise
// <RTI/Exception.h> class for ANY Annex C name — plus keep the
// DEPRECATED detail sniffs and the RTIinternalError fallback alive for
// pre-M39 servers. No RTI subprocess, no gRPC.

#include <gtest/gtest.h>

#include <RTI/Exception.h>

#include <stdexcept>
#include <string>

#include "src/dlc/BridgeErrorTranslation.h"

using gorti::dlc::translateBridgeError;

namespace {

// Helper: run translateBridgeError on a message and expect exception T.
template <typename T>
void expectThrows(std::string const& what) {
  EXPECT_THROW(
      translateBridgeError(std::runtime_error(what)), T)
      << "message: " << what;
}

// ---- PRIMARY: Annex C prefix channel --------------------------------------

TEST(BridgeErrorTranslation, PrefixedSpecName_ThrowsPreciseType) {
  // Names with NO m17 typed class — only reachable via the M39
  // rti-spec-exception trailer → m17::SpecException carrier → guard()
  // prefix. Sample across service groups.
  expectThrows<rti1516e::InvalidLogicalTime>(
      "InvalidLogicalTime: sendInteraction: invalid logical time: TSO "
      "timestamp precedes current time plus lookahead [op=sendInteraction]");
  expectThrows<rti1516e::AttributeNotOwned>(
      "AttributeNotOwned: updateAttributeValues: attribute not owned by "
      "federate [op=updateAttributeValues]");
  expectThrows<rti1516e::InTimeAdvancingState>(
      "InTimeAdvancingState: nextMessageRequest: federate has an "
      "outstanding advance request [op=nextMessageRequest]");
  expectThrows<rti1516e::SynchronizationPointLabelNotAnnounced>(
      "SynchronizationPointLabelNotAnnounced: synchronizationPointAchieved: "
      "synchronization point not registered in federation [op=spa]");
  expectThrows<rti1516e::ObjectInstanceNameInUse>(
      "ObjectInstanceNameInUse: reserveObjectInstanceName: object instance "
      "name already reserved or in use [op=reserveObjectInstanceName]");
  expectThrows<rti1516e::DeletePrivilegeNotHeld>(
      "DeletePrivilegeNotHeld: deleteObjectInstance: object not owned by "
      "federate (cannot delete or change transport) [op=deleteObjectInstance]");
}

TEST(BridgeErrorTranslation, PrefixedM17TypedNames_StillPrecise) {
  // Names that DO have m17 typed classes — the pre-M39 vocabulary must
  // keep working unchanged through the new table.
  expectThrows<rti1516e::FederationExecutionAlreadyExists>(
      "FederationExecutionAlreadyExists: createFederationExecution: "
      "federation already exists [op=createFederationExecution]");
  expectThrows<rti1516e::NotConnected>(
      "NotConnected: joinFederationExecution: not connected [op=join]");
  expectThrows<rti1516e::NameNotFound>(
      "NameNotFound: getObjectClassHandle(Bogus): not found [op=goch]");
  expectThrows<rti1516e::ObjectClassNotPublished>(
      "ObjectClassNotPublished: registerObjectInstance: object class not "
      "published by federate [op=registerObjectInstance]");
}

TEST(BridgeErrorTranslation, PrefixMustAnchorAtPositionZero) {
  // A spec name mentioned mid-message is NOT a prefix — falls through
  // (here to the RTIinternalError fallback).
  expectThrows<rti1516e::RTIinternalError>(
      "RTIinternalError: server said InvalidLogicalTime: but folded [op=x]");
}

// ---- M37 EE special case ----------------------------------------------------

TEST(BridgeErrorTranslation, FederateNotExecutionMember_NestedPublicationSniffs) {
  // Legacy M17 clients fold generic FAILED_PRECONDITION under this
  // prefix; the §5 gate details must still win over the bare prefix.
  expectThrows<rti1516e::InteractionClassNotPublished>(
      "FederateNotExecutionMember: sendInteraction: interaction class not "
      "published [op=sendInteraction]");
  expectThrows<rti1516e::ObjectClassNotPublished>(
      "FederateNotExecutionMember: registerObjectInstance: object class not "
      "published by federate [op=registerObjectInstance]");
  expectThrows<rti1516e::FederateNotExecutionMember>(
      "FederateNotExecutionMember: resignFederationExecution: federate not "
      "joined [op=resignFederationExecution]");
}

// ---- DEPRECATED: detail sniffs (pre-M39 fallback) ---------------------------

TEST(BridgeErrorTranslation, LegacyDetailSniffs_StillAlive) {
  // Folded RTIinternalError carrying a recognizable errors.go detail —
  // exactly what a pre-M39 rtid produces.
  expectThrows<rti1516e::InvalidLookahead>(
      "RTIinternalError: enableTimeRegulation: lookahead must be "
      "non-negative and finite [op=enableTimeRegulation]");
  expectThrows<rti1516e::LogicalTimeAlreadyPassed>(
      "RTIinternalError: timeAdvanceRequest: requested time is not greater "
      "than current logical time [op=timeAdvanceRequest]");
  expectThrows<rti1516e::InvalidLogicalTime>(
      "RTIinternalError: sendInteraction: invalid logical time: TSO "
      "timestamp precedes current time plus lookahead [op=sendInteraction]");
  expectThrows<rti1516e::InteractionClassNotPublished>(
      "RTIinternalError: sendInteraction: interaction class not published "
      "[op=sendInteraction]");
}

// ---- Fallback ---------------------------------------------------------------

TEST(BridgeErrorTranslation, UnprefixedUnsniffed_FoldsToRTIinternalError) {
  expectThrows<rti1516e::RTIinternalError>("something inscrutable happened");
  expectThrows<rti1516e::RTIinternalError>(
      "StdException: transport is on fire [op=connect]");
  expectThrows<rti1516e::RTIinternalError>("");
}

TEST(BridgeErrorTranslation, MessageTextSurvivesTranslation) {
  const std::string what =
      "InvalidLookahead: modifyLookahead: lookahead must be non-negative "
      "and finite [op=modifyLookahead]";
  try {
    translateBridgeError(std::runtime_error(what));
    FAIL() << "expected InvalidLookahead";
  } catch (rti1516e::InvalidLookahead const& e) {
    // DLC exceptions carry wstring; byte-widened copy must match.
    std::wstring w(what.begin(), what.end());
    EXPECT_EQ(e.what(), w);
  }
}

}  // namespace
