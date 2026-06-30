// om_reserve_multi_atomic — "collider" federate.
//
// First federate that reserves "car-X" via §6.3 reserveObjectInstanceName
// to set up the collision. Holds the reservation, then resigns after
// the second federate has had a chance to observe the failure.

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

class ColliderFed : public rti1516e::NullFederateAmbassador {
 public:
  // §6.3 objectInstanceNameReservationSucceeded — singular form per
  // catalogue row 4.17 (wstring vs M17's string).
  void objectInstanceNameReservationSucceeded(
      std::wstring const& theObjectInstanceName) override {
    std::cout << "COLLIDER: NAME_RESERVED name="
              << ws2s(theObjectInstanceName) << std::endl;
    reserved_.store(true);
  }

  std::atomic<bool> reserved_{false};
};

}  // namespace

int main() {
  try {
    rti1516e::RTIambassadorFactory factory;
    rti1516e::auto_ptr<rti1516e::RTIambassador> amb =
        factory.createRTIambassador();

    ColliderFed fed;
    amb->connect(fed, rti1516e::HLA_IMMEDIATE,
                 L"crcAddress=127.0.0.1:8989");
    std::cout << "COLLIDER: CONNECT" << std::endl;

    std::vector<std::wstring> modules{L"./federation.fom.xml"};
    try {
      amb->createFederationExecution(L"om_reserve_multi_atomic", modules);
    } catch (const rti1516e::FederationExecutionAlreadyExists&) {}

    amb->joinFederationExecution(L"collider", L"om_reserve_multi_atomic");
    std::cout << "COLLIDER: JOIN" << std::endl;

    // Reserve the colliding name.
    amb->reserveObjectInstanceName(L"car-X");
    std::cout << "COLLIDER: RESERVE_REQUEST name=car-X" << std::endl;

    for (int i = 0; i < 100 && !fed.reserved_.load(); ++i) {
      std::this_thread::sleep_for(std::chrono::milliseconds(50));
    }

    // Hold so the multi-reserver hits the collision.
    std::this_thread::sleep_for(std::chrono::seconds(2));

    amb->resignFederationExecution(rti1516e::CANCEL_THEN_DELETE_THEN_DIVEST);
    std::cout << "COLLIDER: RESIGN" << std::endl;
    amb->disconnect();
    return 0;
  } catch (const rti1516e::Exception& e) {
    std::cerr << "COLLIDER: ERROR " << ws2s(e.what()) << std::endl;
    return 1;
  }
}
