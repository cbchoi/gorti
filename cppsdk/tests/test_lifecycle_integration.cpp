// M17.2 — federation lifecycle integration tests.
//
// Spawns a real rtid subprocess and drives RTIambassador through
// create/join/resign/destroy. Verifies cross-language compatibility
// (the federation created here can be subsequently inspected via
// rtid's AdminService — covered in M17.3+).

#include <gtest/gtest.h>

#include <thread>

#include "rti1516e/Exceptions.h"
#include "rti1516e/RtiAmbassador.h"

#include "fixtures/RtidProcess.h"

namespace {

using rti1516e_test::RtidProcess;

TEST(LifecycleIntegration, ConnectToLiveRtid) {
  RtidProcess rtid;
  rti1516e::RTIambassador amb;
  amb.connect(rtid.url());
  EXPECT_TRUE(amb.isConnected());
  amb.disconnect();
  EXPECT_FALSE(amb.isConnected());
}

TEST(LifecycleIntegration, CreateFederationFresh) {
  RtidProcess rtid;
  rti1516e::RTIambassador amb;
  amb.connect(rtid.url());
  // No FOM modules — equivalent to the M2 spec fixtures: a valid
  // empty FOM is accepted, no class declarations available, but
  // the federation lifecycle works.
  amb.createFederationExecution("m17-2-fresh", {});
  amb.destroyFederationExecution("m17-2-fresh");
  amb.disconnect();
}

TEST(LifecycleIntegration, CreateFederationTwiceThrows) {
  RtidProcess rtid;
  rti1516e::RTIambassador amb;
  amb.connect(rtid.url());
  amb.createFederationExecution("m17-2-dup", {});
  EXPECT_THROW(amb.createFederationExecution("m17-2-dup", {}),
               rti1516e::FederationExecutionAlreadyExists);
  amb.destroyFederationExecution("m17-2-dup");
  amb.disconnect();
}

TEST(LifecycleIntegration, JoinAndResign) {
  RtidProcess rtid;
  rti1516e::RTIambassador amb;
  amb.connect(rtid.url());
  amb.createFederationExecution("m17-2-join", {});
  const auto handle = amb.joinFederationExecution("alice", "m17-2-join");
  EXPECT_TRUE(handle.isValid());
  amb.resignFederationExecution();
  amb.destroyFederationExecution("m17-2-join");
  amb.disconnect();
}

TEST(LifecycleIntegration, JoinWithoutCreateFails) {
  RtidProcess rtid;
  rti1516e::RTIambassador amb;
  amb.connect(rtid.url());
  EXPECT_THROW(
      amb.joinFederationExecution("alice", "m17-2-nonexistent"),
      rti1516e::FederationExecutionDoesNotExist);
  amb.disconnect();
}

TEST(LifecycleIntegration, JoinTwiceThrows) {
  RtidProcess rtid;
  rti1516e::RTIambassador amb;
  amb.connect(rtid.url());
  amb.createFederationExecution("m17-2-rejoin", {});
  amb.joinFederationExecution("alice", "m17-2-rejoin");
  EXPECT_THROW(
      amb.joinFederationExecution("alice", "m17-2-rejoin"),
      rti1516e::FederateAlreadyExecutionMember);
  amb.resignFederationExecution();
  amb.destroyFederationExecution("m17-2-rejoin");
  amb.disconnect();
}

TEST(LifecycleIntegration, ResignWithoutJoinThrows) {
  RtidProcess rtid;
  rti1516e::RTIambassador amb;
  amb.connect(rtid.url());
  EXPECT_THROW(amb.resignFederationExecution(),
               rti1516e::FederateNotExecutionMember);
  amb.disconnect();
}

TEST(LifecycleIntegration, OperationsRequireConnection) {
  rti1516e::RTIambassador amb;
  EXPECT_THROW(amb.createFederationExecution("x", {}),
               rti1516e::NotConnected);
  EXPECT_THROW(amb.joinFederationExecution("a", "x"),
               rti1516e::NotConnected);
  EXPECT_THROW(amb.resignFederationExecution(),
               rti1516e::NotConnected);
  EXPECT_THROW(amb.destroyFederationExecution("x"),
               rti1516e::NotConnected);
}

TEST(LifecycleIntegration, TwoSequentialJoinsToDifferentFederations) {
  RtidProcess rtid;
  rti1516e::RTIambassador amb;
  amb.connect(rtid.url());
  amb.createFederationExecution("m17-2-fedA", {});
  amb.createFederationExecution("m17-2-fedB", {});

  amb.joinFederationExecution("alice", "m17-2-fedA");
  amb.resignFederationExecution();
  amb.joinFederationExecution("alice", "m17-2-fedB");
  amb.resignFederationExecution();

  amb.destroyFederationExecution("m17-2-fedA");
  amb.destroyFederationExecution("m17-2-fedB");
  amb.disconnect();
}

}  // namespace
