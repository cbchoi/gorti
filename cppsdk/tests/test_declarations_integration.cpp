// M17.4 — §5 publish/subscribe declarations.
//
// These RPCs return Empty on success — verifying server-side
// observable state requires the cross-language smoke (M17.7). For
// M17.4 we verify the wire round-trips don't throw + the
// connection/join guards work.

#include <gtest/gtest.h>

#include "rti1516e/Exceptions.h"
#include "rti1516e/RtiAmbassador.h"

#include "fixtures/RtidProcess.h"

namespace {

using rti1516e_test::RtidProcess;
const std::string kFomPath = TEST_FOM_PATH;

class DeclarationsIntegration : public ::testing::Test {
 protected:
  void SetUp() override {
    rtid = std::make_unique<RtidProcess>();
    amb.connect(rtid->url());
    amb.createFederationExecution("m17-4-decl", {kFomPath});
    amb.joinFederationExecution("alice", "m17-4-decl");
    vehicle_ = amb.getObjectClassHandle("Vehicle");
    position_ = amb.getAttributeHandle(vehicle_, "Position");
    velocity_ = amb.getAttributeHandle(vehicle_, "Velocity");
    honk_ = amb.getInteractionClassHandle("Honk");
  }
  void TearDown() override {
    if (amb.isConnected()) {
      try { amb.resignFederationExecution(); } catch (...) {}
      try { amb.destroyFederationExecution("m17-4-decl"); } catch (...) {}
      amb.disconnect();
    }
  }
  std::unique_ptr<RtidProcess> rtid;
  rti1516e::RTIambassador amb;
  rti1516e::ObjectClassHandle vehicle_;
  rti1516e::AttributeHandle position_;
  rti1516e::AttributeHandle velocity_;
  rti1516e::InteractionClassHandle honk_;
};

TEST_F(DeclarationsIntegration, PublishObjectClassAttributes) {
  amb.publishObjectClassAttributes(vehicle_, {position_, velocity_});
}

TEST_F(DeclarationsIntegration, SubscribeObjectClassAttributes) {
  amb.subscribeObjectClassAttributes(vehicle_, {position_, velocity_});
}

TEST_F(DeclarationsIntegration, PublishInteractionClass) {
  amb.publishInteractionClass(honk_);
}

TEST_F(DeclarationsIntegration, SubscribeInteractionClass) {
  amb.subscribeInteractionClass(honk_);
}

TEST_F(DeclarationsIntegration, UnpublishObjectClassAttributes) {
  amb.publishObjectClassAttributes(vehicle_, {position_, velocity_});
  amb.unpublishObjectClassAttributes(vehicle_, {position_});
}

TEST_F(DeclarationsIntegration, UnsubscribeObjectClassAttributes) {
  amb.subscribeObjectClassAttributes(vehicle_, {position_, velocity_});
  amb.unsubscribeObjectClassAttributes(vehicle_, {position_});
}

TEST_F(DeclarationsIntegration, EmptyAttributeSetIsAccepted) {
  // The Go-side manager records the publish intent even with no
  // attributes bound. Matches the §5 contract.
  amb.publishObjectClassAttributes(vehicle_, {});
  amb.subscribeObjectClassAttributes(vehicle_, {});
}

TEST_F(DeclarationsIntegration, RepeatedPublishIsIdempotent) {
  amb.publishObjectClassAttributes(vehicle_, {position_});
  amb.publishObjectClassAttributes(vehicle_, {position_});
  amb.publishObjectClassAttributes(vehicle_, {position_, velocity_});
}

TEST(DeclarationsRequireJoin, AllOperationsThrowPreJoin) {
  RtidProcess rtid;
  rti1516e::RTIambassador amb;
  amb.connect(rtid.url());
  EXPECT_THROW(amb.publishObjectClassAttributes(
                   rti1516e::ObjectClassHandle(1), {}),
               rti1516e::FederateNotExecutionMember);
  EXPECT_THROW(amb.subscribeObjectClassAttributes(
                   rti1516e::ObjectClassHandle(1), {}),
               rti1516e::FederateNotExecutionMember);
  EXPECT_THROW(
      amb.publishInteractionClass(rti1516e::InteractionClassHandle(1)),
      rti1516e::FederateNotExecutionMember);
  EXPECT_THROW(
      amb.subscribeInteractionClass(rti1516e::InteractionClassHandle(1)),
      rti1516e::FederateNotExecutionMember);
}

}  // namespace
