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
#include <vector>

#include "Exceptions.h"
#include "Types.h"

namespace rti1516e {

// Forward-declare the impl class so callers don't see grpc++ headers
// transitively. The pimpl lives in src/RtiAmbassador.cpp.
class RTIambassadorImpl;

// FOM module passed to createFederationExecution / joinFederationExecution.
// Cut-1 accepts a filesystem path; the SDK reads the file and forwards
// the XML to rtid via the FOMModule proto. A future cut may accept
// in-memory blobs directly.
using FomModuleList = std::vector<std::string>;

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

  // §4.5 — create a federation execution.
  //
  // ``federation_name`` is the per-RTI identifier; ``fom_modules`` is
  // a list of FOM module file paths (read + forwarded to rtid as
  // FOMModule.xml). Empty list is accepted — the federation has no
  // FOM constraints.
  //
  // Throws:
  //   FederationExecutionAlreadyExists  duplicate name
  //   NotConnected                      connect() not called
  //   RTIinternalError                  any other rtid failure
  void createFederationExecution(
      const std::string& federation_name,
      const FomModuleList& fom_modules);

  // §4.6 — destroy a federation execution. Errors if any federate is
  // still joined. Idempotent only when the federation does not exist
  // — re-destroying a still-live federation throws.
  //
  // Throws:
  //   FederationExecutionDoesNotExist   federation already gone
  //   NotConnected                      connect() not called
  void destroyFederationExecution(const std::string& federation_name);

  // §4.9 — join a federation. Returns the assigned FederateHandle
  // and binds this ambassador to that federate for the lifetime of
  // the federate (until resignFederationExecution).
  //
  // Cut-1 surface: no additional FOM modules at join time. M17.4+
  // adds an overload accepting ``additional_fom_modules``.
  //
  // Throws:
  //   FederationExecutionDoesNotExist   federation not created
  //   FederateAlreadyExecutionMember    this ambassador already joined
  //   NotConnected                      connect() not called
  FederateHandle joinFederationExecution(
      const std::string& federate_name,
      const std::string& federation_name);

  // §4.10 — resign from the joined federation.
  //
  // Cut-1 default action: UNCONDITIONALLY_DIVEST_ATTRIBUTES (Pitch's
  // default). M27 Phase D in the Python SDK widened this to all 6
  // ResignAction values; C++ Cut-1 leaves the action implicit and
  // exposes the wider API in Cut-2.
  //
  // Throws:
  //   FederateNotExecutionMember        ambassador not joined
  //   NotConnected                      connect() not called
  void resignFederationExecution();

  // §10.2 — handle / name lookup services. All go over SupportService
  // and are cached client-side after the first lookup. Repeated
  // queries for the same name return the cached handle without a
  // wire round-trip.
  //
  // Errors:
  //   NameNotFound       lookup target does not exist in the FOM
  //   RTIinternalError   any other rtid failure (e.g. not joined)

  ObjectClassHandle getObjectClassHandle(const std::string& name);
  std::string getObjectClassName(ObjectClassHandle handle);

  AttributeHandle getAttributeHandle(ObjectClassHandle cls,
                                     const std::string& name);
  std::string getAttributeName(ObjectClassHandle cls,
                               AttributeHandle handle);

  InteractionClassHandle getInteractionClassHandle(const std::string& name);
  std::string getInteractionClassName(InteractionClassHandle handle);

  ParameterHandle getParameterHandle(InteractionClassHandle cls,
                                     const std::string& name);
  std::string getParameterName(InteractionClassHandle cls,
                               ParameterHandle handle);

 private:
  std::unique_ptr<RTIambassadorImpl> impl_;
};

}  // namespace rti1516e
