// test_fm_save_restore_roundtrip.cpp — gtest driver for fixture #5.

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

TEST(Conformance, fm_save_restore_roundtrip) {
  RtidRunner rtid("/tmp/gorti_fm_save_restore_rtid.log");
  std::thread fed([]() {
    runFederate("./fm_save_restore_federate",
                "/tmp/gorti_fm_save_restore_fed.log");
  });
  fed.join();
  EXPECT_EQ(diffAgainstGolden(
                bucketSortRO(splitNonComment(
                    slurp("/tmp/gorti_fm_save_restore_fed.log"))),
                loadGolden("expected.federate.log")),
            "");
}
