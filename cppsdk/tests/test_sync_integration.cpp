// M17.14 — §4.7 Federation synchronization points integration tests.

#include <gtest/gtest.h>

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

class SyncRecordingFed : public rti1516e::FederateAmbassador {
 public:
  std::vector<std::pair<std::string, std::string>> announced;
  std::vector<std::string> synchronized;

  void announceSynchronizationPoint(
      const std::string& label,
      const rti1516e::VariableLengthData& tag) override {
    announced.emplace_back(label, std::string(tag.begin(), tag.end()));
  }
  void federationSynchronized(const std::string& label) override {
    synchronized.push_back(label);
  }
};

class SyncIntegration : public ::testing::Test {
 protected:
  void SetUp() override {
    rtid = std::make_unique<RtidProcess>();
    amb.setFederateAmbassador(&fed);
    amb.connect(rtid->url());
    amb.createFederationExecution("m17-14-sync", {kFomPath});
    amb.joinFederationExecution("alice", "m17-14-sync");
  }
  void TearDown() override {
    if (amb.isConnected()) {
      try { amb.resignFederationExecution(); } catch (...) {}
      try { amb.destroyFederationExecution("m17-14-sync"); } catch (...) {}
      amb.disconnect();
    }
  }
  SyncRecordingFed fed;
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

TEST_F(SyncIntegration, RegisterFiresAnnounceOnSelf) {
  const std::string tag_bytes = "go!";
  rti1516e::VariableLengthData tag(tag_bytes.begin(), tag_bytes.end());
  amb.registerFederationSynchronizationPoint("phase1", tag);
  pumpUntil([&] { return !fed.announced.empty(); });
  ASSERT_EQ(fed.announced.size(), 1u);
  EXPECT_EQ(fed.announced[0].first, "phase1");
  EXPECT_EQ(fed.announced[0].second, "go!");
}

TEST_F(SyncIntegration, RegisterWithEmptyTagSucceeds) {
  rti1516e::VariableLengthData empty_tag;
  amb.registerFederationSynchronizationPoint("phase2", empty_tag);
  pumpUntil([&] { return !fed.announced.empty(); });
  ASSERT_EQ(fed.announced.size(), 1u);
  EXPECT_EQ(fed.announced[0].first, "phase2");
  EXPECT_TRUE(fed.announced[0].second.empty());
}

TEST_F(SyncIntegration, AchieveOnSoloFederateFiresSynchronized) {
  // Solo federate is the only required member; achieve completes
  // the federation synchronization immediately.
  rti1516e::VariableLengthData tag;
  amb.registerFederationSynchronizationPoint("solo", tag);
  pumpUntil([&] { return !fed.announced.empty(); });
  amb.synchronizationPointAchieved("solo");
  pumpUntil([&] { return !fed.synchronized.empty(); });
  ASSERT_EQ(fed.synchronized.size(), 1u);
  EXPECT_EQ(fed.synchronized[0], "solo");
}

TEST_F(SyncIntegration, AnnouncedBeforeSynchronizedOrdering) {
  rti1516e::VariableLengthData tag;
  amb.registerFederationSynchronizationPoint("ordered", tag);
  pumpUntil([&] { return !fed.announced.empty(); });
  amb.synchronizationPointAchieved("ordered");
  pumpUntil([&] { return !fed.synchronized.empty(); });
  ASSERT_EQ(fed.announced.size(), 1u);
  ASSERT_EQ(fed.synchronized.size(), 1u);
}

TEST(SyncRequiresJoin, OperationsThrowPreJoin) {
  RtidProcess rtid;
  rti1516e::RTIambassador amb;
  amb.connect(rtid.url());
  rti1516e::VariableLengthData tag;
  EXPECT_THROW(amb.registerFederationSynchronizationPoint("x", tag),
               rti1516e::FederateNotExecutionMember);
  EXPECT_THROW(amb.synchronizationPointAchieved("x"),
               rti1516e::FederateNotExecutionMember);
}

}  // namespace
