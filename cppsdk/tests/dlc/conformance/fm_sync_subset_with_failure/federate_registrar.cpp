// fm_sync_subset_with_failure — registrar federate (alice).
//
// Calls the 2-arg overload of registerFederationSynchronizationPoint
// passing a FederateHandleSet containing only {bob, carol} per §4.11
// (catalogue row 3.10 BLOCKING). Alice is NOT in the sync set; she
// observes the registration success but no announce/achieve, and
// federationSynchronized fires only on the participating federates.

#include <RTI/RTIambassador.h>
#include <RTI/RTIambassadorFactory.h>
#include <RTI/NullFederateAmbassador.h>
#include <RTI/Enums.h>
#include <RTI/Handle.h>
#include <RTI/Typedefs.h>

#include <atomic>
#include <chrono>
#include <iostream>
#include <string>
#include <thread>

namespace {
std::string ws2s(const std::wstring& w) {
  return std::string(w.begin(), w.end());
}

class RegistrarFed : public rti1516e::NullFederateAmbassador {
 public:
  void synchronizationPointRegistrationSucceeded(
      std::wstring const& label) override {
    std::cout << "REG: SYNC_REGISTRATION_SUCCEEDED label=" << ws2s(label)
              << std::endl;
    registered_.store(true);
  }

  std::atomic<bool> registered_{false};
};
}  // namespace

int main() {
  try {
    rti1516e::RTIambassadorFactory factory;
    auto amb = factory.createRTIambassador();
    RegistrarFed fed;
    amb->connect(fed, rti1516e::HLA_IMMEDIATE, L"gortiAddress=127.0.0.1:8080");
    std::cout << "REG: CONNECT" << std::endl;

    std::vector<std::wstring> modules{L"./federation.fom.xml"};
    try {
      amb->createFederationExecution(L"fm_sync_subset", modules);
    } catch (const rti1516e::FederationExecutionAlreadyExists&) {}
    amb->joinFederationExecution(L"alice", L"fm_sync_subset");
    std::cout << "REG: JOIN federate=alice" << std::endl;

    // Wait for bob and carol to join.
    std::this_thread::sleep_for(std::chrono::milliseconds(700));

    // §4.11 2-arg overload — explicit subset.
    rti1516e::FederateHandleSet subset;
    subset.insert(amb->getFederateHandle(L"bob"));
    subset.insert(amb->getFederateHandle(L"carol"));
    rti1516e::VariableLengthData tag;
    amb->registerFederationSynchronizationPoint(L"checkpoint", tag, subset);
    std::cout << "REG: REGISTER_SYNC_POINT_SUBSET label=checkpoint size=2"
              << std::endl;

    for (int i = 0; i < 200 && !fed.registered_.load(); ++i) {
      amb->evokeMultipleCallbacks(0.05, 0.1);  // caller-thread drain (gorti M17 buffers; harmless yield under reference_rti)
    }

    // Hold long enough for the subset sync to play out.
    std::this_thread::sleep_for(std::chrono::seconds(2));

    amb->resignFederationExecution(rti1516e::CANCEL_THEN_DELETE_THEN_DIVEST);
    std::cout << "REG: RESIGN" << std::endl;
    amb->disconnect();
    return 0;
  } catch (const rti1516e::Exception& e) {
    std::cerr << "REG: ERROR " << ws2s(e.what()) << std::endl;
    return 1;
  }
}
