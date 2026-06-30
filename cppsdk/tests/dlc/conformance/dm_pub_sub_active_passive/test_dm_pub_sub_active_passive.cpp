// test_dm_pub_sub_active_passive.cpp — gtest driver for fixture #6.

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

TEST(Conformance, dm_pub_sub_active_passive) {
  RtidRunner rtid("/tmp/gorti_dm_pub_sub_rtid.log");

  std::thread pub([]() {
    runFederate("./dm_pub_sub_active_passive_publisher",
                "/tmp/gorti_dm_pub_sub_pub.log");
  });
  std::this_thread::sleep_for(std::chrono::milliseconds(300));
  std::thread sub([]() {
    runFederate("./dm_pub_sub_active_passive_subscriber",
                "/tmp/gorti_dm_pub_sub_sub.log");
  });
  pub.join();
  sub.join();

  EXPECT_EQ(diffAgainstGolden(
                bucketSortRO(splitNonComment(
                    slurp("/tmp/gorti_dm_pub_sub_pub.log"))),
                loadGolden("expected.publisher.log")),
            "");
  EXPECT_EQ(diffAgainstGolden(
                bucketSortRO(splitNonComment(
                    slurp("/tmp/gorti_dm_pub_sub_sub.log"))),
                loadGolden("expected.subscriber.log")),
            "");
}
