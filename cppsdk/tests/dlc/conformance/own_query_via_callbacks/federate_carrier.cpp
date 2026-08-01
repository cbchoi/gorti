// own_query_via_callbacks — carrier (publishes OwnedAttr, doesn't
// publish UnownedAttr).
//
// The carrier publishes OwnedAttr only; UnownedAttr is never published
// by anyone, so the spec says it is "not owned" (§7.18 attributeIsNotOwned).
// HLAprivilegeToDelete is an implicit attribute managed by the RTI;
// queries against it fire §7.18 attributeIsOwnedByRTI.

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
                 L"gortiAddress=127.0.0.1:8080");
    std::cout << "CARRIER: CONNECT" << std::endl;

    std::vector<std::wstring> modules{L"./federation.fom.xml"};
    try {
      amb->createFederationExecution(L"own_query_callbacks", modules);
    } catch (const rti1516e::FederationExecutionAlreadyExists&) {}
    amb->joinFederationExecution(L"carrier", L"own_query_callbacks");
    std::cout << "CARRIER: JOIN" << std::endl;

    const auto vehicle = amb->getObjectClassHandle(L"HLAobjectRoot.Vehicle");
    const auto owned_attr = amb->getAttributeHandle(vehicle, L"OwnedAttr");
    // UnownedAttr exists in the FOM but carrier does NOT publish it.

    rti1516e::AttributeHandleSet pub_set;
    pub_set.insert(owned_attr);
    amb->publishObjectClassAttributes(vehicle, pub_set);

    const auto obj = amb->registerObjectInstance(vehicle, L"car-query");
    std::cout << "CARRIER: REGISTER name=car-query handle=<H>" << std::endl;

    // Stay joined long enough for querier to issue all three queries.
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
