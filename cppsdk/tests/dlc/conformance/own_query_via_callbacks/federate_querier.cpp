// own_query_via_callbacks — querier.
//
// queryAttributeOwnership(object, attr) is VOID (§7.17, catalogue
// row 12.6 BLOCKING — gorti M17 returned an OwnershipQueryResult struct
// synchronously; the spec is callback-driven).
//
// Result arrives via one of three callbacks (§7.18, catalogue row 4.32):
//   - informAttributeOwnership(object, attr, owner)  — known owner
//   - attributeIsNotOwned(object, attr)              — unowned
//   - attributeIsOwnedByRTI(object, attr)            — RTI-owned
//
// The querier issues three queries:
//   1. OwnedAttr        → informAttributeOwnership(..., carrier)
//   2. UnownedAttr      → attributeIsNotOwned(...)
//   3. HLAprivilegeToDelete → attributeIsOwnedByRTI(...)

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

class QuerierFed : public rti1516e::NullFederateAmbassador {
 public:
  void discoverObjectInstance(
      rti1516e::ObjectInstanceHandle theObject,
      rti1516e::ObjectClassHandle theObjectClass,
      std::wstring const& theObjectInstanceName) override {
    std::cout << "QUERIER: DISCOVER name=" << ws2s(theObjectInstanceName)
              << " handle=<H>" << std::endl;
    discovered_obj_ = theObject;
    has_object_.store(true);
  }

  // §7.18 informAttributeOwnership — federate-owned answer.
  void informAttributeOwnership(
      rti1516e::ObjectInstanceHandle theObject,
      rti1516e::AttributeHandle theAttribute,
      rti1516e::FederateHandle theOwner) override {
    std::cout << "QUERIER: INFORM_OWNERSHIP attr=OwnedAttr owner=<H>"
              << std::endl;
    ++callbacks_fired_;
  }

  // §7.18 attributeIsNotOwned — unowned answer.
  void attributeIsNotOwned(
      rti1516e::ObjectInstanceHandle theObject,
      rti1516e::AttributeHandle theAttribute) override {
    std::cout << "QUERIER: ATTRIBUTE_IS_NOT_OWNED attr=UnownedAttr"
              << std::endl;
    ++callbacks_fired_;
  }

  // §7.18 attributeIsOwnedByRTI — RTI-owned answer.
  void attributeIsOwnedByRTI(
      rti1516e::ObjectInstanceHandle theObject,
      rti1516e::AttributeHandle theAttribute) override {
    std::cout << "QUERIER: ATTRIBUTE_IS_OWNED_BY_RTI attr=HLAprivilegeToDelete"
              << std::endl;
    ++callbacks_fired_;
  }

  rti1516e::ObjectInstanceHandle discovered_obj_;
  std::atomic<bool> has_object_{false};
  std::atomic<int> callbacks_fired_{0};
};
}  // namespace

int main() {
  try {
    rti1516e::RTIambassadorFactory factory;
    auto amb = factory.createRTIambassador();
    QuerierFed fed;

    amb->connect(fed, rti1516e::HLA_IMMEDIATE,
                 L"crcAddress=127.0.0.1:8989");
    std::cout << "QUERIER: CONNECT" << std::endl;

    std::vector<std::wstring> modules{L"./federation.fom.xml"};
    try {
      amb->createFederationExecution(L"own_query_callbacks", modules);
    } catch (const rti1516e::FederationExecutionAlreadyExists&) {}
    amb->joinFederationExecution(L"querier", L"own_query_callbacks");
    std::cout << "QUERIER: JOIN" << std::endl;

    const auto vehicle = amb->getObjectClassHandle(L"HLAobjectRoot.Vehicle");
    const auto owned_attr = amb->getAttributeHandle(vehicle, L"OwnedAttr");
    const auto unowned_attr = amb->getAttributeHandle(vehicle, L"UnownedAttr");
    const auto privilege_to_delete =
        amb->getAttributeHandle(vehicle, L"HLAprivilegeToDelete");

    rti1516e::AttributeHandleSet sub_set;
    sub_set.insert(owned_attr);
    sub_set.insert(unowned_attr);
    amb->subscribeObjectClassAttributes(vehicle, sub_set, true, L"");

    for (int i = 0; i < 200 && !fed.has_object_.load(); ++i) {
      std::this_thread::sleep_for(std::chrono::milliseconds(25));
    }

    // §7.17 queryAttributeOwnership — VOID return per catalogue row 12.6.
    amb->queryAttributeOwnership(fed.discovered_obj_, owned_attr);
    std::cout << "QUERIER: QUERY_OWNERSHIP attr=OwnedAttr" << std::endl;
    amb->queryAttributeOwnership(fed.discovered_obj_, unowned_attr);
    std::cout << "QUERIER: QUERY_OWNERSHIP attr=UnownedAttr" << std::endl;
    amb->queryAttributeOwnership(fed.discovered_obj_, privilege_to_delete);
    std::cout << "QUERIER: QUERY_OWNERSHIP attr=HLAprivilegeToDelete"
              << std::endl;

    // Wait for all three callbacks to fire.
    for (int i = 0; i < 200 && fed.callbacks_fired_.load() < 3; ++i) {
      std::this_thread::sleep_for(std::chrono::milliseconds(25));
    }

    amb->resignFederationExecution(rti1516e::CANCEL_THEN_DELETE_THEN_DIVEST);
    std::cout << "QUERIER: RESIGN" << std::endl;
    amb->disconnect();
    return 0;
  } catch (const rti1516e::Exception& e) {
    std::cerr << "QUERIER: ERROR " << ws2s(e.what()) << std::endl;
    return 1;
  }
}
