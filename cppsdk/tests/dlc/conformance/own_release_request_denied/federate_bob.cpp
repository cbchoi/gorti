// own_release_request_denied — Bob (acquirer who gets refused).
//
// Bob calls attributeOwnershipAcquisition (§7.8 with tag per catalogue
// row 12.2). Alice refuses via §7.12 attributeOwnershipReleaseDenied.
// Bob therefore does NOT receive §7.7 acquisitionNotification.

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

class BobFed : public rti1516e::NullFederateAmbassador {
 public:
  void discoverObjectInstance(
      rti1516e::ObjectInstanceHandle theObject,
      rti1516e::ObjectClassHandle theObjectClass,
      std::wstring const& theObjectInstanceName) override {
    std::cout << "BOB: DISCOVER name=" << ws2s(theObjectInstanceName)
              << " handle=<H>" << std::endl;
    discovered_obj_ = theObject;
    has_object_.store(true);
  }

  void attributeOwnershipAcquisitionNotification(
      rti1516e::ObjectInstanceHandle theObject,
      rti1516e::AttributeHandleSet const& securedAttributes,
      rti1516e::VariableLengthData const& theUserSuppliedTag) override {
    // Bob does NOT expect this — Alice refused. If we see it the
    // test will fail at golden diff time.
    std::cout << "BOB: UNEXPECTED_ACQUISITION_NOTIFICATION" << std::endl;
  }

  rti1516e::ObjectInstanceHandle discovered_obj_;
  std::atomic<bool> has_object_{false};
};
}  // namespace

int main() {
  try {
    rti1516e::RTIambassadorFactory factory;
    auto amb = factory.createRTIambassador();
    BobFed fed;

    amb->connect(fed, rti1516e::HLA_IMMEDIATE,
                 L"crcAddress=127.0.0.1:8989");
    std::cout << "BOB: CONNECT" << std::endl;

    std::vector<std::wstring> modules{L"./federation.fom.xml"};
    try {
      amb->createFederationExecution(L"own_release_denied", modules);
    } catch (const rti1516e::FederationExecutionAlreadyExists&) {}
    amb->joinFederationExecution(L"bob", L"own_release_denied");
    std::cout << "BOB: JOIN" << std::endl;

    const auto vehicle = amb->getObjectClassHandle(L"HLAobjectRoot.Vehicle");
    const auto pos = amb->getAttributeHandle(vehicle, L"Position");
    rti1516e::AttributeHandleSet attrs;
    attrs.insert(pos);
    amb->publishObjectClassAttributes(vehicle, attrs);
    amb->subscribeObjectClassAttributes(vehicle, attrs, true, L"");

    for (int i = 0; i < 200 && !fed.has_object_.load(); ++i) {
      std::this_thread::sleep_for(std::chrono::milliseconds(25));
    }

    // §7.8 attributeOwnershipAcquisition — with mandatory tag arg
    // per catalogue row 12.2.
    rti1516e::VariableLengthData tag;
    amb->attributeOwnershipAcquisition(fed.discovered_obj_, attrs, tag);
    std::cout << "BOB: OWNERSHIP_ACQUISITION" << std::endl;

    // Wait for Alice to deny — no callback fires on Bob's side for
    // the denial; he just times out the wait.
    std::this_thread::sleep_for(std::chrono::seconds(2));

    amb->resignFederationExecution(rti1516e::CANCEL_THEN_DELETE_THEN_DIVEST);
    std::cout << "BOB: RESIGN" << std::endl;
    amb->disconnect();
    return 0;
  } catch (const rti1516e::Exception& e) {
    std::cerr << "BOB: ERROR " << ws2s(e.what()) << std::endl;
    return 1;
  }
}
