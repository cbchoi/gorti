// own_acquire_if_available_race — carrier (initial owner).
//
// Registers car-race, holds Position, then divests negotiation-free
// by simply staying around. Bob and Carol race to acquire-if-available.
// Exactly one of them wins; the other gets §7.10 unavailable.

#include <RTI/RTIambassador.h>
#include <RTI/RTIambassadorFactory.h>
#include <RTI/NullFederateAmbassador.h>
#include <RTI/Enums.h>
#include <RTI/Handle.h>
#include <RTI/Typedefs.h>

#include <chrono>
#include <iostream>
#include <string>
#include <thread>

namespace {
std::string ws2s(const std::wstring& w) {
  return std::string(w.begin(), w.end());
}

class CarrierFed : public rti1516e::NullFederateAmbassador {};
}  // namespace

int main() {
  try {
    rti1516e::RTIambassadorFactory factory;
    auto amb = factory.createRTIambassador();
    CarrierFed fed;

    amb->connect(fed, rti1516e::HLA_IMMEDIATE,
                 L"crcAddress=127.0.0.1:8989");
    std::cout << "CARRIER: CONNECT" << std::endl;

    std::vector<std::wstring> modules{L"./federation.fom.xml"};
    try {
      amb->createFederationExecution(L"own_acquire_race", modules);
    } catch (const rti1516e::FederationExecutionAlreadyExists&) {}
    amb->joinFederationExecution(L"carrier", L"own_acquire_race");
    std::cout << "CARRIER: JOIN" << std::endl;

    const auto vehicle = amb->getObjectClassHandle(L"HLAobjectRoot.Vehicle");
    const auto pos = amb->getAttributeHandle(vehicle, L"Position");
    rti1516e::AttributeHandleSet attrs;
    attrs.insert(pos);
    amb->publishObjectClassAttributes(vehicle, attrs);

    const auto obj = amb->registerObjectInstance(vehicle, L"car-race");
    std::cout << "CARRIER: REGISTER name=car-race handle=<H>" << std::endl;

    // §7.2 unconditionalAttributeOwnershipDivestiture — Position must
    // be UNOWNED for §7.9 acquireIfAvailable to have a winner at all:
    // acquisitionIfAvailable only ever acquires unowned attributes and
    // never solicits release from a current owner. The M31 fixture had
    // the carrier hold Position, under which NO compliant RTI could
    // deliver the golden's §7.7 win to either racer (parity-CD fix;
    // goldens updated, see README).
    amb->unconditionalAttributeOwnershipDivestiture(obj, attrs);
    std::cout << "CARRIER: UNCONDITIONAL_DIVEST attrs=[Position]"
              << std::endl;

    // Hold long enough for bob + carol to race.
    std::this_thread::sleep_for(std::chrono::seconds(3));

    amb->resignFederationExecution(rti1516e::CANCEL_THEN_DELETE_THEN_DIVEST);
    std::cout << "CARRIER: RESIGN" << std::endl;
    amb->disconnect();
    return 0;
  } catch (const rti1516e::Exception& e) {
    std::cerr << "CARRIER: ERROR " << ws2s(e.what()) << std::endl;
    return 1;
  }
}
