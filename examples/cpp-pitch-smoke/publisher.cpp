// examples/cpp-pitch-smoke/publisher — C++ federate that publishes
// a Vehicle and emits a Honk for a Python (or C++) subscriber to
// observe. Used by M17.7 cross-language interop test.
//
// Usage:
//   publisher --url grpc://127.0.0.1:8080 [--federation NAME] [--fom PATH]
//
// CLI args (all optional with sensible defaults):
//   --url         RTI URL (default: grpc://127.0.0.1:8080)
//   --federation  federation name (default: cpp-pitch-smoke)
//   --fom         FOM file path (default: ./federation.fom.xml)
//   --hold       seconds to stay joined after the last emit (default: 3)
//
// Prints one line per phase to stdout so the test harness can sync.

#include <chrono>
#include <cstdint>
#include <cstdlib>
#include <iostream>
#include <string>
#include <thread>

#include "rti1516e/Exceptions.h"
#include "rti1516e/RtiAmbassador.h"

namespace {

rti1516e::VariableLengthData encodeUint64BE(std::uint64_t v) {
  rti1516e::VariableLengthData out(8);
  for (int i = 0; i < 8; ++i) {
    out[7 - i] = static_cast<std::uint8_t>((v >> (i * 8)) & 0xff);
  }
  return out;
}

rti1516e::VariableLengthData encodeUint32BE(std::uint32_t v) {
  rti1516e::VariableLengthData out(4);
  for (int i = 0; i < 4; ++i) {
    out[3 - i] = static_cast<std::uint8_t>((v >> (i * 8)) & 0xff);
  }
  return out;
}

}  // namespace

int main(int argc, char** argv) {
  // Tiny CLI parser — defaults preserved if flag absent.
  std::string url = "grpc://127.0.0.1:8080";
  std::string federation = "cpp-pitch-smoke";
  std::string fom = "./federation.fom.xml";
  int hold_seconds = 3;
  for (int i = 1; i < argc; ++i) {
    const std::string arg = argv[i];
    if (arg == "--url" && i + 1 < argc) url = argv[++i];
    else if (arg == "--federation" && i + 1 < argc) federation = argv[++i];
    else if (arg == "--fom" && i + 1 < argc) fom = argv[++i];
    else if (arg == "--hold" && i + 1 < argc) hold_seconds = std::atoi(argv[++i]);
  }

  try {
    rti1516e::RTIambassador amb;
    amb.connect(url);
    std::cout << "publisher: connected" << std::endl;

    // Create-or-join: tolerate the federation already existing
    // (another federate may have created it first).
    try {
      amb.createFederationExecution(federation, {fom});
      std::cout << "publisher: created federation " << federation << std::endl;
    } catch (const rti1516e::FederationExecutionAlreadyExists&) {
      std::cout << "publisher: federation already exists; joining" << std::endl;
    }

    amb.joinFederationExecution("cpp-publisher", federation);
    std::cout << "publisher: joined" << std::endl;

    const auto vehicle = amb.getObjectClassHandle("Vehicle");
    const auto pos = amb.getAttributeHandle(vehicle, "Position");
    const auto vel = amb.getAttributeHandle(vehicle, "Velocity");
    const auto honk = amb.getInteractionClassHandle("Honk");
    const auto vol = amb.getParameterHandle(honk, "Volume");

    amb.publishObjectClassAttributes(vehicle, {pos, vel});
    amb.publishInteractionClass(honk);
    std::cout << "publisher: declared pub/sub" << std::endl;

    // Brief settle so the subscriber's subscribe lands first if it
    // joined concurrently. The cross-lang test orchestrator does its
    // own coordination; this sleep just smooths the typical case.
    std::this_thread::sleep_for(std::chrono::milliseconds(500));

    const auto h = amb.registerObjectInstance(vehicle, "cpp-car-1");
    std::cout << "publisher: registered cpp-car-1 (handle=" << h.raw() << ")"
              << std::endl;

    {
      rti1516e::AttributeHandleValueMap values;
      values[pos] = encodeUint64BE(42);
      values[vel] = encodeUint64BE(7);
      amb.updateAttributeValues(h, values);
    }
    {
      rti1516e::ParameterHandleValueMap params;
      params[vol] = encodeUint32BE(5);
      amb.sendInteraction(honk, params);
    }
    std::cout << "publisher: emitted update + interaction" << std::endl;

    // Stay joined for `hold_seconds` so the subscriber's stream has
    // time to drain. The Python harness usually allows ~3 s.
    std::this_thread::sleep_for(std::chrono::seconds(hold_seconds));

    amb.resignFederationExecution();
    amb.disconnect();
    std::cout << "publisher: done" << std::endl;
    return 0;
  } catch (const std::exception& e) {
    std::cerr << "publisher error: " << e.what() << std::endl;
    return 1;
  }
}
