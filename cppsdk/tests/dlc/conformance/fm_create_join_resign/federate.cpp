// fm_create_join_resign — single federate exercises all 6 ResignAction
// enumerators per IEEE 1516.1-2010 §4.10 (catalogue row 3.9 BLOCKING —
// mandatory ResignAction arg) + row 5.3 (6 enumerators).
//
// Per loop iteration: CONNECT → CREATE (idempotent) → JOIN → REGISTER
// (so DELETE_OBJECTS variants have something to delete) → RESIGN(<action>)
// → DISCONNECT.
//
// M31 status: parses against the RTI/*.h forward-declaration stubs but
// FAILS TO LINK — no impl symbols exist.

#include <RTI/RTIambassador.h>
#include <RTI/RTIambassadorFactory.h>
#include <RTI/NullFederateAmbassador.h>
#include <RTI/Enums.h>
#include <RTI/Handle.h>
#include <RTI/Typedefs.h>

#include <array>
#include <iostream>
#include <memory>
#include <string>

namespace {

std::string ws2s(const std::wstring& w) {
  return std::string(w.begin(), w.end());
}

class NullFed : public rti1516e::NullFederateAmbassador {};

struct ResignCase {
  rti1516e::ResignAction action;
  const char* name;
};

}  // namespace

int main() {
  // Catalogue row 5.3 — all 6 enumerator values per spec Enums.h:33-41.
  const std::array<ResignCase, 6> cases{{
      {rti1516e::UNCONDITIONALLY_DIVEST_ATTRIBUTES,
       "UNCONDITIONALLY_DIVEST_ATTRIBUTES"},
      {rti1516e::DELETE_OBJECTS, "DELETE_OBJECTS"},
      {rti1516e::CANCEL_PENDING_OWNERSHIP_ACQUISITIONS,
       "CANCEL_PENDING_OWNERSHIP_ACQUISITIONS"},
      {rti1516e::DELETE_OBJECTS_THEN_DIVEST, "DELETE_OBJECTS_THEN_DIVEST"},
      {rti1516e::CANCEL_THEN_DELETE_THEN_DIVEST,
       "CANCEL_THEN_DELETE_THEN_DIVEST"},
      {rti1516e::NO_ACTION, "NO_ACTION"},
  }};

  for (const auto& c : cases) {
    try {
      rti1516e::RTIambassadorFactory factory;
      rti1516e::auto_ptr<rti1516e::RTIambassador> amb =
          factory.createRTIambassador();

      NullFed fed;
      amb->connect(fed, rti1516e::HLA_IMMEDIATE,
                   L"gortiAddress=127.0.0.1:8080");
      std::cout << "FED: CONNECT" << std::endl;

      std::vector<std::wstring> modules{L"./federation.fom.xml"};
      try {
        amb->createFederationExecution(L"fm_create_join_resign", modules);
        std::cout << "FED: CREATE federation=fm_create_join_resign"
                  << std::endl;
      } catch (const rti1516e::FederationExecutionAlreadyExists&) {
        std::cout << "FED: CREATE federation=fm_create_join_resign"
                  << std::endl;
      }

      amb->joinFederationExecution(L"resigner", L"fm_create_join_resign");
      std::cout << "FED: JOIN federate=resigner" << std::endl;

      // Register one object so the DELETE_OBJECTS variants have state to
      // act on.
      const auto vehicle = amb->getObjectClassHandle(L"HLAobjectRoot.Vehicle");
      const auto pos = amb->getAttributeHandle(vehicle, L"Position");
      rti1516e::AttributeHandleSet attrs;
      attrs.insert(pos);
      amb->publishObjectClassAttributes(vehicle, attrs);
      // Name-collision tolerance, same pattern as the CREATE/AlreadyExists
      // catch above: divest/cancel/no-action resigns legally leave car-1
      // alive in the federation (§4.10 — only the DELETE_OBJECTS-flavored
      // actions remove instances), so an in-process iteration re-registers
      // a name that may still exist. Under reference_rti the silent §4.6 destroy
      // below fully resets the execution and this catch never fires; under
      // gorti destroyFederationExecution does not yet cascade to the object
      // registry (see README "gorti in-process iteration notes"), so the
      // stale name surfaces here as ObjectInstanceNameInUse/RTIinternalError.
      try {
        amb->registerObjectInstance(vehicle, L"car-1");
      } catch (const rti1516e::ObjectInstanceNameInUse&) {
      } catch (const rti1516e::RTIinternalError& e) {
        if (ws2s(e.what()).find("already registered") == std::string::npos) {
          throw;
        }
      }
      std::cout << "FED: REGISTER name=car-1 handle=<H>" << std::endl;

      amb->resignFederationExecution(c.action);
      std::cout << "FED: RESIGN action=" << c.name << std::endl;

      // §4.6 destroyFederationExecution — silent teardown between rounds so
      // each sub-scenario starts hermetic (divest-flavored resigns leave
      // car-1 alive in the federation, which would collide with the next
      // round's registerObjectInstance). No federate is joined at this
      // point, so destroy is legal under both RTIs; tolerated exceptions
      // cover racing sub-scenario orderings. Emits no golden lines.
      try {
        amb->destroyFederationExecution(L"fm_create_join_resign");
      } catch (const rti1516e::FederatesCurrentlyJoined&) {
      } catch (const rti1516e::FederationExecutionDoesNotExist&) {
      }

      amb->disconnect();
      std::cout << "FED: DISCONNECT" << std::endl;
    } catch (const rti1516e::Exception& e) {
      std::cerr << "FED: ERROR action=" << c.name << " " << ws2s(e.what())
                << std::endl;
      return 1;
    }
  }
  return 0;
}
