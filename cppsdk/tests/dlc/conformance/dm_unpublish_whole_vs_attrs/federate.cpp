// dm_unpublish_whole_vs_attrs — single federate exercises both forms.
//
// Phase 1: publish [Position, Velocity], then unpublish ATTRIBUTE
// SUBSET {Position} via unpublishObjectClassAttributes(class, set) (§5.7).
//   State: Velocity remains published; subsequent register/update on
//          Position must fail with AttributeNotPublished.
//
// Phase 2: re-publish [Position, Velocity], then unpublish WHOLE
// CLASS via unpublishObjectClass(class) (§5.3 — catalogue row 11.10:
// MAJOR — gorti M17 lacks the whole-class form).
//   State: BOTH attributes drop; subsequent register must fail with
//          ObjectClassNotPublished.

#include <RTI/RTIambassador.h>
#include <RTI/RTIambassadorFactory.h>
#include <RTI/NullFederateAmbassador.h>
#include <RTI/Enums.h>
#include <RTI/Handle.h>
#include <RTI/Typedefs.h>

#include <iostream>
#include <string>

namespace {
std::string ws2s(const std::wstring& w) {
  return std::string(w.begin(), w.end());
}
class NullFed : public rti1516e::NullFederateAmbassador {};
}  // namespace

int main() {
  try {
    rti1516e::RTIambassadorFactory factory;
    auto amb = factory.createRTIambassador();
    NullFed fed;
    amb->connect(fed, rti1516e::HLA_IMMEDIATE, L"crcAddress=127.0.0.1:8989");
    std::cout << "FED: CONNECT" << std::endl;

    std::vector<std::wstring> modules{L"./federation.fom.xml"};
    try {
      amb->createFederationExecution(L"dm_unpublish", modules);
    } catch (const rti1516e::FederationExecutionAlreadyExists&) {}
    amb->joinFederationExecution(L"unpublisher", L"dm_unpublish");
    std::cout << "FED: JOIN federate=unpublisher" << std::endl;

    const auto vehicle = amb->getObjectClassHandle(L"HLAobjectRoot.Vehicle");
    const auto pos = amb->getAttributeHandle(vehicle, L"Position");
    const auto vel = amb->getAttributeHandle(vehicle, L"Velocity");

    rti1516e::AttributeHandleSet both;
    both.insert(pos);
    both.insert(vel);

    // Phase 1: publish both.
    amb->publishObjectClassAttributes(vehicle, both);
    std::cout << "FED: PUBLISH class=Vehicle attributes=[Position,Velocity]"
              << std::endl;

    // §5.7 unpublishObjectClassAttributes (subset form).
    rti1516e::AttributeHandleSet just_pos;
    just_pos.insert(pos);
    amb->unpublishObjectClassAttributes(vehicle, just_pos);
    std::cout << "FED: UNPUBLISH_ATTRS class=Vehicle attributes=[Position]"
              << std::endl;

    // Try to register — should still succeed because Velocity remains
    // published (we still publish at least one attribute of the class).
    try {
      amb->registerObjectInstance(vehicle, L"car-phase1");
      std::cout << "FED: REGISTER class=Vehicle name=car-phase1 handle=<H>"
                << std::endl;
    } catch (const rti1516e::ObjectClassNotPublished&) {
      std::cout << "FED: REGISTER_FAILED reason=ObjectClassNotPublished"
                << std::endl;
    }

    // Phase 2: re-publish both, then unpublish whole class.
    amb->publishObjectClassAttributes(vehicle, both);
    std::cout << "FED: PUBLISH class=Vehicle attributes=[Position,Velocity]"
              << std::endl;

    // §5.3 unpublishObjectClass (whole-class form — catalogue row 11.10).
    amb->unpublishObjectClass(vehicle);
    std::cout << "FED: UNPUBLISH_CLASS class=Vehicle" << std::endl;

    try {
      amb->registerObjectInstance(vehicle, L"car-phase2");
      std::cout << "FED: REGISTER class=Vehicle name=car-phase2 handle=<H>"
                << std::endl;
    } catch (const rti1516e::ObjectClassNotPublished&) {
      std::cout << "FED: REGISTER_FAILED reason=ObjectClassNotPublished"
                << std::endl;
    }

    amb->resignFederationExecution(rti1516e::CANCEL_THEN_DELETE_THEN_DIVEST);
    std::cout << "FED: RESIGN" << std::endl;
    amb->disconnect();
    return 0;
  } catch (const rti1516e::Exception& e) {
    std::cerr << "FED: ERROR " << ws2s(e.what()) << std::endl;
    return 1;
  }
}
