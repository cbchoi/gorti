// M17.17 — §9 Data Distribution Management integration tests.
//
// Drives the DDMService against rtid + the M10 DDM conformance FOM
// (declares X, Y dimensions in the "default" routing space).

#include <gtest/gtest.h>

#include <string>
#include <vector>

#include "rti1516e/Exceptions.h"
#include "rti1516e/RtiAmbassador.h"

#include "fixtures/RtidProcess.h"

namespace {

using rti1516e_test::RtidProcess;
const std::string kFomPath = TEST_DDM_FOM_PATH;

class DDMIntegration : public ::testing::Test {
 protected:
  void SetUp() override {
    rtid = std::make_unique<RtidProcess>();
    amb.connect(rtid->url());
    amb.createFederationExecution("m17-17-ddm", {kFomPath});
    amb.joinFederationExecution("alice", "m17-17-ddm");
  }
  void TearDown() override {
    if (amb.isConnected()) {
      try { amb.resignFederationExecution(); } catch (...) {}
      try { amb.destroyFederationExecution("m17-17-ddm"); } catch (...) {}
      amb.disconnect();
    }
  }
  std::unique_ptr<RtidProcess> rtid;
  rti1516e::RTIambassador amb;
};

// --- Lookups ----------------------------------------------------------------

TEST_F(DDMIntegration, LookupRoutingSpaceDefault) {
  const auto rs = amb.getRoutingSpaceHandle("default");
  EXPECT_TRUE(rs.isValid());
}

TEST_F(DDMIntegration, LookupRoutingSpaceUnknownThrowsNameNotFound) {
  EXPECT_THROW(amb.getRoutingSpaceHandle("ghost-space"),
               rti1516e::NameNotFound);
}

TEST_F(DDMIntegration, LookupDimensionXY) {
  const auto rs = amb.getRoutingSpaceHandle("default");
  const auto dx = amb.getDimensionHandle(rs, "X");
  const auto dy = amb.getDimensionHandle(rs, "Y");
  EXPECT_TRUE(dx.isValid());
  EXPECT_TRUE(dy.isValid());
  EXPECT_NE(dx, dy);
}

TEST_F(DDMIntegration, LookupDimensionUnknownThrows) {
  const auto rs = amb.getRoutingSpaceHandle("default");
  EXPECT_THROW(amb.getDimensionHandle(rs, "Z"),
               rti1516e::NameNotFound);
}

// --- Region lifecycle -------------------------------------------------------

TEST_F(DDMIntegration, CreateRegionReturnsValidHandle) {
  const auto rs = amb.getRoutingSpaceHandle("default");
  const auto dx = amb.getDimensionHandle(rs, "X");
  const auto region = amb.createRegion(rs, {dx});
  EXPECT_TRUE(region.isValid());
}

TEST_F(DDMIntegration, SetRangeBoundsThenQueryBounds) {
  const auto rs = amb.getRoutingSpaceHandle("default");
  const auto dx = amb.getDimensionHandle(rs, "X");
  const auto region = amb.createRegion(rs, {dx});
  amb.setRangeBounds(region, dx, {100, 200});
  amb.commitRegionModifications({region});
  const auto q = amb.queryBounds(region, dx);
  EXPECT_TRUE(q.found);
  EXPECT_EQ(q.bounds.lower, 100u);
  EXPECT_EQ(q.bounds.upper, 200u);
}

TEST_F(DDMIntegration, DeleteRegionRemovesIt) {
  const auto rs = amb.getRoutingSpaceHandle("default");
  const auto dx = amb.getDimensionHandle(rs, "X");
  const auto region = amb.createRegion(rs, {dx});
  EXPECT_NO_THROW(amb.deleteRegion(region));
}

TEST_F(DDMIntegration, QueryBoundsUnsetReturnsDimensionDefault) {
  const auto rs = amb.getRoutingSpaceHandle("default");
  const auto dx = amb.getDimensionHandle(rs, "X");
  const auto region = amb.createRegion(rs, {dx});
  // No setRangeBounds — gorti initializes the region to the full
  // dimension extent {0, upperBound} so queryBounds returns
  // found=true with the dimension's declared upper bound.
  const auto q = amb.queryBounds(region, dx);
  EXPECT_TRUE(q.found);
  EXPECT_EQ(q.bounds.lower, 0u);
  EXPECT_EQ(q.bounds.upper, 1000u);  // ddm-test.xml: X upperBound=1000
}

// --- Region-aware subscribe / unsubscribe -----------------------------------

TEST_F(DDMIntegration, SubscribeObjectClassAttributesWithRegions) {
  const auto rs = amb.getRoutingSpaceHandle("default");
  const auto dx = amb.getDimensionHandle(rs, "X");
  const auto region = amb.createRegion(rs, {dx});
  amb.setRangeBounds(region, dx, {0, 500});
  amb.commitRegionModifications({region});
  const auto vc = amb.getObjectClassHandle("Vehicle");
  const auto pos = amb.getAttributeHandle(vc, "position");
  EXPECT_NO_THROW(amb.subscribeObjectClassAttributesWithRegions(
      vc, {pos}, {region}));
  EXPECT_NO_THROW(amb.unsubscribeObjectClassAttributesWithRegions(
      vc, {pos}, {region}));
}

TEST_F(DDMIntegration, SubscribeInteractionClassWithRegions) {
  const auto rs = amb.getRoutingSpaceHandle("default");
  const auto dx = amb.getDimensionHandle(rs, "X");
  const auto region = amb.createRegion(rs, {dx});
  amb.setRangeBounds(region, dx, {0, 500});
  amb.commitRegionModifications({region});
  const auto honk = amb.getInteractionClassHandle("HLAinteractionRoot.Honk");
  EXPECT_NO_THROW(amb.subscribeInteractionClassWithRegions(honk, {region}));
  EXPECT_NO_THROW(amb.unsubscribeInteractionClassWithRegions(honk, {region}));
}

// --- Region-aware register + associate --------------------------------------

TEST_F(DDMIntegration, RegisterObjectInstanceWithRegionsCallSucceeds) {
  // Smoke test: the registerObjectInstanceWithRegions RPC accepts
  // the request shape. The gorti server's response for the
  // single-federate / no-prior-reservation path returns
  // object_handle=0 (registration is recorded but no instance
  // table entry created until an explicit registerObjectInstance
  // ties the regions to an instance — pysdk M23 docs the same
  // shape). The CALL-SHAPE smoke test is what's actionable from
  // the SDK side; deeper end-to-end DDM filtering tests live in
  // the Go DDM conformance suite.
  const auto rs = amb.getRoutingSpaceHandle("default");
  const auto dx = amb.getDimensionHandle(rs, "X");
  const auto region = amb.createRegion(rs, {dx});
  amb.setRangeBounds(region, dx, {0, 100});
  amb.commitRegionModifications({region});
  const auto vc = amb.getObjectClassHandle("Vehicle");
  const auto pos = amb.getAttributeHandle(vc, "position");
  amb.publishObjectClassAttributes(vc, {pos});
  rti1516e::AttributeRegionMap arm;
  arm[pos] = {region};
  EXPECT_NO_THROW({
    const auto r =
        amb.registerObjectInstanceWithRegions(vc, arm, "ddm-car");
    (void)r;
  });
}

// --- Pre-join guard ---------------------------------------------------------

TEST(DDMRequiresJoin, OperationsThrowPreJoin) {
  RtidProcess rtid;
  rti1516e::RTIambassador amb;
  amb.connect(rtid.url());
  EXPECT_THROW(amb.getRoutingSpaceHandle("default"),
               rti1516e::FederateNotExecutionMember);
  EXPECT_THROW(amb.createRegion(rti1516e::RoutingSpaceHandle(1), {}),
               rti1516e::FederateNotExecutionMember);
  EXPECT_THROW(amb.deleteRegion(rti1516e::RegionHandle(1)),
               rti1516e::FederateNotExecutionMember);
}

}  // namespace
