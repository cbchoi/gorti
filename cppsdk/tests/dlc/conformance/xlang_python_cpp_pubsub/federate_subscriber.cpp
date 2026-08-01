// M31 conformance fixture: xlang_python_cpp_pubsub — C++ DLC subscriber.
//
// Spec § anchors:
//   §4.2  connect
//   §5.6  subscribeObjectClassAttributes (catalogue 11.9, active flag)
//   §6.9  discoverObjectInstance (catalogue 4.19 wstring overload)
//   §6.11 reflectAttributeValues RO (catalogue 4.20)
//   §6.15 removeObjectInstance (catalogue 4.22)
//
// Goal: verify wire-level interop — the C++ subscriber decodes byte-
// identical values to what the Python publisher (pysdk/rti1516e/standard.py)
// emitted. If pysdk and cppsdk DLC paths use the same encoding rules
// (HLAfloat64BE = big-endian IEEE 754 double per Annex B), the subscriber
// reads 10.0 / 20.0 / 30.0 verbatim. Any divergence (endianness,
// width, padding) shows up as a mismatched golden line.

#include <RTI/RTIambassador.h>
#include <RTI/RTIambassadorFactory.h>
#include <RTI/NullFederateAmbassador.h>
#include <RTI/Enums.h>
#include <RTI/encoding/HLAfloat64BE.h>

#include <cstdio>
#include <memory>
#include <string>
#include <vector>

namespace {

rti1516e::AttributeHandle gPositionAttr;

class XlangSubFed : public rti1516e::NullFederateAmbassador {
 public:
  bool removed = false;
  void discoverObjectInstance(
      rti1516e::ObjectInstanceHandle /*theObject*/,
      rti1516e::ObjectClassHandle /*theObjectClass*/,
      std::wstring const& theObjectInstanceName) override {
    std::string n(theObjectInstanceName.begin(), theObjectInstanceName.end());
    std::printf("SUB: DISCOVER name=%s\n", n.c_str());
  }
  void reflectAttributeValues(
      rti1516e::ObjectInstanceHandle /*theObject*/,
      rti1516e::AttributeHandleValueMap const& vals,
      rti1516e::VariableLengthData const& /*tag*/,
      rti1516e::OrderType /*sentOrder*/,
      rti1516e::TransportationType /*type*/,
      rti1516e::SupplementalReflectInfo /*info*/) override {
    rti1516e::HLAfloat64BE pos;
    pos.decode(vals.at(gPositionAttr));
    std::printf("SUB: REFLECT Position=%.6f\n", pos.get());
  }
  void removeObjectInstance(
      rti1516e::ObjectInstanceHandle /*theObject*/,
      rti1516e::VariableLengthData const& /*theUserSuppliedTag*/,
      rti1516e::OrderType /*sentOrder*/,
      rti1516e::SupplementalRemoveInfo /*theRemoveInfo*/) override {
    removed = true;
    std::printf("SUB: REMOVE\n");
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
  XlangSubFed fed;
  std::wstring settings = L"gortiAddress=" + std::wstring(url.begin() + 7, url.end());
  amb->connect(fed, rti1516e::HLA_IMMEDIATE, settings);
  std::printf("SUB: CONNECT\n");

  std::wstring fedName = L"xlang_python_cpp_pubsub";
  try {
    std::vector<std::wstring> modules{std::wstring(fom.begin(), fom.end())};
    amb->createFederationExecution(fedName, modules);
  } catch (rti1516e::FederationExecutionAlreadyExists const&) {}
  amb->joinFederationExecution(L"cpp-sub", fedName);
  std::printf("SUB: JOIN federate=cpp-sub\n");

  auto vClass = amb->getObjectClassHandle(L"HLAobjectRoot.Vehicle");
  gPositionAttr = amb->getAttributeHandle(vClass, L"Position");
  rti1516e::AttributeHandleSet attrs;
  attrs.insert(gPositionAttr);
  amb->subscribeObjectClassAttributes(vClass, attrs, /*active=*/true);
  std::printf("SUB: SUBSCRIBE class=Vehicle attributes=[Position] active=true\n");

  // Pump for the publisher's lifetime — suite-standard evoke-drain.
  // Break early once the REMOVE lands (publisher resigned); otherwise
  // keep draining for the full window so slow Python-side startup
  // cannot truncate the capture.
  for (int i = 0; i < 150 && !fed.removed; ++i) amb->evokeMultipleCallbacks(0.05, 0.1);

  amb->resignFederationExecution(rti1516e::CANCEL_THEN_DELETE_THEN_DIVEST);
  std::printf("SUB: RESIGN action=CANCEL_THEN_DELETE_THEN_DIVEST\n");
  return 0;
}
