// test_om_request_attribute_update_instance.cpp — gtest driver for fixture #13.

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

TEST(Conformance, om_request_attribute_update_instance) {
  RtidRunner rtid("/tmp/gorti_om_req_upd_inst_rtid.log");

  std::thread pub([]() {
    runFederate("./om_request_attribute_update_instance_publisher",
                "/tmp/gorti_om_req_upd_inst_pub.log");
  });
  std::this_thread::sleep_for(std::chrono::milliseconds(300));
  std::thread sub([]() {
    runFederate("./om_request_attribute_update_instance_subscriber",
                "/tmp/gorti_om_req_upd_inst_sub.log");
  });

  pub.join();
  sub.join();

  EXPECT_EQ(diffAgainstGolden(
                bucketSortRO(splitNonComment(
                    slurp("/tmp/gorti_om_req_upd_inst_pub.log"))),
                loadGolden("expected.publisher.log")),
            "");
  EXPECT_EQ(diffAgainstGolden(
                bucketSortRO(splitNonComment(
                    slurp("/tmp/gorti_om_req_upd_inst_sub.log"))),
                loadGolden("expected.subscriber.log")),
            "");
}
