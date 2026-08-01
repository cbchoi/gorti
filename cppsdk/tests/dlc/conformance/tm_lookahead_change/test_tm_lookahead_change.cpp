// M31 conformance driver: tm_lookahead_change.

#include <gtest/gtest.h>

#include "_harness/rtid_runner.h"
#include "_harness/log_diff.h"
#include "_harness/golden_loader.h"

#include <filesystem>

namespace fs = std::filesystem;
using gorti::dlc::conformance::GoldenLoader;
using gorti::dlc::conformance::LogDiff;
using gorti::dlc::conformance::RtidRunner;

TEST(Conformance_TmLookaheadChange, modify_propagates_to_galt) {
  RtidRunner rtid;
  ASSERT_TRUE(rtid.start());
  fs::path dir(FIXTURE_DIR);

  auto reg_log = rtid.run_federate(
      dir / "federate_regulator",
      {"--url", rtid.url(), "--fom", (dir / "federation.fom.xml").string()});
  auto obs_log = rtid.run_federate(
      dir / "federate_observer",
      {"--url", rtid.url(), "--fom", (dir / "federation.fom.xml").string()});

  GoldenLoader gold(dir);
  LogDiff diff;
  diff.set_mode(LogDiff::Mode::TsoStrict);
  EXPECT_EQ(diff.normalize(reg_log), gold.load("expected.regulator.log"));
  EXPECT_EQ(diff.normalize(obs_log), gold.load("expected.observer.log"));
}
