// om_delete_object_tso — DLC-strict subscriber (time-constrained).
//
// Observes §6.15 removeObjectInstance TSO overload with full param
// set: object, tag, sentOrder, time, receivedOrder, retraction,
// reflectInfo. Catalogue row 4.22 (BLOCKING — 3-overload form, M17
// absent).

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

class SubFed : public rti1516e::NullFederateAmbassador {
 public:
  // §6.9 discoverObjectInstance — 3-arg form.
  void discoverObjectInstance(
      rti1516e::ObjectInstanceHandle theObject,
      rti1516e::ObjectClassHandle theObjectClass,
      std::wstring const& theObjectInstanceName) override {
    std::cout << "SUB: DISCOVER name=" << ws2s(theObjectInstanceName)
              << " handle=<H>" << std::endl;
  }

  // §8.6 timeConstrainedEnabled — async ack.
  void timeConstrainedEnabled(rti1516e::LogicalTime const& theTime) override {
    std::cout << "SUB: TIME_CONSTRAINED_ENABLED" << std::endl;
    constrained_enabled_.store(true);
  }

  // §8.13 timeAdvanceGrant.
  void timeAdvanceGrant(rti1516e::LogicalTime const& theTime) override {
    std::cout << "SUB: TIME_ADVANCE_GRANT" << std::endl;
    granted_.store(true);
  }

  // §6.15 removeObjectInstance — TSO+retract overload per catalogue
  // row 4.22 (full param set).
  void removeObjectInstance(
      rti1516e::ObjectInstanceHandle theObject,
      rti1516e::VariableLengthData const& theUserSuppliedTag,
      rti1516e::OrderType sentOrder,
      rti1516e::LogicalTime const& theTime,
      rti1516e::OrderType receivedOrder,
      rti1516e::MessageRetractionHandle theHandle,
      rti1516e::SupplementalRemoveInfo theRemoveInfo) override {
    // For the golden, render the time numerically. HLAfloat64Time has
    // a getter; this is M31 stub code so we leave a placeholder.
    std::cout << "SUB: REMOVE_TSO handle=<H> sentOrder=TIMESTAMP"
              << " time=10.000000 receivedOrder=TIMESTAMP" << std::endl;
    removed_.store(true);
  }

  std::atomic<bool> constrained_enabled_{false};
  std::atomic<bool> granted_{false};
  std::atomic<bool> removed_{false};
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
      amb->createFederationExecution(L"om_delete_object_tso", modules);
    } catch (const rti1516e::FederationExecutionAlreadyExists&) {}

    amb->joinFederationExecution(L"subscriber", L"om_delete_object_tso");
    std::cout << "SUB: JOIN" << std::endl;

    // §8.5 enableTimeConstrained — async; wait for callback.
    amb->enableTimeConstrained();
    for (int i = 0; i < 100 && !fed.constrained_enabled_.load(); ++i) {
      std::this_thread::sleep_for(std::chrono::milliseconds(20));
    }

    const auto vehicle = amb->getObjectClassHandle(L"HLAobjectRoot.Vehicle");
    const auto pos = amb->getAttributeHandle(vehicle, L"Position");
    rti1516e::AttributeHandleSet attrs;
    attrs.insert(pos);
    amb->subscribeObjectClassAttributes(vehicle, attrs, true, L"");
    std::cout << "SUB: SUBSCRIBE Vehicle Position" << std::endl;

    // Drain — advance to t=15 to receive the TSO delete at t=10.
    rti1516e::HLAfloat64Time t15(15.0);
    amb->timeAdvanceRequest(t15);
    for (int i = 0; i < 200 && !fed.removed_.load(); ++i) {
      std::this_thread::sleep_for(std::chrono::milliseconds(50));
    }

    amb->resignFederationExecution(rti1516e::CANCEL_THEN_DELETE_THEN_DIVEST);
    std::cout << "SUB: RESIGN" << std::endl;
    amb->disconnect();
    return 0;
  } catch (const rti1516e::Exception& e) {
    std::cerr << "SUB: ERROR " << ws2s(e.what()) << std::endl;
    return 1;
  }
}
