// test_om_helloworld_pubsub.cpp — gtest driver for fixture #8.
//
// M31 status: this test FAILS TO LINK because the federate*.cpp TUs
// reference rti1516e::* impl symbols that don't exist yet. CMake
// property WILL_FAIL=TRUE per docs/M31_DISPATCH_PLAN.md §3 criterion 2.

#include "../_harness/rtid_runner.h"
#include "../_harness/log_diff.h"
#include "../_harness/golden_loader.h"

#include <cstdio>
#include <cstdlib>
#include <fstream>
#include <sstream>
#include <string>
#include <thread>
#include <vector>

#include <gtest/gtest.h>

namespace {

using namespace gorti_dlc_harness;

// Run a federate binary, capture stdout to `out_path`, wait, return content.
std::string runFederate(const std::string& bin,
                        const std::string& out_path) {
  const std::string cmd = bin + " > " + out_path + " 2>&1";
  int rc = std::system(cmd.c_str());
  (void)rc;  // RED in M31 — federate exits non-zero from link errors.
  std::ifstream is(out_path);
  std::ostringstream os;
  os << is.rdbuf();
  return os.str();
}

}  // namespace

TEST(Conformance, om_helloworld_pubsub) {
  RtidRunner rtid("/tmp/gorti_om_helloworld_rtid.log");

  // Launch publisher async, subscriber slightly after.
  std::thread pub_thread([]() {
    runFederate("./om_helloworld_publisher",
                "/tmp/gorti_om_helloworld_pub.log");
  });
  std::this_thread::sleep_for(std::chrono::milliseconds(200));
  std::thread sub_thread([]() {
    runFederate("./om_helloworld_subscriber",
                "/tmp/gorti_om_helloworld_sub.log");
  });

  pub_thread.join();
  sub_thread.join();

  // Diff captured logs against goldens.
  const auto pub_actual = bucketSortRO(
      splitNonComment(slurp("/tmp/gorti_om_helloworld_pub.log")));
  const auto pub_golden = loadGolden("expected.publisher.log");
  EXPECT_EQ(diffAgainstGolden(pub_actual, pub_golden), "");

  const auto sub_actual = bucketSortRO(
      splitNonComment(slurp("/tmp/gorti_om_helloworld_sub.log")));
  const auto sub_golden = loadGolden("expected.subscriber.log");
  EXPECT_EQ(diffAgainstGolden(sub_actual, sub_golden), "");
}
