// M31 conformance fixture: ddm_region_mod_in_flight — subscriber.
//
// Spec § anchors:
//   §9.5  registerObjectInstanceWithRegions (publisher side; see fed_publisher)
//   §9.6  associateRegionsForUpdates / commitRegionModifications shape  — catalogue 10.4
//   §6.17 attributesInScope   (FederateAmbassador callback)             — catalogue 4.23
//   §6.18 attributesOutOfScope                                          — catalogue 4.23
//   §6.11 reflectAttributeValues RO                                     — catalogue 4.20
//
// Scenario: subscriber creates region R_sub covering Channel=[0,50] —
//   overlaps publisher's R_pub at [40,50]. Receives reflects + an
//   `attributesInScope` advisory. Halfway through publisher's stream,
//   subscriber calls setRangeBounds(R_sub, Channel, [0,30]) and
//   commitRegionModifications — this drops the overlap, so the RTI
//   fires `attributesOutOfScope` and stops delivering further reflects.

#include <RTI/RTIambassador.h>
#include <RTI/RTIambassadorFactory.h>
#include <RTI/NullFederateAmbassador.h>
#include <RTI/Enums.h>
#include <RTI/RangeBounds.h>
#include <RTI/encoding/HLAfloat64BE.h>

#include <chrono>
#include <cstdio>
#include <memory>
#include <string>
#include <thread>
#include <utility>
#include <vector>

namespace {

rti1516e::AttributeHandle gValueAttr;

class SubFed : public rti1516e::NullFederateAmbassador {
 public:
  int reflects = 0;
  void discoverObjectInstance(
      rti1516e::ObjectInstanceHandle /*theObject*/,
      rti1516e::ObjectClassHandle /*theObjectClass*/,
      std::wstring const& name) override {
    std::string n(name.begin(), name.end());
    std::printf("SUB: DISCOVER name=%s\n", n.c_str());
  }
  void reflectAttributeValues(
      rti1516e::ObjectInstanceHandle /*theObject*/,
      rti1516e::AttributeHandleValueMap const& vals,
      rti1516e::VariableLengthData const& /*tag*/,
      rti1516e::OrderType /*sentOrder*/,
      rti1516e::TransportationType /*type*/,
      rti1516e::SupplementalReflectInfo /*info*/) override {
    rti1516e::HLAfloat64BE v;
    v.decode(vals.at(gValueAttr));
    ++reflects;
    std::printf("SUB: REFLECT Value=%.6f\n", v.get());
  }
  // §6.17 attributesInScope (catalogue 4.23)
  void attributesInScope(
      rti1516e::ObjectInstanceHandle /*theObject*/,
      rti1516e::AttributeHandleSet const& /*theAttributes*/) override {
    std::printf("SUB: IN_SCOPE attributes=[Value]\n");
  }
  // §6.18 attributesOutOfScope (catalogue 4.23)
  void attributesOutOfScope(
      rti1516e::ObjectInstanceHandle /*theObject*/,
      rti1516e::AttributeHandleSet const& /*theAttributes*/) override {
    std::printf("SUB: OUT_OF_SCOPE attributes=[Value]\n");
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
  SubFed fed;
  std::wstring settings = L"crcAddress=" + std::wstring(url.begin() + 7, url.end());
  amb->connect(fed, rti1516e::HLA_IMMEDIATE, settings);
  std::printf("SUB: CONNECT\n");

  std::wstring fedName = L"ddm_region_mod_in_flight";
  try {
    std::vector<std::wstring> modules{std::wstring(fom.begin(), fom.end())};
    amb->createFederationExecution(fedName, modules);
  } catch (rti1516e::FederationExecutionAlreadyExists const&) {}
  amb->joinFederationExecution(L"sub", fedName);
  std::printf("SUB: JOIN federate=sub\n");

  auto sClass = amb->getObjectClassHandle(L"HLAobjectRoot.Sensor");
  gValueAttr = amb->getAttributeHandle(sClass, L"Value");

  // §6.27 enableAttributeScopeAdvisorySwitch — advisory must be on at runtime
  //       even when the FOM declares the switch enabled (catalogue 13.10).
  amb->enableAttributeScopeAdvisorySwitch();
  std::printf("SUB: ENABLE_ADVISORY attribute_scope=true\n");

  auto cDim = amb->getDimensionHandle(L"Channel");
  rti1516e::DimensionHandleSet dims;
  dims.insert(cDim);
  auto rSub = amb->createRegion(dims);
  amb->setRangeBounds(rSub, cDim, rti1516e::RangeBounds(0u, 50u));
  std::printf("SUB: CREATE_REGION dim=Channel lower=0 upper=50\n");

  rti1516e::RegionHandleSet regions;
  regions.insert(rSub);
  amb->commitRegionModifications(regions);
  std::printf("SUB: COMMIT_REGION region=<R>\n");

  rti1516e::AttributeHandleSet attrs;
  attrs.insert(gValueAttr);
  rti1516e::AttributeHandleSetRegionHandleSetPairVector pairs;
  pairs.push_back(std::make_pair(attrs, regions));
  amb->subscribeObjectClassAttributesWithRegions(sClass, pairs,
                                                 /*active=*/true,
                                                 /*updateRate=*/L"");
  std::printf("SUB: SUBSCRIBE_WITH_REGIONS class=Sensor attributes=[Value] active=true\n");

  // Pump until reflects 1..3 have arrived (bounded ~10 s). The scenario
  // contract (see golden header) is that the region modification lands
  // between updates 3 and 4 — counting reflects realises that contract
  // robustly instead of racing the publisher on wall-clock.
  for (int i = 0; i < 200 && fed.reflects < 3; ++i) {
    amb->evokeMultipleCallbacks(0.05, 0.06);
  }

  // §10.30 setRangeBounds + §9.3 commitRegionModifications — shrink to [0,30].
  amb->setRangeBounds(rSub, cDim, rti1516e::RangeBounds(0u, 30u));
  amb->commitRegionModifications(regions);
  std::printf("SUB: MODIFY_REGION dim=Channel new_lower=0 new_upper=30\n");

  // Pump for the rest — reflects 4..6 should be filtered out;
  // attributesOutOfScope advisory should fire.
  for (int i = 0; i < 20; ++i) {
    amb->evokeMultipleCallbacks(0.05, 0.06);
  }

  amb->resignFederationExecution(rti1516e::CANCEL_THEN_DELETE_THEN_DIVEST);
  std::printf("SUB: RESIGN action=CANCEL_THEN_DELETE_THEN_DIVEST\n");
  return 0;
}
