// fm_sync_subset_with_failure — bob: participates, achieves
// successfully=true. Observes failedToSyncSet containing carol's handle.

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

class BobFed : public rti1516e::NullFederateAmbassador {
 public:
  void announceSynchronizationPoint(
      std::wstring const& label,
      rti1516e::VariableLengthData const& tag) override {
    std::cout << "BOB: ANNOUNCE_SYNC label=" << ws2s(label) << std::endl;
    announced_.store(true);
  }

  void federationSynchronized(
      std::wstring const& label,
      rti1516e::FederateHandleSet const& failedToSyncSet) override {
    std::cout << "BOB: FEDERATION_SYNCHRONIZED label=" << ws2s(label)
              << " failedToSyncSet.size=" << failedToSyncSet.size()
              << std::endl;
    synchronized_.store(true);
  }

  std::atomic<bool> announced_{false};
  std::atomic<bool> synchronized_{false};
};
}  // namespace

int main() {
  try {
    rti1516e::RTIambassadorFactory factory;
    auto amb = factory.createRTIambassador();
    BobFed fed;
    amb->connect(fed, rti1516e::HLA_IMMEDIATE, L"crcAddress=127.0.0.1:8989");
    std::cout << "BOB: CONNECT" << std::endl;

    std::vector<std::wstring> modules{L"./federation.fom.xml"};
    try {
      amb->createFederationExecution(L"fm_sync_subset", modules);
    } catch (const rti1516e::FederationExecutionAlreadyExists&) {}
    amb->joinFederationExecution(L"bob", L"fm_sync_subset");
    std::cout << "BOB: JOIN federate=bob" << std::endl;

    for (int i = 0; i < 400 && !fed.announced_.load(); ++i) {
      amb->evokeMultipleCallbacks(0.05, 0.1);  // caller-thread drain (gorti M17 buffers; harmless yield under Pitch)
    }

    amb->synchronizationPointAchieved(L"checkpoint", true);
    std::cout << "BOB: ACHIEVED label=checkpoint successfully=true"
              << std::endl;

    for (int i = 0; i < 400 && !fed.synchronized_.load(); ++i) {
      amb->evokeMultipleCallbacks(0.05, 0.1);  // caller-thread drain (gorti M17 buffers; harmless yield under Pitch)
    }

    amb->resignFederationExecution(rti1516e::CANCEL_THEN_DELETE_THEN_DIVEST);
    std::cout << "BOB: RESIGN" << std::endl;
    amb->disconnect();
    return 0;
  } catch (const rti1516e::Exception& e) {
    std::cerr << "BOB: ERROR " << ws2s(e.what()) << std::endl;
    return 1;
  }
}
