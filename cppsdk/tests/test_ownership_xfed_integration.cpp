// M17.27 — two-federate §7 ownership transfer integration tests.
//
// Covers the cross-federate path the single-federate
// test_ownership_integration suite couldn't exercise:
//
//   1. alice publishes Vehicle.Position, registers car-x, becomes
//      implicit owner.
//   2. bob subscribes to Vehicle.Position; discovers car-x.
//   3. alice calls negotiatedAttributeOwnershipDivestiture →
//      manager fans out requestAttributeOwnershipAssumption to bob.
//   4. bob calls attributeOwnershipAcquisition → manager fans out
//      attributeOwnershipAcquisitionNotification (to bob, the new
//      owner) AND requestDivestitureConfirmation (to alice, the
//      previous owner).
//   5. The post-transfer queryAttributeOwnership reports bob as
//      the current owner.

#include <gtest/gtest.h>

#include <chrono>
#include <memory>
#include <string>
#include <thread>
#include <vector>

#include "rti1516e/FederateAmbassador.h"
#include "rti1516e/RtiAmbassador.h"

#include "fixtures/RtidProcess.h"

namespace {

using rti1516e_test::RtidProcess;
const std::string kFomPath = TEST_FOM_PATH;

class RecordingFed : public rti1516e::FederateAmbassador {
 public:
  std::vector<std::pair<rti1516e::ObjectInstanceHandle, std::string>> discovered;
  std::vector<rti1516e::ObjectInstanceHandle> assumed_objects;
  std::vector<rti1516e::AttributeHandleSet> assumed_attrs;
  std::vector<rti1516e::ObjectInstanceHandle> acquired_objects;
  std::vector<rti1516e::AttributeHandleSet> acquired_attrs;
  std::vector<rti1516e::ObjectInstanceHandle> divest_confirmed_objects;

