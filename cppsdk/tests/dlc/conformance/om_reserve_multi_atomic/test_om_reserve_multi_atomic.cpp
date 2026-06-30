// test_om_reserve_multi_atomic.cpp — gtest driver for fixture #9.

#include "../_harness/rtid_runner.h"
#include "../_harness/log_diff.h"
#include "../_harness/golden_loader.h"

#include <cstdlib>
#include <chrono>
#include <thread>

#include <gtest/gtest.h>

namespace {
using namespace gorti_dlc_harness;
void runFederate(const std::string& bin, const std::string& out) {
  std::system((bin + " > " + out + " 2>&1").c_str());
}
}  // namespace

TEST(Conformance, om_reserve_multi_atomic) {
  RtidRunner rtid("/tmp/gorti_om_reserve_multi_rtid.log");

  std::thread collider([]() {
    runFederate("./om_reserve_multi_collider",
                "/tmp/gorti_om_reserve_multi_collider.log");
  });
  std::this_thread::sleep_for(std::chrono::milliseconds(300));
  std::thread reserver([]() {
    runFederate("./om_reserve_multi_reserver",
                "/tmp/gorti_om_reserve_multi_reserver.log");
  });

  collider.join();
  reserver.join();

  EXPECT_EQ(diffAgainstGolden(
                bucketSortRO(splitNonComment(
                    slurp("/tmp/gorti_om_reserve_multi_collider.log"))),
                loadGolden("expected.collider.log")),
            "");
  EXPECT_EQ(diffAgainstGolden(
                bucketSortRO(splitNonComment(
                    slurp("/tmp/gorti_om_reserve_multi_reserver.log"))),
                loadGolden("expected.reserver.log")),
            "");
}
