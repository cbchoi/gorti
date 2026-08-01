// M31 conformance fixture: tm_lookahead_change — constrained observer side.
//
// Spec § anchors:
//   §8.5  enableTimeConstrained                    — async, ack via §8.6
//   §8.16 queryGALT(LogicalTime&) → bool
//
// Scenario: constrained federate joins, enables constrained, and probes
//   GALT three times: at startup, after the regulator's first advance,
//   and after modifyLookahead. The third probe must show the GALT that
//   peers see has tracked the regulator's new (smaller) lookahead — this
//   is the witness that modifyLookahead propagates to GALT (§8.19).

#include <RTI/RTIambassador.h>
#include <RTI/RTIambassadorFactory.h>
#include <RTI/NullFederateAmbassador.h>
#include <RTI/Enums.h>
#include <RTI/time/HLAfloat64Time.h>

#include <chrono>
#include <cstdio>
#include <memory>
#include <string>
#include <thread>

namespace {

class ObsFed : public rti1516e::NullFederateAmbassador {
 public:
  bool constrained = false;
  void timeConstrainedEnabled(rti1516e::LogicalTime const& t) override {
    constrained = true;
    double tt = static_cast<rti1516e::HLAfloat64Time const&>(t).getTime();
    std::printf("OBS: TIME_CONSTRAINED_ENABLED time=%.6f\n", tt);
  }
};

void probe(rti1516e::RTIambassador* amb, char const* tag) {
  rti1516e::HLAfloat64Time galt(0.0);
  bool defined = amb->queryGALT(galt);
  std::printf("OBS: GALT phase=%s defined=%d value=%.6f\n",
              tag, defined ? 1 : 0, galt.getTime());
}

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
  ObsFed fed;
  std::wstring settings = L"gortiAddress=" + std::wstring(url.begin() + 7, url.end());
  amb->connect(fed, rti1516e::HLA_IMMEDIATE, settings);
  std::printf("OBS: CONNECT\n");

  std::wstring fedName = L"tm_lookahead_change";
  try {
    std::vector<std::wstring> modules{std::wstring(fom.begin(), fom.end())};
    amb->createFederationExecution(fedName, modules);
  } catch (rti1516e::FederationExecutionAlreadyExists const&) {}
  amb->joinFederationExecution(L"observer", fedName);
  std::printf("OBS: JOIN federate=observer\n");

  amb->enableTimeConstrained();
  while (!fed.constrained) amb->evokeMultipleCallbacks(0.05, 0.1);

  // Sample GALT three times — the launcher starts this federate right after
  // the regulator prints its after-enable STATE; probes are spaced with
  // wall-clock dwells matched to the regulator's pacing sleeps
  // (federate_regulator.cpp): probe1 inside the regulator's first 1500 ms
  // hold (t=0, LA=2), probe2 inside its second hold (t=1, LA=2), probe3
  // after modify+TAR(2) (t=2, LA=0.5).
  probe(amb.get(), "after-enable");
  std::this_thread::sleep_for(std::chrono::milliseconds(1800));
  amb->evokeMultipleCallbacks(0.0, 0.1);  // §10.42 — 2-arg, no defaults (catalogue 13.12)
  probe(amb.get(), "after-first-advance");
  std::this_thread::sleep_for(std::chrono::milliseconds(1800));
  amb->evokeMultipleCallbacks(0.0, 0.1);
  probe(amb.get(), "after-modify");

  amb->resignFederationExecution(rti1516e::CANCEL_THEN_DELETE_THEN_DIVEST);
  std::printf("OBS: RESIGN action=CANCEL_THEN_DELETE_THEN_DIVEST\n");
  return 0;
}
