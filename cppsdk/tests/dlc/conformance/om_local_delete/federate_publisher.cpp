// om_local_delete — DLC-strict publisher.
//
// Registers car-local on the federation. The publisher is NOT the
// federate calling localDeleteObjectInstance (that's the subscriber);
// the publisher just registers and updates so the subscriber has
// something to local-delete.

#include <RTI/RTIambassador.h>
#include <RTI/RTIambassadorFactory.h>
#include <RTI/NullFederateAmbassador.h>
#include <RTI/Enums.h>
#include <RTI/Handle.h>
#include <RTI/Typedefs.h>
#include <RTI/encoding/BasicDataElements.h>

#include <chrono>
#include <iostream>
#include <string>
#include <thread>

namespace {

std::string ws2s(const std::wstring& w) {
  return std::string(w.begin(), w.end());
}

class PubFed : public rti1516e::NullFederateAmbassador {};

}  // namespace

int main() {
  try {
    rti1516e::RTIambassadorFactory factory;
    rti1516e::auto_ptr<rti1516e::RTIambassador> amb =
        factory.createRTIambassador();

    PubFed fed;
    amb->connect(fed, rti1516e::HLA_IMMEDIATE,
                 L"crcAddress=127.0.0.1:8989");
    std::cout << "PUB: CONNECT" << std::endl;

    std::vector<std::wstring> modules{L"./federation.fom.xml"};
    try {
      amb->createFederationExecution(L"om_local_delete", modules);
    } catch (const rti1516e::FederationExecutionAlreadyExists&) {}

    amb->joinFederationExecution(L"publisher", L"om_local_delete");
    std::cout << "PUB: JOIN" << std::endl;

    const auto vehicle = amb->getObjectClassHandle(L"HLAobjectRoot.Vehicle");
    const auto pos = amb->getAttributeHandle(vehicle, L"Position");
    rti1516e::AttributeHandleSet attrs;
    attrs.insert(pos);
    amb->publishObjectClassAttributes(vehicle, attrs);

    // Wait for subscriber subscribe.
    std::this_thread::sleep_for(std::chrono::milliseconds(500));

    const auto obj = amb->registerObjectInstance(vehicle, L"car-local");
    std::cout << "PUB: REGISTER name=car-local handle=<H>" << std::endl;

    {
      rti1516e::HLAfloat64BE p(99.0);
      rti1516e::AttributeHandleValueMap vals;
      vals[pos] = p.encode();
      amb->updateAttributeValues(obj, vals, rti1516e::VariableLengthData());
      std::cout << "PUB: UPDATE name=car-local Position=99.000000" << std::endl;
    }

    // Hold so the subscriber can DISCOVER, REFLECT, then localDelete.
    std::this_thread::sleep_for(std::chrono::seconds(3));

    // Publisher should NOT see anything happen on its side — the
    // subscriber's local delete is invisible to it.
    amb->resignFederationExecution(rti1516e::CANCEL_THEN_DELETE_THEN_DIVEST);
    std::cout << "PUB: RESIGN" << std::endl;
    amb->disconnect();
    return 0;
  } catch (const rti1516e::Exception& e) {
    std::cerr << "PUB: ERROR " << ws2s(e.what()) << std::endl;
    return 1;
  }
}
