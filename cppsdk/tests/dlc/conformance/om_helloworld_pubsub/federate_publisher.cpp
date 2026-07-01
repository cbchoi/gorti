// om_helloworld_pubsub — DLC-strict publisher federate.
//
// IEEE 1516.1-2010 §6 Object Management surface exercise. Publishes
// Vehicle.Position + Vehicle.Velocity and emits one Honk interaction.
//
// Per docs/DLC_COMPLIANCE_PROGRAM.md §5.5 migration recipe:
//   - <RTI/*.h> spec header paths (NOT <rti1516e/*.h>)
//   - RTIambassadorFactory + auto_ptr<RTIambassador>
//   - NullFederateAmbassador subclass for callbacks
//   - std::wstring for every string the RTI accepts
//   - resignFederationExecution(ResignAction) mandatory arg
//
// M31 status: this TU PARSES against Agent E's RTI/*.h forward-decl
// stubs but FAILS TO LINK ("undefined reference to rti1516e::*")
// because no impl symbols exist yet. That's the M31 expected RED.

#include <RTI/RTIambassador.h>
#include <RTI/RTIambassadorFactory.h>
#include <RTI/NullFederateAmbassador.h>
#include <RTI/Enums.h>
#include <RTI/Handle.h>
#include <RTI/Typedefs.h>
#include <RTI/encoding/BasicDataElements.h>

#include <chrono>
#include <iostream>
#include <memory>
#include <string>
#include <thread>

namespace {

// Minimal NullFederateAmbassador subclass — the publisher doesn't
// observe many callbacks, but per §10 every federate must construct
// one to pass to connect().
class PublisherFed : public rti1516e::NullFederateAmbassador {};

std::string ws2s(const std::wstring& w) {
  return std::string(w.begin(), w.end());
}

}  // namespace

int main() {
  try {
    rti1516e::RTIambassadorFactory factory;
    rti1516e::auto_ptr<rti1516e::RTIambassador> amb =
        factory.createRTIambassador();

    PublisherFed fed;
    amb->connect(fed, rti1516e::HLA_IMMEDIATE,
                 L"crcAddress=127.0.0.1:8989");
    std::cout << "PUB: CONNECT" << std::endl;

    std::vector<std::wstring> fom_modules{L"./federation.fom.xml"};
    try {
      amb->createFederationExecution(L"om_helloworld_pubsub", fom_modules);
    } catch (const rti1516e::FederationExecutionAlreadyExists&) {
      // Race-tolerant: another federate (or a prior run holding the
      // manager's federation registry) may have created it. Per Pitch
      // pRTI's golden trace, `PUB: CREATE` is unconditional — the spec
      // §4.5 postcondition is "the named federation exists", which is
      // true in both the created-just-now and already-existed branches.
    }
    std::cout << "PUB: CREATE federation=om_helloworld_pubsub" << std::endl;

    amb->joinFederationExecution(L"publisher", L"om_helloworld_pubsub");
    std::cout << "PUB: JOIN federate=publisher" << std::endl;

    const auto vehicle = amb->getObjectClassHandle(L"HLAobjectRoot.Vehicle");
    const auto pos = amb->getAttributeHandle(vehicle, L"Position");
    const auto vel = amb->getAttributeHandle(vehicle, L"Velocity");
    const auto honk = amb->getInteractionClassHandle(L"HLAinteractionRoot.Honk");
    const auto vol = amb->getParameterHandle(honk, L"Volume");

    rti1516e::AttributeHandleSet attrs;
    attrs.insert(pos);
    attrs.insert(vel);
    amb->publishObjectClassAttributes(vehicle, attrs);
    amb->publishInteractionClass(honk);
    std::cout << "PUB: PUBLISH class=Vehicle attributes=[Position,Velocity]"
              << std::endl;
    std::cout << "PUB: PUBLISH interaction=Honk" << std::endl;

    // Let the subscriber's subscribeObjectClassAttributes land first.
    std::this_thread::sleep_for(std::chrono::milliseconds(500));

    const auto obj = amb->registerObjectInstance(vehicle, L"car-1");
    std::cout << "PUB: REGISTER class=Vehicle name=car-1 handle=<H>"
              << std::endl;

    {
      rti1516e::AttributeHandleValueMap values;
      rti1516e::HLAfloat64BE p(42.0);
      rti1516e::HLAfloat64BE v(7.0);
      values[pos] = p.encode();
      values[vel] = v.encode();
      amb->updateAttributeValues(obj, values, rti1516e::VariableLengthData());
      std::cout << "PUB: UPDATE name=car-1 Position=42.000000 Velocity=7.000000"
                << std::endl;
    }
    {
      rti1516e::ParameterHandleValueMap params;
      rti1516e::HLAinteger32BE volume(5);
      params[vol] = volume.encode();
      amb->sendInteraction(honk, params, rti1516e::VariableLengthData());
      std::cout << "PUB: SEND class=Honk Volume=5" << std::endl;
    }

    // Hold for subscriber drain.
    std::this_thread::sleep_for(std::chrono::seconds(2));

    amb->resignFederationExecution(rti1516e::CANCEL_THEN_DELETE_THEN_DIVEST);
    std::cout << "PUB: RESIGN action=CANCEL_THEN_DELETE_THEN_DIVEST"
              << std::endl;
    amb->disconnect();
    return 0;
  } catch (const rti1516e::Exception& e) {
    std::cerr << "PUB: ERROR " << ws2s(e.what()) << std::endl;
    return 1;
  }
}
