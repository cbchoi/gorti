// M17.6 — §10.4 tickCallback + FederateAmbassador integration tests.
//
// Verifies the callback dispatch path end-to-end:
//   1. A "publisher" ambassador joins, publishes, registers, updates,
//      sends interaction.
//   2. A "subscriber" ambassador (with a FederateAmbassador subclass
//      bound) joins, subscribes, then loops tickCallback() for a
//      bounded window.
//   3. The subclass's override slots accumulate the observed callbacks;
//      the test asserts on the captured state.
//
// Note: both ambassadors are in the same process for simplicity. The
// gRPC federate-handle / federation routing ensures publisher's
// updates reach the subscriber via rtid's event stream.

#include <gtest/gtest.h>

#include <atomic>
#include <chrono>
#include <cstdint>
#include <optional>
#include <thread>
#include <vector>

#include "rti1516e/Exceptions.h"
#include "rti1516e/FederateAmbassador.h"
#include "rti1516e/RtiAmbassador.h"

#include "fixtures/RtidProcess.h"

namespace {

using rti1516e_test::RtidProcess;
const std::string kFomPath = TEST_FOM_PATH;

// Recording FederateAmbassador — captures each callback into a
// thread-safe queue the test reads after tickCallback returns.
class RecordingFed : public rti1516e::FederateAmbassador {
 public:
  struct Discovered {
    rti1516e::ObjectInstanceHandle object;
    rti1516e::ObjectClassHandle object_class;
    std::string object_name;
  };
  struct Reflected {
    rti1516e::ObjectInstanceHandle object;
    rti1516e::AttributeHandleValueMap values;
  };
  struct Received {
    rti1516e::InteractionClassHandle interaction_class;
    rti1516e::ParameterHandleValueMap parameters;
  };

  std::vector<Discovered> discovered;
  std::vector<Reflected> reflected;
  std::vector<Received> received;

  void discoverObjectInstance(rti1516e::ObjectInstanceHandle obj,
                              rti1516e::ObjectClassHandle cls,
                              const std::string& name) override {
    discovered.push_back({obj, cls, name});
  }
  void reflectAttributeValues(
      rti1516e::ObjectInstanceHandle obj,
      const rti1516e::AttributeHandleValueMap& values,
      std::optional<double> /*ts*/) override {
    reflected.push_back({obj, values});
  }
  void receiveInteraction(
      rti1516e::InteractionClassHandle cls,
      const rti1516e::ParameterHandleValueMap& parameters,
      std::optional<double> /*ts*/) override {
    received.push_back({cls, parameters});
  }
};

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

TEST(CallbacksIntegration, DiscoverReflectReceiveEndToEnd) {
  RtidProcess rtid;

  // Publisher ambassador.
  rti1516e::RTIambassador pub;
  pub.connect(rtid.url());
  pub.createFederationExecution("m17-6-cb", {kFomPath});
  pub.joinFederationExecution("publisher", "m17-6-cb");
  const auto vehicle = pub.getObjectClassHandle("Vehicle");
  const auto pos = pub.getAttributeHandle(vehicle, "Position");
  const auto vel = pub.getAttributeHandle(vehicle, "Velocity");
  const auto honk = pub.getInteractionClassHandle("Honk");
  const auto vol = pub.getParameterHandle(honk, "Volume");
  pub.publishObjectClassAttributes(vehicle, {pos, vel});
  pub.publishInteractionClass(honk);

  // Subscriber ambassador — must be created BEFORE publisher emits
  // so the subscribe lands first.
  rti1516e::RTIambassador sub;
  RecordingFed fed;
  sub.setFederateAmbassador(&fed);
  sub.connect(rtid.url());
  sub.joinFederationExecution("subscriber", "m17-6-cb");
  const auto v2 = sub.getObjectClassHandle("Vehicle");
  const auto pos2 = sub.getAttributeHandle(v2, "Position");
  const auto vel2 = sub.getAttributeHandle(v2, "Velocity");
  const auto honk2 = sub.getInteractionClassHandle("Honk");
  sub.subscribeObjectClassAttributes(v2, {pos2, vel2});
  sub.subscribeInteractionClass(honk2);

  // Brief settle for the subscriber's subscription to land server-side
  // before the publisher emits.
  std::this_thread::sleep_for(std::chrono::milliseconds(150));

  const auto h = pub.registerObjectInstance(vehicle, "car-callback");
  {
    rti1516e::AttributeHandleValueMap values;
    values[pos] = encodeUint64BE(42);
    values[vel] = encodeUint64BE(7);
    pub.updateAttributeValues(h, values);
  }
  {
    rti1516e::ParameterHandleValueMap params;
    params[vol] = encodeUint32BE(5);
    pub.sendInteraction(honk, params);
  }

  // Drain subscriber callbacks. Loop until we see all three event
  // types or the wall-clock budget expires.
  const auto deadline =
      std::chrono::steady_clock::now() + std::chrono::seconds(3);
  while (std::chrono::steady_clock::now() < deadline) {
    sub.tickCallback(0.05, 0.1);
    if (!fed.discovered.empty() && !fed.reflected.empty() &&
        !fed.received.empty()) {
      break;
    }
  }

  EXPECT_FALSE(fed.discovered.empty()) << "no discoverObjectInstance fired";
  EXPECT_FALSE(fed.reflected.empty())  << "no reflectAttributeValues fired";
  EXPECT_FALSE(fed.received.empty())   << "no receiveInteraction fired";

  // The publisher registered exactly "car-callback".
  if (!fed.discovered.empty()) {
    EXPECT_EQ(fed.discovered[0].object_name, "car-callback");
    EXPECT_TRUE(fed.discovered[0].object.isValid());
  }
  // Reflect should carry both attributes.
  if (!fed.reflected.empty()) {
    EXPECT_EQ(fed.reflected[0].values.size(), 2u);
  }

  sub.resignFederationExecution();
  pub.resignFederationExecution();
  pub.destroyFederationExecution("m17-6-cb");
  pub.disconnect();
  sub.disconnect();
}

TEST(CallbacksIntegration, TickCallbackOnIdleReturnsFalse) {
  RtidProcess rtid;
  rti1516e::RTIambassador amb;
  RecordingFed fed;
  amb.setFederateAmbassador(&fed);
  amb.connect(rtid.url());
  amb.createFederationExecution("m17-6-idle", {kFomPath});
  amb.joinFederationExecution("solo", "m17-6-idle");

  // No subscriptions, no other federates — tick should observe
  // zero callbacks.
  const auto fired = amb.tickCallback(0.05, 0.05);
  EXPECT_FALSE(fired);

  amb.resignFederationExecution();
  amb.destroyFederationExecution("m17-6-idle");
  amb.disconnect();
}

}  // namespace
