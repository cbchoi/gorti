// test_own_release_request_denied.cpp — gtest driver for fixture #17.

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

TEST(Conformance, own_release_request_denied) {
  RtidRunner rtid("/tmp/gorti_own_release_denied_rtid.log");

  std::thread alice([]() {
    runFederate("./own_release_denied_alice",
                "/tmp/gorti_own_release_denied_alice.log");
  });
  std::this_thread::sleep_for(std::chrono::milliseconds(300));
  std::thread bob([]() {
    runFederate("./own_release_denied_bob",
                "/tmp/gorti_own_release_denied_bob.log");
  });

  alice.join();
  bob.join();

  EXPECT_EQ(diffAgainstGolden(
                bucketSortRO(splitNonComment(
                    slurp("/tmp/gorti_own_release_denied_alice.log"))),
                loadGolden("expected.alice.log")),
            "");
  EXPECT_EQ(diffAgainstGolden(
                bucketSortRO(splitNonComment(
                    slurp("/tmp/gorti_own_release_denied_bob.log"))),
                loadGolden("expected.bob.log")),
            "");
}