  void discoverObjectInstance(
      rti1516e::ObjectInstanceHandle obj,
      rti1516e::ObjectClassHandle /*cls*/,
      const std::string& name) override {
    discovered.emplace_back(obj, name);
  }
  void requestAttributeOwnershipAssumption(
      rti1516e::ObjectInstanceHandle obj,
      const rti1516e::AttributeHandleSet& attrs,
      rti1516e::FederateHandle /*divesting*/,
      const rti1516e::VariableLengthData& /*tag*/) override {
    assumed_objects.push_back(obj);
    assumed_attrs.push_back(attrs);
  }
  void attributeOwnershipAcquisitionNotification(
      rti1516e::ObjectInstanceHandle obj,
      const rti1516e::AttributeHandleSet& attrs,
      rti1516e::FederateHandle /*owner*/) override {
    acquired_objects.push_back(obj);
    acquired_attrs.push_back(attrs);
  }
  void requestDivestitureConfirmation(
      rti1516e::ObjectInstanceHandle obj,
      const rti1516e::AttributeHandleSet& /*attrs*/) override {
    divest_confirmed_objects.push_back(obj);
  }
};

// Pump tickCallback on `amb` until `pred()` is true or `timeout`
// elapses.
template <typename Amb, typename Pred>
void pumpUntil(Amb& amb, Pred pred, double timeout = 3.0) {
  const auto deadline =
      std::chrono::steady_clock::now() +
      std::chrono::milliseconds(static_cast<int>(timeout * 1000));
  while (std::chrono::steady_clock::now() < deadline) {
    amb.tickCallback(0.05, 0.1);
    if (pred()) return;
  }
}

class OwnershipXFedIntegration : public ::testing::Test {
 protected:
  void SetUp() override {
    rtid = std::make_unique<RtidProcess>();
    // alice
    alice_amb.setFederateAmbassador(&alice);
    alice_amb.connect(rtid->url());
    alice_amb.createFederationExecution("m17-27-xfed", {kFomPath});
    alice_handle = alice_amb.joinFederationExecution("alice", "m17-27-xfed");
    vehicle = alice_amb.getObjectClassHandle("Vehicle");
    pos = alice_amb.getAttributeHandle(vehicle, "Position");
    alice_amb.publishObjectClassAttributes(vehicle, {pos});
    // bob
    bob_amb.setFederateAmbassador(&bob);
    bob_amb.connect(rtid->url());
    bob_handle = bob_amb.joinFederationExecution("bob", "m17-27-xfed");
    // bob subscribes BEFORE alice registers so the discover fires.
    bob_amb.subscribeObjectClassAttributes(vehicle, {pos});
  }
  void TearDown() override {
    for (auto* amb : {&bob_amb, &alice_amb}) {
      if (amb->isConnected()) {
        try { amb->resignFederationExecution(); } catch (...) {}
        amb->disconnect();
      }
    }
    // alice destroyed the federation? both resigned, so destroy from
    // a fresh ambassador (lightweight — already connected to rtid).
    rti1516e::RTIambassador cleanup;
    cleanup.connect(rtid->url());
    try { cleanup.destroyFederationExecution("m17-27-xfed"); } catch (...) {}
    cleanup.disconnect();
  }
  std::unique_ptr<RtidProcess> rtid;
  RecordingFed alice;
  RecordingFed bob;
  rti1516e::RTIambassador alice_amb;
  rti1516e::RTIambassador bob_amb;
  rti1516e::FederateHandle alice_handle;
  rti1516e::FederateHandle bob_handle;
  rti1516e::ObjectClassHandle vehicle;
  rti1516e::AttributeHandle pos;
};

TEST_F(OwnershipXFedIntegration, NegotiatedDivestFiresAssumptionOnSubscriber) {
  const auto obj = alice_amb.registerObjectInstance(vehicle, "car-neg");
  // bob should discover the new instance.
  pumpUntil(bob_amb, [&] { return !bob.discovered.empty(); });
  ASSERT_EQ(bob.discovered.size(), 1u);
  EXPECT_EQ(bob.discovered[0].second, "car-neg");

  // alice offers ownership of Position.
  rti1516e::VariableLengthData tag;
  alice_amb.negotiatedAttributeOwnershipDivestiture(obj, {pos}, tag);
  // bob receives the assumption request.
  pumpUntil(bob_amb, [&] { return !bob.assumed_objects.empty(); });
  ASSERT_EQ(bob.assumed_objects.size(), 1u);
  EXPECT_EQ(bob.assumed_objects[0], bob.discovered[0].first);
  ASSERT_EQ(bob.assumed_attrs.size(), 1u);
  EXPECT_EQ(bob.assumed_attrs[0].count(pos), 1u);
}

TEST_F(OwnershipXFedIntegration,
       AcquireAfterDivestFiresAcquiredAndConfirmation) {
  const auto obj = alice_amb.registerObjectInstance(vehicle, "car-acq");
  pumpUntil(bob_amb, [&] { return !bob.discovered.empty(); });
  ASSERT_EQ(bob.discovered.size(), 1u);
  const auto bob_obj = bob.discovered[0].first;

  rti1516e::VariableLengthData tag;
  alice_amb.negotiatedAttributeOwnershipDivestiture(obj, {pos}, tag);
  pumpUntil(bob_amb, [&] { return !bob.assumed_objects.empty(); });

  // bob accepts.
  bob_amb.attributeOwnershipAcquisition(bob_obj, {pos});

  // bob sees the acquired notification.
  pumpUntil(bob_amb, [&] { return !bob.acquired_objects.empty(); });
  ASSERT_EQ(bob.acquired_objects.size(), 1u);
  EXPECT_EQ(bob.acquired_objects[0], bob_obj);

  // alice sees the divest confirmation.
  pumpUntil(alice_amb, [&] { return !alice.divest_confirmed_objects.empty(); });
  ASSERT_EQ(alice.divest_confirmed_objects.size(), 1u);
  EXPECT_EQ(alice.divest_confirmed_objects[0], obj);

  // queryAttributeOwnership now reports bob as the owner.
  const auto q = alice_amb.queryAttributeOwnership(obj, pos);
  EXPECT_TRUE(q.owned);
  EXPECT_EQ(q.owner, bob_handle);
}

TEST_F(OwnershipXFedIntegration, UnconditionalDivestFreesAttributeForAcquirer) {
  const auto obj = alice_amb.registerObjectInstance(vehicle, "car-uncond");
  pumpUntil(bob_amb, [&] { return !bob.discovered.empty(); });
  const auto bob_obj = bob.discovered[0].first;

  // alice unconditionally divests. The unconditional path doesn't
  // fire a Assumption callback on subscribers (per IEEE 1516.1 §7.2);
  // bob has to call acquire on his own initiative.
  alice_amb.unconditionalAttributeOwnershipDivestiture(obj, {pos});
  // Give the server a window to propagate.
  std::this_thread::sleep_for(std::chrono::milliseconds(100));
  EXPECT_FALSE(
      alice_amb.isAttributeOwnedByFederate(obj, pos))
      << "alice should no longer own Position after unconditional divest";

  // bob acquires.
  bob_amb.attributeOwnershipAcquisition(bob_obj, {pos});
  pumpUntil(bob_amb, [&] { return !bob.acquired_objects.empty(); });
  ASSERT_EQ(bob.acquired_objects.size(), 1u);
  EXPECT_TRUE(bob_amb.isAttributeOwnedByFederate(bob_obj, pos));
}

}  // namespace
