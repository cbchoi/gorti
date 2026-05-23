// M17.5 — §6 register / update / send integration tests.
//
// Verifies the wire round-trips for the §6 object/interaction surface.
// Like M17.4, observable cross-federate semantics is M17.7's job —
// here we just verify the federate-side calls succeed against a real
// rtid + the proper guards apply.

#include <gtest/gtest.h>

#include <cstdint>

#include "rti1516e/Exceptions.h"
#include "rti1516e/RtiAmbassador.h"

#include "fixtures/RtidProcess.h"

namespace {

using rti1516e_test::RtidProcess;
const std::string kFomPath = TEST_FOM_PATH;

// 8 bytes for HLAfloat64BE — big-endian double. Cut-1 doesn't ship
// an encoder library; tests build the bytes by hand. A future cut
// adds rti1516e::encoding::HLAfloat64BE etc.
rti1516e::VariableLengthData encodeUint64BE(std::uint64_t v) {
  rti1516e::VariableLengthData out(8);
  for (int i = 0; i < 8; ++i) {
    out[7 - i] = static_cast<std::uint8_t>((v >> (i * 8)) & 0xff);
  }
  return out;
}

rti1516e::VariableLengthData encodeUint32BE(std::uint32_t v) {
  rti1516e::VariableLengthData out(4);
  for (int i = 0; i < 4; ++i) {
    out[3 - i] = static_cast<std::uint8_t>((v >> (i * 8)) & 0xff);
  }
  return out;
}

class ObjectsIntegration : public ::testing::Test {
 protected:
  void SetUp() override {
    rtid = std::make_unique<RtidProcess>();
    amb.connect(rtid->url());
    amb.createFederationExecution("m17-5-obj", {kFomPath});
    amb.joinFederationExecution("publisher", "m17-5-obj");
    vehicle_ = amb.getObjectClassHandle("Vehicle");
    pos_ = amb.getAttributeHandle(vehicle_, "Position");
    vel_ = amb.getAttributeHandle(vehicle_, "Velocity");
    honk_ = amb.getInteractionClassHandle("Honk");
    vol_ = amb.getParameterHandle(honk_, "Volume");
    amb.publishObjectClassAttributes(vehicle_, {pos_, vel_});
    amb.publishInteractionClass(honk_);
  }
  void TearDown() override {
    if (amb.isConnected()) {
      try { amb.resignFederationExecution(); } catch (...) {}
      try { amb.destroyFederationExecution("m17-5-obj"); } catch (...) {}
      amb.disconnect();
    }
  }
  std::unique_ptr<RtidProcess> rtid;
  rti1516e::RTIambassador amb;
  rti1516e::ObjectClassHandle vehicle_;
  rti1516e::AttributeHandle pos_;
  rti1516e::AttributeHandle vel_;
  rti1516e::InteractionClassHandle honk_;
  rti1516e::ParameterHandle vol_;
};

TEST_F(ObjectsIntegration, RegisterObjectInstanceReturnsValidHandle) {
  const auto h = amb.registerObjectInstance(vehicle_);
  EXPECT_TRUE(h.isValid());
}

TEST_F(ObjectsIntegration, RegisterObjectInstanceWithName) {
  const auto h = amb.registerObjectInstance(vehicle_, "car-7");
  EXPECT_TRUE(h.isValid());
}

TEST_F(ObjectsIntegration, RegisterMultipleInstancesGetDistinctHandles) {
  const auto h1 = amb.registerObjectInstance(vehicle_);
  const auto h2 = amb.registerObjectInstance(vehicle_);
  EXPECT_TRUE(h1.isValid());
  EXPECT_TRUE(h2.isValid());
  EXPECT_NE(h1, h2);
}

TEST_F(ObjectsIntegration, UpdateAttributeValuesRoundTrip) {
  const auto h = amb.registerObjectInstance(vehicle_, "car-update");
  rti1516e::AttributeHandleValueMap values;
  values[pos_] = encodeUint64BE(42);
  values[vel_] = encodeUint64BE(7);
  amb.updateAttributeValues(h, values);
}

TEST_F(ObjectsIntegration, SendInteractionRoundTrip) {
  rti1516e::ParameterHandleValueMap params;
  params[vol_] = encodeUint32BE(5);
  amb.sendInteraction(honk_, params);
}

TEST_F(ObjectsIntegration, SendInteractionWithEmptyParametersOk) {
  amb.sendInteraction(honk_, {});
}

TEST(ObjectsRequireJoin, AllOperationsThrowPreJoin) {
  RtidProcess rtid;
  rti1516e::RTIambassador amb;
  amb.connect(rtid.url());
  EXPECT_THROW(amb.registerObjectInstance(rti1516e::ObjectClassHandle(1)),
               rti1516e::FederateNotExecutionMember);
  EXPECT_THROW(amb.updateAttributeValues(
                   rti1516e::ObjectInstanceHandle(1), {}),
               rti1516e::FederateNotExecutionMember);
  EXPECT_THROW(amb.sendInteraction(rti1516e::InteractionClassHandle(1), {}),
               rti1516e::FederateNotExecutionMember);
}

}  // namespace
