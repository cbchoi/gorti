// M17.16 — §4.8-15 Save / Restore integration tests.
//
// The federation save protocol fires three callbacks:
//   1. initiateFederateSave(label, save_time?)
//   2. federationSaved(label) OR federationNotSaved(label)
// A solo federate driving requestFederationSave → save_complete
// observes initiate then saved.

#include <gtest/gtest.h>

#include <chrono>
#include <cstdio>
#include <optional>
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

class SaveRecordingFed : public rti1516e::FederateAmbassador {
 public:
  std::vector<std::pair<std::string, std::optional<double>>> initiated;
  std::vector<std::string> saved;
  std::vector<std::string> not_saved;

  void initiateFederateSave(
      const std::string& label,
      std::optional<double> save_time) override {
    initiated.emplace_back(label, save_time);
  }
  void federationSaved(const std::string& label) override {
    saved.push_back(label);
  }
  void federationNotSaved(const std::string& label) override {
    not_saved.push_back(label);
  }
};

class SavepointIntegration : public ::testing::Test {
 protected:
  void SetUp() override {
    rtid = std::make_unique<RtidProcess>();
    amb.setFederateAmbassador(&fed);
    amb.connect(rtid->url());
    // Per-test federation name: gorti's FSStorage rejects an
    // already-on-disk bundle for (federation, label), so reusing
    // a fixed name across runs flips outcomes to NotSaved on the
    // second run. Use a unique federation name per test instance.
    // Federation name has a 32-byte cap server-side, so we hex-pack
    // a per-process counter that's short enough.
    const auto ns = std::chrono::steady_clock::now()
                        .time_since_epoch().count();
    char suffix[24];
    std::snprintf(suffix, sizeof(suffix), "m16-%llx",
                  static_cast<unsigned long long>(ns));
    federation_ = suffix;
    amb.createFederationExecution(federation_, {kFomPath});
    amb.joinFederationExecution("alice", federation_);
  }
  void TearDown() override {
    if (amb.isConnected()) {
      try { amb.resignFederationExecution(); } catch (...) {}
      try { amb.destroyFederationExecution(federation_); } catch (...) {}
      amb.disconnect();
    }
  }
  std::string federation_;
  SaveRecordingFed fed;
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

// --- Save protocol ----------------------------------------------------------

TEST_F(SavepointIntegration, RequestSaveFiresInitiate) {
  amb.requestFederationSave("phase1");
  pumpUntil([&] { return !fed.initiated.empty(); });
  ASSERT_EQ(fed.initiated.size(), 1u);
  EXPECT_EQ(fed.initiated[0].first, "phase1");
  EXPECT_FALSE(fed.initiated[0].second.has_value());  // no save_time pin
}

TEST_F(SavepointIntegration, RequestSaveWithTimePinPropagates) {
  amb.requestFederationSave("phase2", 12.5);
  pumpUntil([&] { return !fed.initiated.empty(); });
  ASSERT_EQ(fed.initiated.size(), 1u);
  ASSERT_TRUE(fed.initiated[0].second.has_value());
  EXPECT_DOUBLE_EQ(*fed.initiated[0].second, 12.5);
}

TEST_F(SavepointIntegration, SaveCompleteFiresFederationSaved) {
  amb.requestFederationSave("solo");
  pumpUntil([&] { return !fed.initiated.empty(); });
  amb.federateSaveComplete();
  pumpUntil([&] { return !fed.saved.empty(); });
  ASSERT_EQ(fed.saved.size(), 1u);
  EXPECT_EQ(fed.saved[0], "solo");
  EXPECT_TRUE(fed.not_saved.empty());
}

TEST_F(SavepointIntegration, SaveNotCompleteFiresFederationNotSaved) {
  amb.requestFederationSave("fail");
  pumpUntil([&] { return !fed.initiated.empty(); });
  amb.federateSaveNotComplete();
  pumpUntil([&] { return !fed.not_saved.empty(); });
  ASSERT_EQ(fed.not_saved.size(), 1u);
  EXPECT_EQ(fed.not_saved[0], "fail");
  EXPECT_TRUE(fed.saved.empty());
}

TEST_F(SavepointIntegration, QuerySaveStateTransitionsToSaved) {
  amb.requestFederationSave("query");
  pumpUntil([&] { return !fed.initiated.empty(); });
  amb.federateSaveComplete();
  pumpUntil([&] { return !fed.saved.empty(); });
  EXPECT_EQ(amb.querySaveState("query"),
            rti1516e::RTIambassador::SaveState::Saved);
}

// --- Restore RPC surface (no callbacks emitted server-side) -----------------

TEST_F(SavepointIntegration, RestoreRequestAcceptedForExistingLabel) {
  // First land a successful save, then drive the restore RPC.
  amb.requestFederationSave("restorable");
  pumpUntil([&] { return !fed.initiated.empty(); });
  amb.federateSaveComplete();
  pumpUntil([&] { return !fed.saved.empty(); });
  // Restore should succeed; callbacks aren't wired yet, only the
  // state machine transitions.
  EXPECT_NO_THROW(amb.requestFederationRestore("restorable"));
}

TEST_F(SavepointIntegration, QueryRestoreStateIdleForUnknown) {
  EXPECT_EQ(amb.queryRestoreState("never-saved"),
            rti1516e::RTIambassador::RestoreState::Idle);
}

// --- Pre-join guard ---------------------------------------------------------

TEST(SavepointRequiresJoin, OperationsThrowPreJoin) {
  RtidProcess rtid;
  rti1516e::RTIambassador amb;
  amb.connect(rtid.url());
  EXPECT_THROW(amb.requestFederationSave("x"),
               rti1516e::FederateNotExecutionMember);
  EXPECT_THROW(amb.federateSaveComplete(),
               rti1516e::FederateNotExecutionMember);
  EXPECT_THROW(amb.querySaveState("x"),
               rti1516e::FederateNotExecutionMember);
  EXPECT_THROW(amb.requestFederationRestore("x"),
               rti1516e::FederateNotExecutionMember);
  EXPECT_THROW(amb.queryRestoreState("x"),
               rti1516e::FederateNotExecutionMember);
}

}  // namespace
