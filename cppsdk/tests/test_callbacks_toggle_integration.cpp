// M17.18 — strict HLA_EVOKED aliases + enable/disableCallbacks toggle.

#include <gtest/gtest.h>

#include <chrono>
#include <string>
#include <thread>
#include <vector>

#include "rti1516e/FederateAmbassador.h"
#include "rti1516e/RtiAmbassador.h"

#include "fixtures/RtidProcess.h"

namespace {

using rti1516e_test::RtidProcess;
const std::string kFomPath = TEST_FOM_PATH;

class SyncRecordingFed : public rti1516e::FederateAmbassador {
 public:
  std::vector<std::string> announced;
  void announceSynchronizationPoint(
      const std::string& label,
      const rti1516e::VariableLengthData&) override {
    announced.push_back(label);
  }
};

class CallbacksToggleIntegration : public ::testing::Test {
 protected:
  void SetUp() override {
    rtid = std::make_unique<RtidProcess>();
    amb.setFederateAmbassador(&fed);
    amb.connect(rtid->url());
    amb.createFederationExecution("m17-18-cbs", {kFomPath});
    amb.joinFederationExecution("alice", "m17-18-cbs");
  }
  void TearDown() override {
    if (amb.isConnected()) {
      try { amb.resignFederationExecution(); } catch (...) {}
      try { amb.destroyFederationExecution("m17-18-cbs"); } catch (...) {}
      amb.disconnect();
    }
  }
  SyncRecordingFed fed;
  std::unique_ptr<RtidProcess> rtid;
  rti1516e::RTIambassador amb;
};

// --- evokeCallback / evokeMultipleCallbacks aliases -------------------------

TEST_F(CallbacksToggleIntegration, EvokeCallbackDispatchesQueuedEvents) {
  rti1516e::VariableLengthData tag;
  amb.registerFederationSynchronizationPoint("phase-evoke", tag);
  // Spin evokeCallback until the announce arrives.
  for (int i = 0; i < 40 && fed.announced.empty(); ++i) {
    amb.evokeCallback(0.05, 0.1);
  }
  ASSERT_EQ(fed.announced.size(), 1u);
  EXPECT_EQ(fed.announced[0], "phase-evoke");
}

TEST_F(CallbacksToggleIntegration,
       EvokeMultipleCallbacksDispatchesQueuedEvents) {
  rti1516e::VariableLengthData tag;
  amb.registerFederationSynchronizationPoint("phase-multi", tag);
  for (int i = 0; i < 40 && fed.announced.empty(); ++i) {
    amb.evokeMultipleCallbacks(0.05, 0.1);
  }
  ASSERT_EQ(fed.announced.size(), 1u);
  EXPECT_EQ(fed.announced[0], "phase-multi");
}

// --- disableCallbacks / enableCallbacks -------------------------------------

TEST_F(CallbacksToggleIntegration, DisabledCallbacksDoNotFire) {
  amb.disableCallbacks();
  rti1516e::VariableLengthData tag;
  amb.registerFederationSynchronizationPoint("phase-paused", tag);
  // Pump for ~500 ms — nothing should fire while disabled.
  for (int i = 0; i < 5; ++i) {
    amb.tickCallback(0.05, 0.1);
  }
  EXPECT_TRUE(fed.announced.empty()) << "callback fired while disabled";
}

TEST_F(CallbacksToggleIntegration, ReEnableDeliversBufferedEvents) {
  amb.disableCallbacks();
  rti1516e::VariableLengthData tag;
  amb.registerFederationSynchronizationPoint("phase-buffered", tag);
  // Let the server enqueue the event in the background reader.
  std::this_thread::sleep_for(std::chrono::milliseconds(300));
  EXPECT_TRUE(fed.announced.empty());
  amb.enableCallbacks();
  // Buffered event drains on the next tickCallback.
  for (int i = 0; i < 20 && fed.announced.empty(); ++i) {
    amb.tickCallback(0.05, 0.1);
  }
  ASSERT_EQ(fed.announced.size(), 1u);
  EXPECT_EQ(fed.announced[0], "phase-buffered");
}

// --- M17.22 strict at-most-one evokeCallback --------------------------------

TEST_F(CallbacksToggleIntegration, EvokeCallbackDispatchesExactlyOnePerCall) {
  // Register 3 distinct sync points back-to-back. Each fires an
  // announce event server-side. evokeCallback should pop EXACTLY
  // ONE per call and return true while more are queued.
  rti1516e::VariableLengthData tag;
  amb.registerFederationSynchronizationPoint("one", tag);
  amb.registerFederationSynchronizationPoint("two", tag);
  amb.registerFederationSynchronizationPoint("three", tag);

  // Drain via single-event evokeCallback. Give each call up to 1s
  // to receive the buffered event. Track how many fired per call.
  std::vector<std::size_t> sizes_after;
  for (int i = 0; i < 3; ++i) {
    const bool more = amb.evokeCallback(0.0, 1.0);
    sizes_after.push_back(fed.announced.size());
    // After the third dispatch the queue should be empty; ``more``
    // tells the federate whether to keep looping.
    if (i < 2) EXPECT_TRUE(more) << "expected more events queued after call " << i;
  }
  // Each call must have added EXACTLY ONE event.
  ASSERT_EQ(sizes_after.size(), 3u);
  EXPECT_EQ(sizes_after[0], 1u);
  EXPECT_EQ(sizes_after[1], 2u);
  EXPECT_EQ(sizes_after[2], 3u);
}

TEST_F(CallbacksToggleIntegration,
       EvokeCallbackReturnsFalseWhenNothingArrives) {
  // No events queued; evokeCallback waits up to max_time and
  // returns false.
  EXPECT_FALSE(amb.evokeCallback(0.0, 0.05));
}

TEST_F(CallbacksToggleIntegration, ToggleCyclePreservesEventOrder) {
  rti1516e::VariableLengthData tag;
  // First event — fires through tickCallback path.
  amb.registerFederationSynchronizationPoint("phase-first", tag);
  for (int i = 0; i < 20 && fed.announced.size() < 1u; ++i) {
    amb.tickCallback(0.05, 0.1);
  }
  ASSERT_EQ(fed.announced.size(), 1u);

  // Toggle off, second event buffers, toggle on, second event drains.
  amb.disableCallbacks();
  amb.registerFederationSynchronizationPoint("phase-second", tag);
  std::this_thread::sleep_for(std::chrono::milliseconds(200));
  EXPECT_EQ(fed.announced.size(), 1u);
  amb.enableCallbacks();
  for (int i = 0; i < 20 && fed.announced.size() < 2u; ++i) {
    amb.tickCallback(0.05, 0.1);
  }
  ASSERT_EQ(fed.announced.size(), 2u);
  EXPECT_EQ(fed.announced[0], "phase-first");
  EXPECT_EQ(fed.announced[1], "phase-second");
}

}  // namespace
