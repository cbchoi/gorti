// dm_pub_sub_active_passive — subscriber federate.
//
// Phase 1: subscribeObjectClassAttributes(active=false) — passive
// subscription. Per IEEE 1516.1-2010 §5.6 (catalogue row 11.9 BLOCKING
// — gorti M17 has no active flag) this MUST NOT trigger
// startRegistrationForObjectClass on the publisher.
//
// Phase 2: subscribeObjectClassAttributes(active=true) — re-subscribe
// active. This MUST trigger startRegistrationForObjectClass on the
// publisher.

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

class SubscriberFed : public rti1516e::NullFederateAmbassador {};
}  // namespace

int main() {
  try {
    rti1516e::RTIambassadorFactory factory;
    auto amb = factory.createRTIambassador();
    SubscriberFed fed;
    amb->connect(fed, rti1516e::HLA_IMMEDIATE, L"gortiAddress=127.0.0.1:8080");
    std::cout << "SUB: CONNECT" << std::endl;

    std::vector<std::wstring> modules{L"./federation.fom.xml"};
    try {
      amb->createFederationExecution(L"dm_pub_sub", modules);
    } catch (const rti1516e::FederationExecutionAlreadyExists&) {}
    amb->joinFederationExecution(L"subscriber", L"dm_pub_sub");
    std::cout << "SUB: JOIN federate=subscriber" << std::endl;

    const auto vehicle = amb->getObjectClassHandle(L"HLAobjectRoot.Vehicle");
    const auto pos = amb->getAttributeHandle(vehicle, L"Position");
    rti1516e::AttributeHandleSet attrs;
    attrs.insert(pos);

    // Phase 1: passive subscribe (active=false). Should NOT trigger
    // publisher's startRegistration callback.
    amb->subscribeObjectClassAttributes(vehicle, attrs, false, L"");
    std::cout << "SUB: SUBSCRIBE class=Vehicle active=false" << std::endl;

    std::this_thread::sleep_for(std::chrono::seconds(1));

    // Phase 2: re-subscribe active=true. This MUST trigger
    // startRegistration on the publisher.
    amb->subscribeObjectClassAttributes(vehicle, attrs, true, L"");
    std::cout << "SUB: SUBSCRIBE class=Vehicle active=true" << std::endl;

    std::this_thread::sleep_for(std::chrono::seconds(1));

    amb->resignFederationExecution(rti1516e::CANCEL_THEN_DELETE_THEN_DIVEST);
    std::cout << "SUB: RESIGN" << std::endl;
    amb->disconnect();
    return 0;
  } catch (const rti1516e::Exception& e) {
    std::cerr << "SUB: ERROR " << ws2s(e.what()) << std::endl;
    return 1;
  }
}
