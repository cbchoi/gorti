// M31 conformance driver: ddm_region_mod_in_flight.

#include <gtest/gtest.h>

#include "_harness/rtid_runner.h"
#include "_harness/log_diff.h"
#include "_harness/golden_loader.h"

#include <filesystem>

namespace fs = std::filesystem;
using gorti::dlc::conformance::GoldenLoader;
using gorti::dlc::conformance::LogDiff;
using gorti::dlc::conformance::RtidRunner;

TEST(Conformance_DdmRegionModInFlight, modify_triggers_scope_advisories) {
  RtidRunner rtid;
  ASSERT_TRUE(rtid.start());
  fs::path dir(FIXTURE_DIR);
  std::string fom = (dir / "federation.fom.xml").string();

  // Subscriber first — it creates the federation, subscribes, and starts
  // pumping before the publisher runs its scripted send loop.
  auto sub_handle = rtid.spawn_federate(
      dir / "federate_subscriber",
      {"--url", rtid.url(), "--fom", fom});
  rtid.wait_for_join("sub");

  auto pub_log = rtid.run_federate(
      dir / "federate_publisher",
      {"--url", rtid.url(), "--fom", fom});
  auto sub_log = rtid.wait_federate(sub_handle);

  GoldenLoader gold(dir);
  LogDiff diff;
  diff.set_mode(LogDiff::Mode::LbtsRoSort);
  EXPECT_EQ(diff.normalize(pub_log), gold.load("expected.publisher.log"));
  EXPECT_EQ(diff.normalize(sub_log), gold.load("expected.subscriber.log"));
}
