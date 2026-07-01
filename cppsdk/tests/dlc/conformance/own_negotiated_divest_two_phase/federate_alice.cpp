// own_negotiated_divest_two_phase — Alice (initial owner / divester).
//
// IEEE 1516.1-2010 §7 ownership state machine (figure 7.1):
//   1. Alice creates object, owns Position.
//   2. Alice calls negotiatedAttributeOwnershipDivestiture (§7.3) with tag.
//   3. RTI fires requestAttributeOwnershipAssumption on Bob (§7.4).
//   4. Bob calls attributeOwnershipAcquisition (§7.8).
//   5. RTI fires requestDivestitureConfirmation on Alice (§7.5) — the
//      spec-correct callback name; M33-K-2 fix (M31 fixture used
//      the non-existent "attributeOwnershipDivestitureNotification").
//   6. Alice calls confirmDivestiture (§7.6); RTI fires
//      attributeOwnershipAcquisitionNotification on Bob (§7.7).
//
// Per `docs/DLC_DIVERGENCE_CATALOGUE.md §12` rows 12.1-12.4.

#include <RTI/RTIambassador.h>
#include <RTI/RTIambassadorFactory.h>
#include <RTI/NullFederateAmbassador.h>
#include <RTI/Enums.h>
#include <RTI/Handle.h>
#include <RTI/Typedefs.h>

#include <atomic>
#include <chrono>
#include <iostream>
#include <string>
#include <thread>

namespace {

std::string ws2s(const std::wstring& w) {
  return std::string(w.begin(), w.end());
}

class AliceFed : public rti1516e::NullFederateAmbassador {
 public:
  // §7.5 requestDivestitureConfirmation — RTI tells divester the
  // assumption has happened; divester must confirm. 2-arg (no tag) per
  // IEEE 1516.1-2010 FederateAmbassador.h line 414. M33-K-2 fix:
  // M31 fixture wrote "attributeOwnershipDivestitureNotification" —
  // that callback does NOT exist in the spec.
  void requestDivestitureConfirmation(
      rti1516e::ObjectInstanceHandle theObject,
      rti1516e::AttributeHandleSet const& releasedAttributes) override {
    std::cout << "ALICE: REQUEST_DIVESTITURE_CONFIRMATION attrs="
              << releasedAttributes.size() << std::endl;
    divestiture_notified_.store(true);
  }

  std::atomic<bool> divestiture_notified_{false};
};

}  // namespace

int main() {
  try {
    rti1516e::RTIambassadorFactory factory;
    rti1516e::auto_ptr<rti1516e::RTIambassador> amb =
        factory.createRTIambassador();

    AliceFed fed;
    amb->connect(fed, rti1516e::HLA_IMMEDIATE,
                 L"crcAddress=127.0.0.1:8989");
    std::cout << "ALICE: CONNECT" << std::endl;

    std::vector<std::wstring> modules{L"./federation.fom.xml"};
    try {
      amb->createFederationExecution(L"own_negotiated_divest", modules);
    } catch (const rti1516e::FederationExecutionAlreadyExists&) {}
    amb->joinFederationExecution(L"alice", L"own_negotiated_divest");
    std::cout << "ALICE: JOIN" << std::endl;

    const auto vehicle = amb->getObjectClassHandle(L"HLAobjectRoot.Vehicle");
    const auto pos = amb->getAttributeHandle(vehicle, L"Position");
    rti1516e::AttributeHandleSet attrs;
    attrs.insert(pos);
    amb->publishObjectClassAttributes(vehicle, attrs);

    const auto obj = amb->registerObjectInstance(vehicle, L"car-divest");
    std::cout << "ALICE: REGISTER name=car-divest handle=<H>" << std::endl;

    // Wait for Bob to join + subscribe.
    std::this_thread::sleep_for(std::chrono::seconds(1));

    rti1516e::VariableLengthData tag;  // empty tag is fine here.
    amb->negotiatedAttributeOwnershipDivestiture(obj, attrs, tag);
    std::cout << "ALICE: NEGOTIATED_DIVEST attrs=[Position]" << std::endl;

    // Wait for §7.5 divestiture notification.
    for (int i = 0; i < 100 && !fed.divestiture_notified_.load(); ++i) {
      std::this_thread::sleep_for(std::chrono::milliseconds(50));
    }

    amb->confirmDivestiture(obj, attrs, tag);
    std::cout << "ALICE: CONFIRM_DIVESTITURE" << std::endl;

    std::this_thread::sleep_for(std::chrono::milliseconds(500));

    amb->resignFederationExecution(rti1516e::CANCEL_THEN_DELETE_THEN_DIVEST);
    std::cout << "ALICE: RESIGN" << std::endl;
    amb->disconnect();
    return 0;
  } catch (const rti1516e::Exception& e) {
    std::cerr << "ALICE: ERROR " << ws2s(e.what()) << std::endl;
    return 1;
  }
}
