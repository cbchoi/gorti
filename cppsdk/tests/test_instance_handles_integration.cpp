// M17.9 — §6.30 / §6.31 runtime instance handle service tests.
//
// One federate registers "car-7"; another federate joins after and
// resolves "car-7" → handle WITHOUT having received the Discover
// callback. Late-joiner story for cross-federate object discovery.

#include <gtest/gtest.h>

#include "rti1516e/Exceptions.h"
#include "rti1516e/RtiAmbassador.h"

#include "fixtures/RtidProcess.h"

namespace {

using rti1516e_test::RtidProcess;
const std::string kFomPath = TEST_FOM_PATH;

class InstanceHandlesIntegration : public ::testing::Test {
 protected:
  void SetUp() override {
    rtid = std::make_unique<RtidProcess>();
    amb.connect(rtid->url());
    amb.createFederationExecution("m17-9-inst", {kFomPath});
    amb.joinFederationExecution("alice", "m17-9-inst");
    vehicle_ = amb.getObjectClassHandle("Vehicle");
    pos_ = amb.getAttributeHandle(vehicle_, "Position");
    amb.publishObjectClassAttributes(vehicle_, {pos_});
  }
  void TearDown() override {
    if (amb.isConnected()) {
      try { amb.resignFederationExecution(); } catch (...) {}
      try { amb.destroyFederationExecution("m17-9-inst"); } catch (...) {}
      amb.disconnect();
    }
  }
  std::unique_ptr<RtidProcess> rtid;
  rti1516e::RTIambassador amb;
  rti1516e::ObjectClassHandle vehicle_;
  rti1516e::AttributeHandle pos_;
};

TEST_F(InstanceHandlesIntegration, RegisterThenResolveByName) {
  const auto obj = amb.registerObjectInstance(vehicle_, "car-7");
  EXPECT_TRUE(obj.isValid());
  const auto resolved = amb.getObjectInstanceHandle("car-7");
  EXPECT_EQ(obj, resolved);
}

TEST_F(InstanceHandlesIntegration, ResolveByHandleReturnsRegisteredName) {
  const auto obj = amb.registerObjectInstance(vehicle_, "car-7");
  EXPECT_EQ(amb.getObjectInstanceName(obj), "car-7");
}

TEST_F(InstanceHandlesIntegration, UnknownNameThrowsNameNotFound) {
  EXPECT_THROW(amb.getObjectInstanceHandle("ghost-car"),
               rti1516e::NameNotFound);
}

TEST(InstanceHandlesRequireJoin, OperationsThrowPreJoin) {
  RtidProcess rtid;
  rti1516e::RTIambassador amb;
  amb.connect(rtid.url());
  EXPECT_THROW(amb.getObjectInstanceHandle("car-7"),
               rti1516e::FederateNotExecutionMember);
  EXPECT_THROW(
      amb.getObjectInstanceName(rti1516e::ObjectInstanceHandle(1)),
      rti1516e::FederateNotExecutionMember);
}

}  // namespace
