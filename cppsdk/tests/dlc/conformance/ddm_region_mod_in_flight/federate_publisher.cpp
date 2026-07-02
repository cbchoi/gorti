// M31 conformance fixture: ddm_region_mod_in_flight — publisher.
//
// Spec § anchors:
//   §9.2  createRegion
//   §9.3  commitRegionModifications
//   §9.5  registerObjectInstanceWithRegions
//   §6.10 updateAttributeValues (RO)
//
// Scenario: publisher creates one region for Channel=[40,60], registers
//   sensor-1 there, then emits 6 updates spaced ~50 ms apart. The driver
//   times the subscriber's region modification to land between updates
//   3 and 4 — see test_ddm_region_mod_in_flight.cpp for orchestration.

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

  std::wstring fedName = L"ddm_region_mod_in_flight";
  try {
    std::vector<std::wstring> modules{std::wstring(fom.begin(), fom.end())};
    amb->createFederationExecution(fedName, modules);
  } catch (rti1516e::FederationExecutionAlreadyExists const&) {}
  amb->joinFederationExecution(L"pub", fedName);
  std::printf("PUB: JOIN federate=pub\n");

  auto sClass = amb->getObjectClassHandle(L"HLAobjectRoot.Sensor");
  auto vAttr = amb->getAttributeHandle(sClass, L"Value");
  rti1516e::AttributeHandleSet attrs;
  attrs.insert(vAttr);
  amb->publishObjectClassAttributes(sClass, attrs);
  std::printf("PUB: PUBLISH class=Sensor attributes=[Value]\n");

  auto cDim = amb->getDimensionHandle(L"Channel");
  rti1516e::DimensionHandleSet dims;
  dims.insert(cDim);
  auto rPub = amb->createRegion(dims);
  amb->setRangeBounds(rPub, cDim, rti1516e::RangeBounds(40u, 60u));
  std::printf("PUB: CREATE_REGION dim=Channel lower=40 upper=60\n");

  rti1516e::RegionHandleSet regions;
  regions.insert(rPub);
  amb->commitRegionModifications(regions);
  std::printf("PUB: COMMIT_REGION region=<R>\n");

  amb->reserveObjectInstanceName(L"sensor-1");
  while (!fed.reservation_ok) amb->evokeMultipleCallbacks(0.05, 0.1);

  rti1516e::AttributeHandleSetRegionHandleSetPairVector pairs;
  pairs.push_back(std::make_pair(attrs, regions));
  auto inst = amb->registerObjectInstanceWithRegions(sClass, pairs, L"sensor-1");
  std::printf("PUB: REGISTER class=Sensor name=sensor-1\n");

  rti1516e::VariableLengthData tag;
  for (int i = 1; i <= 6; ++i) {
    rti1516e::HLAfloat64BE v(static_cast<double>(i));
    rti1516e::AttributeHandleValueMap values;
    values[vAttr] = v.encode();
    amb->updateAttributeValues(inst, values, tag);
    std::printf("PUB: UPDATE name=sensor-1 Value=%d.000000\n", i);
    // 150 ms spacing gives the subscriber's reflect-counted region modify
    // (see federate_subscriber.cpp) a clean window between updates 3 and 4.
    std::this_thread::sleep_for(std::chrono::milliseconds(150));
  }

  amb->resignFederationExecution(rti1516e::CANCEL_THEN_DELETE_THEN_DIVEST);
  std::printf("PUB: RESIGN action=CANCEL_THEN_DELETE_THEN_DIVEST\n");
  try { amb->destroyFederationExecution(fedName); } catch (...) {}
  return 0;
}
