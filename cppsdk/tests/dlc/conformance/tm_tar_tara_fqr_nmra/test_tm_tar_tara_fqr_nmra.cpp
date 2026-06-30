// M31 conformance driver: tm_tar_tara_fqr_nmra.
// Walks TAR/TARA/FQR/NMR/NMRA in one federate; compares grant log.

#include <gtest/gtest.h>

#include "_harness/rtid_runner.h"
#include "_harness/log_diff.h"
#include "_harness/golden_loader.h"

#include <filesystem>

namespace fs = std::filesystem;
using gorti::dlc::conformance::GoldenLoader;
using gorti::dlc::conformance::LogDiff;
using gorti::dlc::conformance::RtidRunner;

TEST(Conformance_TmAdvancePrimitives, all_four_grant_walk) {
  RtidRunner rtid;
  ASSERT_TRUE(rtid.start());
  fs::path dir(FIXTURE_DIR);

  auto log = rtid.run_federate(
      dir / "federate_walker",
      {"--url", rtid.url(), "--fom", (dir / "federation.fom.xml").string()});

  GoldenLoader gold(dir);
  LogDiff diff;
  diff.set_mode(LogDiff::Mode::TsoStrict);
  EXPECT_EQ(diff.normalize(log), gold.load("expected.walker.log"));
}
