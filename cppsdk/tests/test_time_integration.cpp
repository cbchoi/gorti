// M17.11 — §8 Time Management integration tests.
//
// Drives the gorti TimeService against a live rtid subprocess. The
// scenarios mirror the pysdk M27 Phase G suite — a regulating
// federate advances via TAR and observes the grant callback; a
// constrained-only federate waits on its regulating peer's lookahead.

#include <gtest/gtest.h>

#include <atomic>
#include <chrono>
#include <string>
#include <thread>
#include <vector>

#include "rti1516e/Exceptions.h"
#include "rti1516e/FederateAmbassador.h"
#include "rti1516e/RtiAmbassador.h"

#include "fixtures/RtidProcess.h"

namespace {

using rti1516e_test::RtidProcess;
const std::string kFomPath = TEST_FOM_PATH;

class GrantRecordingFed : public rti1516e::FederateAmbassador {
 public:
  std::vector<double> grants;
  void timeAdvanceGrant(double t) override { grants.push_back(t); }
};

class TimeIntegration : public ::testing::Test {
 protected:
  void SetUp() override {
    rtid = std::make_unique<RtidProcess>();
    amb.setFederateAmbassador(&fed);
    amb.connect(rtid->url());
    amb.createFederationExecution("m17-11-time", {kFomPath});
    amb.joinFederationExecution("alice", "m17-11-time");
  }
  void TearDown() override {
    if (amb.isConnected()) {
      try { amb.resignFederationExecution(); } catch (...) {}
      try { amb.destroyFederationExecution("m17-11-time"); } catch (...) {}
      amb.disconnect();
    }
  }
  GrantRecordingFed fed;
  std::unique_ptr<RtidProcess> rtid;
  rti1516e::RTIambassador amb;

