// M31 conformance fixture: threading_callback_reentry — publisher.
//
// Plain-vanilla publisher: registers car-1 and sends one RO Position
// update. The complementary subscriber is the one that exercises the
// re-entrancy check.

#include <RTI/RTIambassador.h>
#include <RTI/RTIambassadorFactory.h>
#include <RTI/NullFederateAmbassador.h>
#include <RTI/Enums.h>
#include <RTI/encoding/HLAfloat64BE.h>

#include <cstdio>
#include <memory>
#include <string>
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

  std::wstring fedName = L"threading_callback_reentry";
  try {
    std::vector<std::wstring> modules{std::wstring(fom.begin(), fom.end())};
    amb->createFederationExecution(fedName, modules);
  } catch (rti1516e::FederationExecutionAlreadyExists const&) {}
  amb->joinFederationExecution(L"pub", fedName);
  std::printf("PUB: JOIN federate=pub\n");

  auto vClass = amb->getObjectClassHandle(L"HLAobjectRoot.Vehicle");
  auto pAttr  = amb->getAttributeHandle(vClass, L"Position");
  rti1516e::AttributeHandleSet attrs;
  attrs.insert(pAttr);
  amb->publishObjectClassAttributes(vClass, attrs);
  std::printf("PUB: PUBLISH class=Vehicle attributes=[Position]\n");

  amb->reserveObjectInstanceName(L"car-1");
  while (!fed.reservation_ok) amb->evokeMultipleCallbacks(0.05, 0.1);

  auto inst = amb->registerObjectInstance(vClass, L"car-1");
  std::printf("PUB: REGISTER class=Vehicle name=car-1\n");

  rti1516e::HLAfloat64BE position(42.0);
  rti1516e::AttributeHandleValueMap values;
  values[pAttr] = position.encode();
  rti1516e::VariableLengthData tag;  // empty mandatory tag
  amb->updateAttributeValues(inst, values, tag);
  std::printf("PUB: UPDATE name=car-1 Position=42.000000\n");

  amb->resignFederationExecution(rti1516e::CANCEL_THEN_DELETE_THEN_DIVEST);
  std::printf("PUB: RESIGN action=CANCEL_THEN_DELETE_THEN_DIVEST\n");
  try { amb->destroyFederationExecution(fedName); } catch (...) {}
  return 0;
}
