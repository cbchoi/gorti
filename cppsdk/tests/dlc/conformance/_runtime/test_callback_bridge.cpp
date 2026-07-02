// M34 (Agent AD) — DLCFederateAmbassadorBridge delivery tests.
//
// Verifies the bridge converts M17-shaped callback arguments (raw uint64
// handles, std::string, std::optional<double> timestamps) into DLC-shaped
// callback arguments (typed handle classes, std::wstring, LogicalTime const&)
// and dispatches them to the user-supplied DLC federate ambassador.
//
// Runs GREEN with WILL_FAIL=OFF — mock-based, no RTI subprocess, no gRPC.
// This is the safety net for the callback conversion layer that makes M34
// conformance fixtures verify events instead of just link.

#include "src/dlc/FederateAmbassadorBridge.h"

#include <RTI/Enums.h>
#include <RTI/Handle.h>
#include <RTI/NullFederateAmbassador.h>
#include <RTI/Typedefs.h>
#include <RTI/VariableLengthData.h>
#include <RTI/time/HLAfloat64Time.h>

#include <cstdint>
#include <memory>
#include <optional>
#include <string>
#include <vector>

#include <gtest/gtest.h>

namespace {

// Tracking DLC ambassador — inherits NullFederateAmbassador so we only pay
// per-callback we care about. Every override records what the bridge
// delivered so the test asserts on the DLC-side representation.
class TrackingDLCFed : public rti1516e::NullFederateAmbassador {
 public:
  // discoverObjectInstance (2-arg) — records typed handles + wstring name.
  struct DiscoverCall {
    rti1516e::ObjectInstanceHandle obj;
    rti1516e::ObjectClassHandle cls;
    std::wstring name;
  };
  std::vector<DiscoverCall> discover_calls;

  void discoverObjectInstance(rti1516e::ObjectInstanceHandle obj,
                              rti1516e::ObjectClassHandle cls,
                              std::wstring const& name)
      RTI_THROW(rti1516e::FederateInternalError) override {
    discover_calls.push_back({obj, cls, name});
  }

  // reflectAttributeValues — no-time overload.
  struct ReflectNoTimeCall {
    rti1516e::ObjectInstanceHandle obj;
    rti1516e::AttributeHandleValueMap values;
    rti1516e::OrderType sentOrder;
    rti1516e::TransportationType tType;
  };
  std::vector<ReflectNoTimeCall> reflect_no_time_calls;

  void reflectAttributeValues(rti1516e::ObjectInstanceHandle obj,
                              rti1516e::AttributeHandleValueMap const& v,
                              rti1516e::VariableLengthData const& /*tag*/,
                              rti1516e::OrderType sent,
                              rti1516e::TransportationType tt,
                              rti1516e::SupplementalReflectInfo /*supp*/)
      RTI_THROW(rti1516e::FederateInternalError) override {
    reflect_no_time_calls.push_back({obj, v, sent, tt});
  }

  // reflectAttributeValues — with-time overload.
  struct ReflectWithTimeCall {
    rti1516e::ObjectInstanceHandle obj;
    rti1516e::AttributeHandleValueMap values;
    double time;
    rti1516e::OrderType sentOrder;
  };
  std::vector<ReflectWithTimeCall> reflect_with_time_calls;

  // M36 Agent DA — the bridge delivers TSO through the 9-arg
  // retraction-handle overload (Pitch shape); the mock records that form.
  void reflectAttributeValues(rti1516e::ObjectInstanceHandle obj,
                              rti1516e::AttributeHandleValueMap const& v,
                              rti1516e::VariableLengthData const& /*tag*/,
                              rti1516e::OrderType sent,
                              rti1516e::TransportationType /*tt*/,
                              rti1516e::LogicalTime const& t,
                              rti1516e::OrderType /*recvOrder*/,
                              rti1516e::MessageRetractionHandle /*retract*/,
                              rti1516e::SupplementalReflectInfo /*supp*/)
      RTI_THROW(rti1516e::FederateInternalError) override {
    // Downcast to HLAfloat64Time to read the double value.
    auto const& tf = static_cast<rti1516e::HLAfloat64Time const&>(t);
    reflect_with_time_calls.push_back({obj, v, tf.getTime(), sent});
  }

  // receiveInteraction — no-time overload.
  struct ReceiveInteractionCall {
    rti1516e::InteractionClassHandle ich;
    rti1516e::ParameterHandleValueMap params;
  };
  std::vector<ReceiveInteractionCall> receive_calls;

  void receiveInteraction(rti1516e::InteractionClassHandle ich,
                          rti1516e::ParameterHandleValueMap const& p,
                          rti1516e::VariableLengthData const& /*tag*/,
                          rti1516e::OrderType /*sent*/,
                          rti1516e::TransportationType /*tt*/,
                          rti1516e::SupplementalReceiveInfo /*supp*/)
      RTI_THROW(rti1516e::FederateInternalError) override {
    receive_calls.push_back({ich, p});
  }

