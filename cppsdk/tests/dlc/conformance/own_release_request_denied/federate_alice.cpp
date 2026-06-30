// own_release_request_denied — Alice (owner who refuses to release).
//
// Sequence:
//   1. Alice owns car-1.Position.
//   2. Bob calls attributeOwnershipAcquisition (§7.8).
//   3. RTI fires requestAttributeOwnershipRelease on Alice (§7.11,
//      catalogue row 4.30: gorti M17 absent).
//   4. Alice REFUSES via attributeOwnershipReleaseDenied (§7.12,
//      catalogue row 12.4: gorti M17 absent).
//   5. Bob continues to NOT receive §7.7 acquisitionNotification —
//      ownership stays with Alice.

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
  // §7.11 requestAttributeOwnershipRelease — RTI tells Alice that Bob
  // wants the attribute. Alice may release or deny.
  void requestAttributeOwnershipRelease(
      rti1516e::ObjectInstanceHandle theObject,
      rti1516e::AttributeHandleSet const& candidateAttributes,
      rti1516e::VariableLengthData const& theUserSuppliedTag) override {
    std::cout << "ALICE: RELEASE_REQUEST attrs="
              << candidateAttributes.size() << std::endl;
    pending_attrs_ = candidateAttributes;
    pending_object_ = theObject;
    release_requested_.store(true);
  }

  rti1516e::ObjectInstanceHandle pending_object_;
  rti1516e::AttributeHandleSet pending_attrs_;
  std::atomic<bool> release_requested_{false};
};
}  // namespace

int main() {
  try {
    rti1516e::RTIambassadorFactory factory;
    auto amb = factory.createRTIambassador();
    AliceFed fed;

    amb->connect(fed, rti1516e::HLA_IMMEDIATE,
                 L"crcAddress=127.0.0.1:8989");
    std::cout << "ALICE: CONNECT" << std::endl;

    std::vector<std::wstring> modules{L"./federation.fom.xml"};
    try {
      amb->createFederationExecution(L"own_release_denied", modules);
    } catch (const rti1516e::FederationExecutionAlreadyExists&) {}
    amb->joinFederationExecution(L"alice", L"own_release_denied");
    std::cout << "ALICE: JOIN" << std::endl;

    const auto vehicle = amb->getObjectClassHandle(L"HLAobjectRoot.Vehicle");
    const auto pos = amb->getAttributeHandle(vehicle, L"Position");
    rti1516e::AttributeHandleSet attrs;
    attrs.insert(pos);
    amb->publishObjectClassAttributes(vehicle, attrs);

    const auto obj = amb->registerObjectInstance(vehicle, L"car-1");
    std::cout << "ALICE: REGISTER name=car-1 handle=<H>" << std::endl;

    // Wait for §7.11 from Bob's acquisition request.
    for (int i = 0; i < 200 && !fed.release_requested_.load(); ++i) {
      std::this_thread::sleep_for(std::chrono::milliseconds(50));
    }

    // §7.12 attributeOwnershipReleaseDenied — refuse.
    amb->attributeOwnershipReleaseDenied(fed.pending_object_,
                                          fed.pending_attrs_);
    std::cout << "ALICE: RELEASE_DENIED attrs=[Position]" << std::endl;

    // Stay joined a bit to confirm no further transfer happens.
    std::this_thread::sleep_for(std::chrono::seconds(1));

    amb->resignFederationExecution(rti1516e::CANCEL_THEN_DELETE_THEN_DIVEST);
    std::cout << "ALICE: RESIGN" << std::endl;
    amb->disconnect();
    return 0;
  } catch (const rti1516e::Exception& e) {
    std::cerr << "ALICE: ERROR " << ws2s(e.what()) << std::endl;
    return 1;
  }
}
