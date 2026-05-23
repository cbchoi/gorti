// rti1516e::RTIambassador — IEEE 1516.1-2010 Layer-2 ambassador.
//
// M17 Cut-1 surface (this header):
//   - connection lifecycle: ctor / dtor / dial-style connect via URL
//
// Future Cut-1 milestones append:
//   M17.2  federation lifecycle (createFederationExecution, join, resign)
//   M17.3  §10.2 handle services
//   M17.4  §5 pub/sub declarations
//   M17.5  §6 register/update/send
//   M17.6  tickCallback + FederateAmbassador
//
// Shape mirrors Pitch's `rti1516e::RTIambassador` so a federate ported
// from Pitch / Portico / MAK can recompile against this header with
// minimal call-site change. See docs/PITCH_PARITY.md for the
// divergence table.

#pragma once

#include <memory>
#include <string>

#include "Exceptions.h"
#include "Types.h"

namespace rti1516e {

// Forward-declare the impl class so callers don't see grpc++ headers
// transitively. The pimpl lives in src/RtiAmbassador.cpp.
class RTIambassadorImpl;

// RTIambassador is the federate's primary handle on the RTI. One
// instance per federate; the lifetime spans connect..disconnect.
//
// Thread safety: synchronous calls are thread-safe — internal locking
// serializes wire I/O. Override callback slots fire from the
// tickCallback caller's thread (M17.6).
class RTIambassador {
 public:
  RTIambassador();
  ~RTIambassador();

  // Move-only. The pimpl owns a grpc channel + per-federate state;
  // copying would split the channel/state ownership.
  RTIambassador(const RTIambassador&) = delete;
  RTIambassador& operator=(const RTIambassador&) = delete;
  RTIambassador(RTIambassador&&) noexcept;
  RTIambassador& operator=(RTIambassador&&) noexcept;

  // §4.2 connect — dial the RTI at the given URL.
  //
  // ``url`` accepts:
  //   - ``grpc://host:port``   plaintext gRPC (default rtid --listen)
  //   - ``grpcs://host:port``  TLS gRPC (M14 mTLS — Cut-2 territory)
  //
  // Throws AlreadyConnected if connect() was called previously on
  // this instance without an intervening disconnect(). Throws
  // ConnectionFailed if the URL parse fails or the initial dial
  // can't establish a channel.
  //
  // gRPC channels are lazy by default; this call does NOT block on a
  // server connection. A subsequent RPC will surface a transient
  // server-unreachable as the matching exception.
  void connect(const std::string& url);

  // §4.3 disconnect — close the channel. Idempotent. After
  // disconnect() any subsequent call other than connect() or
  // destruction throws NotConnected.
  void disconnect();

  // Returns true between connect() and disconnect(). Useful for
  // assertions in federate code.
  bool isConnected() const noexcept;

 private:
  std::unique_ptr<RTIambassadorImpl> impl_;
};

}  // namespace rti1516e
