// M31 conformance fixture: mom_federation_lifecycle — passive member.
//
// Spec § anchors:
//   §4.9  joinFederationExecution
//   §4.10 resignFederationExecution
//
// Each member just joins, idles briefly, and resigns. The MOM observer
// (federate_observer.cpp) is the one that watches the lifecycle via
// standard pub/sub on HLAobjectRoot.HLAmanager.HLAfederate.

#include <RTI/RTIambassador.h>
#include <RTI/RTIambassadorFactory.h>
#include <RTI/NullFederateAmbassador.h>
#include <RTI/Enums.h>

#include <chrono>
#include <cstdio>
#include <memory>
#include <string>
#include <thread>
#include <vector>

namespace {

class MemberFed : public rti1516e::NullFederateAmbassador {};

}  // namespace

int main(int argc, char** argv) {
  std::string url = "grpc://127.0.0.1:8080";
  std::string fom = "./federation.fom.xml";
  std::string name = "member";
  int dwell_ms = 500;
  for (int i = 1; i + 1 < argc; i += 2) {
    std::string k = argv[i];
    if (k == "--url") url = argv[i + 1];
    else if (k == "--fom") fom = argv[i + 1];
    else if (k == "--name") name = argv[i + 1];
    else if (k == "--dwell-ms") dwell_ms = std::stoi(argv[i + 1]);
  }

  rti1516e::RTIambassadorFactory factory;
  rti1516e::auto_ptr<rti1516e::RTIambassador> amb(factory.createRTIambassador());
  MemberFed fed;
  std::wstring settings = L"crcAddress=" + std::wstring(url.begin() + 7, url.end());
  amb->connect(fed, rti1516e::HLA_IMMEDIATE, settings);
  std::printf("%s: CONNECT\n", name.c_str());

  std::wstring fedName = L"mom_federation_lifecycle";
  try {
    std::vector<std::wstring> modules{std::wstring(fom.begin(), fom.end())};
    amb->createFederationExecution(fedName, modules);
  } catch (rti1516e::FederationExecutionAlreadyExists const&) {}
  amb->joinFederationExecution(std::wstring(name.begin(), name.end()), fedName);
  std::printf("%s: JOIN federate=%s\n", name.c_str(), name.c_str());

  std::this_thread::sleep_for(std::chrono::milliseconds(dwell_ms));

  amb->resignFederationExecution(rti1516e::CANCEL_THEN_DELETE_THEN_DIVEST);
  std::printf("%s: RESIGN action=CANCEL_THEN_DELETE_THEN_DIVEST\n", name.c_str());
  return 0;
}