  // Name reservation callbacks.
  std::vector<std::wstring> reserv_ok;
  std::vector<std::wstring> reserv_fail;
  void objectInstanceNameReservationSucceeded(std::wstring const& n)
      RTI_THROW(rti1516e::FederateInternalError) override {
    reserv_ok.push_back(n);
  }
  void objectInstanceNameReservationFailed(std::wstring const& n)
      RTI_THROW(rti1516e::FederateInternalError) override {
    reserv_fail.push_back(n);
  }

  std::vector<std::set<std::wstring>> multi_reserv_ok;
  std::vector<std::set<std::wstring>> multi_reserv_fail;
  void multipleObjectInstanceNameReservationSucceeded(
      std::set<std::wstring> const& s)
      RTI_THROW(rti1516e::FederateInternalError) override {
    multi_reserv_ok.push_back(s);
  }
  void multipleObjectInstanceNameReservationFailed(
      std::set<std::wstring> const& s)
      RTI_THROW(rti1516e::FederateInternalError) override {
    multi_reserv_fail.push_back(s);
  }

  // Time advance.
  std::vector<double> tag_grants;
  void timeAdvanceGrant(rti1516e::LogicalTime const& t)
      RTI_THROW(rti1516e::FederateInternalError) override {
    auto const& tf = static_cast<rti1516e::HLAfloat64Time const&>(t);
    tag_grants.push_back(tf.getTime());
  }

  // Sync point.
  struct AnnounceSyncCall {
    std::wstring label;
    std::vector<std::uint8_t> tag_bytes;
  };
  std::vector<AnnounceSyncCall> sync_announces;
  void announceSynchronizationPoint(std::wstring const& label,
                                    rti1516e::VariableLengthData const& tag)
      RTI_THROW(rti1516e::FederateInternalError) override {
    std::vector<std::uint8_t> bytes(
        static_cast<std::uint8_t const*>(tag.data()),
        static_cast<std::uint8_t const*>(tag.data()) + tag.size());
    sync_announces.push_back({label, std::move(bytes)});
  }

  struct FedSyncCall {
    std::wstring label;
    std::size_t failed_set_size;
  };
  std::vector<FedSyncCall> fed_syncs;
  void federationSynchronized(std::wstring const& label,
                              rti1516e::FederateHandleSet const& failed)
      RTI_THROW(rti1516e::FederateInternalError) override {
    fed_syncs.push_back({label, failed.size()});
  }

  // Save / restore.
  struct SaveInitiateCall {
    std::wstring label;
    std::optional<double> time;
  };
  std::vector<SaveInitiateCall> save_initiates;
  void initiateFederateSave(std::wstring const& label)
      RTI_THROW(rti1516e::FederateInternalError) override {
    save_initiates.push_back({label, std::nullopt});
  }
  void initiateFederateSave(std::wstring const& label,
                            rti1516e::LogicalTime const& t)
      RTI_THROW(rti1516e::FederateInternalError) override {
    auto const& tf = static_cast<rti1516e::HLAfloat64Time const&>(t);
    save_initiates.push_back({label, tf.getTime()});
  }

  int federation_saved_count = 0;
  rti1516e::SaveFailureReason last_save_failure{
      rti1516e::RTI_UNABLE_TO_SAVE};
  int federation_not_saved_count = 0;
  void federationSaved()
      RTI_THROW(rti1516e::FederateInternalError) override {
    ++federation_saved_count;
  }
  void federationNotSaved(rti1516e::SaveFailureReason r)
      RTI_THROW(rti1516e::FederateInternalError) override {
    ++federation_not_saved_count;
    last_save_failure = r;
  }

  // M36 Agent CA-4 — §7 ownership converters (own_* fixtures).
  struct OwnershipAssumptionCall {
    rti1516e::ObjectInstanceHandle obj;
    rti1516e::AttributeHandleSet attrs;
    std::vector<std::uint8_t> tag_bytes;
  };
  std::vector<OwnershipAssumptionCall> ownership_assumptions;
  void requestAttributeOwnershipAssumption(
      rti1516e::ObjectInstanceHandle obj,
      rti1516e::AttributeHandleSet const& offered,
      rti1516e::VariableLengthData const& tag)
      RTI_THROW(rti1516e::FederateInternalError) override {
    std::vector<std::uint8_t> bytes(
        static_cast<std::uint8_t const*>(tag.data()),
        static_cast<std::uint8_t const*>(tag.data()) + tag.size());
    ownership_assumptions.push_back({obj, offered, std::move(bytes)});
  }

