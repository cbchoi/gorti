// fm_sync_full — registrar federate: registers the sync point and
// observes the success ack + announce + own achieve + sync-complete.
//
// Per IEEE 1516.1-2010:
//   §4.11 registerFederationSynchronizationPoint (1-arg overload — no
//         FederateHandleSet means whole federation).
//   §4.12 synchronizationPointRegistrationSucceeded callback.
//   §4.13 announceSynchronizationPoint callback.
//   §4.14 synchronizationPointAchieved (with successfully=true per
//         catalogue row 3.11).
//   §4.15 federationSynchronized callback with failedToSyncSet (per
//         catalogue row 4.7).

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

class SyncFed : public rti1516e::NullFederateAmbassador {
 public:
  void synchronizationPointRegistrationSucceeded(
      std::wstring const& synchronizationPointLabel) override {
    std::cout << "REG: SYNC_REGISTRATION_SUCCEEDED label="
              << ws2s(synchronizationPointLabel) << std::endl;
    registered_.store(true);
  }

  void announceSynchronizationPoint(
      std::wstring const& synchronizationPointLabel,
      rti1516e::VariableLengthData const& theUserSuppliedTag) override {
    std::cout << "REG: ANNOUNCE_SYNC label="
              << ws2s(synchronizationPointLabel) << std::endl;
    announced_.store(true);
  }

  void federationSynchronized(
      std::wstring const& synchronizationPointLabel,
      rti1516e::FederateHandleSet const& failedToSyncSet) override {
    std::cout << "REG: FEDERATION_SYNCHRONIZED label="
              << ws2s(synchronizationPointLabel)
              << " failedToSyncSet.size=" << failedToSyncSet.size()
              << std::endl;
    synchronized_.store(true);
  }

  std::atomic<bool> registered_{false};
  std::atomic<bool> announced_{false};
  std::atomic<bool> synchronized_{false};
};
}  // namespace

int main() {
  try {
    rti1516e::RTIambassadorFactory factory;
    auto amb = factory.createRTIambassador();
    SyncFed fed;
    amb->connect(fed, rti1516e::HLA_IMMEDIATE, L"crcAddress=127.0.0.1:8989");
    std::cout << "REG: CONNECT" << std::endl;

    std::vector<std::wstring> modules{L"./federation.fom.xml"};
    try {
      amb->createFederationExecution(L"fm_sync_full", modules);
    } catch (const rti1516e::FederationExecutionAlreadyExists&) {}
    amb->joinFederationExecution(L"registrar", L"fm_sync_full");
    std::cout << "REG: JOIN federate=registrar" << std::endl;

    // Wait for the other two federates to join.
    std::this_thread::sleep_for(std::chrono::milliseconds(700));

    // §4.11 register sync point — 1-arg overload (no FederateHandleSet
    // means whole federation).
    rti1516e::VariableLengthData tag;
    amb->registerFederationSynchronizationPoint(L"checkpoint", tag);
    std::cout << "REG: REGISTER_SYNC_POINT label=checkpoint" << std::endl;

    for (int i = 0; i < 100 && !fed.announced_.load(); ++i) {
      std::this_thread::sleep_for(std::chrono::milliseconds(50));
    }

    // §4.14 synchronizationPointAchieved — explicit successfully=true.
    amb->synchronizationPointAchieved(L"checkpoint", true);
    std::cout << "REG: ACHIEVED label=checkpoint successfully=true"
              << std::endl;

    for (int i = 0; i < 200 && !fed.synchronized_.load(); ++i) {
      std::this_thread::sleep_for(std::chrono::milliseconds(50));
    }

    amb->resignFederationExecution(rti1516e::CANCEL_THEN_DELETE_THEN_DIVEST);
    std::cout << "REG: RESIGN" << std::endl;
    amb->disconnect();
    return 0;
  } catch (const rti1516e::Exception& e) {
    std::cerr << "REG: ERROR " << ws2s(e.what()) << std::endl;
    return 1;
  }
}
