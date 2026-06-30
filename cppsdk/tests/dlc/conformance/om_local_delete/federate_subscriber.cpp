// om_local_delete — DLC-strict subscriber.
//
// Subscribes to Vehicle, discovers car-local, then calls
// §6.16 localDeleteObjectInstance (catalogue row 11.6 — gorti M17
// absent). The subscriber's view is removed; the publisher is NOT
// notified (no resign-style propagation).
//
// CRITICAL: the spec is clear that localDelete does NOT fire
// removeObjectInstance — that callback only fires when a remote
// federate has called the full §6.14 deleteObjectInstance. See
// §6.16 sentence "This service does not result in...".

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

class SubFed : public rti1516e::NullFederateAmbassador {
 public:
  void discoverObjectInstance(
      rti1516e::ObjectInstanceHandle theObject,
      rti1516e::ObjectClassHandle theObjectClass,
      std::wstring const& theObjectInstanceName) override {
    std::cout << "SUB: DISCOVER name=" << ws2s(theObjectInstanceName)
              << " handle=<H>" << std::endl;
    object_ = theObject;
    has_object_.store(true);
  }

  // RO reflect overload.
  void reflectAttributeValues(
      rti1516e::ObjectInstanceHandle theObject,
      rti1516e::AttributeHandleValueMap const& theAttributeValues,
      rti1516e::VariableLengthData const& theUserSuppliedTag,
      rti1516e::OrderType sentOrder,
      rti1516e::TransportationType theType,
      rti1516e::SupplementalReflectInfo theReflectInfo) override {
    std::cout << "SUB: REFLECT name=car-local Position=99.000000" << std::endl;
    reflected_.store(true);
  }

  // We OVERRIDE removeObjectInstance to detect any spurious firing —
  // per §6.16 it MUST NOT fire after localDelete.
  void removeObjectInstance(
      rti1516e::ObjectInstanceHandle theObject,
      rti1516e::VariableLengthData const& theUserSuppliedTag,
      rti1516e::OrderType sentOrder,
      rti1516e::SupplementalRemoveInfo theRemoveInfo) override {
    std::cout << "SUB: REMOVE_SPURIOUS handle=<H>"
              << " // FAILS golden — §6.16 forbids" << std::endl;
  }

  rti1516e::ObjectInstanceHandle object_;
  std::atomic<bool> has_object_{false};
  std::atomic<bool> reflected_{false};
};

}  // namespace

int main() {
  try {
    rti1516e::RTIambassadorFactory factory;
    rti1516e::auto_ptr<rti1516e::RTIambassador> amb =
        factory.createRTIambassador();

    SubFed fed;
    amb->connect(fed, rti1516e::HLA_IMMEDIATE,
                 L"crcAddress=127.0.0.1:8989");
    std::cout << "SUB: CONNECT" << std::endl;

    std::vector<std::wstring> modules{L"./federation.fom.xml"};
    try {
      amb->createFederationExecution(L"om_local_delete", modules);
    } catch (const rti1516e::FederationExecutionAlreadyExists&) {}

    amb->joinFederationExecution(L"subscriber", L"om_local_delete");
    std::cout << "SUB: JOIN" << std::endl;

    const auto vehicle = amb->getObjectClassHandle(L"HLAobjectRoot.Vehicle");
    const auto pos = amb->getAttributeHandle(vehicle, L"Position");
    rti1516e::AttributeHandleSet attrs;
    attrs.insert(pos);
    amb->subscribeObjectClassAttributes(vehicle, attrs, true, L"");
    std::cout << "SUB: SUBSCRIBE Vehicle Position" << std::endl;

    for (int i = 0; i < 200 && !fed.reflected_.load(); ++i) {
      std::this_thread::sleep_for(std::chrono::milliseconds(50));
    }

    // §6.16 — local delete.
    amb->localDeleteObjectInstance(fed.object_);
    std::cout << "SUB: LOCAL_DELETE handle=<H>" << std::endl;

    // Stay a bit to confirm no removeObjectInstance fires.
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
