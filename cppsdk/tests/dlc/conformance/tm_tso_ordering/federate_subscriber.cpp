// M31 conformance fixture: tm_tso_ordering — subscriber.
//
// Spec § anchors:
//   §6.13 receiveInteraction (TSO overload — catalogue 4.21, 3 overloads)
//   §8.5  enableTimeConstrained
//   §8.13-§8.15 TSO ordering — strict within-bucket order
//
// Scenario: subscribe to Tick (active). Constrained, NER walk to t=2.
//   Expect: three RECV events at logical time 1.0 in spec-canonical
//   tie-break order (FederateHandle ascending — §8.15). Subscriber
//   federate is "sub"; it joins LAST so all three publishers have
//   already started.

#include <RTI/RTIambassador.h>
#include <RTI/RTIambassadorFactory.h>
#include <RTI/NullFederateAmbassador.h>
#include <RTI/Enums.h>
#include <RTI/time/HLAfloat64Time.h>
#include <RTI/encoding/HLAinteger32BE.h>
#include <RTI/encoding/HLAASCIIstring.h>

#include <cstdio>
#include <memory>
#include <string>

namespace {

rti1516e::ParameterHandle gSeqParam;
rti1516e::ParameterHandle gSrcParam;

class SubFed : public rti1516e::NullFederateAmbassador {
 public:
  bool constrained = false;
  double lastGrant = 0.0;

  void timeConstrainedEnabled(rti1516e::LogicalTime const& t) override {
    constrained = true;
    lastGrant = static_cast<rti1516e::HLAfloat64Time const&>(t).getTime();
    std::printf("SUB: TIME_CONSTRAINED_ENABLED time=%.6f\n", lastGrant);
  }
  void timeAdvanceGrant(rti1516e::LogicalTime const& t) override {
    lastGrant = static_cast<rti1516e::HLAfloat64Time const&>(t).getTime();
    std::printf("SUB: GRANT time=%.6f\n", lastGrant);
  }

  // §6.13 TSO overload (catalogue 4.21, the 3-overload set).
  void receiveInteraction(
      rti1516e::InteractionClassHandle /*ick*/,
      rti1516e::ParameterHandleValueMap const& params,
      rti1516e::VariableLengthData const& /*tag*/,
      rti1516e::OrderType /*sentOrder*/,
      rti1516e::TransportationType /*type*/,
      rti1516e::LogicalTime const& theTime,
      rti1516e::OrderType /*receivedOrder*/,
      rti1516e::MessageRetractionHandle /*retract*/,
      rti1516e::SupplementalReceiveInfo /*info*/) override {
    double t = static_cast<rti1516e::HLAfloat64Time const&>(theTime).getTime();
    rti1516e::HLAinteger32BE seq;
    rti1516e::HLAASCIIstring src;
    seq.decode(params.at(gSeqParam));
    src.decode(params.at(gSrcParam));
    std::printf("SUB: RECV interaction=Tick time=%.6f seq=%d source=%s order=TIMESTAMP\n",
                t, seq.get(), src.get().c_str());
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
  SubFed fed;
  std::wstring settings = L"gortiAddress=" + std::wstring(url.begin() + 7, url.end());
  amb->connect(fed, rti1516e::HLA_IMMEDIATE, settings);
  std::printf("SUB: CONNECT\n");

  std::wstring fedName = L"tm_tso_ordering";
  try {
    std::vector<std::wstring> modules{std::wstring(fom.begin(), fom.end())};
    amb->createFederationExecution(fedName, modules);
  } catch (rti1516e::FederationExecutionAlreadyExists const&) {}
  amb->joinFederationExecution(L"sub", fedName);
  std::printf("SUB: JOIN federate=sub\n");

  auto ick = amb->getInteractionClassHandle(L"HLAinteractionRoot.Tick");
  gSeqParam = amb->getParameterHandle(ick, L"seq");
  gSrcParam = amb->getParameterHandle(ick, L"source");
  amb->subscribeInteractionClass(ick, /*active=*/true);
  std::printf("SUB: SUBSCRIBE interaction=Tick active=true\n");

  amb->enableTimeConstrained();
  while (!fed.constrained) amb->evokeMultipleCallbacks(0.05, 0.1);

  // Launch-order gate (§8.16 queryGALT, same pattern as tm_ner_pair's
  // constrained federate): GALT is defined iff at least one regulating
  // federate exists. Without this the NER below races the publishers'
  // §8.2 enableTimeRegulation — a constrained federate with no regulator
  // present is granted straight to t=2, and the T=1.0 Ticks land in the
  // subscriber's past (0 RECVs; the M37 regression rerun).
  // Emits no golden events (queries are not logged).
  {
    rti1516e::HLAfloat64Time galt(0.0);
    while (!amb->queryGALT(galt)) amb->evokeMultipleCallbacks(0.05, 0.1);
  }

  // NER walk to t=2 so that t=1 messages are delivered. Per §8.8 a
  // nextMessageRequest COMPLETES at min(requested, next-TSO-message
  // time): with the publishers' T=1.0 Ticks queued, the first NMR(2.0)
  // is granted at 1.0 (after the three RECVs — §8.14), so the walk must
  // RE-ISSUE the request after every early grant until it reaches the
  // target — one grant per request, §8.8 defines no interim callbacks
  // (M38 GA; the pre-M38 single-request loop relied on gorti's retired
  // "forced grant at LBTS keeps pending" interim semantics).
  // The drain stays bounded (~6 s) purely as a hang guard.
  {
    rti1516e::HLAfloat64Time target(2.0);
    amb->nextMessageRequest(target);
    double requestedFrom = fed.lastGrant;
    int rounds = 0;
    while (fed.lastGrant < 2.0 - 1e-9 && rounds++ < 60) {
      amb->evokeMultipleCallbacks(0.05, 0.1);
      if (fed.lastGrant > requestedFrom + 1e-9 && fed.lastGrant < 2.0 - 1e-9) {
        // Early grant at a message time below the target (§8.8):
        // request completed — walk on.
        requestedFrom = fed.lastGrant;
        amb->nextMessageRequest(target);
      }
    }
  }

  amb->resignFederationExecution(rti1516e::CANCEL_THEN_DELETE_THEN_DIVEST);
  std::printf("SUB: RESIGN action=CANCEL_THEN_DELETE_THEN_DIVEST\n");
  return 0;
}
