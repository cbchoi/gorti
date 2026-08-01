// M31 conformance driver: xlang_python_cpp_pubsub.
//
// gorti-only fixture (no cross-implementation parity leg — reference_rti Free recorded local version ships
// no Python binding). Spawns:
//   - C++ DLC subscriber via cppsdk
//   - Python publisher via pysdk M28 typed-handle path
// Both connect to the same rtid; subscriber's REFLECT lines must
// match publisher's emitted values byte-for-byte.

#include <gtest/gtest.h>

#include "_harness/rtid_runner.h"
#include "_harness/log_diff.h"
#include "_harness/golden_loader.h"

#include <filesystem>

namespace fs = std::filesystem;
using gorti::dlc::conformance::GoldenLoader;
using gorti::dlc::conformance::LogDiff;
using gorti::dlc::conformance::RtidRunner;

TEST(Conformance_XlangPythonCppPubSub, python_pub_cpp_sub_wire_match) {
  RtidRunner rtid;
  ASSERT_TRUE(rtid.start());
  fs::path dir(FIXTURE_DIR);
  std::string fom = (dir / "federation.fom.xml").string();

  auto sub_handle = rtid.spawn_federate(
      dir / "federate_subscriber",
      {"--url", rtid.url(), "--fom", fom});
  rtid.wait_for_join("cpp-sub");

  // Python publisher runs as a subprocess invoked via the system Python.
  // PYTHONPATH is configured by the test driver to include the repo's
  // pysdk dir.
  auto pub_log = rtid.run_python(
      dir / "python_pub.py",
      {"--url", rtid.url(), "--fom", fom});
  auto sub_log = rtid.wait_federate(sub_handle);

  GoldenLoader gold(dir);
  LogDiff diff;
  diff.set_mode(LogDiff::Mode::LbtsRoSort);
  EXPECT_EQ(diff.normalize(pub_log), gold.load("expected.python_pub.log"));
  EXPECT_EQ(diff.normalize(sub_log), gold.load("expected.cpp_sub.log"));
}
