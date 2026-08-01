// M31 conformance fixture: tm_tso_ordering — publisher.
//
// Spec § anchors:
//   §6.12 sendInteraction (TSO overload, mandatory tag) — §8.13-§8.15 TSO ordering
//   §8.2  enableTimeRegulation (async)
//   §8.10 timeAdvanceRequest
//
// Invocation: ./federate_publisher --url ... --fom ... --name alice
// Each publisher sends ONE Tick(seq=1, source=$name) at T=1.0 then resigns.
// The federation has THREE publishers (alice/bob/carol); all send at T=1.0,
// so the subscriber's TSO delivery sequence is governed by the spec
// canonical order (catalogue 17.1: TSO is strict — no RO-style re-sort).

#include <RTI/RTIambassador.h>
#include <RTI/RTIambassadorFactory.h>
#include <RTI/NullFederateAmbassador.h>
#include <RTI/Enums.h>
#include <RTI/time/HLAfloat64Time.h>
#include <RTI/time/HLAfloat64Interval.h>
#include <RTI/encoding/HLAinteger32BE.h>
#include <RTI/encoding/HLAASCIIstring.h>

#include <chrono>
#include <cstdio>
#include <memory>
#include <string>
#include <thread>

namespace {

class PubFed : public rti1516e::NullFederateAmbassador {
 public:
  bool regulating = false;
  bool constrained = false;
  double lastGrant = 0.0;
  void timeRegulationEnabled(rti1516e::LogicalTime const& t) override {
    regulating = true;
    lastGrant = static_cast<rti1516e::HLAfloat64Time const&>(t).getTime();
  }
  void timeConstrainedEnabled(rti1516e::LogicalTime const& t) override {
    constrained = true;
    lastGrant = static_cast<rti1516e::HLAfloat64Time const&>(t).getTime();
  }
  void timeAdvanceGrant(rti1516e::LogicalTime const& t) override {
    lastGrant = static_cast<rti1516e::HLAfloat64Time const&>(t).getTime();
  }
};

}  // namespace

int main(int argc, char** argv) {
  std::string url = "grpc://127.0.0.1:8080";
  std::string fom = "./federation.fom.xml";
  std::string name = "pub";
  for (int i = 1; i < argc; ++i) {
    std::string k = argv[i];
    if (k == "--url" && i + 1 < argc) url = argv[++i];
    else if (k == "--fom" && i + 1 < argc) fom = argv[++i];
    else if (k == "--name" && i + 1 < argc) name = argv[++i];
  }

  rti1516e::RTIambassadorFactory factory;
  rti1516e::auto_ptr<rti1516e::RTIambassador> amb(factory.createRTIambassador());
  PubFed fed;
  std::wstring settings = L"gortiAddress=" + std::wstring(url.begin() + 7, url.end());
  amb->connect(fed, rti1516e::HLA_IMMEDIATE, settings);
  std::printf("%s: CONNECT\n", name.c_str());

  std::wstring fedName = L"tm_tso_ordering";
  try {
    std::vector<std::wstring> modules{std::wstring(fom.begin(), fom.end())};
    amb->createFederationExecution(fedName, modules);
  } catch (rti1516e::FederationExecutionAlreadyExists const&) {}
  amb->joinFederationExecution(std::wstring(name.begin(), name.end()), fedName);
  std::printf("%s: JOIN federate=%s\n", name.c_str(), name.c_str());

  auto ick = amb->getInteractionClassHandle(L"HLAinteractionRoot.Tick");
  amb->publishInteractionClass(ick);
  std::printf("%s: PUBLISH interaction=Tick\n", name.c_str());

  // Regulating with lookahead=1.0 lets us send TSO at theTime=federate_time+1.
  rti1516e::HLAfloat64Interval lookahead(1.0);
  amb->enableTimeRegulation(lookahead);
  while (!fed.regulating) amb->evokeMultipleCallbacks(0.05, 0.1);
  amb->enableTimeConstrained();
  while (!fed.constrained) amb->evokeMultipleCallbacks(0.05, 0.1);

  // Launch-order dwell: the subscriber joins LAST (see fixture header); give
  // it wall-clock room to subscribe before the T=1.0 sends so delivery is a
  // routing question, not a join race. Emits no golden events.
  std::this_thread::sleep_for(std::chrono::milliseconds(2000));

  // §6.12 TSO send at T=1.0 — same logical time across all 3 publishers
  // so subscriber sees the §5.2.1 TSO tie-break ordering.
  auto seqParam = amb->getParameterHandle(ick, L"seq");
  auto srcParam = amb->getParameterHandle(ick, L"source");
  rti1516e::HLAinteger32BE seq(1);
  rti1516e::HLAASCIIstring src(name);
  rti1516e::ParameterHandleValueMap params;
  params[seqParam] = seq.encode();
  params[srcParam] = src.encode();
  rti1516e::VariableLengthData tag;
  rti1516e::HLAfloat64Time atTime(1.0);
  amb->sendInteraction(ick, params, tag, atTime);
  std::printf("%s: SEND interaction=Tick seq=1 source=%s time=1\n",
              name.c_str(), name.c_str());

  // Advance to release the message.
  {
    rti1516e::HLAfloat64Time target(1.0);
    double prior = fed.lastGrant;
    amb->timeAdvanceRequest(target);
    while (fed.lastGrant < 1.0 - 1e-9 && fed.lastGrant <= prior)
      amb->evokeMultipleCallbacks(0.05, 0.1);
  }

  amb->resignFederationExecution(rti1516e::CANCEL_THEN_DELETE_THEN_DIVEST);
  std::printf("%s: RESIGN action=CANCEL_THEN_DELETE_THEN_DIVEST\n", name.c_str());
  return 0;
}
