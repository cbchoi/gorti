// om_request_attribute_update_instance — DLC-strict publisher.
//
// Symmetric to om_request_attribute_update_class but the subscriber
// targets a SPECIFIC instance handle. Publisher's
// provideAttributeValueUpdate callback (§6.20) fires with only that
// object handle; publisher updates and resigns.

#include <RTI/RTIambassador.h>
#include <RTI/RTIambassadorFactory.h>
#include <RTI/NullFederateAmbassador.h>
#include <RTI/Enums.h>
#include <RTI/Handle.h>
#include <RTI/Typedefs.h>
#include <RTI/encoding/BasicDataElements.h>

#include <atomic>
#include <chrono>
#include <iostream>
#include <string>
#include <thread>

namespace {

std::string ws2s(const std::wstring& w) {
  return std::string(w.begin(), w.end());
}

class PubFed : public rti1516e::NullFederateAmbassador {
 public:
  void provideAttributeValueUpdate(
      rti1516e::ObjectInstanceHandle theObject,
      rti1516e::AttributeHandleSet const& theAttributes,
      rti1516e::VariableLengthData const& theUserSuppliedTag) override {
    std::cout << "PUB: PROVIDE_UPDATE_INSTANCE handle=<H> attrs="
              << theAttributes.size() << std::endl;
    object_ = theObject;
    provide_requested_.store(true);
  }

  rti1516e::ObjectInstanceHandle object_;
  std::atomic<bool> provide_requested_{false};
};

}  // namespace

int main() {
  try {
    rti1516e::RTIambassadorFactory factory;
    rti1516e::auto_ptr<rti1516e::RTIambassador> amb =
        factory.createRTIambassador();

    PubFed fed;
    amb->connect(fed, rti1516e::HLA_IMMEDIATE,
                 L"gortiAddress=127.0.0.1:8080");
    std::cout << "PUB: CONNECT" << std::endl;

    std::vector<std::wstring> modules{L"./federation.fom.xml"};
    try {
      amb->createFederationExecution(L"om_request_update_instance", modules);
    } catch (const rti1516e::FederationExecutionAlreadyExists&) {}

    amb->joinFederationExecution(L"publisher", L"om_request_update_instance");
    std::cout << "PUB: JOIN" << std::endl;

    const auto vehicle = amb->getObjectClassHandle(L"HLAobjectRoot.Vehicle");
    const auto pos = amb->getAttributeHandle(vehicle, L"Position");
    const auto vel = amb->getAttributeHandle(vehicle, L"Velocity");
    rti1516e::AttributeHandleSet attrs;
    attrs.insert(pos);
    attrs.insert(vel);
    amb->publishObjectClassAttributes(vehicle, attrs);
    const auto obj = amb->registerObjectInstance(vehicle, L"car-inst");
    std::cout << "PUB: REGISTER name=car-inst handle=<H>" << std::endl;

    // Drain via §10.42 evokeMultipleCallbacks — legal under HLA_IMMEDIATE
    // on both RTIs (reference_rti delivers on background threads and the evoke is
    // a harmless yield; gorti M17 buffers events and drains them on the
    // evoking thread). Emits no canonical lines, so goldens are unaffected.
    for (int i = 0; i < 300 && !fed.provide_requested_.load(); ++i) {
      amb->evokeMultipleCallbacks(0.05, 0.1);
    }

    // Respond only if the provide callback actually arrived (the UPDATE
    // is the §6.20 *response*; a conforming RTI always delivers the
    // provide, so this branch always runs under reference_rti. Under gorti M17
    // the provide never arrives — catalogue row 4.24 — and fed.object_
    // would be an invalid handle, so skip and resign cleanly.)
    if (fed.provide_requested_.load()) {
      rti1516e::HLAfloat64BE p(33.0);
      rti1516e::HLAfloat64BE v(44.0);
      rti1516e::AttributeHandleValueMap vals;
      vals[pos] = p.encode();
      vals[vel] = v.encode();
      amb->updateAttributeValues(fed.object_, vals,
                                 rti1516e::VariableLengthData());
      std::cout
          << "PUB: UPDATE name=car-inst Position=33.000000 Velocity=44.000000"
          << std::endl;
    }

    std::this_thread::sleep_for(std::chrono::seconds(1));

    amb->resignFederationExecution(rti1516e::CANCEL_THEN_DELETE_THEN_DIVEST);
    std::cout << "PUB: RESIGN" << std::endl;
    amb->disconnect();
    return 0;
  } catch (const rti1516e::Exception& e) {
    std::cerr << "PUB: ERROR " << ws2s(e.what()) << std::endl;
    return 1;
  }
}
