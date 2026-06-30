// M31 conformance fixture: ddm_region_overlap — subscriber side.
//
// Spec § anchors:
//   §6.9  discoverObjectInstance (catalogue 4.19, 2 overloads — 3-arg + 4-arg)
//   §6.11 reflectAttributeValues (catalogue 4.20, 3 overloads RO/TSO/TSO+retract)
//   §9.8  subscribeObjectClassAttributesWithRegions
//                                                       — catalogue 10.5
//
// Scenario: subscriber creates region R_sub at X=[250,750] Y=[250,750].
//   Subscribes Vehicle.Position with that region. Receives all updates
//   the publisher sends because R_pub and R_sub overlap (in the
//   X=[250,500] Y=[250,500] corner). For this fixture the publisher
//   sends with the pub region attached; the subscription is satisfied
//   by intersection.

#include <RTI/RTIambassadorFactory.h>
#include <RTI/NullFederateAmbassador.h>
#include <RTI/Enums.h>
#include <RTI/RangeBounds.h>
#include <RTI/encoding/HLAfloat64BE.h>

#include <cstdio>
#include <memory>
#include <string>
#include <utility>
#include <vector>

namespace {

rti1516e::AttributeHandle gPositionAttr;

class SubFed : public rti1516e::NullFederateAmbassador {
 public:
  // §6.9 3-arg overload (catalogue 4.19) — most common.
  void discoverObjectInstance(
      rti1516e::ObjectInstanceHandle /*theObject*/,
      rti1516e::ObjectClassHandle /*theObjectClass*/,
      std::wstring const& theObjectInstanceName) override {
    std::string n(theObjectInstanceName.begin(), theObjectInstanceName.end());
    std::printf("SUB: DISCOVER name=%s\n", n.c_str());
  }
  // §6.11 RO overload (catalogue 4.20) — Position is order=Receive.
  void reflectAttributeValues(
      rti1516e::ObjectInstanceHandle /*theObject*/,
      rti1516e::AttributeHandleValueMap const& theAttributeValues,
      rti1516e::VariableLengthData const& /*theUserSuppliedTag*/,
      rti1516e::OrderType /*sentOrder*/,
      rti1516e::TransportationType /*theType*/,
      rti1516e::SupplementalReflectInfo /*theReflectInfo*/) override {
    rti1516e::HLAfloat64BE pos;
    pos.decode(theAttributeValues.at(gPositionAttr));
    std::printf("SUB: REFLECT Position=%.6f\n", pos.get());
  }
};

}  // namespace

int main(int argc, char** argv) {
  std::string url = "grpc://127.0.0.1:8080";
  std::string fom = "./federation.fom.xml";
  for (int i = 1; i + 1 < argc; i += 2) {
    std::string k = argv[i];
    if (k == "--url") url = argv[i + 1];
    else if (k == "--fom") fom = argv[i + 1];
  }

  rti1516e::RTIambassadorFactory factory;
  rti1516e::auto_ptr<rti1516e::RTIambassador> amb(factory.createRTIambassador());
  SubFed fed;
  std::wstring settings = L"crcAddress=" + std::wstring(url.begin() + 7, url.end());
  amb->connect(fed, rti1516e::HLA_IMMEDIATE, settings);
  std::printf("SUB: CONNECT\n");

  std::wstring fedName = L"ddm_region_overlap";
  try {
    std::vector<std::wstring> modules{std::wstring(fom.begin(), fom.end())};
    amb->createFederationExecution(fedName, modules);
  } catch (rti1516e::FederationExecutionAlreadyExists const&) {}
  amb->joinFederationExecution(L"sub", fedName);
  std::printf("SUB: JOIN federate=sub\n");

  auto vClass = amb->getObjectClassHandle(L"HLAobjectRoot.Vehicle");
  gPositionAttr = amb->getAttributeHandle(vClass, L"Position");

  auto xDim = amb->getDimensionHandle(L"X");
  auto yDim = amb->getDimensionHandle(L"Y");
  rti1516e::DimensionHandleSet dims;
  dims.insert(xDim);
  dims.insert(yDim);

  auto rSub = amb->createRegion(dims);
  amb->setRangeBounds(rSub, xDim, rti1516e::RangeBounds(250u, 750u));
  amb->setRangeBounds(rSub, yDim, rti1516e::RangeBounds(250u, 750u));
  std::printf("SUB: CREATE_REGION dims=[X,Y]\n");
  std::printf("SUB: SET_RANGE region=<R> dim=X lower=250 upper=750\n");
  std::printf("SUB: SET_RANGE region=<R> dim=Y lower=250 upper=750\n");

  rti1516e::RegionHandleSet regions;
  regions.insert(rSub);
  amb->commitRegionModifications(regions);
  std::printf("SUB: COMMIT_REGION region=<R>\n");

  // §9.8 subscribeObjectClassAttributesWithRegions — catalogue 10.5.
  //   (cls, pair-vector, bool active=true, wstring updateRate=L"")
  rti1516e::AttributeHandleSet attrs;
  attrs.insert(gPositionAttr);
  rti1516e::AttributeHandleSetRegionHandleSetPairVector pairs;
  pairs.push_back(std::make_pair(attrs, regions));
  amb->subscribeObjectClassAttributesWithRegions(vClass, pairs,
                                                 /*active=*/true,
                                                 /*updateRate=*/L"");
  std::printf("SUB: SUBSCRIBE_WITH_REGIONS class=Vehicle attributes=[Position] active=true\n");

  // Pump until the publisher exits — 3 expected reflects.
  for (int i = 0; i < 100; ++i) amb->evokeMultipleCallbacks(0.05, 0.1);

  amb->resignFederationExecution(rti1516e::CANCEL_THEN_DELETE_THEN_DIVEST);
  std::printf("SUB: RESIGN action=CANCEL_THEN_DELETE_THEN_DIVEST\n");
  return 0;
}
