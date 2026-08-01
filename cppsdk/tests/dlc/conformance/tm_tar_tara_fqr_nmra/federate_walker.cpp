// M31 conformance fixture: tm_tar_tara_fqr_nmra — single federate that
// walks each of the 4 spec-mandated advance primitives.
//
// Spec § anchors:
//   §8.8  nextMessageRequest(LogicalTime)            — NMR
//   §8.9  nextMessageRequestAvailable(LogicalTime)   — NMRA (available-mode)
//   §8.10 timeAdvanceRequest(LogicalTime)            — TAR
//   §8.11 timeAdvanceRequestAvailable(LogicalTime)   — TARA
//   §8.12 flushQueueRequest(LogicalTime)             — FQR
//   §8.13 timeAdvanceGrant(LogicalTime)              — grant callback
//
// Catalogue rows: 9.6 (LogicalTime const& param across all 5 primitives).
//
// Scenario: single regulator+constrained federate; for each primitive
//           in order [TAR, TARA, FQR, NMR, NMRA], request advance to
//           t = base+1 and record the grant. Identical lookahead (1.0)
//           between requests so every grant has predictable timestamp.

#include <RTI/RTIambassador.h>
#include <RTI/RTIambassadorFactory.h>
#include <RTI/NullFederateAmbassador.h>
#include <RTI/Enums.h>
#include <RTI/time/HLAfloat64Time.h>
#include <RTI/time/HLAfloat64Interval.h>

#include <cstdio>
#include <memory>
#include <string>

namespace {

class WalkerFed : public rti1516e::NullFederateAmbassador {
 public:
  bool regulating = false;
  bool constrained = false;
  double lastGrant = 0.0;

  void timeRegulationEnabled(rti1516e::LogicalTime const& t) override {
    regulating = true;
    lastGrant = static_cast<rti1516e::HLAfloat64Time const&>(t).getTime();
    std::printf("WLK: TIME_REGULATION_ENABLED time=%.6f\n", lastGrant);
  }
  void timeConstrainedEnabled(rti1516e::LogicalTime const& t) override {
    constrained = true;
    lastGrant = static_cast<rti1516e::HLAfloat64Time const&>(t).getTime();
    std::printf("WLK: TIME_CONSTRAINED_ENABLED time=%.6f\n", lastGrant);
  }
  void timeAdvanceGrant(rti1516e::LogicalTime const& t) override {
    lastGrant = static_cast<rti1516e::HLAfloat64Time const&>(t).getTime();
    std::printf("WLK: GRANT time=%.6f\n", lastGrant);
  }
};

void wait_grant(rti1516e::RTIambassador* amb, WalkerFed& fed, double prior) {
  while (fed.lastGrant <= prior) amb->evokeMultipleCallbacks(0.05, 0.1);
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
  WalkerFed fed;

  std::wstring settings = L"gortiAddress=" + std::wstring(url.begin() + 7, url.end());
  amb->connect(fed, rti1516e::HLA_IMMEDIATE, settings);
  std::printf("WLK: CONNECT\n");

  std::wstring fedName = L"tm_tar_tara_fqr_nmra";
  std::vector<std::wstring> modules{std::wstring(fom.begin(), fom.end())};
  amb->createFederationExecution(fedName, modules);
  amb->joinFederationExecution(L"walker", fedName);
  std::printf("WLK: JOIN federate=walker\n");

  // §8.2 regulation + §8.5 constrained, both async.
  rti1516e::HLAfloat64Interval lookahead(1.0);
  amb->enableTimeRegulation(lookahead);
  while (!fed.regulating) amb->evokeMultipleCallbacks(0.05, 0.1);
  amb->enableTimeConstrained();
  while (!fed.constrained) amb->evokeMultipleCallbacks(0.05, 0.1);

  // TAR §8.10
  {
    double prior = fed.lastGrant;
    rti1516e::HLAfloat64Time target(prior + 1.0);
    amb->timeAdvanceRequest(target);
    std::printf("WLK: REQUEST primitive=TAR target=%.6f\n", target.getTime());
    wait_grant(amb.get(), fed, prior);
  }
  // TARA §8.11
  {
    double prior = fed.lastGrant;
    rti1516e::HLAfloat64Time target(prior + 1.0);
    amb->timeAdvanceRequestAvailable(target);
    std::printf("WLK: REQUEST primitive=TARA target=%.6f\n", target.getTime());
    wait_grant(amb.get(), fed, prior);
  }
  // FQR §8.12
  {
    double prior = fed.lastGrant;
    rti1516e::HLAfloat64Time target(prior + 1.0);
    amb->flushQueueRequest(target);
    std::printf("WLK: REQUEST primitive=FQR target=%.6f\n", target.getTime());
    wait_grant(amb.get(), fed, prior);
  }
  // NMR §8.8
  {
    double prior = fed.lastGrant;
    rti1516e::HLAfloat64Time target(prior + 1.0);
    amb->nextMessageRequest(target);
    std::printf("WLK: REQUEST primitive=NMR target=%.6f\n", target.getTime());
    wait_grant(amb.get(), fed, prior);
  }
  // NMRA §8.9
  {
    double prior = fed.lastGrant;
    rti1516e::HLAfloat64Time target(prior + 1.0);
    amb->nextMessageRequestAvailable(target);
    std::printf("WLK: REQUEST primitive=NMRA target=%.6f\n", target.getTime());
    wait_grant(amb.get(), fed, prior);
  }

  amb->resignFederationExecution(rti1516e::CANCEL_THEN_DELETE_THEN_DIVEST);
  std::printf("WLK: RESIGN action=CANCEL_THEN_DELETE_THEN_DIVEST\n");
  try { amb->destroyFederationExecution(fedName); } catch (...) {}
  return 0;
}
