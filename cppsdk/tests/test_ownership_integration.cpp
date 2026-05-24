// M17.15 — §7 Ownership Management integration tests.
//
// The federate that calls registerObjectInstance becomes the implicit
// owner of every published attribute. We exercise the query / divest
// path against that single-federate baseline, then add two-federate
// scenarios for the transfer protocol.

#include <gtest/gtest.h>

#include <chrono>
#include <string>
#include <thread>
#include <vector>

#include "rti1516e/Exceptions.h"
#include "rti1516e/FederateAmbassador.h"
#include "rti1516e/RtiAmbassador.h"

#include "fixtures/RtidProcess.h"

namespace {

using rti1516e_test::RtidProcess;
const std::string kFomPath = TEST_FOM_PATH;

class OwnershipRecordingFed : public rti1516e::FederateAmbassador {
 public:
  struct Assumption {
    rti1516e::ObjectInstanceHandle object;
    rti1516e::AttributeHandleSet attributes;
    rti1516e::FederateHandle divesting_federate;
    std::string tag;
  };
  struct Acquired {
    rti1516e::ObjectInstanceHandle object;
    rti1516e::AttributeHandleSet attributes;
    rti1516e::FederateHandle owning_federate;
  };
  struct DivestConfirmed {
    rti1516e::ObjectInstanceHandle object;
    rti1516e::AttributeHandleSet attributes;
  };

  std::vector<Assumption> assumptions;
  std::vector<Acquired> acquireds;
  std::vector<DivestConfirmed> divest_confirmeds;

  void requestAttributeOwnershipAssumption(
      rti1516e::ObjectInstanceHandle object,
      const rti1516e::AttributeHandleSet& attrs,
      rti1516e::FederateHandle divesting,
      const rti1516e::VariableLengthData& tag) override {
    assumptions.push_back(
        {object, attrs, divesting, std::string(tag.begin(), tag.end())});
  }
  void attributeOwnershipAcquisitionNotification(
      rti1516e::ObjectInstanceHandle object,
      const rti1516e::AttributeHandleSet& attrs,
      rti1516e::FederateHandle owner) override {
    acquireds.push_back({object, attrs, owner});
  }
  void requestDivestitureConfirmation(
      rti1516e::ObjectInstanceHandle object,
      const rti1516e::AttributeHandleSet& attrs) override {
    divest_confirmeds.push_back({object, attrs});
  }
};

class OwnershipIntegration : public ::testing::Test {
 protected:
  void SetUp() override {
    rtid = std::make_unique<RtidProcess>();
    amb.setFederateAmbassador(&fed);
    amb.connect(rtid->url());
    amb.createFederationExecution("m17-15-own", {kFomPath});
    fed_handle_ = amb.joinFederationExecution("alice", "m17-15-own");
    vehicle_ = amb.getObjectClassHandle("Vehicle");
    pos_ = amb.getAttributeHandle(vehicle_, "Position");
    vel_ = amb.getAttributeHandle(vehicle_, "Velocity");
    amb.publishObjectClassAttributes(vehicle_, {pos_, vel_});
    obj_ = amb.registerObjectInstance(vehicle_, "car-own");
  }
  void TearDown() override {
    if (amb.isConnected()) {
      try { amb.resignFederationExecution(); } catch (...) {}
      try { amb.destroyFederationExecution("m17-15-own"); } catch (...) {}
      amb.disconnect();
    }
  }
  OwnershipRecordingFed fed;
  std::unique_ptr<RtidProcess> rtid;
  rti1516e::RTIambassador amb;
  rti1516e::FederateHandle fed_handle_;
  rti1516e::ObjectClassHandle vehicle_;
  rti1516e::AttributeHandle pos_;
  rti1516e::AttributeHandle vel_;
  rti1516e::ObjectInstanceHandle obj_;

  template <typename Pred>
  void pumpUntil(Pred pred, double timeout = 2.0) {
    const auto deadline =
        std::chrono::steady_clock::now() +
        std::chrono::milliseconds(static_cast<int>(timeout * 1000));
    while (std::chrono::steady_clock::now() < deadline) {
      amb.tickCallback(0.05, 0.1);
      if (pred()) return;
    }
  }
};

// --- Queries (single-federate baseline) ------------------------------------

TEST_F(OwnershipIntegration, RegisteredAttributesOwnedBySelf) {
  EXPECT_TRUE(amb.isAttributeOwnedByFederate(obj_, pos_));
  EXPECT_TRUE(amb.isAttributeOwnedByFederate(obj_, vel_));
}

TEST_F(OwnershipIntegration, QueryAttributeOwnershipReturnsRegisterer) {
  const auto q = amb.queryAttributeOwnership(obj_, pos_);
  EXPECT_TRUE(q.owned);
  EXPECT_EQ(q.owner, fed_handle_);
}

// --- Divest (single-federate) ----------------------------------------------

TEST_F(OwnershipIntegration, UnconditionalDivestReleasesOwnership) {
  amb.unconditionalAttributeOwnershipDivestiture(obj_, {pos_});
  // After unconditional divest, the attribute has no owner (no
  // acquirer in this single-federate scenario).
  EXPECT_FALSE(amb.isAttributeOwnedByFederate(obj_, pos_));
  // Velocity still ours.
  EXPECT_TRUE(amb.isAttributeOwnedByFederate(obj_, vel_));
}

TEST_F(OwnershipIntegration, DivestIfWantedNoCandidatesIsNoOp) {
  // No subscribers want pos_; divest_if_wanted returns without
  // moving ownership.
  EXPECT_NO_THROW(
      amb.attributeOwnershipDivestitureIfWanted(obj_, {pos_}));
  EXPECT_TRUE(amb.isAttributeOwnedByFederate(obj_, pos_));
}

// --- Negotiated divest cancel ----------------------------------------------

TEST_F(OwnershipIntegration, NegotiatedDivestThenCancelKeepsOwnership) {
  rti1516e::VariableLengthData tag;
  amb.negotiatedAttributeOwnershipDivestiture(obj_, {pos_}, tag);
  amb.cancelNegotiatedAttributeOwnershipDivestiture(obj_, {pos_});
  EXPECT_TRUE(amb.isAttributeOwnedByFederate(obj_, pos_));
}

// --- Acquire cancel (single-federate — nothing to acquire from) ------------

TEST_F(OwnershipIntegration, CancelAcquireWithoutPendingThrows) {
  EXPECT_ANY_THROW(amb.cancelAttributeOwnershipAcquisition(obj_, {pos_}));
}

// --- Pre-join guard ---------------------------------------------------------

TEST(OwnershipRequiresJoin, OperationsThrowPreJoin) {
  RtidProcess rtid;
  rti1516e::RTIambassador amb;
  amb.connect(rtid.url());
  const auto obj = rti1516e::ObjectInstanceHandle(1);
  const auto attr = rti1516e::AttributeHandle(1);
  EXPECT_THROW(amb.unconditionalAttributeOwnershipDivestiture(obj, {attr}),
               rti1516e::FederateNotExecutionMember);
  EXPECT_THROW(amb.isAttributeOwnedByFederate(obj, attr),
               rti1516e::FederateNotExecutionMember);
  EXPECT_THROW(amb.queryAttributeOwnership(obj, attr),
               rti1516e::FederateNotExecutionMember);
}

}  // namespace
