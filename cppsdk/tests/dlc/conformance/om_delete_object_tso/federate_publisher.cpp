// om_delete_object_tso — DLC-strict publisher.
//
// Time-managed publisher. Registers car-tso, advances time, calls
// the 2-arg deleteObjectInstance(object, tag, time) overload (catalogue
// row 11.5: M17 has NO deleteObjectInstance at all). The TSO delete
// rides the time-management machinery from §8.

#include <RTI/RTIambassador.h>
#include <RTI/RTIambassadorFactory.h>
#include <RTI/NullFederateAmbassador.h>
#include <RTI/Enums.h>
#include <RTI/Handle.h>
#include <RTI/Typedefs.h>
#include <RTI/time/HLAfloat64Time.h>
#include <RTI/time/HLAfloat64Interval.h>

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
  // §8.3 timeRegulationEnabled — async ack of enableTimeRegulation.
  void timeRegulationEnabled(rti1516e::LogicalTime const& theTime) override {
    std::cout << "PUB: TIME_REGULATION_ENABLED" << std::endl;
    regulation_enabled_.store(true);
  }
  // §8.13 timeAdvanceGrant — async ack of timeAdvanceRequest.
  void timeAdvanceGrant(rti1516e::LogicalTime const& theTime) override {
    std::cout << "PUB: TIME_ADVANCE_GRANT" << std::endl;
    granted_.store(true);
  }

  std::atomic<bool> regulation_enabled_{false};
  std::atomic<bool> granted_{false};
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
      amb->createFederationExecution(L"om_delete_object_tso", modules);
    } catch (const rti1516e::FederationExecutionAlreadyExists&) {}

    amb->joinFederationExecution(L"publisher", L"om_delete_object_tso");
    std::cout << "PUB: JOIN" << std::endl;

    const auto vehicle = amb->getObjectClassHandle(L"HLAobjectRoot.Vehicle");
    const auto pos = amb->getAttributeHandle(vehicle, L"Position");
    rti1516e::AttributeHandleSet attrs;
    attrs.insert(pos);
    amb->publishObjectClassAttributes(vehicle, attrs);

    // Enable time regulation per §8.2 with a lookahead interval.
    rti1516e::HLAfloat64Interval lookahead(1.0);
    // All wait loops drain via §10.42 evokeMultipleCallbacks — legal
    // under HLA_IMMEDIATE on both RTIs (reference_rti delivers on background
    // threads and the evoke is a harmless yield; gorti M17 buffers
    // events and drains them on the evoking thread). No canonical lines.
    amb->enableTimeRegulation(lookahead);
    for (int i = 0; i < 100 && !fed.regulation_enabled_.load(); ++i) {
      amb->evokeMultipleCallbacks(0.05, 0.1);
    }

    const auto obj = amb->registerObjectInstance(vehicle, L"car-tso");
    std::cout << "PUB: REGISTER name=car-tso handle=<H>" << std::endl;

    // Let subscriber subscribe.
    std::this_thread::sleep_for(std::chrono::milliseconds(500));

    // §8.8 timeAdvanceRequest to t=5.0.
    rti1516e::HLAfloat64Time t5(5.0);
    amb->timeAdvanceRequest(t5);
    for (int i = 0; i < 100 && !fed.granted_.load(); ++i) {
      amb->evokeMultipleCallbacks(0.05, 0.1);
    }
    fed.granted_.store(false);

    // §6.14 TSO delete with tag and time.
    rti1516e::HLAfloat64Time t10(10.0);
    rti1516e::VariableLengthData tag;
    amb->deleteObjectInstance(obj, tag, t10);
    std::cout << "PUB: DELETE_TSO name=car-tso time=10.000000" << std::endl;

    // Advance past the delete time so the subscriber drains.
    rti1516e::HLAfloat64Time t15(15.0);
    amb->timeAdvanceRequest(t15);
    for (int i = 0; i < 100 && !fed.granted_.load(); ++i) {
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