  struct AcquisitionNotificationCall {
    rti1516e::ObjectInstanceHandle obj;
    rti1516e::AttributeHandleSet attrs;
    std::size_t tag_size;
  };
  std::vector<AcquisitionNotificationCall> acquisition_notifications;
  void attributeOwnershipAcquisitionNotification(
      rti1516e::ObjectInstanceHandle obj,
      rti1516e::AttributeHandleSet const& secured,
      rti1516e::VariableLengthData const& tag)
      RTI_THROW(rti1516e::FederateInternalError) override {
    acquisition_notifications.push_back({obj, secured, tag.size()});
  }

  struct DivestConfirmationCall {
    rti1516e::ObjectInstanceHandle obj;
    rti1516e::AttributeHandleSet attrs;
  };
  std::vector<DivestConfirmationCall> divest_confirmations;
  void requestDivestitureConfirmation(
      rti1516e::ObjectInstanceHandle obj,
      rti1516e::AttributeHandleSet const& released)
      RTI_THROW(rti1516e::FederateInternalError) override {
    divest_confirmations.push_back({obj, released});
  }

  // M36 Agent CA-4 — §4 restore converters (fm_save_restore_roundtrip).
  struct RestoreInitiateCall {
    std::wstring label;
    std::wstring federate_name;
    rti1516e::FederateHandle handle;
  };
  std::vector<RestoreInitiateCall> restore_initiates;
  void initiateFederateRestore(std::wstring const& label,
                               std::wstring const& federateName,
                               rti1516e::FederateHandle handle)
      RTI_THROW(rti1516e::FederateInternalError) override {
    restore_initiates.push_back({label, federateName, handle});
  }

  int federation_restored_count = 0;
  int federation_not_restored_count = 0;
  rti1516e::RestoreFailureReason last_restore_failure{
      rti1516e::RTI_UNABLE_TO_RESTORE};
  void federationRestored()
      RTI_THROW(rti1516e::FederateInternalError) override {
    ++federation_restored_count;
  }
  void federationNotRestored(rti1516e::RestoreFailureReason r)
      RTI_THROW(rti1516e::FederateInternalError) override {
    ++federation_not_restored_count;
    last_restore_failure = r;
  }

  // M36 Agent DA — §6.15 removeObjectInstance (RO 4-arg + TSO 6-arg).
  struct RemoveNoTimeCall {
    rti1516e::ObjectInstanceHandle obj;
    std::vector<std::uint8_t> tag_bytes;
    rti1516e::OrderType sentOrder;
  };
  std::vector<RemoveNoTimeCall> remove_no_time_calls;
  void removeObjectInstance(rti1516e::ObjectInstanceHandle obj,
                            rti1516e::VariableLengthData const& tag,
                            rti1516e::OrderType sent,
                            rti1516e::SupplementalRemoveInfo /*supp*/)
      RTI_THROW(rti1516e::FederateInternalError) override {
    std::vector<std::uint8_t> bytes(
        static_cast<std::uint8_t const*>(tag.data()),
        static_cast<std::uint8_t const*>(tag.data()) + tag.size());
    remove_no_time_calls.push_back({obj, std::move(bytes), sent});
  }

  struct RemoveWithTimeCall {
    rti1516e::ObjectInstanceHandle obj;
    double time;
    rti1516e::OrderType sentOrder;
    rti1516e::OrderType recvOrder;
  };
  std::vector<RemoveWithTimeCall> remove_with_time_calls;
  void removeObjectInstance(rti1516e::ObjectInstanceHandle obj,
                            rti1516e::VariableLengthData const& /*tag*/,
                            rti1516e::OrderType sent,
                            rti1516e::LogicalTime const& t,
                            rti1516e::OrderType recv,
                            rti1516e::MessageRetractionHandle /*retract*/,
                            rti1516e::SupplementalRemoveInfo /*supp*/)
      RTI_THROW(rti1516e::FederateInternalError) override {
    auto const& tf = static_cast<rti1516e::HLAfloat64Time const&>(t);
    remove_with_time_calls.push_back({obj, tf.getTime(), sent, recv});
  }

