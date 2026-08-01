// M31 conformance driver: threading_callback_reentry.
//
// THE ONLY M31 FIXTURE that exercises catalogue row 17.2
// (CallNotAllowedFromWithinCallback). Without this, the exception
// class has no runtime witness.

#include <gtest/gtest.h>

#include "_harness/rtid_runner.h"
#include "_harness/log_diff.h"
#include "_harness/golden_loader.h"

#include <filesystem>

namespace fs = std::filesystem;
using gorti::dlc::conformance::GoldenLoader;
using gorti::dlc::conformance::LogDiff;
using gorti::dlc::conformance::RtidRunner;

TEST(Conformance_ThreadingCallbackReentry, reentry_throws_spec_exception) {
  RtidRunner rtid;
  ASSERT_TRUE(rtid.start());
  fs::path dir(FIXTURE_DIR);
  std::string fom = (dir / "federation.fom.xml").string();

  // Subscriber joins first so the federation exists when publisher runs.
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
