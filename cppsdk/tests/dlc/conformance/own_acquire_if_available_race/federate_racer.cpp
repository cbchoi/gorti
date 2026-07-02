// own_acquire_if_available_race — racer (bob or carol).
//
// Calls attributeOwnershipAcquisitionIfAvailable (§7.9, catalogue
// row 12.3 — gorti M17 absent). Receives EITHER:
//   - §7.7 attributeOwnershipAcquisitionNotification (won the race)
//   - §7.10 attributeOwnershipUnavailable (catalogue row 4.29 — gorti M17 absent)
//
// Federate name driven from argv[1]: "bob" or "carol".

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

class RacerFed : public rti1516e::NullFederateAmbassador {
 public:
  explicit RacerFed(std::string name) : name_(std::move(name)) {}

  void discoverObjectInstance(
      rti1516e::ObjectInstanceHandle theObject,
      rti1516e::ObjectClassHandle theObjectClass,
      std::wstring const& theObjectInstanceName) override {
    std::cout << name_ << ": DISCOVER name=" << ws2s(theObjectInstanceName)
              << " handle=<H>" << std::endl;
    discovered_obj_ = theObject;
    has_object_.store(true);
  }

  void attributeOwnershipAcquisitionNotification(
      rti1516e::ObjectInstanceHandle theObject,
      rti1516e::AttributeHandleSet const& securedAttributes,
      rti1516e::VariableLengthData const& theUserSuppliedTag) override {
    std::cout << name_ << ": ACQUISITION_NOTIFICATION attrs="
              << securedAttributes.size() << std::endl;
    won_.store(true);
  }

  void attributeOwnershipUnavailable(
      rti1516e::ObjectInstanceHandle theObject,
      rti1516e::AttributeHandleSet const& theAttributes) override {
    std::cout << name_ << ": OWNERSHIP_UNAVAILABLE attrs="
              << theAttributes.size() << std::endl;
    lost_.store(true);
  }

  std::string name_;
  rti1516e::ObjectInstanceHandle discovered_obj_;
  std::atomic<bool> has_object_{false};
  std::atomic<bool> won_{false};
  std::atomic<bool> lost_{false};
};
}  // namespace

int main(int argc, char** argv) {
  std::string name = (argc > 1) ? argv[1] : "bob";
  std::string upper = name;
  for (auto& c : upper) c = static_cast<char>(::toupper(c));
  try {
    rti1516e::RTIambassadorFactory factory;
    auto amb = factory.createRTIambassador();
    RacerFed fed(upper);

    amb->connect(fed, rti1516e::HLA_IMMEDIATE,
                 L"crcAddress=127.0.0.1:8989");
    std::cout << upper << ": CONNECT" << std::endl;

    std::vector<std::wstring> modules{L"./federation.fom.xml"};
    try {
      amb->createFederationExecution(L"own_acquire_race", modules);
    } catch (const rti1516e::FederationExecutionAlreadyExists&) {}
    std::wstring fed_name(name.begin(), name.end());
    amb->joinFederationExecution(fed_name, L"own_acquire_race");
    std::cout << upper << ": JOIN" << std::endl;

    const auto vehicle = amb->getObjectClassHandle(L"HLAobjectRoot.Vehicle");
    const auto pos = amb->getAttributeHandle(vehicle, L"Position");
    rti1516e::AttributeHandleSet attrs;
    attrs.insert(pos);
    // Publish to be eligible to acquire.
    amb->publishObjectClassAttributes(vehicle, attrs);
    amb->subscribeObjectClassAttributes(vehicle, attrs, true, L"");

    // Wait to discover the carrier's object (evoke-drain: gorti M17
    // delivers callbacks on the evoking thread).
    for (int i = 0; i < 200 && !fed.has_object_.load(); ++i) {
      amb->evokeMultipleCallbacks(0.05, 0.1);
    }

    // §7.9 — race: both bob and carol fire this simultaneously. Per
    // IEEE 1516.1-2010 §7.9 (RTIambassador.h line 773-787), this method
    // is (object, attributes) — NO tag arg. M33-K-3 fix: M31 fixture
    // (Agent C) passed a 3rd tag arg matching §7.8 attributeOwnership-
    // Acquisition; that signature is wrong for §7.9.
    amb->attributeOwnershipAcquisitionIfAvailable(fed.discovered_obj_,
                                                  attrs);
    std::cout << upper << ": ACQUIRE_IF_AVAILABLE attrs=[Position]"
              << std::endl;

    // Wait for either §7.7 or §7.10 (evoke-drain; the win arrives as
    // an OwnershipAcquired stream event, the loss is synthesized).
    for (int i = 0; i < 100 && !(fed.won_.load() || fed.lost_.load()); ++i) {
      amb->evokeMultipleCallbacks(0.05, 0.1);
    }

    amb->resignFederationExecution(rti1516e::CANCEL_THEN_DELETE_THEN_DIVEST);
    std::cout << upper << ": RESIGN" << std::endl;
    amb->disconnect();
    return 0;
  } catch (const rti1516e::Exception& e) {
    std::cerr << upper << ": ERROR " << ws2s(e.what()) << std::endl;
    return 1;
  }
}
