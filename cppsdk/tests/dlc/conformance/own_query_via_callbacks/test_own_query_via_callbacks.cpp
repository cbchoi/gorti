// test_own_query_via_callbacks.cpp — gtest driver for fixture #18.

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

TEST(Conformance, own_query_via_callbacks) {
  RtidRunner rtid("/tmp/gorti_own_query_rtid.log");

  std::thread carrier([]() {
    runFederate("./own_query_callbacks_carrier",
                "/tmp/gorti_own_query_carrier.log");
  });
  std::this_thread::sleep_for(std::chrono::milliseconds(300));
  std::thread querier([]() {
    runFederate("./own_query_callbacks_querier",
                "/tmp/gorti_own_query_querier.log");
  });

  carrier.join();
  querier.join();

  EXPECT_EQ(diffAgainstGolden(
                bucketSortRO(splitNonComment(
                    slurp("/tmp/gorti_own_query_carrier.log"))),
                loadGolden("expected.carrier.log")),
            "");
  EXPECT_EQ(diffAgainstGolden(
                bucketSortRO(splitNonComment(
                    slurp("/tmp/gorti_own_query_querier.log"))),
                loadGolden("expected.querier.log")),
            "");
}
