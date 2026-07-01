// om_helloworld_pubsub — DLC-strict subscriber federate.
//
// IEEE 1516.1-2010 §5/§6 surface exercise. Subscribes to Vehicle and
// Honk, observes one DISCOVER → REFLECT → RECEIVE sequence from the
// publisher.

#include <RTI/RTIambassador.h>
#include <RTI/RTIambassadorFactory.h>
#include <RTI/NullFederateAmbassador.h>
#include <RTI/Enums.h>
#include <RTI/Handle.h>
#include <RTI/Typedefs.h>
#include <RTI/encoding/BasicDataElements.h>

#include <atomic>
#include <chrono>
#include <cstdio>
#include <iostream>
#include <memory>
#include <string>
#include <thread>

namespace {

std::string ws2s(const std::wstring& w) {
  return std::string(w.begin(), w.end());
}

class SubscriberFed : public rti1516e::NullFederateAmbassador {
 public:
  // §6.9 discoverObjectInstance — 4-arg form (with producingFederate).
  // We use the spec's 3-arg overload here; the 4-arg is exercised by
  // mom_federation_lifecycle.
  void discoverObjectInstance(
      rti1516e::ObjectInstanceHandle theObject,
      rti1516e::ObjectClassHandle theObjectClass,
      std::wstring const& theObjectInstanceName) override {
    std::cout << "SUB: DISCOVER class=Vehicle name="
              << ws2s(theObjectInstanceName) << " handle=<H>" << std::endl;
    object_handle_ = theObject;
  }

  // §6.11 reflectAttributeValues — RO overload (no time).
  void reflectAttributeValues(
      rti1516e::ObjectInstanceHandle theObject,
      rti1516e::AttributeHandleValueMap const& theAttributeValues,
      rti1516e::VariableLengthData const& theUserSuppliedTag,
      rti1516e::OrderType sentOrder,
      rti1516e::TransportationType theType,
      rti1516e::SupplementalReflectInfo theReflectInfo) override {
    double position = 0.0, velocity = 0.0;
    if (theAttributeValues.find(position_attr_) != theAttributeValues.end()) {
      rti1516e::HLAfloat64BE p;
      p.decode(theAttributeValues.at(position_attr_));
      position = p.get();
    }
    if (theAttributeValues.find(velocity_attr_) != theAttributeValues.end()) {
      rti1516e::HLAfloat64BE v;
      v.decode(theAttributeValues.at(velocity_attr_));
      velocity = v.get();
    }
    std::printf("SUB: REFLECT name=car-1 Position=%.6f Velocity=%.6f\n",
                position, velocity);
    std::fflush(stdout);
    reflected_.store(true);
  }

  // §6.13 receiveInteraction — RO overload (no time).
  void receiveInteraction(
      rti1516e::InteractionClassHandle theInteraction,
      rti1516e::ParameterHandleValueMap const& theParameterValues,
      rti1516e::VariableLengthData const& theUserSuppliedTag,
      rti1516e::OrderType sentOrder,
      rti1516e::TransportationType theType,
      rti1516e::SupplementalReceiveInfo theReceiveInfo) override {
    int volume = 0;
    if (theParameterValues.find(volume_param_) != theParameterValues.end()) {
      rti1516e::HLAinteger32BE v;
      v.decode(theParameterValues.at(volume_param_));
      volume = v.get();
    }
    std::cout << "SUB: RECEIVE class=Honk Volume=" << volume << std::endl;
    received_interaction_.store(true);
  }

  rti1516e::AttributeHandle position_attr_;
  rti1516e::AttributeHandle velocity_attr_;
  rti1516e::ParameterHandle volume_param_;
  rti1516e::ObjectInstanceHandle object_handle_;
  std::atomic<bool> reflected_{false};
  std::atomic<bool> received_interaction_{false};
};

}  // namespace

int main() {
  try {
    rti1516e::RTIambassadorFactory factory;
    rti1516e::auto_ptr<rti1516e::RTIambassador> amb =
        factory.createRTIambassador();

    SubscriberFed fed;
    amb->connect(fed, rti1516e::HLA_IMMEDIATE,
                 L"crcAddress=127.0.0.1:8989");
    std::cout << "SUB: CONNECT" << std::endl;

    std::vector<std::wstring> fom_modules{L"./federation.fom.xml"};
    try {
      amb->createFederationExecution(L"om_helloworld_pubsub", fom_modules);
    } catch (const rti1516e::FederationExecutionAlreadyExists&) {
      // Race-tolerant.
    }

    amb->joinFederationExecution(L"subscriber", L"om_helloworld_pubsub");
    std::cout << "SUB: JOIN federate=subscriber" << std::endl;

    const auto vehicle = amb->getObjectClassHandle(L"HLAobjectRoot.Vehicle");
    fed.position_attr_ = amb->getAttributeHandle(vehicle, L"Position");
    fed.velocity_attr_ = amb->getAttributeHandle(vehicle, L"Velocity");
    const auto honk = amb->getInteractionClassHandle(L"HLAinteractionRoot.Honk");
    fed.volume_param_ = amb->getParameterHandle(honk, L"Volume");

    rti1516e::AttributeHandleSet attrs;
    attrs.insert(fed.position_attr_);
    attrs.insert(fed.velocity_attr_);
    // §5.6 subscribeObjectClassAttributes — active=true (default).
    amb->subscribeObjectClassAttributes(vehicle, attrs, true, L"");
    amb->subscribeInteractionClass(honk, true);
    std::cout << "SUB: SUBSCRIBE Vehicle Position Velocity Honk" << std::endl;

    // Drain via §10.42 evokeMultipleCallbacks. Legal under HLA_IMMEDIATE
    // on both RTIs (Pitch delivers on background threads and the evoke is
    // a harmless yield; gorti M17 buffers events and drains them on the
    // evoking thread). Emits no canonical lines, so goldens are unaffected.
    for (int i = 0; i < 50; ++i) {
      if (fed.reflected_.load() && fed.received_interaction_.load()) break;
      amb->evokeMultipleCallbacks(0.05, 0.1);
    }

    amb->resignFederationExecution(rti1516e::CANCEL_THEN_DELETE_THEN_DIVEST);
    std::cout << "SUB: RESIGN action=CANCEL_THEN_DELETE_THEN_DIVEST" << std::endl;
    amb->disconnect();
    return 0;
  } catch (const rti1516e::Exception& e) {
    std::cerr << "SUB: ERROR " << ws2s(e.what()) << std::endl;
    return 1;
  }
}
