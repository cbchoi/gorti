// om_reserve_multi_atomic — "reserver" federate.
//
// IEEE 1516.1-2010 §6.5 reserveMultipleObjectInstanceName + §6.6 ack.
// Calls reserveMultipleObjectInstanceName({"car-Y", "car-X", "car-Z"})
// — "car-X" is held by the collider, so ALL THREE names must fail
// atomically per the spec's "all-or-nothing" guarantee. The RTI fires
// multipleObjectInstanceNameReservationFailed(set<wstring>) with the
// colliding-names set.
//
// Catalogue row 11.1 (BLOCKING — singular "Name" + set + wstring vs.
// M17's plural "Names" + vector) and row 4.18 (BLOCKING — set<wstring>
// for both succeeded/failed callbacks; 1-arg failed not 2-arg).

#include <RTI/RTIambassador.h>
#include <RTI/RTIambassadorFactory.h>
#include <RTI/NullFederateAmbassador.h>
#include <RTI/Enums.h>
#include <RTI/Handle.h>
#include <RTI/Typedefs.h>

#include <atomic>
#include <chrono>
#include <iostream>
#include <set>
#include <string>
#include <thread>

namespace {

std::string ws2s(const std::wstring& w) {
  return std::string(w.begin(), w.end());
}

class ReserverFed : public rti1516e::NullFederateAmbassador {
 public:
  // §6.6 multipleObjectInstanceNameReservationSucceeded — set<wstring>
  // form (catalogue row 4.18).
  void multipleObjectInstanceNameReservationSucceeded(
      std::set<std::wstring> const& theObjectInstanceNames) override {
    std::cout << "RESERVER: MULTI_RESERVED_OK count="
              << theObjectInstanceNames.size() << std::endl;
    succeeded_.store(true);
  }

  // §6.6 multipleObjectInstanceNameReservationFailed — 1-arg form per
  // catalogue row 4.18 (M17 had 2-arg with vector).
  void multipleObjectInstanceNameReservationFailed(
      std::set<std::wstring> const& theObjectInstanceNames) override {
    std::cout << "RESERVER: MULTI_RESERVED_FAILED colliding=[";
    bool first = true;
    for (auto const& n : theObjectInstanceNames) {
      if (!first) std::cout << ",";
      std::cout << ws2s(n);
      first = false;
    }
    std::cout << "]" << std::endl;
    failed_.store(true);
  }

  std::atomic<bool> succeeded_{false};
  std::atomic<bool> failed_{false};
};

}  // namespace

int main() {
  try {
    rti1516e::RTIambassadorFactory factory;
    rti1516e::auto_ptr<rti1516e::RTIambassador> amb =
        factory.createRTIambassador();

    ReserverFed fed;
    amb->connect(fed, rti1516e::HLA_IMMEDIATE,
                 L"crcAddress=127.0.0.1:8989");
    std::cout << "RESERVER: CONNECT" << std::endl;

    std::vector<std::wstring> modules{L"./federation.fom.xml"};
    try {
      amb->createFederationExecution(L"om_reserve_multi_atomic", modules);
    } catch (const rti1516e::FederationExecutionAlreadyExists&) {}

    amb->joinFederationExecution(L"reserver", L"om_reserve_multi_atomic");
    std::cout << "RESERVER: JOIN" << std::endl;

    // Give collider time to reserve "car-X".
    std::this_thread::sleep_for(std::chrono::seconds(1));

    std::set<std::wstring> names{L"car-Y", L"car-X", L"car-Z"};
    amb->reserveMultipleObjectInstanceName(names);
    std::cout << "RESERVER: MULTI_RESERVE_REQUEST names=[car-X,car-Y,car-Z]"
              << std::endl;

    // Wait for the failed callback. Drain via §10.42
    // evokeMultipleCallbacks — legal under HLA_IMMEDIATE on both RTIs
    // (Pitch delivers on background threads and the evoke is a harmless
    // yield; gorti M17 buffers events and drains them on the evoking
    // thread). Emits no canonical lines, so goldens are unaffected.
    for (int i = 0; i < 200 && !fed.failed_.load(); ++i) {
      amb->evokeMultipleCallbacks(0.05, 0.1);
    }

    amb->resignFederationExecution(rti1516e::CANCEL_THEN_DELETE_THEN_DIVEST);
    std::cout << "RESERVER: RESIGN" << std::endl;
    amb->disconnect();
    return 0;
  } catch (const rti1516e::Exception& e) {
    std::cerr << "RESERVER: ERROR " << ws2s(e.what()) << std::endl;
    return 1;
  }
}
