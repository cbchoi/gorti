// test_om_delete_object_tso.cpp — gtest driver for fixture #10.

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

TEST(Conformance, om_delete_object_tso) {
  RtidRunner rtid("/tmp/gorti_om_delete_tso_rtid.log");

  std::thread pub([]() {
    runFederate("./om_delete_object_tso_publisher",
                "/tmp/gorti_om_delete_tso_pub.log");
  });
  std::this_thread::sleep_for(std::chrono::milliseconds(300));
  std::thread sub([]() {
    runFederate("./om_delete_object_tso_subscriber",
                "/tmp/gorti_om_delete_tso_sub.log");
  });

  pub.join();
  sub.join();

  EXPECT_EQ(diffAgainstGolden(
                bucketSortRO(splitNonComment(
                    slurp("/tmp/gorti_om_delete_tso_pub.log"))),
                loadGolden("expected.publisher.log")),
            "");
  EXPECT_EQ(diffAgainstGolden(
                bucketSortRO(splitNonComment(
                    slurp("/tmp/gorti_om_delete_tso_sub.log"))),
                loadGolden("expected.subscriber.log")),
            "");
}
