// om_request_attribute_update_class — DLC-strict late-joining subscriber.
//
// Joins AFTER publisher has registered car-cls (so subscriber missed
// initial publication). Subscribes; calls
//   §6.19 requestAttributeValueUpdate(classHandle, attributes, tag)
// — the class-handle overload (catalogue row 11.7: gorti M17 only has
// the DDM variant of this method). RTI fires
// provideAttributeValueUpdate on the publisher; publisher sends a
// fresh update; subscriber observes the REFLECT.

#include <RTI/RTIambassador.h>
#include <RTI/RTIambassadorFactory.h>
#include <RTI/NullFederateAmbassador.h>
#include <RTI/Enums.h>
#include <RTI/Handle.h>
#include <RTI/Typedefs.h>
#include <RTI/encoding/BasicDataElements.h>

#include <atomic>
#include <chrono>
#include <iostream>
#include <string>
#include <thread>

namespace {

std::string ws2s(const std::wstring& w) {
  return std::string(w.begin(), w.end());
}

class SubFed : public rti1516e::NullFederateAmbassador {
 public:
  void discoverObjectInstance(
      rti1516e::ObjectInstanceHandle theObject,
      rti1516e::ObjectClassHandle theObjectClass,
      std::wstring const& theObjectInstanceName) override {
    std::cout << "SUB: DISCOVER name=" << ws2s(theObjectInstanceName)
              << " handle=<H>" << std::endl;
    discovered_.store(true);
  }

  void reflectAttributeValues(
      rti1516e::ObjectInstanceHandle theObject,
      rti1516e::AttributeHandleValueMap const& theAttributeValues,
      rti1516e::VariableLengthData const& theUserSuppliedTag,
      rti1516e::OrderType sentOrder,
      rti1516e::TransportationType theType,
      rti1516e::SupplementalReflectInfo theReflectInfo) override {
    double position = 0.0, velocity = 0.0;
    if (theAttributeValues.find(position_attr_) != theAttributeValues.end()) {
      rti1516e::HLAfloat64BE p;
      p.decode(theAttributeValues.at(position_attr_));
      position = p.get();
    }
    if (theAttributeValues.find(velocity_attr_) != theAttributeValues.end()) {
      rti1516e::HLAfloat64BE v;
      v.decode(theAttributeValues.at(velocity_attr_));
      velocity = v.get();
    }
    std::cout << "SUB: REFLECT name=car-cls Position=" << position
              << " Velocity=" << velocity << std::endl;
    reflected_.store(true);
  }

  rti1516e::AttributeHandle position_attr_;
  rti1516e::AttributeHandle velocity_attr_;
  std::atomic<bool> discovered_{false};
  std::atomic<bool> reflected_{false};
};

}  // namespace

int main() {
  // Sleep a little so the publisher registers first.
  std::this_thread::sleep_for(std::chrono::seconds(1));

  try {
    rti1516e::RTIambassadorFactory factory;
    rti1516e::auto_ptr<rti1516e::RTIambassador> amb =
        factory.createRTIambassador();

    SubFed fed;
    amb->connect(fed, rti1516e::HLA_IMMEDIATE,
                 L"crcAddress=127.0.0.1:8989");
    std::cout << "SUB: CONNECT" << std::endl;

    std::vector<std::wstring> modules{L"./federation.fom.xml"};
    try {
      amb->createFederationExecution(L"om_request_update_class", modules);
    } catch (const rti1516e::FederationExecutionAlreadyExists&) {}

    amb->joinFederationExecution(L"subscriber", L"om_request_update_class");
    std::cout << "SUB: JOIN" << std::endl;

    const auto vehicle = amb->getObjectClassHandle(L"HLAobjectRoot.Vehicle");
    fed.position_attr_ = amb->getAttributeHandle(vehicle, L"Position");
    fed.velocity_attr_ = amb->getAttributeHandle(vehicle, L"Velocity");
    rti1516e::AttributeHandleSet attrs;
    attrs.insert(fed.position_attr_);
    attrs.insert(fed.velocity_attr_);
    amb->subscribeObjectClassAttributes(vehicle, attrs, true, L"");
    std::cout << "SUB: SUBSCRIBE Vehicle Position Velocity" << std::endl;

    // Wait for discover.
    for (int i = 0; i < 200 && !fed.discovered_.load(); ++i) {
      std::this_thread::sleep_for(std::chrono::milliseconds(50));
    }

    // §6.19 requestAttributeValueUpdate — class-handle form per
    // catalogue row 11.7.
    amb->requestAttributeValueUpdate(vehicle, attrs,
                                     rti1516e::VariableLengthData());
    std::cout << "SUB: REQUEST_UPDATE class=Vehicle attrs=[Position,Velocity]"
              << std::endl;

    // Wait for REFLECT.
    for (int i = 0; i < 200 && !fed.reflected_.load(); ++i) {
      std::this_thread::sleep_for(std::chrono::milliseconds(50));
    }

    amb->resignFederationExecution(rti1516e::CANCEL_THEN_DELETE_THEN_DIVEST);
    std::cout << "SUB: RESIGN" << std::endl;
    amb->disconnect();
    return 0;
  } catch (const rti1516e::Exception& e) {
    std::cerr << "SUB: ERROR " << ws2s(e.what()) << std::endl;
    return 1;
  }
}
