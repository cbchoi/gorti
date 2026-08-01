// M31 conformance fixture: tm_ner_pair — constrained side.
//
// Spec § anchors:
//   §4.2  connect
//   §4.9  joinFederationExecution
//   §8.5  enableTimeConstrained                    — async, ack via §8.6 timeConstrainedEnabled
//   §8.8  nextMessageRequest(LogicalTime)
//   §8.13 timeAdvanceGrant(LogicalTime)
//   §6.13 receiveInteraction TSO overload         — Tick callback under TSO
//   §4.10 resignFederationExecution
//
// Scenario: enable constrained; NER to t=1, 2, 3, 4, 5; receive Tick callbacks
//           under TSO between grants.

#include <RTI/RTIambassador.h>
#include <RTI/RTIambassadorFactory.h>
#include <RTI/NullFederateAmbassador.h>
#include <RTI/Enums.h>
#include <RTI/time/HLAfloat64Time.h>
#include <RTI/time/HLAfloat64TimeFactory.h>

#include <cstdio>
#include <memory>
#include <string>

namespace {

class ConstrainedFed : public rti1516e::NullFederateAmbassador {
 public:
  bool constrained = false;
  double lastGrant = 0.0;

  void timeConstrainedEnabled(rti1516e::LogicalTime const& theTime) override {
    // §8.6 callback ack.
    constrained = true;
    lastGrant = static_cast<rti1516e::HLAfloat64Time const&>(theTime).getTime();
    std::printf("CON: TIME_CONSTRAINED_ENABLED time=%.6f\n", lastGrant);
  }
  void timeAdvanceGrant(rti1516e::LogicalTime const& theTime) override {
    // §8.13 grant.
    lastGrant = static_cast<rti1516e::HLAfloat64Time const&>(theTime).getTime();
    std::printf("CON: GRANT time=%.6f\n", lastGrant);
  }
  // §6.13 TSO overload (3-arg form per catalogue §4.21):
  //   (InteractionClassHandle, ParameterHandleValueMap, VariableLengthData tag,
  //    OrderType sent, TransportationType, LogicalTime, OrderType received,
  //    MessageRetractionHandle, SupplementalReceiveInfo)
  void receiveInteraction(
      rti1516e::InteractionClassHandle theInteraction,
      rti1516e::ParameterHandleValueMap const& /*theParameterValues*/,
      rti1516e::VariableLengthData const& /*theUserSuppliedTag*/,
      rti1516e::OrderType /*sentOrder*/,
      rti1516e::TransportationType /*theType*/,
      rti1516e::LogicalTime const& theTime,
      rti1516e::OrderType /*receivedOrder*/,
      rti1516e::MessageRetractionHandle /*theHandle*/,
      rti1516e::SupplementalReceiveInfo /*theReceiveInfo*/) override {
    double t = static_cast<rti1516e::HLAfloat64Time const&>(theTime).getTime();
    (void)theInteraction;
    std::printf("CON: RECV interaction=Tick time=%.6f order=TIMESTAMP\n", t);
  }
};

}  // namespace

int main(int argc, char** argv) {
  std::string url = "grpc://127.0.0.1:8080";
  std::string fom = "./federation.fom.xml";
  for (int i = 1; i < argc; ++i) {
    std::string k = argv[i];
    if (k == "--url" && i + 1 < argc) url = argv[++i];
    else if (k == "--fom" && i + 1 < argc) fom = argv[++i];
  }

  rti1516e::RTIambassadorFactory factory;
  rti1516e::auto_ptr<rti1516e::RTIambassador> amb(factory.createRTIambassador());
  ConstrainedFed fed;

  std::wstring settings = L"gortiAddress=" + std::wstring(url.begin() + 7, url.end());
  amb->connect(fed, rti1516e::HLA_IMMEDIATE, settings);
  std::printf("CON: CONNECT\n");

  std::wstring fedName = L"tm_ner_pair";
  // §4.5 create idempotent — regulator creates; subscribers join.
  try {
    std::vector<std::wstring> modules{std::wstring(fom.begin(), fom.end())};
    amb->createFederationExecution(fedName, modules);
  } catch (rti1516e::FederationExecutionAlreadyExists const&) {}
  amb->joinFederationExecution(L"constrained", fedName);
  std::printf("CON: JOIN federate=constrained\n");

  // §5.6 subscribeInteractionClass(active=true).
  auto ick = amb->getInteractionClassHandle(L"HLAinteractionRoot.Tick");
  amb->subscribeInteractionClass(ick, /*active=*/true);
  std::printf("CON: SUBSCRIBE interaction=Tick active=true\n");

  // §8.5 enable constrained. Async — wait for §8.6 callback (evoke-drain).
  amb->enableTimeConstrained();
  while (!fed.constrained) amb->evokeMultipleCallbacks(0.05, 0.1);

  // Launch-order gate (§8.16 queryGALT): GALT is defined iff at least one
  // regulating federate exists. Without this the NER loop below races the
  // regulator's §8.2 enableTimeRegulation — a constrained federate with no
  // regulator present is granted immediately and the TSO cycle never engages.
  // Emits no golden events (queries are not logged).
  {
    rti1516e::HLAfloat64Time galt(0.0);
    while (!amb->queryGALT(galt)) amb->evokeMultipleCallbacks(0.05, 0.1);
  }

  for (int t = 1; t <= 5; ++t) {
    rti1516e::HLAfloat64Time target(static_cast<double>(t));
    double priorGrant = fed.lastGrant;
    amb->nextMessageRequest(target);
    while (fed.lastGrant == priorGrant) amb->evokeMultipleCallbacks(0.05, 0.1);
  }

  amb->resignFederationExecution(rti1516e::CANCEL_THEN_DELETE_THEN_DIVEST);
  std::printf("CON: RESIGN action=CANCEL_THEN_DELETE_THEN_DIVEST\n");
  return 0;
}
