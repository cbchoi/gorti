// test_dm_unpublish_whole_vs_attrs.cpp — gtest driver for fixture #7.

#include "../_harness/rtid_runner.h"
#include "../_harness/log_diff.h"
#include "../_harness/golden_loader.h"

#include <cstdlib>
#include <thread>

#include <gtest/gtest.h>

namespace {
using namespace gorti_dlc_harness;
void runFederate(const std::string& bin, const std::string& out) {
  std::system((bin + " > " + out + " 2>&1").c_str());
}
}  // namespace

TEST(Conformance, dm_unpublish_whole_vs_attrs) {
  RtidRunner rtid("/tmp/gorti_dm_unpublish_rtid.log");
  std::thread fed([]() {
    runFederate("./dm_unpublish_whole_vs_attrs_federate",
                "/tmp/gorti_dm_unpublish_fed.log");
  });
  fed.join();
  EXPECT_EQ(diffAgainstGolden(
                bucketSortRO(splitNonComment(
                    slurp("/tmp/gorti_dm_unpublish_fed.log"))),
                loadGolden("expected.federate.log")),
            "");
}
