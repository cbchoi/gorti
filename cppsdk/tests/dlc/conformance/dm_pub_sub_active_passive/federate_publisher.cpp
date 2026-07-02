// dm_pub_sub_active_passive — publisher federate.
//
// Publishes Vehicle.Position, then enables the OBJECT_CLASS_RELEVANCE
// advisory (precondition for §5.10 callbacks). Watches for
// startRegistrationForObjectClass / stopRegistrationForObjectClass
// callbacks per IEEE 1516.1-2010 §5.10 (catalogue row 4.16 — absent in
// gorti M17).
//
// Subscriber's active=false subscribe MUST NOT fire startRegistration;
// toggling to active=true MUST. The golden enforces this.

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

class PublisherFed : public rti1516e::NullFederateAmbassador {
 public:
  // §5.10 startRegistrationForObjectClass.
  void startRegistrationForObjectClass(
      rti1516e::ObjectClassHandle theClass) override {
    std::cout << "PUB: START_REGISTRATION class=Vehicle" << std::endl;
    start_count_.fetch_add(1);
  }

  // §5.11 stopRegistrationForObjectClass.
  void stopRegistrationForObjectClass(
      rti1516e::ObjectClassHandle theClass) override {
    std::cout << "PUB: STOP_REGISTRATION class=Vehicle" << std::endl;
  }

  std::atomic<int> start_count_{0};
};
}  // namespace

int main() {
  try {
    rti1516e::RTIambassadorFactory factory;
    auto amb = factory.createRTIambassador();
    PublisherFed fed;
    amb->connect(fed, rti1516e::HLA_IMMEDIATE, L"crcAddress=127.0.0.1:8989");
    std::cout << "PUB: CONNECT" << std::endl;

    std::vector<std::wstring> modules{L"./federation.fom.xml"};
    try {
      amb->createFederationExecution(L"dm_pub_sub", modules);
    } catch (const rti1516e::FederationExecutionAlreadyExists&) {}
    amb->joinFederationExecution(L"publisher", L"dm_pub_sub");
    std::cout << "PUB: JOIN federate=publisher" << std::endl;

    const auto vehicle = amb->getObjectClassHandle(L"HLAobjectRoot.Vehicle");
    const auto pos = amb->getAttributeHandle(vehicle, L"Position");
    rti1516e::AttributeHandleSet attrs;
    attrs.insert(pos);
    amb->publishObjectClassAttributes(vehicle, attrs);
    std::cout << "PUB: PUBLISH class=Vehicle attributes=[Position]"
              << std::endl;

    // Enable the §6.21-style advisory so we get startRegistration
    // callbacks (per spec they require an explicit enable in some
    // implementations; tracking the call here for clarity).
    amb->enableObjectClassRelevanceAdvisorySwitch();
    std::cout << "PUB: ADVISORY_SWITCH=ObjectClassRelevance" << std::endl;

    // Hold long enough for the subscriber to toggle passive → active
    // and trigger START_REGISTRATION at the right point. Drain via
    // §10.42 evokeMultipleCallbacks — legal under HLA_IMMEDIATE on both
    // RTIs (Pitch delivers on background threads and the evoke is a
    // harmless yield; gorti M17 buffers events and drains them on the
    // evoking thread). Emits no canonical lines, so goldens are
    // unaffected. Keep draining the full window even after the first
    // START_REGISTRATION so a spurious second one (e.g. if the RTI
    // wrongly fires on the passive subscribe too) would be captured —
    // the golden asserts EXACTLY ONCE.
    const auto deadline =
        std::chrono::steady_clock::now() + std::chrono::seconds(4);
    while (std::chrono::steady_clock::now() < deadline) {
      amb->evokeMultipleCallbacks(0.05, 0.1);
    }

    amb->resignFederationExecution(rti1516e::CANCEL_THEN_DELETE_THEN_DIVEST);
    std::cout << "PUB: RESIGN" << std::endl;
    amb->disconnect();
    return 0;
  } catch (const rti1516e::Exception& e) {
    std::cerr << "PUB: ERROR " << ws2s(e.what()) << std::endl;
    return 1;
  }
}
