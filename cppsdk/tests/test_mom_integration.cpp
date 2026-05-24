// M17.13 — §11 MOM ambassador delegates.
//
// Read-only introspection of the HLAfederation + per-federate
// HLAfederate MOM instances. Mirrors the pysdk M27 D.1 test suite.

#include <gtest/gtest.h>

#include <algorithm>
#include <string>
#include <vector>

#include "rti1516e/Exceptions.h"
#include "rti1516e/RtiAmbassador.h"

#include "fixtures/RtidProcess.h"

namespace {

using rti1516e_test::RtidProcess;
const std::string kFomPath = TEST_FOM_PATH;

class MomIntegration : public ::testing::Test {
 protected:
  void SetUp() override {
    rtid = std::make_unique<RtidProcess>();
    amb.connect(rtid->url());
    amb.createFederationExecution("m17-13-mom", {kFomPath});
    fed_ = amb.joinFederationExecution("alice", "m17-13-mom");
  }
  void TearDown() override {
    if (amb.isConnected()) {
      try { amb.resignFederationExecution(); } catch (...) {}
      try { amb.destroyFederationExecution("m17-13-mom"); } catch (...) {}
      amb.disconnect();
    }
  }
  std::unique_ptr<RtidProcess> rtid;
  rti1516e::RTIambassador amb;
  rti1516e::FederateHandle fed_;
};

TEST_F(MomIntegration, QueryFederationAttributesReturnsName) {
  const auto attrs = amb.queryFederationAttributes();
  EXPECT_EQ(attrs.federation_name, "m17-13-mom");
}

TEST_F(MomIntegration, QueryFederationAttributesIncludesSelfHandle) {
  const auto attrs = amb.queryFederationAttributes();
  ASSERT_FALSE(attrs.federate_handles.empty());
  bool found = false;
  for (auto h : attrs.federate_handles) {
    if (h == fed_) { found = true; break; }
  }
  EXPECT_TRUE(found) << "self federate handle missing from federation snapshot";
}

TEST_F(MomIntegration, QueryFederationAttributesReportsFomModule) {
  const auto attrs = amb.queryFederationAttributes();
  EXPECT_FALSE(attrs.fom_module_names.empty());
}

TEST_F(MomIntegration, QueryFederateAttributesReturnsSelf) {
  const auto attrs = amb.queryFederateAttributes(fed_);
  EXPECT_TRUE(attrs.found);
  EXPECT_EQ(attrs.federate_handle, fed_);
  EXPECT_EQ(attrs.federate_name, "alice");
}

TEST_F(MomIntegration, QueryFederateAttributesTimeStateDefaultsOff) {
  const auto attrs = amb.queryFederateAttributes(fed_);
  EXPECT_FALSE(attrs.time_regulating);
  EXPECT_FALSE(attrs.time_constrained);
}

TEST_F(MomIntegration, QueryFederateAttributesUnknownNotFound) {
  // Handle far above any joined federate.
  const auto attrs =
      amb.queryFederateAttributes(rti1516e::FederateHandle(99999));
  EXPECT_FALSE(attrs.found);
}

TEST_F(MomIntegration, QueryFederateReflectsRegulatingState) {
  // M17.26 — the rtid composition root now wires the time.Manager's
  // OnTimeStateChanged hook into mom.Manager.TimeStateChanged. After
  // enableTimeRegulation(1.5) the MOM snapshot mirrors the state.
  amb.enableTimeRegulation(1.5);
  const auto attrs = amb.queryFederateAttributes(fed_);
  EXPECT_TRUE(attrs.time_regulating);
  EXPECT_DOUBLE_EQ(attrs.lookahead, 1.5);
}

TEST_F(MomIntegration, QueryFederateReflectsConstrainedState) {
  amb.enableTimeConstrained();
  const auto attrs = amb.queryFederateAttributes(fed_);
  EXPECT_TRUE(attrs.time_constrained);
}

TEST_F(MomIntegration, EnumerateMomInstancesIncludesFederationSingleton) {
  const auto insts = amb.enumerateMomInstances();
  bool found_federation = false;
  for (const auto& i : insts) {
    if (i.class_name == "HLAobjectRoot.HLAmanager.HLAfederation") {
      found_federation = true;
      EXPECT_EQ(i.instance_name, "m17-13-mom");
    }
  }
  EXPECT_TRUE(found_federation);
}

TEST_F(MomIntegration, EnumerateMomInstancesIncludesPerFederate) {
  const auto insts = amb.enumerateMomInstances();
  bool found_self = false;
  for (const auto& i : insts) {
    if (i.class_name == "HLAobjectRoot.HLAmanager.HLAfederate" &&
        i.federate_handle == fed_) {
      found_self = true;
      EXPECT_EQ(i.instance_name, "alice");
    }
  }
  EXPECT_TRUE(found_self);
}

TEST(MomRequiresJoin, OperationsThrowPreJoin) {
  RtidProcess rtid;
  rti1516e::RTIambassador amb;
  amb.connect(rtid.url());
  EXPECT_THROW(amb.queryFederationAttributes(),
               rti1516e::FederateNotExecutionMember);
  EXPECT_THROW(amb.queryFederateAttributes(rti1516e::FederateHandle(1)),
               rti1516e::FederateNotExecutionMember);
  EXPECT_THROW(amb.enumerateMomInstances(),
               rti1516e::FederateNotExecutionMember);
}

}  // namespace
