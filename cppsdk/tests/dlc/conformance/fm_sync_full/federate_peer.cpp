// fm_sync_full — peer federate (used as both bob and carol via $FED_NAME
// env var). Joins, waits for announce, achieves successfully=true,
// observes federationSynchronized.

#include <RTI/RTIambassador.h>
#include <RTI/RTIambassadorFactory.h>
#include <RTI/NullFederateAmbassador.h>
#include <RTI/Enums.h>
#include <RTI/Handle.h>
#include <RTI/Typedefs.h>

#include <atomic>
#include <chrono>
#include <cstdlib>
#include <iostream>
#include <string>
#include <thread>

namespace {
std::string ws2s(const std::wstring& w) {
  return std::string(w.begin(), w.end());
}

std::wstring s2ws(const std::string& s) {
  return std::wstring(s.begin(), s.end());
}

class SyncPeerFed : public rti1516e::NullFederateAmbassador {
 public:
  void announceSynchronizationPoint(
      std::wstring const& synchronizationPointLabel,
      rti1516e::VariableLengthData const& theUserSuppliedTag) override {
    std::cout << name_ << ": ANNOUNCE_SYNC label="
              << ws2s(synchronizationPointLabel) << std::endl;
    announced_.store(true);
  }

  void federationSynchronized(
      std::wstring const& synchronizationPointLabel,
      rti1516e::FederateHandleSet const& failedToSyncSet) override {
    std::cout << name_ << ": FEDERATION_SYNCHRONIZED label="
              << ws2s(synchronizationPointLabel)
              << " failedToSyncSet.size=" << failedToSyncSet.size()
              << std::endl;
    synchronized_.store(true);
  }

  std::string name_;
  std::atomic<bool> announced_{false};
  std::atomic<bool> synchronized_{false};
};
}  // namespace

int main() {
  const char* name_env = std::getenv("FED_NAME");
  const std::string name = name_env ? name_env : "PEER";
  try {
    rti1516e::RTIambassadorFactory factory;
    auto amb = factory.createRTIambassador();
    SyncPeerFed fed;
    fed.name_ = name;

    amb->connect(fed, rti1516e::HLA_IMMEDIATE, L"gortiAddress=127.0.0.1:8080");
    std::cout << name << ": CONNECT" << std::endl;

    std::vector<std::wstring> modules{L"./federation.fom.xml"};
    try {
      amb->createFederationExecution(L"fm_sync_full", modules);
    } catch (const rti1516e::FederationExecutionAlreadyExists&) {}
    amb->joinFederationExecution(s2ws(name), L"fm_sync_full");
    std::cout << name << ": JOIN federate=" << name << std::endl;

    for (int i = 0; i < 400 && !fed.announced_.load(); ++i) {
      amb->evokeMultipleCallbacks(0.05, 0.1);  // caller-thread drain (gorti M17 buffers; harmless yield under reference_rti)
    }

    // §4.14 successfully=true.
    amb->synchronizationPointAchieved(L"checkpoint", true);
    std::cout << name << ": ACHIEVED label=checkpoint successfully=true"
              << std::endl;

    for (int i = 0; i < 200 && !fed.synchronized_.load(); ++i) {
      amb->evokeMultipleCallbacks(0.05, 0.1);  // caller-thread drain (gorti M17 buffers; harmless yield under reference_rti)
    }

    amb->resignFederationExecution(rti1516e::CANCEL_THEN_DELETE_THEN_DIVEST);
    std::cout << name << ": RESIGN" << std::endl;
    amb->disconnect();
    return 0;
  } catch (const rti1516e::Exception& e) {
    std::cerr << name << ": ERROR " << ws2s(e.what()) << std::endl;
    return 1;
  }
}
