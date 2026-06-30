// om_message_retraction — DLC-strict subscriber.
//
// Time-constrained. Observes §8.22 requestRetraction callback
// (catalogue row 4.36: BLOCKING — absent in M17) FIRES BEFORE the
// original §6.13 receiveInteraction would have delivered the Honk.
// The retraction wins; the original message is suppressed.

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
  void timeConstrainedEnabled(rti1516e::LogicalTime const&) override {
    std::cout << "SUB: TIME_CONSTRAINED_ENABLED" << std::endl;
    constrained_enabled_.store(true);
  }
  void timeAdvanceGrant(rti1516e::LogicalTime const&) override {
    std::cout << "SUB: TIME_ADVANCE_GRANT" << std::endl;
    granted_.store(true);
  }

  // §8.22 requestRetraction — RTI signals that an upcoming TSO message
  // should be dropped. Catalogue row 4.36.
  void requestRetraction(
      rti1516e::MessageRetractionHandle theHandle) override {
    std::cout << "SUB: REQUEST_RETRACTION handle=<H>" << std::endl;
    retraction_received_.store(true);
  }

  // Defensive: if a Honk receiveInteraction fires, the retraction
  // failed to suppress.
  void receiveInteraction(
      rti1516e::InteractionClassHandle theInteraction,
      rti1516e::ParameterHandleValueMap const& theParameterValues,
      rti1516e::VariableLengthData const& theUserSuppliedTag,
      rti1516e::OrderType sentOrder,
      rti1516e::TransportationType theType,
      rti1516e::LogicalTime const& theTime,
      rti1516e::OrderType receivedOrder,
      rti1516e::MessageRetractionHandle theHandle,
      rti1516e::SupplementalReceiveInfo theReceiveInfo) override {
    std::cout << "SUB: RECEIVE_SPURIOUS class=Honk"
              << " // FAILS golden — §8.21 expects retraction to suppress"
              << std::endl;
  }

  std::atomic<bool> constrained_enabled_{false};
  std::atomic<bool> granted_{false};
  std::atomic<bool> retraction_received_{false};
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
      amb->createFederationExecution(L"om_message_retraction", modules);
    } catch (const rti1516e::FederationExecutionAlreadyExists&) {}

    amb->joinFederationExecution(L"subscriber", L"om_message_retraction");
    std::cout << "SUB: JOIN" << std::endl;

    amb->enableTimeConstrained();
    for (int i = 0; i < 100 && !fed.constrained_enabled_.load(); ++i) {
      std::this_thread::sleep_for(std::chrono::milliseconds(20));
    }

    const auto honk = amb->getInteractionClassHandle(L"HLAinteractionRoot.Honk");
    amb->subscribeInteractionClass(honk, true);
    std::cout << "SUB: SUBSCRIBE Honk" << std::endl;

    // Advance to t=15 — well past the retracted TSO send at t=10.
    // Spec §8.21: the requestRetraction callback fires BEFORE the
    // would-be receiveInteraction for the same TSO message.
    rti1516e::HLAfloat64Time t15(15.0);
    amb->timeAdvanceRequest(t15);

    for (int i = 0; i < 200 && !fed.retraction_received_.load(); ++i) {
      std::this_thread::sleep_for(std::chrono::milliseconds(50));
    }

    // Hold a moment to detect any spurious RECEIVE that the retraction
    // failed to suppress.
    std::this_thread::sleep_for(std::chrono::milliseconds(500));

    amb->resignFederationExecution(rti1516e::CANCEL_THEN_DELETE_THEN_DIVEST);
    std::cout << "SUB: RESIGN" << std::endl;
    amb->disconnect();
    return 0;
  } catch (const rti1516e::Exception& e) {
    std::cerr << "SUB: ERROR " << ws2s(e.what()) << std::endl;
    return 1;
  }
}
