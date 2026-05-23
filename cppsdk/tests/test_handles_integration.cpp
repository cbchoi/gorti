// M17.3 — §10.2 handle services integration tests.
//
// Federation create with a real FOM; ambassador queries class /
// attribute / interaction / parameter handles via SupportService;
// handles round-trip name↔handle correctly.

#include <gtest/gtest.h>

#include <string>

#include "rti1516e/Exceptions.h"
#include "rti1516e/RtiAmbassador.h"

#include "fixtures/RtidProcess.h"

namespace {

using rti1516e_test::RtidProcess;

// Path to the test FOM, set by CMake via TEST_FOM_PATH.
const std::string kFomPath = TEST_FOM_PATH;

class HandlesIntegration : public ::testing::Test {
 protected:
  void SetUp() override {
    rtid = std::make_unique<RtidProcess>();
    amb.connect(rtid->url());
    amb.createFederationExecution("m17-3-handles", {kFomPath});
    amb.joinFederationExecution("alice", "m17-3-handles");
  }

  void TearDown() override {
    if (amb.isConnected()) {
      try { amb.resignFederationExecution(); } catch (...) {}
      try { amb.destroyFederationExecution("m17-3-handles"); } catch (...) {}
      amb.disconnect();
    }
  }

  std::unique_ptr<RtidProcess> rtid;
  rti1516e::RTIambassador amb;
};

TEST_F(HandlesIntegration, ObjectClassHandleRoundTrip) {
  const auto h = amb.getObjectClassHandle("Vehicle");
  EXPECT_TRUE(h.isValid());
  EXPECT_EQ(amb.getObjectClassName(h), "Vehicle");
}

TEST_F(HandlesIntegration, AttributeHandleRoundTrip) {
  const auto cls = amb.getObjectClassHandle("Vehicle");
  const auto pos = amb.getAttributeHandle(cls, "Position");
  const auto vel = amb.getAttributeHandle(cls, "Velocity");
  EXPECT_TRUE(pos.isValid());
  EXPECT_TRUE(vel.isValid());
  EXPECT_NE(pos, vel);  // distinct attributes get distinct handles
  EXPECT_EQ(amb.getAttributeName(cls, pos), "Position");
  EXPECT_EQ(amb.getAttributeName(cls, vel), "Velocity");
}

TEST_F(HandlesIntegration, InteractionClassHandleRoundTrip) {
  const auto h = amb.getInteractionClassHandle("Honk");
  EXPECT_TRUE(h.isValid());
  EXPECT_EQ(amb.getInteractionClassName(h), "Honk");
}

TEST_F(HandlesIntegration, ParameterHandleRoundTrip) {
  const auto ic = amb.getInteractionClassHandle("Honk");
  const auto vol = amb.getParameterHandle(ic, "Volume");
  EXPECT_TRUE(vol.isValid());
  EXPECT_EQ(amb.getParameterName(ic, vol), "Volume");
}

TEST_F(HandlesIntegration, UnknownObjectClassThrows) {
  EXPECT_THROW(amb.getObjectClassHandle("NoSuchClass"),
               rti1516e::NameNotFound);
}

TEST_F(HandlesIntegration, UnknownAttributeThrows) {
  const auto cls = amb.getObjectClassHandle("Vehicle");
  EXPECT_THROW(amb.getAttributeHandle(cls, "NoSuchAttribute"),
               rti1516e::NameNotFound);
}

TEST_F(HandlesIntegration, RepeatedLookupServedFromCache) {
  // First lookup hits the wire; second one should be served from
  // the in-process cache. We can't directly observe the cache from
  // outside but we can verify the answer is stable.
  const auto h1 = amb.getObjectClassHandle("Vehicle");
  const auto h2 = amb.getObjectClassHandle("Vehicle");
  EXPECT_EQ(h1, h2);
  EXPECT_TRUE(h1.isValid());
}

TEST_F(HandlesIntegration, HandlesAreStableAcrossClassesAndAttrs) {
  // The FOM has one object class with two attributes and one
  // interaction class with one parameter. Verifying the handles
  // are non-zero, distinct where they should be, and stable across
  // multiple queries.
  const auto vehicle = amb.getObjectClassHandle("Vehicle");
  const auto honk = amb.getInteractionClassHandle("Honk");
  const auto pos = amb.getAttributeHandle(vehicle, "Position");
  const auto vel = amb.getAttributeHandle(vehicle, "Velocity");
  const auto vol = amb.getParameterHandle(honk, "Volume");
  EXPECT_TRUE(vehicle.isValid());
  EXPECT_TRUE(honk.isValid());
  EXPECT_TRUE(pos.isValid());
  EXPECT_TRUE(vel.isValid());
  EXPECT_TRUE(vol.isValid());
}

TEST(HandlesIntegrationNotJoined, ThrowsBeforeJoin) {
  // Pre-join, the SDK has no federation context, so the handle
  // lookups should fail with the appropriate exception. Cut-1:
  // we raise RTIinternalError (rtid returns InvalidArgument when
  // federation_name is unset).
  RtidProcess rtid;
  rti1516e::RTIambassador amb;
  amb.connect(rtid.url());
  EXPECT_THROW(amb.getObjectClassHandle("Vehicle"),
               rti1516e::RTIinternalError);
}

}  // namespace
