// M31 conformance driver: tm_tso_ordering — strict TSO order witness.
//
// IMPORTANT (per §5.2.1 of docs/DLC_COMPLIANCE_PROGRAM.md):
//   - LogDiff::Mode::TsoStrict — do NOT sort within an LBTS bucket.
//     RO would be allowed to sort; TSO must not. Locking this mode here
//     is half the point of the fixture.

#include <gtest/gtest.h>

#include "_harness/rtid_runner.h"
#include "_harness/log_diff.h"
#include "_harness/golden_loader.h"

#include <filesystem>

namespace fs = std::filesystem;
using gorti::dlc::conformance::GoldenLoader;
using gorti::dlc::conformance::LogDiff;
using gorti::dlc::conformance::RtidRunner;

TEST(Conformance_TmTsoOrdering, three_pubs_strict_canonical_order) {
  RtidRunner rtid;
  ASSERT_TRUE(rtid.start());
  fs::path dir(FIXTURE_DIR);
  std::string fom = (dir / "federation.fom.xml").string();

  // Spawn the subscriber first (it creates the federation and subscribes
  // before publishers send), then alice/bob/carol in that handle order.
  auto sub_handle = rtid.spawn_federate(
      dir / "federate_subscriber",
      {"--url", rtid.url(), "--fom", fom});
  rtid.wait_for_join("sub");

  auto alice_log = rtid.run_federate(
      dir / "federate_publisher",
      {"--url", rtid.url(), "--fom", fom, "--name", "alice"});
  auto bob_log = rtid.run_federate(
      dir / "federate_publisher",
      {"--url", rtid.url(), "--fom", fom, "--name", "bob"});
  auto carol_log = rtid.run_federate(
      dir / "federate_publisher",
      {"--url", rtid.url(), "--fom", fom, "--name", "carol"});

  auto sub_log = rtid.wait_federate(sub_handle);

  GoldenLoader gold(dir);
  LogDiff diff;
  diff.set_mode(LogDiff::Mode::TsoStrict);  // CRITICAL: no LBTS re-sort.
  EXPECT_EQ(diff.normalize(alice_log), gold.load("expected.alice.log"));
  EXPECT_EQ(diff.normalize(bob_log),   gold.load("expected.bob.log"));
  EXPECT_EQ(diff.normalize(carol_log), gold.load("expected.carol.log"));
  EXPECT_EQ(diff.normalize(sub_log),   gold.load("expected.subscriber.log"));
}
