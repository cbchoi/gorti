// M31 conformance driver: tm_ner_pair.
//
// RED in M31 by construction:
//   - Federate sources include <RTI/*.h> ( forward-decl stubs).
//   - No rti1516e impl symbols exist → expected fail-to-link.
//   - This driver uses the shared _harness/ utilities (RtidRunner,
//     log_diff, golden_loader) shared with TASK-348.
//
// When goldens are real (post-TASK-363) and impl lands (M32+), this
// driver flips GREEN: spawn rtid, run both federates, normalize both
// logs (handle ints → <H>, strip wall-clock), diff vs goldens.

#include <gtest/gtest.h>

#include "_harness/rtid_runner.h"
#include "_harness/log_diff.h"
#include "_harness/golden_loader.h"

#include <filesystem>
#include <string>

using gorti::dlc::conformance::GoldenLoader;
using gorti::dlc::conformance::LogDiff;
using gorti::dlc::conformance::RtidRunner;

namespace fs = std::filesystem;

namespace {

fs::path fixture_dir() {
  // Build sets FIXTURE_DIR via target_compile_definitions.
  return fs::path(FIXTURE_DIR);
}

}  // namespace

TEST(Conformance_TmNerPair, regulator_constrained_pair) {
  RtidRunner rtid;
  ASSERT_TRUE(rtid.start());

  // Spawn regulator + constrained, capture each canonical log.
  // §8.8 NER cycle — order between regulator's SEND and constrained's
  // RECV is logical, not wall-clock, so harness must use TSO-strict
  // diff (no LBTS re-sort) per §5.2.1 of DLC_COMPLIANCE_PROGRAM.md.
  auto reg_log = rtid.run_federate(
      fixture_dir() / "federate_regulator",
      {"--url", rtid.url(), "--fom",
       (fixture_dir() / "federation.fom.xml").string()});
  auto con_log = rtid.run_federate(
      fixture_dir() / "federate_constrained",
      {"--url", rtid.url(), "--fom",
       (fixture_dir() / "federation.fom.xml").string()});

  GoldenLoader gold(fixture_dir());
  LogDiff diff;
  diff.set_mode(LogDiff::Mode::TsoStrict);
  EXPECT_EQ(diff.normalize(reg_log), gold.load("expected.regulator.log"));
  EXPECT_EQ(diff.normalize(con_log), gold.load("expected.constrained.log"));
}
