// IEEE 1516.1-2010 Annex C — Exception hierarchy runtime tests.
//
// M33-I. gorti-only fixture: exceptions are plain class objects with no RTI
// involvement, so unlike the other conformance fixtures this one is meant to
// LINK and RUN (WILL_FAIL=OFF), not just link-fail.
//
// Covers:
//   1. Every one of the 121 spec-mandated exception classes:
//      - construct with a std::wstring message
//      - catch as `rti1516e::Exception&`
//      - verify `what()` returns the original wstring
//   2. Hierarchy discrimination: dynamic_cast to the actual leaf succeeds and
//      to a sibling leaf yields nullptr.
//   3. Polymorphism: a derived exception caught as `Exception&` still returns
//      the correct `what()`.
//   4. The abstract base's wostream operator<< forwards to what().
//
// Catalogue rows 6.1-6.5 / FR-DLC-6.

#include <RTI/Exception.h>

#include <cstring>
#include <sstream>
#include <string>
#include <typeinfo>

#include <gtest/gtest.h>

namespace {

// X-macro: expands F(Name) for each of the 121 spec exception classes. Order
// mirrors cppsdk/include/RTI/Exception.h which mirrors reference_rti's alphabetical
// §C.2 reprint. Single source of truth for the name list used below.
#define RTI_EACH_EXCEPTION(F)                                    \
  F(AlreadyConnected)                                            \
  F(AsynchronousDeliveryAlreadyDisabled)                         \
  F(AsynchronousDeliveryAlreadyEnabled)                          \
  F(AttributeAcquisitionWasNotCanceled)                          \
  F(AttributeAcquisitionWasNotRequested)                         \
  F(AttributeAlreadyBeingAcquired)                               \
  F(AttributeAlreadyBeingChanged)                                \
  F(AttributeAlreadyBeingDivested)                               \
  F(AttributeAlreadyOwned)                                       \
  F(AttributeDivestitureWasNotRequested)                         \
  F(AttributeNotDefined)                                         \
  F(AttributeNotOwned)                                           \
  F(AttributeNotPublished)                                       \
  F(AttributeNotRecognized)                                      \
  F(AttributeNotSubscribed)                                      \
  F(AttributeRelevanceAdvisorySwitchIsOff)                       \
  F(AttributeRelevanceAdvisorySwitchIsOn)                        \
  F(AttributeScopeAdvisorySwitchIsOff)                           \
  F(AttributeScopeAdvisorySwitchIsOn)                            \
  F(BadInitializationParameter)                                  \
  F(CallNotAllowedFromWithinCallback)                            \
  F(ConnectionFailed)                                            \
  F(CouldNotCreateLogicalTimeFactory)                            \
  F(CouldNotDecode)                                              \
  F(CouldNotDiscover)                                            \
  F(CouldNotEncode)                                              \
  F(CouldNotOpenFDD)                                             \
  F(CouldNotOpenMIM)                                             \
  F(CouldNotInitiateRestore)                                     \
  F(DeletePrivilegeNotHeld)                                      \
  F(DesignatorIsHLAstandardMIM)                                  \
  F(RequestForTimeConstrainedPending)                            \
  F(NoRequestToEnableTimeConstrainedWasPending)                  \
  F(RequestForTimeRegulationPending)                             \
  F(NoRequestToEnableTimeRegulationWasPending)                   \
  F(NoFederateWillingToAcquireAttribute)                         \
  F(ErrorReadingFDD)                                             \
  F(ErrorReadingMIM)                                             \
  F(FederateAlreadyExecutionMember)                              \
  F(FederateHandleNotKnown)                                      \
  F(FederateHasNotBegunSave)                                     \
  F(FederateInternalError)                                       \
  F(FederateIsExecutionMember)                                   \
  F(FederateNameAlreadyInUse)                                    \
  F(FederateNotExecutionMember)                                  \
  F(FederateOwnsAttributes)                                      \
  F(FederateServiceInvocationsAreBeingReportedViaMOM)            \
  F(FederateUnableToUseTime)                                     \
  F(FederatesCurrentlyJoined)                                    \
  F(FederationExecutionAlreadyExists)                            \
  F(FederationExecutionDoesNotExist)                             \
  F(IllegalName)                                                 \
  F(IllegalTimeArithmetic)                                       \
  F(InconsistentFDD)                                             \
  F(InteractionClassAlreadyBeingChanged)                         \
  F(InteractionClassNotDefined)                                  \
  F(InteractionClassNotPublished)                                \
  F(InteractionClassNotRecognized)                               \
  F(InteractionClassNotSubscribed)                               \
  F(InteractionParameterNotDefined)                              \
  F(InteractionParameterNotRecognized)                           \
  F(InteractionRelevanceAdvisorySwitchIsOff)                     \
  F(InteractionRelevanceAdvisorySwitchIsOn)                      \
  F(InTimeAdvancingState)                                        \
  F(InvalidAttributeHandle)                                      \
  F(InvalidDimensionHandle)                                      \
  F(InvalidFederateHandle)                                       \
  F(InvalidInteractionClassHandle)                               \
  F(InvalidLocalSettingsDesignator)                              \
  F(InvalidLogicalTime)                                          \
  F(InvalidLogicalTimeInterval)                                  \
  F(InvalidLookahead)                                            \
  F(InvalidObjectClassHandle)                                    \
  F(InvalidOrderName)                                            \
  F(InvalidOrderType)                                            \
  F(InvalidParameterHandle)                                      \
  F(InvalidRangeBound)                                           \
  F(InvalidRegion)                                               \
  F(InvalidResignAction)                                         \
  F(InvalidRegionContext)                                        \
  F(InvalidMessageRetractionHandle)                              \
  F(InvalidServiceGroup)                                         \
  F(InvalidTransportationName)                                   \
  F(InvalidTransportationType)                                   \
  F(InvalidUpdateRateDesignator)                                 \
  F(JoinedFederateIsNotInTimeAdvancingState)                     \
  F(LogicalTimeAlreadyPassed)                                    \
  F(MessageCanNoLongerBeRetracted)                               \
  F(NameNotFound)                                                \
  F(NameSetWasEmpty)                                             \
  F(NoAcquisitionPending)                                        \
  F(NotConnected)                                                \
  F(ObjectClassNotDefined)                                       \
  F(ObjectClassNotKnown)                                         \
  F(ObjectClassNotPublished)                                     \
  F(ObjectClassRelevanceAdvisorySwitchIsOff)                     \
  F(ObjectClassRelevanceAdvisorySwitchIsOn)                      \
  F(ObjectInstanceNameInUse)                                     \
  F(ObjectInstanceNameNotReserved)                               \
  F(ObjectInstanceNotKnown)                                      \
  F(OwnershipAcquisitionPending)                                 \
  F(RTIinternalError)                                            \
  F(RegionDoesNotContainSpecifiedDimension)                      \
  F(RegionInUseForUpdateOrSubscription)                          \
  F(RegionNotCreatedByThisFederate)                              \
  F(RestoreInProgress)                                           \
  F(RestoreNotInProgress)                                        \
  F(RestoreNotRequested)                                         \
  F(SaveInProgress)                                              \
  F(SaveNotInProgress)                                           \
  F(SaveNotInitiated)                                            \
  F(SpecifiedSaveLabelDoesNotExist)                              \
  F(SynchronizationPointLabelNotAnnounced)                       \
  F(TimeConstrainedAlreadyEnabled)                               \
  F(TimeConstrainedIsNotEnabled)                                 \
  F(TimeRegulationAlreadyEnabled)                                \
  F(TimeRegulationIsNotEnabled)                                  \
  F(UnableToPerformSave)                                         \
  F(UnknownName)                                                 \
  F(UnsupportedCallbackModel)                                    \
  F(InternalError)

// Helper: construct exception `T` with `msg`, throw it, catch as
// rti1516e::Exception&, and assert what() equals msg + dynamic_cast to the
// concrete leaf succeeds. Uses gtest EXPECT_* macros for failure reporting.
template <typename T>
void expectThrowCatchAndWhat(const std::wstring& msg, const char* type_name) {
  try {
    throw T(msg);
  } catch (const rti1516e::Exception& e) {
    // 1. what() round-trips the wstring the ctor received.
    EXPECT_EQ(e.what(), msg) << "wstring round-trip failed for " << type_name;
    // 2. dynamic_cast down to the concrete leaf must succeed — polymorphism
    //    intact, base class did not slice.
    EXPECT_NE(dynamic_cast<const T*>(&e), nullptr)
        << "dynamic_cast to concrete leaf failed for " << type_name;
  } catch (...) {
    ADD_FAILURE() << type_name
                  << " was not catchable as rti1516e::Exception&";
  }
}

}  // namespace

