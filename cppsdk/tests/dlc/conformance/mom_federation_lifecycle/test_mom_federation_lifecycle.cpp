// M31 conformance driver: mom_federation_lifecycle.

#include <gtest/gtest.h>

#include "_harness/rtid_runner.h"
#include "_harness/log_diff.h"
#include "_harness/golden_loader.h"

#include <filesystem>

namespace fs = std::filesystem;
using gorti::dlc::conformance::GoldenLoader;
using gorti::dlc::conformance::LogDiff;
using gorti::dlc::conformance::RtidRunner;

TEST(Conformance_MomFederationLifecycle, observe_join_resign_via_standard_pubsub) {
  RtidRunner rtid;
  ASSERT_TRUE(rtid.start());
  fs::path dir(FIXTURE_DIR);
  std::string fom = (dir / "federation.fom.xml").string();

  // Observer joins first and subscribes the MOM HLAfederate class.
  auto obs_handle = rtid.spawn_federate(
      dir / "federate_observer",
      {"--url", rtid.url(), "--fom", fom});
  rtid.wait_for_join("observer");

  // Alice joins, idles, resigns first.
  auto alice_log = rtid.run_federate(
      dir / "federate_member",
      {"--url", rtid.url(), "--fom", fom, "--name", "alice",
       "--dwell-ms", "300"});
  // Bob joins after alice has already resigned.
  auto bob_log = rtid.run_federate(
      dir / "federate_member",
      {"--url", rtid.url(), "--fom", fom, "--name", "bob",
       "--dwell-ms", "300"});

  auto obs_log = rtid.wait_federate(obs_handle);

  GoldenLoader gold(dir);
  LogDiff diff;
  diff.set_mode(LogDiff::Mode::LbtsRoSort);
  EXPECT_EQ(diff.normalize(alice_log), gold.load("expected.alice.log"));
  EXPECT_EQ(diff.normalize(bob_log),   gold.load("expected.bob.log"));
  EXPECT_EQ(diff.normalize(obs_log),   gold.load("expected.observer.log"));
}
