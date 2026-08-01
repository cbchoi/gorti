// M31 conformance fixture: mom_federation_lifecycle — MOM observer.
//
// Spec § anchors:
//   §11   MOM via STANDARD pub/sub on HLAobjectRoot.HLAmanager.HLAfederation
//   §5.6  subscribeObjectClassAttributes(active=true)
//   §6.9  discoverObjectInstance (catalogue 4.19, wstring)
//   §6.11 reflectAttributeValues RO (catalogue 4.20)
//
// Catalogue row: 16.1 — REMOVE bespoke queryFederation/queryFederate
// /enumerateMomInstances API. MOM is delivered via the same callbacks
// that user-defined objects use.
//
// Scenario: observer joins first; subscribes HLAfederate (object class
//   under the HLAmanager.HLAfederation hierarchy) for the
//   HLAfederateHandle and HLAfederateName attributes; pumps callbacks.
//   The driver spawns "alice" and "bob" sequentially, then alice resigns.
//   Observer must see two DISCOVER + each federate name in reflects, then
//   removeObjectInstance when alice resigns.

#include <RTI/RTIambassador.h>
#include <RTI/RTIambassadorFactory.h>
#include <RTI/NullFederateAmbassador.h>
#include <RTI/Enums.h>
#include <RTI/encoding/HLAunicodeString.h>

#include <cstdio>
#include <memory>
#include <string>
#include <vector>

namespace {

rti1516e::AttributeHandle gFedHandleAttr;
rti1516e::AttributeHandle gFedNameAttr;

class ObsFed : public rti1516e::NullFederateAmbassador {
 public:
  void discoverObjectInstance(
      rti1516e::ObjectInstanceHandle /*theObject*/,
      rti1516e::ObjectClassHandle /*theObjectClass*/,
      std::wstring const& theObjectInstanceName) override {
    std::string n(theObjectInstanceName.begin(), theObjectInstanceName.end());
    std::printf("OBS: DISCOVER name=%s\n", n.c_str());
  }
  void reflectAttributeValues(
      rti1516e::ObjectInstanceHandle /*theObject*/,
      rti1516e::AttributeHandleValueMap const& vals,
      rti1516e::VariableLengthData const& /*tag*/,
      rti1516e::OrderType /*sentOrder*/,
      rti1516e::TransportationType /*type*/,
      rti1516e::SupplementalReflectInfo /*info*/) override {
    auto it = vals.find(gFedNameAttr);
    if (it != vals.end()) {
      rti1516e::HLAunicodeString name;
      name.decode(it->second);
      // M36: HLAunicodeString::get() returns std::wstring BY VALUE (IEEE
      // 1516.1-2010 DLC API shape) — bind once; calling .get() twice for
      // begin()/end() mixes iterators of two temporaries (UB; crashed
      // with std::length_error once MOM reflects actually arrived).
      std::wstring w = name.get();
      std::string s(w.begin(), w.end());
      std::printf("OBS: REFLECT HLAfederateName=%s\n", s.c_str());
    } else {
      std::printf("OBS: REFLECT attributes=[other]\n");
    }
  }
  // §6.15 removeObjectInstance — catalogue 4.22 (3 overloads). 2-arg RO form here.
  void removeObjectInstance(
      rti1516e::ObjectInstanceHandle /*theObject*/,
      rti1516e::VariableLengthData const& /*theUserSuppliedTag*/,
      rti1516e::OrderType /*sentOrder*/,
      rti1516e::SupplementalRemoveInfo /*theRemoveInfo*/) override {
    std::printf("OBS: REMOVE\n");
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
  ObsFed fed;
  std::wstring settings = L"gortiAddress=" + std::wstring(url.begin() + 7, url.end());
  amb->connect(fed, rti1516e::HLA_IMMEDIATE, settings);
  std::printf("OBS: CONNECT\n");

  std::wstring fedName = L"mom_federation_lifecycle";
  try {
    std::vector<std::wstring> modules{std::wstring(fom.begin(), fom.end())};
    amb->createFederationExecution(fedName, modules);
  } catch (rti1516e::FederationExecutionAlreadyExists const&) {}
  amb->joinFederationExecution(L"observer", fedName);
  std::printf("OBS: JOIN federate=observer\n");

  // §11 — MOM is just a regular object class hierarchy provided by the MIM.
  // The standard spec path is:
  //   HLAobjectRoot.HLAmanager.HLAfederate
  // (HLAfederation is a SINGLETON object class; HLAfederate is per-federate.)
  auto fedClass = amb->getObjectClassHandle(
      L"HLAobjectRoot.HLAmanager.HLAfederate");
  gFedHandleAttr = amb->getAttributeHandle(fedClass, L"HLAfederateHandle");
  gFedNameAttr   = amb->getAttributeHandle(fedClass, L"HLAfederateName");

  rti1516e::AttributeHandleSet attrs;
  attrs.insert(gFedHandleAttr);
  attrs.insert(gFedNameAttr);
  // §5.6 subscribeObjectClassAttributes — catalogue 11.9 (active flag mandatory).
  amb->subscribeObjectClassAttributes(fedClass, attrs, /*active=*/true);
  std::printf("OBS: SUBSCRIBE class=HLAobjectRoot.HLAmanager.HLAfederate attributes=[HLAfederateHandle,HLAfederateName] active=true\n");

  // Pump for a while — driver will spawn alice, bob, then make alice resign.
  for (int i = 0; i < 100; ++i) amb->evokeMultipleCallbacks(0.05, 0.1);

  amb->resignFederationExecution(rti1516e::CANCEL_THEN_DELETE_THEN_DIVEST);
  std::printf("OBS: RESIGN action=CANCEL_THEN_DELETE_THEN_DIVEST\n");
  return 0;
}
