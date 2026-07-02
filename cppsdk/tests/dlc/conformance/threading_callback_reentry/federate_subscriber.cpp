// M31 conformance fixture: threading_callback_reentry — subscriber.
//
// Spec § anchors:
//   §10   The RTI must reject ambassador calls re-entered from a
//         callback context.
//   §17.2 of docs/DLC_DIVERGENCE_CATALOGUE.md — `CallNotAllowedFromWithinCallback`
//         is the spec-mandated exception class for this case.
//   §6.11 reflectAttributeValues (catalogue 4.20, RO overload here)
//
// Scenario: subscriber receives a Position update; INSIDE the callback
// it attempts amb->updateAttributeValues on a phantom handle. The RTI
// must throw CallNotAllowedFromWithinCallback (catalogue 17.2). The
// subscriber catches the exception, logs the witness, and continues.
//
// IMPORTANT — this is the only fixture in the M31 suite that exercises
// the re-entry policy. Without it, the catalogue 17.2 row has no
// runtime witness.

#include <RTI/RTIambassador.h>
#include <RTI/RTIambassadorFactory.h>
#include <RTI/NullFederateAmbassador.h>
#include <RTI/Enums.h>
#include <RTI/encoding/HLAfloat64BE.h>
#include <RTI/Exception.h>  // Pitch ships all ~120 exceptions in one header per Annex C

#include <cstdio>
#include <memory>
#include <string>
#include <vector>

namespace {

class ReentryFed : public rti1516e::NullFederateAmbassador {
 public:
  rti1516e::RTIambassador* amb = nullptr;
  bool saw_callback = false;
  bool caught_reentry_throw = false;

  void reflectAttributeValues(
      rti1516e::ObjectInstanceHandle theObject,
      rti1516e::AttributeHandleValueMap const& theAttributeValues,
      rti1516e::VariableLengthData const& theUserSuppliedTag,
      rti1516e::OrderType /*sentOrder*/,
      rti1516e::TransportationType /*type*/,
      rti1516e::SupplementalReflectInfo /*info*/) override {
    saw_callback = true;
    std::printf("SUB: REFLECT Position=...\n");
    std::printf("SUB: ATTEMPT_REENTRY service=updateAttributeValues\n");
    try {
      // Re-enter the ambassador from within the callback.
      // Spec §10 forbids this — must throw CallNotAllowedFromWithinCallback.
      amb->updateAttributeValues(theObject, theAttributeValues, theUserSuppliedTag);
    } catch (rti1516e::CallNotAllowedFromWithinCallback const&) {
      caught_reentry_throw = true;
      std::printf("SUB: CAUGHT exception=CallNotAllowedFromWithinCallback\n");
    } catch (rti1516e::Exception const& e) {
      // Wrong exception type — still log so the diff catches the bug.
      std::wstring w = e.what();
      std::string s(w.begin(), w.end());
      std::printf("SUB: CAUGHT exception=UNEXPECTED(%s)\n", s.c_str());
    }
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
  ReentryFed fed;
  fed.amb = amb.get();

  std::wstring settings = L"crcAddress=" + std::wstring(url.begin() + 7, url.end());
  amb->connect(fed, rti1516e::HLA_IMMEDIATE, settings);
  std::printf("SUB: CONNECT\n");

  std::wstring fedName = L"threading_callback_reentry";
  try {
    std::vector<std::wstring> modules{std::wstring(fom.begin(), fom.end())};
    amb->createFederationExecution(fedName, modules);
  } catch (rti1516e::FederationExecutionAlreadyExists const&) {}
  amb->joinFederationExecution(L"sub", fedName);
  std::printf("SUB: JOIN federate=sub\n");

  auto vClass = amb->getObjectClassHandle(L"HLAobjectRoot.Vehicle");
  auto pAttr  = amb->getAttributeHandle(vClass, L"Position");
  rti1516e::AttributeHandleSet attrs;
  attrs.insert(pAttr);
  amb->subscribeObjectClassAttributes(vClass, attrs, /*active=*/true);
  std::printf("SUB: SUBSCRIBE class=Vehicle attributes=[Position] active=true\n");

  // Pump until we see the reflect + the exception.
  for (int i = 0; i < 100 && !fed.caught_reentry_throw; ++i) {
    amb->evokeMultipleCallbacks(0.05, 0.1);
  }
  if (!fed.saw_callback) {
    std::printf("SUB: ERROR no_reflect_received\n");
  } else if (!fed.caught_reentry_throw) {
    std::printf("SUB: ERROR no_exception_raised\n");
  }

  amb->resignFederationExecution(rti1516e::CANCEL_THEN_DELETE_THEN_DIVEST);
  std::printf("SUB: RESIGN action=CANCEL_THEN_DELETE_THEN_DIVEST\n");
  return 0;
}
