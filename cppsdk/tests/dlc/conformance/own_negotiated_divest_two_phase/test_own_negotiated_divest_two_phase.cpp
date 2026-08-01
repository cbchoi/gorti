// test_own_negotiated_divest_two_phase.cpp — gtest driver for fixture #15.

#include "../_harness/rtid_runner.h"
#include "../_harness/log_diff.h"
#include "../_harness/golden_loader.h"

#include <cstdlib>
#include <fstream>
#include <sstream>
#include <thread>
#include <chrono>

#include <gtest/gtest.h>

namespace {
using namespace gorti_dlc_harness;

void runFederate(const std::string& bin, const std::string& out) {
  std::system((bin + " > " + out + " 2>&1").c_str());
}
}  // namespace

TEST(Conformance, own_negotiated_divest_two_phase) {
  RtidRunner rtid("/tmp/gorti_own_divest_rtid.log");

  std::thread alice([]() {
    runFederate("./own_negotiated_divest_alice",
                "/tmp/gorti_own_divest_alice.log");
  });
  std::this_thread::sleep_for(std::chrono::milliseconds(300));
  std::thread bob([]() {
    runFederate("./own_negotiated_divest_bob",
                "/tmp/gorti_own_divest_bob.log");
  });

  alice.join();
  bob.join();

  EXPECT_EQ(diffAgainstGolden(
                bucketSortRO(splitNonComment(
                    slurp("/tmp/gorti_own_divest_alice.log"))),
                loadGolden("expected.alice.log")),
            "");
  EXPECT_EQ(diffAgainstGolden(
                bucketSortRO(splitNonComment(
                    slurp("/tmp/gorti_own_divest_bob.log"))),
                loadGolden("expected.bob.log")),
            "");
}
