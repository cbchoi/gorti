// test_fm_sync_subset_with_failure.cpp — gtest driver for fixture #4.

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

TEST(Conformance, fm_sync_subset_with_failure) {
  RtidRunner rtid("/tmp/gorti_fm_sync_subset_rtid.log");

  std::thread alice([]() {
    runFederate("./fm_sync_subset_with_failure_registrar",
                "/tmp/gorti_fm_sync_subset_alice.log");
  });
  std::this_thread::sleep_for(std::chrono::milliseconds(150));
  std::thread bob([]() {
    runFederate("./fm_sync_subset_with_failure_bob",
                "/tmp/gorti_fm_sync_subset_bob.log");
  });
  std::thread carol([]() {
    runFederate("./fm_sync_subset_with_failure_carol",
                "/tmp/gorti_fm_sync_subset_carol.log");
  });
  alice.join();
  bob.join();
  carol.join();

  EXPECT_EQ(diffAgainstGolden(
                bucketSortRO(splitNonComment(
                    slurp("/tmp/gorti_fm_sync_subset_alice.log"))),
                loadGolden("expected.registrar.log")),
            "");
  EXPECT_EQ(diffAgainstGolden(
                bucketSortRO(splitNonComment(
                    slurp("/tmp/gorti_fm_sync_subset_bob.log"))),
                loadGolden("expected.bob.log")),
            "");
  EXPECT_EQ(diffAgainstGolden(
                bucketSortRO(splitNonComment(
                    slurp("/tmp/gorti_fm_sync_subset_carol.log"))),
                loadGolden("expected.carol.log")),
            "");
}