  // M36 Agent DA — §6.20 provideAttributeValueUpdate.
  struct ProvideUpdateCall {
    rti1516e::ObjectInstanceHandle obj;
    rti1516e::AttributeHandleSet attrs;
    std::vector<std::uint8_t> tag_bytes;
  };
  std::vector<ProvideUpdateCall> provide_update_calls;
  void provideAttributeValueUpdate(rti1516e::ObjectInstanceHandle obj,
                                   rti1516e::AttributeHandleSet const& attrs,
                                   rti1516e::VariableLengthData const& tag)
      RTI_THROW(rti1516e::FederateInternalError) override {
    std::vector<std::uint8_t> bytes(
        static_cast<std::uint8_t const*>(tag.data()),
        static_cast<std::uint8_t const*>(tag.data()) + tag.size());
    provide_update_calls.push_back({obj, attrs, std::move(bytes)});
  }
};

// ===== Conversion helpers unit-level tests =====

TEST(BridgeConv, StringToWstringWidening) {
  EXPECT_EQ(gorti::dlc::conv::s2ws("hello"), std::wstring(L"hello"));
  EXPECT_EQ(gorti::dlc::conv::s2ws(""), std::wstring(L""));
}

TEST(BridgeConv, VectorStringToSetWstring) {
  auto s = gorti::dlc::conv::s2ws_set({"a", "b", "a"});
  ASSERT_EQ(s.size(), 2u);
  EXPECT_EQ(s.count(L"a"), 1u);
  EXPECT_EQ(s.count(L"b"), 1u);
}

TEST(BridgeConv, RawUint64RoundTripsThroughTypedHandle) {
  // A non-trivial value exercises all 8 bytes of the BE encoding.
  const std::uint64_t raw = 0xDEADBEEFCAFEBABEULL;
  auto h1 = gorti::dlc::conv::to_dlc_handle<rti1516e::ObjectInstanceHandle>(
      raw);
  auto h2 = gorti::dlc::conv::to_dlc_handle<rti1516e::ObjectInstanceHandle>(
      raw);
  EXPECT_TRUE(h1.isValid());
  EXPECT_EQ(h1, h2);

  // A different raw value must NOT compare equal.
  auto h3 = gorti::dlc::conv::to_dlc_handle<rti1516e::ObjectInstanceHandle>(
      raw + 1);
  EXPECT_NE(h1, h3);

  // Zero -> invalid handle (matches DLC Handle semantics).
  auto zero =
      gorti::dlc::conv::to_dlc_handle<rti1516e::AttributeHandle>(0);
  EXPECT_FALSE(zero.isValid());
}

TEST(BridgeConv, VldByteFidelity) {
  std::vector<std::uint8_t> payload = {0x01, 0xFF, 0x00, 0x42};
  auto vld = gorti::dlc::conv::to_dlc_vld(payload);
  ASSERT_EQ(vld.size(), payload.size());
  auto const* out = static_cast<std::uint8_t const*>(vld.data());
  for (size_t i = 0; i < payload.size(); ++i) EXPECT_EQ(out[i], payload[i]);
}

TEST(BridgeConv, EmptyVldHasZeroSize) {
  auto vld = gorti::dlc::conv::to_dlc_vld({});
  EXPECT_EQ(vld.size(), 0u);
}

// ===== Bridge delivery tests =====

TEST(BridgeDelivery, DiscoverObjectInstanceForwardsAndConverts) {
  TrackingDLCFed mock;
  gorti::dlc::DLCFederateAmbassadorBridge bridge(&mock);

  // Deliver an M17-style callback via the shimmed base class.
  rti1516e_m17::ObjectInstanceHandle oh{0x11ULL};
  rti1516e_m17::ObjectClassHandle ch{0x22ULL};
  bridge.discoverObjectInstance(oh, ch, "car-42");

  ASSERT_EQ(mock.discover_calls.size(), 1u);
  const auto& call = mock.discover_calls.front();
  EXPECT_TRUE(call.obj.isValid());
  EXPECT_TRUE(call.cls.isValid());
  // Handle equality: build an expected handle the same way the bridge did.
  EXPECT_EQ(call.obj,
            gorti::dlc::conv::to_dlc_handle<rti1516e::ObjectInstanceHandle>(
                0x11ULL));
  EXPECT_EQ(call.cls,
            gorti::dlc::conv::to_dlc_handle<rti1516e::ObjectClassHandle>(
                0x22ULL));
  EXPECT_EQ(call.name, std::wstring(L"car-42"));
}

TEST(BridgeDelivery, ReflectNoTimestampSelectsNoTimeOverload) {
  TrackingDLCFed mock;
  gorti::dlc::DLCFederateAmbassadorBridge bridge(&mock);

  rti1516e_m17::ObjectInstanceHandle oh{7};
  rti1516e_m17::AttributeHandleValueMap m17_map;
  // M17 handles are StrongHandle<Tag> (public value ctor).
  rti1516e_m17::AttributeHandle ah{101};
  m17_map[ah] = std::vector<std::uint8_t>{0xAA, 0xBB};

  bridge.reflectAttributeValues(oh, m17_map, std::nullopt);

  ASSERT_EQ(mock.reflect_no_time_calls.size(), 1u);
  ASSERT_EQ(mock.reflect_with_time_calls.size(), 0u);
  const auto& call = mock.reflect_no_time_calls.front();
  EXPECT_EQ(call.values.size(), 1u);
  // Sent order = RECEIVE for no-time delivery.
  EXPECT_EQ(call.sentOrder, rti1516e::RECEIVE);
  // Attribute payload survives round-trip.
  auto key = gorti::dlc::conv::to_dlc_handle<rti1516e::AttributeHandle>(101);
  ASSERT_EQ(call.values.count(key), 1u);
  auto const& vld = call.values.at(key);
  ASSERT_EQ(vld.size(), 2u);
  auto const* p = static_cast<std::uint8_t const*>(vld.data());
  EXPECT_EQ(p[0], 0xAA);
  EXPECT_EQ(p[1], 0xBB);
}

TEST(BridgeDelivery, ReflectWithTimestampSelectsWithTimeOverload) {
  TrackingDLCFed mock;
  gorti::dlc::DLCFederateAmbassadorBridge bridge(&mock);

  rti1516e_m17::ObjectInstanceHandle oh{7};
  rti1516e_m17::AttributeHandleValueMap m17_map;
  bridge.reflectAttributeValues(oh, m17_map,
                                std::optional<double>{3.14});

  ASSERT_EQ(mock.reflect_no_time_calls.size(), 0u);
  ASSERT_EQ(mock.reflect_with_time_calls.size(), 1u);
  const auto& call = mock.reflect_with_time_calls.front();
  EXPECT_DOUBLE_EQ(call.time, 3.14);
  EXPECT_EQ(call.sentOrder, rti1516e::TIMESTAMP);
}

TEST(BridgeDelivery, ReceiveInteractionConvertsClassAndParams) {
  TrackingDLCFed mock;
  gorti::dlc::DLCFederateAmbassadorBridge bridge(&mock);

  rti1516e_m17::InteractionClassHandle ich{0x77};
  rti1516e_m17::ParameterHandleValueMap m17_pmap;
  rti1516e_m17::ParameterHandle ph{0x88};
  m17_pmap[ph] = std::vector<std::uint8_t>{0x12};

  bridge.receiveInteraction(ich, m17_pmap, std::nullopt);

  ASSERT_EQ(mock.receive_calls.size(), 1u);
  const auto& call = mock.receive_calls.front();
  EXPECT_EQ(call.ich,
            gorti::dlc::conv::to_dlc_handle<rti1516e::InteractionClassHandle>(
                0x77));
  EXPECT_EQ(call.params.size(), 1u);
}

TEST(BridgeDelivery, ObjectInstanceNameReservationSuccessWidensToWstring) {
  TrackingDLCFed mock;
  gorti::dlc::DLCFederateAmbassadorBridge bridge(&mock);
  bridge.objectInstanceNameReservationSucceeded("veh-1");
  ASSERT_EQ(mock.reserv_ok.size(), 1u);
  EXPECT_EQ(mock.reserv_ok.front(), std::wstring(L"veh-1"));
}

TEST(BridgeDelivery, ObjectInstanceNameReservationFailureWidensToWstring) {
  TrackingDLCFed mock;
  gorti::dlc::DLCFederateAmbassadorBridge bridge(&mock);
  bridge.objectInstanceNameReservationFailed("veh-2");
  ASSERT_EQ(mock.reserv_fail.size(), 1u);
  EXPECT_EQ(mock.reserv_fail.front(), std::wstring(L"veh-2"));
}

TEST(BridgeDelivery, MultipleReservationBatchConvertsToSet) {
  TrackingDLCFed mock;
  gorti::dlc::DLCFederateAmbassadorBridge bridge(&mock);
  std::vector<std::string> batch = {"a", "b", "c"};
  bridge.multipleObjectInstanceNameReservationSucceeded(batch);
  ASSERT_EQ(mock.multi_reserv_ok.size(), 1u);
  EXPECT_EQ(mock.multi_reserv_ok.front(),
            (std::set<std::wstring>{L"a", L"b", L"c"}));
}

TEST(BridgeDelivery, MultipleReservationFailureForwardsCollidingSet) {
  TrackingDLCFed mock;
  gorti::dlc::DLCFederateAmbassadorBridge bridge(&mock);
  std::vector<std::string> requested = {"a", "b"};
  std::vector<std::string> colliding = {"b"};
  bridge.multipleObjectInstanceNameReservationFailed(requested, colliding);
  ASSERT_EQ(mock.multi_reserv_fail.size(), 1u);
  EXPECT_EQ(mock.multi_reserv_fail.front(),
            (std::set<std::wstring>{L"b"}));
}

TEST(BridgeDelivery, TimeAdvanceGrantWrapsAsHLAfloat64Time) {
  TrackingDLCFed mock;
  gorti::dlc::DLCFederateAmbassadorBridge bridge(&mock);
  bridge.timeAdvanceGrant(42.5);
  ASSERT_EQ(mock.tag_grants.size(), 1u);
  EXPECT_DOUBLE_EQ(mock.tag_grants.front(), 42.5);
}

TEST(BridgeDelivery, AnnounceSyncPointForwardsTagBytes) {
  TrackingDLCFed mock;
  gorti::dlc::DLCFederateAmbassadorBridge bridge(&mock);
  rti1516e_m17::VariableLengthData tag = {0x01, 0x02, 0x03};
  bridge.announceSynchronizationPoint("READY_TO_RUN", tag);
  ASSERT_EQ(mock.sync_announces.size(), 1u);
  EXPECT_EQ(mock.sync_announces.front().label,
            std::wstring(L"READY_TO_RUN"));
  EXPECT_EQ(mock.sync_announces.front().tag_bytes,
            (std::vector<std::uint8_t>{0x01, 0x02, 0x03}));
}

TEST(BridgeDelivery, FederationSynchronizedPassesEmptyFailedSet) {
  TrackingDLCFed mock;
  gorti::dlc::DLCFederateAmbassadorBridge bridge(&mock);
  bridge.federationSynchronized("SP_A");
  ASSERT_EQ(mock.fed_syncs.size(), 1u);
  EXPECT_EQ(mock.fed_syncs.front().label, std::wstring(L"SP_A"));
  // M17 does not carry a failed-to-sync set; DLC bridge passes empty.
  EXPECT_EQ(mock.fed_syncs.front().failed_set_size, 0u);
}

TEST(BridgeDelivery, InitiateSaveNoTimeSelectsNoTimeOverload) {
  TrackingDLCFed mock;
  gorti::dlc::DLCFederateAmbassadorBridge bridge(&mock);
  bridge.initiateFederateSave("checkpoint", std::nullopt);
  ASSERT_EQ(mock.save_initiates.size(), 1u);
  EXPECT_EQ(mock.save_initiates.front().label,
            std::wstring(L"checkpoint"));
  EXPECT_FALSE(mock.save_initiates.front().time.has_value());
}

TEST(BridgeDelivery, InitiateSaveWithTimeSelectsWithTimeOverload) {
  TrackingDLCFed mock;
  gorti::dlc::DLCFederateAmbassadorBridge bridge(&mock);
  bridge.initiateFederateSave("checkpoint", std::optional<double>{9.5});
  ASSERT_EQ(mock.save_initiates.size(), 1u);
  ASSERT_TRUE(mock.save_initiates.front().time.has_value());
  EXPECT_DOUBLE_EQ(*mock.save_initiates.front().time, 9.5);
}

TEST(BridgeDelivery, FederationSavedDropsLabelPerDLCSignature) {
  TrackingDLCFed mock;
  gorti::dlc::DLCFederateAmbassadorBridge bridge(&mock);
  bridge.federationSaved("anything");
  EXPECT_EQ(mock.federation_saved_count, 1);
}

TEST(BridgeDelivery, FederationNotSavedSupplementsFailureReason) {
  TrackingDLCFed mock;
  gorti::dlc::DLCFederateAmbassadorBridge bridge(&mock);
  bridge.federationNotSaved("anything");
  EXPECT_EQ(mock.federation_not_saved_count, 1);
  // M17 does not carry a reason; bridge picks RTI_UNABLE_TO_SAVE per header
  // comment.
  EXPECT_EQ(mock.last_save_failure, rti1516e::RTI_UNABLE_TO_SAVE);
}

TEST(BridgeDelivery, NullDLCFederateSilentlyNoOps) {
  gorti::dlc::DLCFederateAmbassadorBridge bridge(nullptr);
  // Should not crash.
  bridge.discoverObjectInstance(rti1516e_m17::ObjectInstanceHandle{1},
                                rti1516e_m17::ObjectClassHandle{2},
                                "x");
  bridge.timeAdvanceGrant(0.0);
  bridge.objectInstanceNameReservationSucceeded("y");
  bridge.federationSaved("z");
  SUCCEED();
}

TEST(BridgeDelivery, DlcFederateAccessorReturnsBoundPointer) {
  TrackingDLCFed mock;
  gorti::dlc::DLCFederateAmbassadorBridge bridge(&mock);
  EXPECT_EQ(bridge.dlcFederate(), &mock);
}

// ===== M36 Agent CA-4 — §7 ownership converters =====

TEST(BridgeDelivery, OwnershipAssumptionDropsDivestorForwardsTag) {
  TrackingDLCFed mock;
  gorti::dlc::DLCFederateAmbassadorBridge bridge(&mock);

  rti1516e_m17::AttributeHandleSet attrs;
  attrs.insert(rti1516e_m17::AttributeHandle{7});
  attrs.insert(rti1516e_m17::AttributeHandle{9});
  rti1516e_m17::VariableLengthData tag = {0xDE, 0xAD};
  // M17 carries the divesting federate; the DLC signature drops it
  // (catalogue row 4.27).
  bridge.requestAttributeOwnershipAssumption(
      rti1516e_m17::ObjectInstanceHandle{0x33}, attrs,
      rti1516e_m17::FederateHandle{42}, tag);

  ASSERT_EQ(mock.ownership_assumptions.size(), 1u);
  auto const& call = mock.ownership_assumptions.front();
  EXPECT_EQ(call.obj,
            gorti::dlc::conv::to_dlc_handle<rti1516e::ObjectInstanceHandle>(
                0x33ULL));
  EXPECT_EQ(call.attrs.size(), 2u);
  EXPECT_EQ(call.attrs.count(
                gorti::dlc::conv::to_dlc_handle<rti1516e::AttributeHandle>(7)),
            1u);
  EXPECT_EQ(call.tag_bytes, (std::vector<std::uint8_t>{0xDE, 0xAD}));
}

TEST(BridgeDelivery, AcquisitionNotificationSupplementsEmptyTag) {
  TrackingDLCFed mock;
  gorti::dlc::DLCFederateAmbassadorBridge bridge(&mock);

  rti1516e_m17::AttributeHandleSet attrs;
  attrs.insert(rti1516e_m17::AttributeHandle{5});
  // M17 carries the new owner; DLC drops it and adds a tag — the bridge
  // supplements an empty one (catalogue row 4.28).
  bridge.attributeOwnershipAcquisitionNotification(
      rti1516e_m17::ObjectInstanceHandle{0x44}, attrs,
      rti1516e_m17::FederateHandle{2});

  ASSERT_EQ(mock.acquisition_notifications.size(), 1u);
  auto const& call = mock.acquisition_notifications.front();
  EXPECT_EQ(call.obj,
            gorti::dlc::conv::to_dlc_handle<rti1516e::ObjectInstanceHandle>(
                0x44ULL));
  EXPECT_EQ(call.attrs.size(), 1u);
  EXPECT_EQ(call.tag_size, 0u);
}

TEST(BridgeDelivery, DivestitureConfirmationConvertsAttributeSet) {
  TrackingDLCFed mock;
  gorti::dlc::DLCFederateAmbassadorBridge bridge(&mock);

  rti1516e_m17::AttributeHandleSet attrs;
  attrs.insert(rti1516e_m17::AttributeHandle{11});
  bridge.requestDivestitureConfirmation(
      rti1516e_m17::ObjectInstanceHandle{0x55}, attrs);

  ASSERT_EQ(mock.divest_confirmations.size(), 1u);
  auto const& call = mock.divest_confirmations.front();
  EXPECT_EQ(call.obj,
            gorti::dlc::conv::to_dlc_handle<rti1516e::ObjectInstanceHandle>(
                0x55ULL));
  EXPECT_EQ(call.attrs.count(
                gorti::dlc::conv::to_dlc_handle<rti1516e::AttributeHandle>(11)),
            1u);
}

// ===== M36 Agent CA-4 — §4 restore converters =====

TEST(BridgeDelivery, InitiateRestoreSupplementsEmptyFederateName) {
  TrackingDLCFed mock;
  gorti::dlc::DLCFederateAmbassadorBridge bridge(&mock);

  bridge.initiateFederateRestore("checkpoint-1",
                                 rti1516e_m17::FederateHandle{3});

  ASSERT_EQ(mock.restore_initiates.size(), 1u);
  auto const& call = mock.restore_initiates.front();
  EXPECT_EQ(call.label, std::wstring(L"checkpoint-1"));
  // M17 does not carry a federate name; bridge passes empty (header
  // contract §4.27).
  EXPECT_EQ(call.federate_name, std::wstring());
  EXPECT_EQ(call.handle,
            gorti::dlc::conv::to_dlc_handle<rti1516e::FederateHandle>(3));
}

TEST(BridgeDelivery, FederationRestoredDropsLabelPerDLCSignature) {
  TrackingDLCFed mock;
  gorti::dlc::DLCFederateAmbassadorBridge bridge(&mock);
  bridge.federationRestored("anything");
  EXPECT_EQ(mock.federation_restored_count, 1);
}

TEST(BridgeDelivery, FederationNotRestoredSupplementsFailureReason) {
  TrackingDLCFed mock;
  gorti::dlc::DLCFederateAmbassadorBridge bridge(&mock);
  bridge.federationNotRestored("anything");
  EXPECT_EQ(mock.federation_not_restored_count, 1);
  // M17 does not carry a reason; bridge picks RTI_UNABLE_TO_RESTORE per
  // header comment.
  EXPECT_EQ(mock.last_restore_failure, rti1516e::RTI_UNABLE_TO_RESTORE);
}

// ===== M36 Agent DA — §6.15 remove / §6.20 provide-update converters =====

TEST(BridgeDelivery, RemoveNoTimestampSelectsROOverloadForwardsTag) {
  TrackingDLCFed mock;
  gorti::dlc::DLCFederateAmbassadorBridge bridge(&mock);

  rti1516e_m17::VariableLengthData tag = {0x5A, 0x5B};
  bridge.removeObjectInstance(rti1516e_m17::ObjectInstanceHandle{0x66},
                              std::nullopt, tag);

  ASSERT_EQ(mock.remove_no_time_calls.size(), 1u);
  ASSERT_EQ(mock.remove_with_time_calls.size(), 0u);
  auto const& call = mock.remove_no_time_calls.front();
  EXPECT_EQ(call.obj,
            gorti::dlc::conv::to_dlc_handle<rti1516e::ObjectInstanceHandle>(
                0x66ULL));
  EXPECT_EQ(call.tag_bytes, (std::vector<std::uint8_t>{0x5A, 0x5B}));
  EXPECT_EQ(call.sentOrder, rti1516e::RECEIVE);
}

TEST(BridgeDelivery, RemoveWithTimestampSelectsTSOOverload) {
  TrackingDLCFed mock;
  gorti::dlc::DLCFederateAmbassadorBridge bridge(&mock);

  bridge.removeObjectInstance(rti1516e_m17::ObjectInstanceHandle{0x67},
                              std::optional<double>{7.25},
                              rti1516e_m17::VariableLengthData{});

  ASSERT_EQ(mock.remove_no_time_calls.size(), 0u);
  ASSERT_EQ(mock.remove_with_time_calls.size(), 1u);
  auto const& call = mock.remove_with_time_calls.front();
  EXPECT_DOUBLE_EQ(call.time, 7.25);
  EXPECT_EQ(call.sentOrder, rti1516e::TIMESTAMP);
  EXPECT_EQ(call.recvOrder, rti1516e::TIMESTAMP);
}

TEST(BridgeDelivery, ProvideAttributeValueUpdateConvertsSetAndTag) {
  TrackingDLCFed mock;
  gorti::dlc::DLCFederateAmbassadorBridge bridge(&mock);

  rti1516e_m17::AttributeHandleSet attrs;
  attrs.insert(rti1516e_m17::AttributeHandle{21});
  attrs.insert(rti1516e_m17::AttributeHandle{22});
  rti1516e_m17::VariableLengthData tag = {0x01};
  bridge.provideAttributeValueUpdate(
      rti1516e_m17::ObjectInstanceHandle{0x99}, attrs, tag);

  ASSERT_EQ(mock.provide_update_calls.size(), 1u);
  auto const& call = mock.provide_update_calls.front();
  EXPECT_EQ(call.obj,
            gorti::dlc::conv::to_dlc_handle<rti1516e::ObjectInstanceHandle>(
                0x99ULL));
  EXPECT_EQ(call.attrs.size(), 2u);
  EXPECT_EQ(call.attrs.count(
                gorti::dlc::conv::to_dlc_handle<rti1516e::AttributeHandle>(21)),
            1u);
  EXPECT_EQ(call.tag_bytes, (std::vector<std::uint8_t>{0x01}));
}

// ===== M36 Agent DA — FR-DLC-14 re-entrancy witness =====

// Observer federate that samples gorti::dlc::tls_in_callback from INSIDE a
// dispatched callback — pins that CallbackScope marks the callback context
// for the ambassador-side requireNotInCallback() gate.
class ReentryProbeFed : public rti1516e::NullFederateAmbassador {
 public:
  bool flag_inside_callback = false;
  void discoverObjectInstance(rti1516e::ObjectInstanceHandle,
                              rti1516e::ObjectClassHandle,
                              std::wstring const&)
      RTI_THROW(rti1516e::FederateInternalError) override {
    flag_inside_callback = gorti::dlc::tls_in_callback;
  }
};

TEST(BridgeReentrancy, TlsFlagSetDuringDispatchClearedAfter) {
  ReentryProbeFed probe;
  gorti::dlc::DLCFederateAmbassadorBridge bridge(&probe);

  EXPECT_FALSE(gorti::dlc::tls_in_callback);
  bridge.discoverObjectInstance(rti1516e_m17::ObjectInstanceHandle{1},
                                rti1516e_m17::ObjectClassHandle{2}, "car-1");
  EXPECT_TRUE(probe.flag_inside_callback);
  // RAII CallbackScope must restore the flag after dispatch returns.
  EXPECT_FALSE(gorti::dlc::tls_in_callback);
}

TEST(BridgeReentrancy, CallbackScopeSavesAndRestoresNestedState) {
  EXPECT_FALSE(gorti::dlc::tls_in_callback);
  {
    gorti::dlc::CallbackScope outer;
    EXPECT_TRUE(gorti::dlc::tls_in_callback);
    {
      gorti::dlc::CallbackScope inner;
      EXPECT_TRUE(gorti::dlc::tls_in_callback);
    }
    // Inner scope restores the OUTER state (true), not false.
    EXPECT_TRUE(gorti::dlc::tls_in_callback);
  }
  EXPECT_FALSE(gorti::dlc::tls_in_callback);
}

}  // namespace
