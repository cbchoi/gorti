// M31 conformance fixture: tm_ner_pair — regulator side.
//
// Spec § anchors:
//   §4.2  connect(CallbackModel, localSettings)
//   §4.5  createFederationExecution(name, fomModules)
//   §4.9  joinFederationExecution
//   §8.2  enableTimeRegulation(LogicalTimeInterval)        — async, ack via §8.3 timeRegulationEnabled
//   §8.8  nextMessageRequest(LogicalTime)                 — NER cycle anchor
//   §8.13 timeAdvanceGrant(LogicalTime)
//   §6.12 sendInteraction(handle, params, tag, LogicalTime) — TSO send
//   §4.10 resignFederationExecution(ResignAction)
//
// Scenario: enable time regulation with lookahead=1.0; NER to t=1, 2, 3, 4, 5;
//           at each grant publish a Tick interaction with seq=t (TSO).
// Expected golden: see expected.regulator.log.

#include <RTI/RTIambassador.h>
#include <RTI/RTIambassadorFactory.h>
#include <RTI/NullFederateAmbassador.h>
#include <RTI/Enums.h>
#include <RTI/time/HLAfloat64Time.h>
#include <RTI/time/HLAfloat64Interval.h>
#include <RTI/time/HLAfloat64TimeFactory.h>
#include <RTI/encoding/HLAinteger32BE.h>

#include <cstdio>
#include <memory>
#include <string>

namespace {

class RegulatorFed : public rti1516e::NullFederateAmbassador {
 public:
  bool regulating = false;
  double lastGrant = 0.0;

  void timeRegulationEnabled(rti1516e::LogicalTime const& theTime) override {
    // §8.3 callback ack — record federate-time after enable.
    regulating = true;
    lastGrant = static_cast<rti1516e::HLAfloat64Time const&>(theTime).getTime();
    std::printf("REG: TIME_REGULATION_ENABLED time=%.6f\n", lastGrant);
  }
  void timeAdvanceGrant(rti1516e::LogicalTime const& theTime) override {
    // §8.13 grant for nextMessageRequest.
    lastGrant = static_cast<rti1516e::HLAfloat64Time const&>(theTime).getTime();
    std::printf("REG: GRANT time=%.6f\n", lastGrant);
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
  RegulatorFed fed;

  // §4.2 — wstring localSettings; HLA_IMMEDIATE per FR-DLC-16 (unscoped enum).
  std::wstring settings = L"crcAddress=" + std::wstring(url.begin() + 7, url.end());
  amb->connect(fed, rti1516e::HLA_IMMEDIATE, settings);
  std::printf("REG: CONNECT\n");

  std::wstring fedName = L"tm_ner_pair";
  // §4.5 create idempotent — either side may create; the launcher starts the
  // constrained federate first so its §5.6 subscription exists before this
  // (regulating-only, hence freely-granted) federate advances.
  try {
    std::vector<std::wstring> modules{std::wstring(fom.begin(), fom.end())};
    amb->createFederationExecution(fedName, modules);
  } catch (rti1516e::FederationExecutionAlreadyExists const&) {}
  amb->joinFederationExecution(L"regulator", fedName);
  std::printf("REG: JOIN federate=regulator\n");

  // §5.4 publish Tick.
  auto ick = amb->getInteractionClassHandle(L"HLAinteractionRoot.Tick");
  amb->publishInteractionClass(ick);
  std::printf("REG: PUBLISH interaction=Tick\n");

  // §8.2 enable regulation with lookahead 1.0. Async — wait for §8.3 callback.
  rti1516e::HLAfloat64Interval lookahead(1.0);
  amb->enableTimeRegulation(lookahead);
  while (!fed.regulating) amb->evokeMultipleCallbacks(0.05, 0.1);

  auto seqParam = amb->getParameterHandle(ick, L"seq");
  for (int t = 1; t <= 5; ++t) {
    // §6.12 send Tick(seq=t) stamped theTime=t (TSO) BEFORE advancing:
    // at logical time t-1 with lookahead 1.0 the timestamp t is exactly
    // current+lookahead — the §8.1.2 legality boundary. (M37 ED fix: the
    // skeleton advanced FIRST and then sent stamped t from logical time
    // t, i.e. ts < current+lookahead — illegal per §8.1.2; the M37 EB-3
    // server-side validation correctly rejects it. Pitch would throw
    // InvalidLogicalTime on the same call.)
    rti1516e::HLAfloat64Time target(static_cast<double>(t));
    rti1516e::HLAinteger32BE seq(t);
    rti1516e::ParameterHandleValueMap params;
    params[seqParam] = seq.encode();
    rti1516e::VariableLengthData tag;  // empty tag
    amb->sendInteraction(ick, params, tag, target);
    std::printf("REG: SEND interaction=Tick seq=%d time=%d\n", t, t);

    // §8.8 NER to t.
    double priorGrant = fed.lastGrant;
    amb->nextMessageRequest(target);
    while (fed.lastGrant == priorGrant) amb->evokeMultipleCallbacks(0.05, 0.1);
  }

  // §4.10 resign + destroy.
  amb->resignFederationExecution(rti1516e::CANCEL_THEN_DELETE_THEN_DIVEST);
  std::printf("REG: RESIGN action=CANCEL_THEN_DELETE_THEN_DIVEST\n");
  try { amb->destroyFederationExecution(fedName); } catch (...) {}
  return 0;
}
