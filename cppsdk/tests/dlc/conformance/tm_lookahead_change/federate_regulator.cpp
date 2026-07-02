// M31 conformance fixture: tm_lookahead_change — regulator side.
//
// Spec § anchors:
//   §8.2  enableTimeRegulation(LogicalTimeInterval const&)
//   §8.13 timeAdvanceGrant
//   §8.16 queryGALT(LogicalTime& out) → bool
//   §8.19 modifyLookahead(LogicalTimeInterval const&)
//   §8.20 queryLookahead(LogicalTimeInterval& out)
//
// Catalogue rows: 9.7, 9.10, 9.11, 9.12 (bool+out queries, async regulation enable,
//                                        modifyLookahead takes LogicalTimeInterval).
//
// Scenario: regulator enables regulation (lookahead=2.0); peer is constrained-only.
//   Step 1: print initial GALT and lookahead.
//   Step 2: advance to t=1.
//   Step 3: modifyLookahead(0.5).
//   Step 4: print GALT and lookahead again.
//   Step 5: advance to t=2; resign.
// Expected: queryLookahead value drops 2.0 → 0.5; GALT visible to peer follows.

#include <RTI/RTIambassador.h>
#include <RTI/RTIambassadorFactory.h>
#include <RTI/NullFederateAmbassador.h>
#include <RTI/Enums.h>
#include <RTI/time/HLAfloat64Time.h>
#include <RTI/time/HLAfloat64Interval.h>

#include <chrono>
#include <cstdio>
#include <memory>
#include <string>
#include <thread>

namespace {

class RegFed : public rti1516e::NullFederateAmbassador {
 public:
  bool regulating = false;
  double lastGrant = 0.0;
  void timeRegulationEnabled(rti1516e::LogicalTime const& t) override {
    regulating = true;
    lastGrant = static_cast<rti1516e::HLAfloat64Time const&>(t).getTime();
    std::printf("REG: TIME_REGULATION_ENABLED time=%.6f\n", lastGrant);
  }
  void timeAdvanceGrant(rti1516e::LogicalTime const& t) override {
    lastGrant = static_cast<rti1516e::HLAfloat64Time const&>(t).getTime();
    std::printf("REG: GRANT time=%.6f\n", lastGrant);
  }
};

void report_state(rti1516e::RTIambassador* amb, char const* tag) {
  // §8.20 queryLookahead out-param.
  rti1516e::HLAfloat64Interval lh(0.0);
  amb->queryLookahead(lh);
  // §8.16 queryGALT: bool indicates whether GALT is defined; out-param holds value.
  rti1516e::HLAfloat64Time galt(0.0);
  bool defined = amb->queryGALT(galt);
  std::printf("REG: STATE phase=%s lookahead=%.6f galt_defined=%d galt=%.6f\n",
              tag, lh.getInterval(), defined ? 1 : 0, galt.getTime());
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
  RegFed fed;
  std::wstring settings = L"crcAddress=" + std::wstring(url.begin() + 7, url.end());
  amb->connect(fed, rti1516e::HLA_IMMEDIATE, settings);
  std::printf("REG: CONNECT\n");

  std::wstring fedName = L"tm_lookahead_change";
  std::vector<std::wstring> modules{std::wstring(fom.begin(), fom.end())};
  amb->createFederationExecution(fedName, modules);
  amb->joinFederationExecution(L"regulator", fedName);
  std::printf("REG: JOIN federate=regulator\n");

  rti1516e::HLAfloat64Interval startLookahead(2.0);
  amb->enableTimeRegulation(startLookahead);
  while (!fed.regulating) amb->evokeMultipleCallbacks(0.05, 0.1);

  report_state(amb.get(), "after-enable");

  // Wall-clock pacing so the observer's GALT probes land between our steps
  // (the observer spaces its probes with fixed dwells — see
  // federate_observer.cpp). This regulator is unconstrained, so without
  // pacing the whole script completes in milliseconds. Emits no events.
  std::this_thread::sleep_for(std::chrono::milliseconds(1500));

  {
    double prior = fed.lastGrant;
    rti1516e::HLAfloat64Time target(1.0);
    // gorti M17 cut-1 divergence (rti/internal/time/lookahead.go
    // checkLookahead, applied to TAR via advance.go dispatchAdvance step 3):
    // any advance request below currentTime+lookahead is rejected with
    // ErrTimeRequestInPast, so TAR(1.0) under lookahead=2.0 — legal per
    // §8.10, which only requires target >= current time — throws
    // (surfaces as RTIinternalError). Catch it so the rest of the run is
    // captured; the golden's `GRANT time=1.000000` stays and is counted
    // as the missing event.
    try {
      amb->timeAdvanceRequest(target);
      while (fed.lastGrant <= prior) amb->evokeMultipleCallbacks(0.05, 0.1);
    } catch (rti1516e::Exception const&) {}
  }

  // Observer's "after-first-advance" probe window: we are at t=1, LA=2.0.
  std::this_thread::sleep_for(std::chrono::milliseconds(1500));

  // §8.19 modifyLookahead with new interval.
  rti1516e::HLAfloat64Interval newLh(0.5);
  amb->modifyLookahead(newLh);
  std::printf("REG: MODIFY_LOOKAHEAD new=%.6f\n", newLh.getInterval());

  report_state(amb.get(), "after-modify");

  {
    double prior = fed.lastGrant;
    rti1516e::HLAfloat64Time target(2.0);
    amb->timeAdvanceRequest(target);
    while (fed.lastGrant <= prior) amb->evokeMultipleCallbacks(0.05, 0.1);
  }

  // Observer's "after-modify" probe window: we are at t=2, LA=0.5 → GALT 2.5.
  // Hold the regulating guarantee until the observer has probed + resigned.
  std::this_thread::sleep_for(std::chrono::milliseconds(3000));

  amb->resignFederationExecution(rti1516e::CANCEL_THEN_DELETE_THEN_DIVEST);
  std::printf("REG: RESIGN action=CANCEL_THEN_DELETE_THEN_DIVEST\n");
  try { amb->destroyFederationExecution(fedName); } catch (...) {}
  return 0;
}
