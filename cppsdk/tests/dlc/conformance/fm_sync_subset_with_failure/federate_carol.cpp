// fm_sync_subset_with_failure — carol: achieves successfully=FALSE.
// The §4.14 successfully=false path (catalogue row 3.11) flows into
// §4.15 federationSynchronized's failedToSyncSet (catalogue row 4.7).

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

class CarolFed : public rti1516e::NullFederateAmbassador {
 public:
  void announceSynchronizationPoint(
      std::wstring const& label,
      rti1516e::VariableLengthData const& tag) override {
    std::cout << "CAROL: ANNOUNCE_SYNC label=" << ws2s(label) << std::endl;
    announced_.store(true);
  }

  void federationSynchronized(
      std::wstring const& label,
      rti1516e::FederateHandleSet const& failedToSyncSet) override {
    std::cout << "CAROL: FEDERATION_SYNCHRONIZED label=" << ws2s(label)
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
    CarolFed fed;
    amb->connect(fed, rti1516e::HLA_IMMEDIATE, L"crcAddress=127.0.0.1:8989");
    std::cout << "CAROL: CONNECT" << std::endl;

    std::vector<std::wstring> modules{L"./federation.fom.xml"};
    try {
      amb->createFederationExecution(L"fm_sync_subset", modules);
    } catch (const rti1516e::FederationExecutionAlreadyExists&) {}
    amb->joinFederationExecution(L"carol", L"fm_sync_subset");
    std::cout << "CAROL: JOIN federate=carol" << std::endl;

    for (int i = 0; i < 400 && !fed.announced_.load(); ++i) {
      std::this_thread::sleep_for(std::chrono::milliseconds(50));
    }

    // §4.14 successfully=FALSE — catalogue row 3.11.
    amb->synchronizationPointAchieved(L"checkpoint", false);
    std::cout << "CAROL: ACHIEVED label=checkpoint successfully=false"
              << std::endl;

    for (int i = 0; i < 400 && !fed.synchronized_.load(); ++i) {
      std::this_thread::sleep_for(std::chrono::milliseconds(50));
    }

    amb->resignFederationExecution(rti1516e::CANCEL_THEN_DELETE_THEN_DIVEST);
    std::cout << "CAROL: RESIGN" << std::endl;
    amb->disconnect();
    return 0;
  } catch (const rti1516e::Exception& e) {
    std::cerr << "CAROL: ERROR " << ws2s(e.what()) << std::endl;
    return 1;
  }
}
