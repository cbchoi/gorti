// M31 conformance fixture: ddm_region_overlap — publisher side.
//
// Spec § anchors:
//   §9.2  createRegion(DimensionHandleSet)              — catalogue 10.1
//   §9.3  commitRegionModifications(RegionHandleSet)    — catalogue 10.9
//   §9.5  registerObjectInstanceWithRegions             — catalogue 10.3
//   §9.6  associateRegionsForUpdates                    — catalogue 10.4
//   §9.10 updateAttributeValues with regions (implicit via association)
//   §10.30 setRangeBounds(RegionHandle, DimensionHandle, RangeBounds)
//                                                       — catalogue 13.8
//
// Scenario: publisher creates region R_pub covering X=[0,500] Y=[0,500].
//   Registers a Vehicle instance bound to R_pub for the Position attribute.
//   Updates Position 3 times (RO since Position order=Receive in FOM).
//   Subscriber subscribes with R_sub at X=[250,750] Y=[250,750] — overlap is
//   the upper-right corner of R_pub. Publisher's updates land in R_pub which
//   intersects R_sub iff the publisher's region is non-empty in the overlap.
//   Since association is over R_pub (always intersects R_sub), subscriber
//   receives all three updates.

#include <RTI/RTIambassador.h>
#include <RTI/RTIambassadorFactory.h>
#include <RTI/NullFederateAmbassador.h>
#include <RTI/Enums.h>
#include <RTI/RangeBounds.h>
#include <RTI/encoding/HLAfloat64BE.h>

#include <cstdio>
#include <memory>
#include <set>
#include <string>
#include <utility>
#include <vector>

namespace {

class PubFed : public rti1516e::NullFederateAmbassador {
 public:
  bool reservation_ok = false;
  void objectInstanceNameReservationSucceeded(std::wstring const& name) override {
    reservation_ok = true;
    std::string n(name.begin(), name.end());
    std::printf("PUB: NAME_RESERVED name=%s\n", n.c_str());
  }
};

}  // namespace

int main(int argc, char** argv) {
  std::string url = "grpc://127.0.0.1:8080";
  std::string fom = "./federation.fom.xml";
  for (int i = 1; i < argc; ++i) {
    std::string k = argv[i];
    if (k == "--url" && i + 1 < argc) url = argv[++i];
    else if (k == "--fom" && i + 1 < argc) fom = argv[++i];
  }

  rti1516e::RTIambassadorFactory factory;
  rti1516e::auto_ptr<rti1516e::RTIambassador> amb(factory.createRTIambassador());
  PubFed fed;
  std::wstring settings = L"crcAddress=" + std::wstring(url.begin() + 7, url.end());
  amb->connect(fed, rti1516e::HLA_IMMEDIATE, settings);
  std::printf("PUB: CONNECT\n");

  std::wstring fedName = L"ddm_region_overlap";
  try {
    std::vector<std::wstring> modules{std::wstring(fom.begin(), fom.end())};
    amb->createFederationExecution(fedName, modules);
  } catch (rti1516e::FederationExecutionAlreadyExists const&) {}
  amb->joinFederationExecution(L"pub", fedName);
  std::printf("PUB: JOIN federate=pub\n");

  // §5.2 publish Vehicle.Position.
  auto vClass = amb->getObjectClassHandle(L"HLAobjectRoot.Vehicle");
  auto pAttr = amb->getAttributeHandle(vClass, L"Position");
  rti1516e::AttributeHandleSet attrs;
  attrs.insert(pAttr);
  amb->publishObjectClassAttributes(vClass, attrs);
  std::printf("PUB: PUBLISH class=Vehicle attributes=[Position]\n");

  // §10.23-§10.30 dimension lookup + region creation + range bounds.
  auto xDim = amb->getDimensionHandle(L"X");
  auto yDim = amb->getDimensionHandle(L"Y");
  rti1516e::DimensionHandleSet dims;
  dims.insert(xDim);
  dims.insert(yDim);
  auto rPub = amb->createRegion(dims);
  std::printf("PUB: CREATE_REGION dims=[X,Y]\n");

  // §10.30 setRangeBounds(region, dim, RangeBounds(lower, upper))
  amb->setRangeBounds(rPub, xDim, rti1516e::RangeBounds(0u, 500u));
  amb->setRangeBounds(rPub, yDim, rti1516e::RangeBounds(0u, 500u));
  std::printf("PUB: SET_RANGE region=<R> dim=X lower=0 upper=500\n");
  std::printf("PUB: SET_RANGE region=<R> dim=Y lower=0 upper=500\n");

  // §9.3 commitRegionModifications(RegionHandleSet) — catalogue 10.9.
  rti1516e::RegionHandleSet regions;
  regions.insert(rPub);
  amb->commitRegionModifications(regions);
  std::printf("PUB: COMMIT_REGION region=<R>\n");

  // §6.4 + §6.5 reservation then §6.8 register with regions.
  amb->reserveObjectInstanceName(L"car-1");
  while (!fed.reservation_ok) amb->evokeMultipleCallbacks(0.05, 0.1);

  // §9.5 registerObjectInstanceWithRegions — catalogue 10.3.
  //   vector< pair< AttributeHandleSet, RegionHandleSet > >
  rti1516e::AttributeHandleSetRegionHandleSetPairVector pairs;
  pairs.push_back(std::make_pair(attrs, regions));
  auto inst = amb->registerObjectInstanceWithRegions(vClass, pairs, L"car-1");
  std::printf("PUB: REGISTER class=Vehicle name=car-1 region=<R>\n");

  // §9.10 updates flow through associated regions implicitly.
  rti1516e::VariableLengthData tag;  // empty mandatory tag (catalogue 17.1)
  for (int i = 1; i <= 3; ++i) {
    rti1516e::HLAfloat64BE position(100.0 + 50.0 * i);
    rti1516e::AttributeHandleValueMap values;
    values[pAttr] = position.encode();
    // §6.10 RO updateAttributeValues (no LogicalTime overload — Position is order=Receive).
    amb->updateAttributeValues(inst, values, tag);
    std::printf("PUB: UPDATE name=car-1 Position=%.6f\n", 100.0 + 50.0 * i);
  }

  amb->resignFederationExecution(rti1516e::CANCEL_THEN_DELETE_THEN_DIVEST);
  std::printf("PUB: RESIGN action=CANCEL_THEN_DELETE_THEN_DIVEST\n");
  try { amb->destroyFederationExecution(fedName); } catch (...) {}
  return 0;
}