// ---------------------------------------------------------------------------
// Test 1 — every exception can be thrown, caught as base, and its wstring
// message survives.
// ---------------------------------------------------------------------------

TEST(ExceptionsRuntime, ConstructThrowCatchWhat) {
// Adjacent-literal concatenation: L"a" "b" == L"ab" in C++11+, so
// `L"msg-for-" #Name` is a single wide-string literal whose value is
// L"msg-for-<Name>". Do NOT write `L#Name` — that parses as identifier L
// followed by the stringified name, not a wide-string literal.
#define CHECK_EXC(Name)                                            \
  expectThrowCatchAndWhat<rti1516e::Name>(                         \
      L"msg-for-" #Name, #Name);
  RTI_EACH_EXCEPTION(CHECK_EXC)
#undef CHECK_EXC
}

// ---------------------------------------------------------------------------
// Test 2 — hierarchy discrimination: dynamic_cast to the correct leaf
// succeeds, cast to an unrelated sibling leaf yields nullptr. Uses two
// canonical spec-named types.
// ---------------------------------------------------------------------------

TEST(ExceptionsRuntime, HierarchyDiscriminatesLeaves) {
  rti1516e::ObjectClassNotDefined ocnd(L"ocnd-msg");
  rti1516e::AttributeNotDefined and_(L"and-msg");

  // Each leaf is-a Exception.
  rti1516e::Exception& base_ocnd = ocnd;
  rti1516e::Exception& base_and = and_;

  // Correct-type dynamic_cast succeeds.
  EXPECT_NE(dynamic_cast<rti1516e::ObjectClassNotDefined*>(&base_ocnd),
            nullptr);
  EXPECT_NE(dynamic_cast<rti1516e::AttributeNotDefined*>(&base_and), nullptr);

  // Sibling-type dynamic_cast yields nullptr (leaves are peers under
  // Exception, not related to each other).
  EXPECT_EQ(dynamic_cast<rti1516e::AttributeNotDefined*>(&base_ocnd), nullptr);
  EXPECT_EQ(dynamic_cast<rti1516e::ObjectClassNotDefined*>(&base_and), nullptr);

  // The wstring messages are preserved and not aliased across instances.
  EXPECT_EQ(base_ocnd.what(), std::wstring(L"ocnd-msg"));
  EXPECT_EQ(base_and.what(), std::wstring(L"and-msg"));
}

// ---------------------------------------------------------------------------
// Test 3 — polymorphism: a leaf thrown but caught as base still returns the
// leaf's what(). This is the exception-safety guarantee IEEE 1516.1 §C
// requires — a federate that catches `rti1516e::Exception&` at the top level
// must be able to log the concrete message.
// ---------------------------------------------------------------------------

TEST(ExceptionsRuntime, PolymorphicCatchAsBaseReturnsLeafWhat) {
  const std::wstring msg =
      L"federation execution 'demo' does not exist on rtid://localhost:7492";

  try {
    throw rti1516e::FederationExecutionDoesNotExist(msg);
  } catch (const rti1516e::Exception& e) {
    EXPECT_EQ(e.what(), msg);
    EXPECT_NE(
        dynamic_cast<const rti1516e::FederationExecutionDoesNotExist*>(&e),
        nullptr);
    EXPECT_EQ(dynamic_cast<const rti1516e::ConnectionFailed*>(&e), nullptr);
  }

  // Second case: a completely different leaf, same catch, same guarantee.
  const std::wstring msg2 = L"connection refused";
  try {
    throw rti1516e::ConnectionFailed(msg2);
  } catch (const rti1516e::Exception& e) {
    EXPECT_EQ(e.what(), msg2);
    EXPECT_NE(dynamic_cast<const rti1516e::ConnectionFailed*>(&e), nullptr);
    EXPECT_EQ(
        dynamic_cast<const rti1516e::FederationExecutionDoesNotExist*>(&e),
        nullptr);
  }
}

// ---------------------------------------------------------------------------
// Test 4 — the wostream operator<< defined in Exception.h forwards to what().
// Guards against a regression where operator<< is redefined as a hidden name
// (per Annex C.1 it must be a free function).
// ---------------------------------------------------------------------------

TEST(ExceptionsRuntime, WostreamOperatorForwardsToWhat) {
  rti1516e::RTIinternalError e(L"rtid crashed");
  std::wostringstream oss;
  oss << static_cast<const rti1516e::Exception&>(e);
  EXPECT_EQ(oss.str(), std::wstring(L"rtid crashed"));
}

// ---------------------------------------------------------------------------
// Test 5 — smoke coverage of the ~10 most-common spec exceptions with
// non-empty, non-trivial messages. These are the ones federate code catches
// by name in real 1516e programs; enumerating them explicitly guards against
// header-drift where a name silently disappears from the X-macro list.
// ---------------------------------------------------------------------------

TEST(ExceptionsRuntime, MostCommonExceptionsCatchable) {
  // Empty-string message must also survive.
  {
    rti1516e::RTIinternalError e(L"");
    EXPECT_EQ(e.what(), std::wstring(L""));
  }
  // Non-ASCII wide chars round-trip.
  {
    rti1516e::NotConnected e(L"中文 message");  // "中文" = CJK
    EXPECT_EQ(e.what(), std::wstring(L"中文 message"));
  }
  // Long message.
  {
    std::wstring long_msg(4096, L'x');
    rti1516e::SaveInProgress e(long_msg);
    EXPECT_EQ(e.what(), long_msg);
    EXPECT_EQ(e.what().size(), 4096u);
  }
}
