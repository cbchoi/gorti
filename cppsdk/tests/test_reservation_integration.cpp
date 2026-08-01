// M17.10 — §6.1-5 object instance name reservation integration tests.

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

class RecordingFed : public rti1516e::FederateAmbassador {
 public:
  std::vector<std::string> reservation_succeeded;
  std::vector<std::string> reservation_failed;
  std::vector<std::vector<std::string>> multi_succeeded;
  std::vector<std::pair<std::vector<std::string>, std::vector<std::string>>>
      multi_failed;

  void objectInstanceNameReservationSucceeded(
      const std::string& object_name) override {
    reservation_succeeded.push_back(object_name);
  }
  void objectInstanceNameReservationFailed(
      const std::string& object_name) override {
    reservation_failed.push_back(object_name);
  }
  void multipleObjectInstanceNameReservationSucceeded(
      const std::vector<std::string>& object_names) override {
    multi_succeeded.push_back(object_names);
  }
  void multipleObjectInstanceNameReservationFailed(
      const std::vector<std::string>& requested_names,
      const std::vector<std::string>& colliding_names) override {
    multi_failed.push_back({requested_names, colliding_names});
  }
};

class ReservationIntegration : public ::testing::Test {
 protected:
  void SetUp() override {
    rtid = std::make_unique<RtidProcess>();
    amb.setFederateAmbassador(&fed);
    amb.connect(rtid->url());
    amb.createFederationExecution("m17-10-resv", {kFomPath});
    amb.joinFederationExecution("alice", "m17-10-resv");
  }
  void TearDown() override {
    if (amb.isConnected()) {
      try { amb.resignFederationExecution(); } catch (...) {}
      try { amb.destroyFederationExecution("m17-10-resv"); } catch (...) {}
      amb.disconnect();
    }
  }
  RecordingFed fed;
  std::unique_ptr<RtidProcess> rtid;
  rti1516e::RTIambassador amb;

  // Pump tickCallback for up to `timeout` seconds or until `pred()`
  // is true.
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

TEST_F(ReservationIntegration, ReserveSucceedsFiresCallback) {
  amb.reserveObjectInstanceName("car-A");
  pumpUntil([&] { return !fed.reservation_succeeded.empty(); });
  ASSERT_EQ(fed.reservation_succeeded.size(), 1u);
  EXPECT_EQ(fed.reservation_succeeded[0], "car-A");
  EXPECT_TRUE(fed.reservation_failed.empty());
}

TEST_F(ReservationIntegration, ReserveDuplicateFiresFailed) {
  amb.reserveObjectInstanceName("car-dup");
  pumpUntil([&] { return !fed.reservation_succeeded.empty(); });
  ASSERT_EQ(fed.reservation_succeeded.size(), 1u);

  amb.reserveObjectInstanceName("car-dup");
  pumpUntil([&] { return !fed.reservation_failed.empty(); });
  ASSERT_EQ(fed.reservation_failed.size(), 1u);
  EXPECT_EQ(fed.reservation_failed[0], "car-dup");
}

TEST_F(ReservationIntegration, MultipleReserveBatchSuccess) {
  amb.reserveMultipleObjectInstanceNames({"A", "B", "C"});
  pumpUntil([&] { return !fed.multi_succeeded.empty(); });
  ASSERT_EQ(fed.multi_succeeded.size(), 1u);
  EXPECT_EQ(fed.multi_succeeded[0].size(), 3u);
}

TEST_F(ReservationIntegration, MultipleReserveCollisionFiresFailed) {
  // Reserve "B" first.
  amb.reserveObjectInstanceName("B");
  pumpUntil([&] { return !fed.reservation_succeeded.empty(); });

  // Batch {A, B, C} — collides on B.
  amb.reserveMultipleObjectInstanceNames({"A", "B", "C"});
  pumpUntil([&] { return !fed.multi_failed.empty(); });
  ASSERT_EQ(fed.multi_failed.size(), 1u);
  const auto& [req, col] = fed.multi_failed[0];
  EXPECT_EQ(req.size(), 3u);
  // The Go-side manager reports "B" as the colliding name.
  bool b_in_col = false;
  for (const auto& n : col) if (n == "B") b_in_col = true;
  EXPECT_TRUE(b_in_col) << "expected 'B' in colliding_names";
}

TEST_F(ReservationIntegration, ReleaseAllowsReReservation) {
  amb.reserveObjectInstanceName("car-rel");
  pumpUntil([&] { return !fed.reservation_succeeded.empty(); });
  amb.releaseObjectInstanceName("car-rel");
  // After release, reserve again — must succeed.
  fed.reservation_succeeded.clear();
  amb.reserveObjectInstanceName("car-rel");
  pumpUntil([&] { return !fed.reservation_succeeded.empty(); });
  ASSERT_EQ(fed.reservation_succeeded.size(), 1u);
}

}  // namespace
