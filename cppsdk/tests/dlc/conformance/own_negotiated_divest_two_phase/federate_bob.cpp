// own_negotiated_divest_two_phase — Bob (acquirer).
//
// Receives §7.4 requestAttributeOwnershipAssumption, calls §7.8
// attributeOwnershipAcquisition (with tag per divergence catalogue
// row 12.2), then receives §7.7 attributeOwnershipAcquisitionNotification.

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

class BobFed : public rti1516e::NullFederateAmbassador {
 public:
  // §6.9 discoverObjectInstance — needed so Bob knows car-divest exists.
  void discoverObjectInstance(
      rti1516e::ObjectInstanceHandle theObject,
      rti1516e::ObjectClassHandle theObjectClass,
      std::wstring const& theObjectInstanceName) override {
    std::cout << "BOB: DISCOVER name=" << ws2s(theObjectInstanceName)
              << " handle=<H>" << std::endl;
    discovered_obj_ = theObject;
    has_object_.store(true);
  }

  // §7.4 requestAttributeOwnershipAssumption — RTI invites Bob to assume.
  void requestAttributeOwnershipAssumption(
      rti1516e::ObjectInstanceHandle theObject,
      rti1516e::AttributeHandleSet const& offeredAttributes,
      rti1516e::VariableLengthData const& theUserSuppliedTag) override {
    std::cout << "BOB: ASSUMPTION_REQUEST attrs=" << offeredAttributes.size()
              << std::endl;
    pending_attrs_ = offeredAttributes;
    assumption_requested_.store(true);
  }

  // §7.7 attributeOwnershipAcquisitionNotification — RTI confirms transfer.
  void attributeOwnershipAcquisitionNotification(
      rti1516e::ObjectInstanceHandle theObject,
      rti1516e::AttributeHandleSet const& securedAttributes,
      rti1516e::VariableLengthData const& theUserSuppliedTag) override {
    std::cout << "BOB: ACQUISITION_NOTIFICATION attrs="
              << securedAttributes.size() << std::endl;
    acquired_.store(true);
  }

  rti1516e::ObjectInstanceHandle discovered_obj_;
  rti1516e::AttributeHandleSet pending_attrs_;
  std::atomic<bool> has_object_{false};
  std::atomic<bool> assumption_requested_{false};
  std::atomic<bool> acquired_{false};
};

}  // namespace

int main() {
  try {
    rti1516e::RTIambassadorFactory factory;
    rti1516e::auto_ptr<rti1516e::RTIambassador> amb =
        factory.createRTIambassador();

    BobFed fed;
    amb->connect(fed, rti1516e::HLA_IMMEDIATE,
                 L"gortiAddress=127.0.0.1:8080");
    std::cout << "BOB: CONNECT" << std::endl;

    std::vector<std::wstring> modules{L"./federation.fom.xml"};
    try {
      amb->createFederationExecution(L"own_negotiated_divest", modules);
    } catch (const rti1516e::FederationExecutionAlreadyExists&) {}
    amb->joinFederationExecution(L"bob", L"own_negotiated_divest");
    std::cout << "BOB: JOIN" << std::endl;

    const auto vehicle = amb->getObjectClassHandle(L"HLAobjectRoot.Vehicle");
    const auto pos = amb->getAttributeHandle(vehicle, L"Position");
    rti1516e::AttributeHandleSet attrs;
    attrs.insert(pos);
    // Need to publish to be eligible to acquire ownership.
    amb->publishObjectClassAttributes(vehicle, attrs);
    amb->subscribeObjectClassAttributes(vehicle, attrs, true, L"");

    // Wait for §7.4 assumption request (evoke-drain: gorti M17
    // delivers callbacks on the evoking thread; §6.9 discover drains
    // through the same loop first).
    for (int i = 0; i < 200 && !fed.assumption_requested_.load(); ++i) {
      amb->evokeMultipleCallbacks(0.05, 0.1);
    }

    rti1516e::VariableLengthData tag;
    amb->attributeOwnershipAcquisition(fed.discovered_obj_,
                                       fed.pending_attrs_, tag);
    std::cout << "BOB: OWNERSHIP_ACQUISITION" << std::endl;

    // Wait for §7.7 acquisition notification (evoke-drain).
    for (int i = 0; i < 200 && !fed.acquired_.load(); ++i) {
      amb->evokeMultipleCallbacks(0.05, 0.1);
    }

    amb->resignFederationExecution(rti1516e::CANCEL_THEN_DELETE_THEN_DIVEST);
    std::cout << "BOB: RESIGN" << std::endl;
    amb->disconnect();
    return 0;
  } catch (const rti1516e::Exception& e) {
    std::cerr << "BOB: ERROR " << ws2s(e.what()) << std::endl;
    return 1;
  }
}