  template <typename Pred>
  void pumpUntil(Pred pred, double timeout = 2.0) {
    const auto deadline =
        std::chrono::steady_clock::now() +
        std::chrono::milliseconds(static_cast<int>(timeout * 1000));
    while (std::chrono::steady_clock::now() < deadline) {
      amb.tickCallback(0.05, 0.1);
      if (pred()) return;
    }
  }
};

// --- Policy enable / disable ------------------------------------------------

TEST_F(TimeIntegration, EnableTimeRegulationSucceeds) {
  EXPECT_NO_THROW(amb.enableTimeRegulation(1.0));
}

TEST_F(TimeIntegration, EnableTimeRegulationTwiceThrows) {
  amb.enableTimeRegulation(1.0);
  EXPECT_ANY_THROW(amb.enableTimeRegulation(1.0));
}

TEST_F(TimeIntegration, DisableTimeRegulationAfterEnable) {
  amb.enableTimeRegulation(1.0);
  EXPECT_NO_THROW(amb.disableTimeRegulation());
}

TEST_F(TimeIntegration, EnableTimeConstrainedSucceeds) {
  EXPECT_NO_THROW(amb.enableTimeConstrained());
}

TEST_F(TimeIntegration, ModifyLookaheadAfterEnable) {
  amb.enableTimeRegulation(1.0);
  EXPECT_NO_THROW(amb.modifyLookahead(2.5));
  EXPECT_DOUBLE_EQ(amb.queryLookahead(), 2.5);
}

TEST_F(TimeIntegration, ModifyLookaheadWithoutRegulationThrows) {
  EXPECT_ANY_THROW(amb.modifyLookahead(1.0));
}

// --- TAR + grant ------------------------------------------------------------

TEST_F(TimeIntegration, TARRegulatingGrantsImmediately) {
  // Solo regulating federate — TAR(5) grants immediately because the
  // LBTS check finds no other regulators.
  amb.enableTimeRegulation(1.0);
  amb.timeAdvanceRequest(5.0);
  pumpUntil([&] { return !fed.grants.empty(); });
  ASSERT_EQ(fed.grants.size(), 1u);
  EXPECT_DOUBLE_EQ(fed.grants[0], 5.0);
}

TEST_F(TimeIntegration, TARAdvancesLogicalTime) {
  amb.enableTimeRegulation(1.0);
  amb.timeAdvanceRequest(3.0);
  pumpUntil([&] { return !fed.grants.empty(); });
  ASSERT_EQ(fed.grants.size(), 1u);
  EXPECT_DOUBLE_EQ(amb.queryLogicalTime(), 3.0);
}

TEST_F(TimeIntegration, MultipleTARsEachGrant) {
  amb.enableTimeRegulation(1.0);
  amb.timeAdvanceRequest(1.0);
  pumpUntil([&] { return fed.grants.size() >= 1u; });
  amb.timeAdvanceRequest(2.0);
  pumpUntil([&] { return fed.grants.size() >= 2u; });
  amb.timeAdvanceRequest(3.0);
  pumpUntil([&] { return fed.grants.size() >= 3u; });
  ASSERT_EQ(fed.grants.size(), 3u);
  EXPECT_DOUBLE_EQ(fed.grants[0], 1.0);
  EXPECT_DOUBLE_EQ(fed.grants[1], 2.0);
  EXPECT_DOUBLE_EQ(fed.grants[2], 3.0);
}

TEST_F(TimeIntegration, NextMessageRequestGrants) {
  amb.enableTimeRegulation(1.0);
  amb.nextMessageRequest(4.0);
  pumpUntil([&] { return !fed.grants.empty(); });
  ASSERT_EQ(fed.grants.size(), 1u);
  EXPECT_DOUBLE_EQ(fed.grants[0], 4.0);
}

TEST_F(TimeIntegration, FlushQueueRequestGrants) {
  amb.enableTimeRegulation(1.0);
  amb.flushQueueRequest(7.0);
  pumpUntil([&] { return !fed.grants.empty(); });
  ASSERT_EQ(fed.grants.size(), 1u);
  EXPECT_DOUBLE_EQ(fed.grants[0], 7.0);
}

// --- Queries ----------------------------------------------------------------

TEST_F(TimeIntegration, QueryLogicalTimeInitiallyZero) {
  EXPECT_DOUBLE_EQ(amb.queryLogicalTime(), 0.0);
}

TEST_F(TimeIntegration, QueryLookaheadReturnsConfigured) {
  amb.enableTimeRegulation(1.5);
  EXPECT_DOUBLE_EQ(amb.queryLookahead(), 1.5);
}

TEST_F(TimeIntegration, QueryLBTSNoRegulatingFederates) {
  const auto r = amb.queryLBTS();
  EXPECT_FALSE(r.finite);
}

TEST_F(TimeIntegration, QueryLBTSWithRegulator) {
  amb.enableTimeRegulation(2.0);
  const auto r = amb.queryLBTS();
  EXPECT_TRUE(r.finite);
  // current time (0) + lookahead (2.0) = 2.0
  EXPECT_DOUBLE_EQ(r.time, 2.0);
}

// --- M20.1 §8.19 queryGALT + §8.20 queryLITS --------------------------------

TEST_F(TimeIntegration, QueryGALTSoloRegulatingIsInfinite) {
  // Solo regulating federate — no OTHER regulators, so GALT is +inf.
  amb.enableTimeRegulation(1.0);
  const auto r = amb.queryGALT();
  EXPECT_FALSE(r.finite);
}

TEST_F(TimeIntegration, QueryGALTUnregulatedIsInfinite) {
  // Not regulating; with no other regulators GALT is also +inf.
  const auto r = amb.queryGALT();
  EXPECT_FALSE(r.finite);
}

TEST_F(TimeIntegration, QueryLITSEmptyBufferReturnsInfinite) {
  // No TSO messages buffered → finite=false.
  const auto r = amb.queryLITS();
  EXPECT_FALSE(r.finite);
}

// --- Async delivery ---------------------------------------------------------

TEST_F(TimeIntegration, EnableAsynchronousDeliverySucceeds) {
  EXPECT_NO_THROW(amb.enableAsynchronousDelivery());
}

TEST_F(TimeIntegration, EnableAsyncTwiceThrows) {
  amb.enableAsynchronousDelivery();
  EXPECT_ANY_THROW(amb.enableAsynchronousDelivery());
}

TEST_F(TimeIntegration, DisableAsyncAfterEnable) {
  amb.enableAsynchronousDelivery();
  EXPECT_NO_THROW(amb.disableAsynchronousDelivery());
}

// --- Pre-join guards --------------------------------------------------------

TEST(TimeRequiresJoin, OperationsThrowPreJoin) {
  RtidProcess rtid;
  rti1516e::RTIambassador amb;
  amb.connect(rtid.url());
  EXPECT_THROW(amb.enableTimeRegulation(1.0),
               rti1516e::FederateNotExecutionMember);
  EXPECT_THROW(amb.timeAdvanceRequest(1.0),
               rti1516e::FederateNotExecutionMember);
  EXPECT_THROW(amb.queryLogicalTime(),
               rti1516e::FederateNotExecutionMember);
  EXPECT_THROW(amb.queryLookahead(),
               rti1516e::FederateNotExecutionMember);
  EXPECT_THROW(amb.queryLBTS(),
               rti1516e::FederateNotExecutionMember);
}

}  // namespace
