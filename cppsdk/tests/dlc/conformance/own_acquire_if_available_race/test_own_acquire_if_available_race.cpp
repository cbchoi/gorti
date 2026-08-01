// test_own_acquire_if_available_race.cpp — gtest driver for fixture #16.

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

TEST(Conformance, own_acquire_if_available_race) {
  RtidRunner rtid("/tmp/gorti_own_race_rtid.log");

  std::thread carrier([]() {
    runFederate("./own_acquire_race_carrier",
                "/tmp/gorti_own_race_carrier.log");
  });
  std::this_thread::sleep_for(std::chrono::milliseconds(300));
  std::thread bob([]() {
    runFederate("./own_acquire_race_racer bob",
                "/tmp/gorti_own_race_bob.log");
  });
  std::thread carol([]() {
    runFederate("./own_acquire_race_racer carol",
                "/tmp/gorti_own_race_carol.log");
  });

  carrier.join();
  bob.join();
  carol.join();

  EXPECT_EQ(diffAgainstGolden(
                bucketSortRO(splitNonComment(
                    slurp("/tmp/gorti_own_race_carrier.log"))),
                loadGolden("expected.carrier.log")),
            "");
  EXPECT_EQ(diffAgainstGolden(
                bucketSortRO(splitNonComment(
                    slurp("/tmp/gorti_own_race_bob.log"))),
                loadGolden("expected.bob.log")),
            "");
  EXPECT_EQ(diffAgainstGolden(
                bucketSortRO(splitNonComment(
                    slurp("/tmp/gorti_own_race_carol.log"))),
                loadGolden("expected.carol.log")),
            "");
}
