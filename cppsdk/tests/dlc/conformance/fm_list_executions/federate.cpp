// fm_list_executions — single federate exercising §4.7 listFederationExecutions
// + §4.8 reportFederationExecutions callback.
//
// Sequence:
//   1. CONNECT (no join — listing is allowed pre-join per spec §4.7).
//   2. Create three federations (alpha, beta, gamma).
//   3. Call listFederationExecutions() — returns void; result comes back
//      asynchronously via the reportFederationExecutions callback (§4.8).
//   4. Wait for callback; print the names it reports.
//   5. Destroy each federation; DISCONNECT.
//
// Locks divergence catalogue row 3.7 (listFederationExecutions absent in
// M17) + row 4.4 (reportFederationExecutions callback absent in M17).

#include <RTI/RTIambassador.h>
#include <RTI/RTIambassadorFactory.h>
#include <RTI/NullFederateAmbassador.h>
#include <RTI/Enums.h>
#include <RTI/Typedefs.h>

#include <algorithm>
#include <atomic>
#include <chrono>
#include <iostream>
#include <memory>
#include <string>
#include <thread>
#include <vector>

namespace {

std::string ws2s(const std::wstring& w) {
  return std::string(w.begin(), w.end());
}

class ListFed : public rti1516e::NullFederateAmbassador {
 public:
  // §4.8 reportFederationExecutions callback — RTI delivers the list
  // asynchronously after listFederationExecutions() returns.
  void reportFederationExecutions(
      rti1516e::FederationExecutionInformationVector const&
          theFederationExecutionInformationList) override {
    std::vector<std::string> names;
    for (const auto& info : theFederationExecutionInformationList) {
      names.push_back(ws2s(info.federationExecutionName));
    }
    std::sort(names.begin(), names.end());
    std::cout << "FED: REPORT_FEDERATION_EXECUTIONS count=" << names.size();
    for (const auto& n : names) std::cout << " " << n;
    std::cout << std::endl;
    reported_.store(true);
  }

  std::atomic<bool> reported_{false};
};

}  // namespace

int main() {
  try {
    rti1516e::RTIambassadorFactory factory;
    rti1516e::auto_ptr<rti1516e::RTIambassador> amb =
        factory.createRTIambassador();

    ListFed fed;
    amb->connect(fed, rti1516e::HLA_IMMEDIATE,
                 L"crcAddress=127.0.0.1:8989");
    std::cout << "FED: CONNECT" << std::endl;

    std::vector<std::wstring> modules{L"./federation.fom.xml"};
    for (const auto& fn : {L"alpha", L"beta", L"gamma"}) {
      try {
        amb->createFederationExecution(fn, modules);
        std::cout << "FED: CREATE federation=" << ws2s(fn) << std::endl;
      } catch (const rti1516e::FederationExecutionAlreadyExists&) {
        std::cout << "FED: CREATE federation=" << ws2s(fn) << std::endl;
      }
    }

    amb->listFederationExecutions();
    std::cout << "FED: LIST_FEDERATION_EXECUTIONS" << std::endl;

    // Drain via §10.42 evokeMultipleCallbacks. Legal under HLA_IMMEDIATE
    // on both RTIs (Pitch delivers on background threads and the evoke is
    // a harmless yield; gorti M17 buffers events and drains them on the
    // evoking thread). Emits no canonical lines, so goldens are unaffected.
    for (int i = 0; i < 200 && !fed.reported_.load(); ++i) {
      amb->evokeMultipleCallbacks(0.05, 0.1);
    }

    for (const auto& fn : {L"alpha", L"beta", L"gamma"}) {
      try {
        amb->destroyFederationExecution(fn);
        std::cout << "FED: DESTROY federation=" << ws2s(fn) << std::endl;
      } catch (const rti1516e::Exception&) {}
    }

    amb->disconnect();
    std::cout << "FED: DISCONNECT" << std::endl;
    return 0;
  } catch (const rti1516e::Exception& e) {
    std::cerr << "FED: ERROR " << ws2s(e.what()) << std::endl;
    return 1;
  }
}
