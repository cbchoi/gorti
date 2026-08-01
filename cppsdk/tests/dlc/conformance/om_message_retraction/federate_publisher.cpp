// om_message_retraction — DLC-strict publisher.
//
// Sends a TSO Honk via the §6.12 sendInteraction(class, params, tag,
// time) overload that RETURNS a MessageRetractionHandle. Then calls
// §8.21 retract(handle) (catalogue row 9.13: BLOCKING when handle is
// promoted from uint64 to class per row 7.2). The retraction must
// arrive at the subscriber BEFORE the original interaction would
// have been delivered.

#include <RTI/RTIambassador.h>
#include <RTI/RTIambassadorFactory.h>
#include <RTI/NullFederateAmbassador.h>
#include <RTI/Enums.h>
#include <RTI/Handle.h>
#include <RTI/Typedefs.h>
#include <RTI/encoding/BasicDataElements.h>
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
  void timeRegulationEnabled(rti1516e::LogicalTime const&) override {
    std::cout << "PUB: TIME_REGULATION_ENABLED" << std::endl;
    regulation_enabled_.store(true);
  }
  void timeAdvanceGrant(rti1516e::LogicalTime const&) override {
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
      amb->createFederationExecution(L"om_message_retraction", modules);
    } catch (const rti1516e::FederationExecutionAlreadyExists&) {}

    amb->joinFederationExecution(L"publisher", L"om_message_retraction");
    std::cout << "PUB: JOIN" << std::endl;

    rti1516e::HLAfloat64Interval lookahead(1.0);
    // Callback-wait loops drain via §10.42 evokeMultipleCallbacks —
    // legal under HLA_IMMEDIATE on both RTIs (reference_rti delivers on
    // background threads and the evoke is a harmless yield; gorti M17
    // buffers events and drains them on the evoking thread). No
    // canonical lines emitted.
    amb->enableTimeRegulation(lookahead);
    for (int i = 0; i < 100 && !fed.regulation_enabled_.load(); ++i) {
      amb->evokeMultipleCallbacks(0.05, 0.1);
    }

    const auto honk = amb->getInteractionClassHandle(L"HLAinteractionRoot.Honk");
    const auto vol = amb->getParameterHandle(honk, L"Volume");
    amb->publishInteractionClass(honk);
    std::cout << "PUB: PUBLISH interaction=Honk" << std::endl;

    // Let subscriber subscribe.
    std::this_thread::sleep_for(std::chrono::milliseconds(500));

    // §6.12 sendInteraction TSO overload returning MessageRetractionHandle
    // (catalogue row 11.4 — 2-overload form). Time = 10.
    rti1516e::HLAinteger32BE volume(7);
    rti1516e::ParameterHandleValueMap params;
    params[vol] = volume.encode();
    rti1516e::HLAfloat64Time t10(10.0);
    rti1516e::MessageRetractionHandle retraction =
        amb->sendInteraction(honk, params, rti1516e::VariableLengthData(),
                             t10);
    std::cout << "PUB: SEND_TSO class=Honk Volume=7 time=10.000000 retraction=<H>"
              << std::endl;

    // Immediately retract — subscriber hasn't drained t=10 yet.
    amb->retract(retraction);
    std::cout << "PUB: RETRACT handle=<H>" << std::endl;

    // Advance past t=10 so the subscriber can drain the retraction.
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
