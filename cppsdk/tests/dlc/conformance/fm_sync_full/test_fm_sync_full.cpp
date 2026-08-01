// test_fm_sync_full.cpp — gtest driver for fixture #3.

#include "../_harness/rtid_runner.h"
#include "../_harness/log_diff.h"
#include "../_harness/golden_loader.h"

#include <cstdlib>
#include <chrono>
#include <thread>

#include <gtest/gtest.h>

namespace {
using namespace gorti_dlc_harness;
void runFederateEnv(const std::string& bin, const std::string& out,
                    const std::string& env) {
  std::system((env + " " + bin + " > " + out + " 2>&1").c_str());
}
void runFederate(const std::string& bin, const std::string& out) {
  std::system((bin + " > " + out + " 2>&1").c_str());
}
}  // namespace

TEST(Conformance, fm_sync_full) {
  RtidRunner rtid("/tmp/gorti_fm_sync_full_rtid.log");

  std::thread reg([]() {
    runFederate("./fm_sync_full_registrar",
                "/tmp/gorti_fm_sync_full_registrar.log");
  });
  std::this_thread::sleep_for(std::chrono::milliseconds(150));
  std::thread bob([]() {
    runFederateEnv("./fm_sync_full_peer",
                   "/tmp/gorti_fm_sync_full_bob.log",
                   "FED_NAME=BOB");
  });
  std::thread carol([]() {
    runFederateEnv("./fm_sync_full_peer",
                   "/tmp/gorti_fm_sync_full_carol.log",
                   "FED_NAME=CAROL");
  });
  reg.join();
  bob.join();
  carol.join();

  EXPECT_EQ(diffAgainstGolden(
                bucketSortRO(splitNonComment(
                    slurp("/tmp/gorti_fm_sync_full_registrar.log"))),
                loadGolden("expected.registrar.log")),
            "");
  EXPECT_EQ(diffAgainstGolden(
                bucketSortRO(splitNonComment(
                    slurp("/tmp/gorti_fm_sync_full_bob.log"))),
                loadGolden("expected.bob.log")),
            "");
  EXPECT_EQ(diffAgainstGolden(
                bucketSortRO(splitNonComment(
                    slurp("/tmp/gorti_fm_sync_full_carol.log"))),
                loadGolden("expected.carol.log")),
            "");
}
