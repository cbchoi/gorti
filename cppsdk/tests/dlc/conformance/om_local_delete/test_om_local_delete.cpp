// test_om_local_delete.cpp — gtest driver for fixture #11.

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

TEST(Conformance, om_local_delete) {
  RtidRunner rtid("/tmp/gorti_om_local_delete_rtid.log");

  std::thread pub([]() {
    runFederate("./om_local_delete_publisher",
                "/tmp/gorti_om_local_delete_pub.log");
  });
  std::this_thread::sleep_for(std::chrono::milliseconds(300));
  std::thread sub([]() {
    runFederate("./om_local_delete_subscriber",
                "/tmp/gorti_om_local_delete_sub.log");
  });

  pub.join();
  sub.join();

  EXPECT_EQ(diffAgainstGolden(
                bucketSortRO(splitNonComment(
                    slurp("/tmp/gorti_om_local_delete_pub.log"))),
                loadGolden("expected.publisher.log")),
            "");
  EXPECT_EQ(diffAgainstGolden(
                bucketSortRO(splitNonComment(
                    slurp("/tmp/gorti_om_local_delete_sub.log"))),
                loadGolden("expected.subscriber.log")),
            "");
}
