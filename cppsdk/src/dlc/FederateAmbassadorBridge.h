// M34 (Agent AD) — DLCFederateAmbassadorBridge.
//
// PURPOSE. Convert M17 callback deliveries (raw uint64 handles, std::string,
// std::optional<double> timestamps) into DLC callback invocations (typed
// handle classes, std::wstring, LogicalTime const&) so subscriber-side
// conformance fixtures can actually observe reflectAttributeValues /
// receiveInteraction / discoverObjectInstance and friends.
//
// WHY A BRIDGE. gorti's M17 impl (cppsdk/src/RtiAmbassador.cpp) already
// dispatches callbacks internally when the RTI emits events, but it dispatches
// against `M17::FederateAmbassador`. The DLC surface uses a differently-shaped
// `DLC::FederateAmbassador`. A federate written against the DLC API cannot
// receive callbacks from M17 directly. This bridge is installed as the M17
// FederateAmbassador during DLCRTIambassadorImpl::connect(); each M17 override
// converts its arguments and delegates to the user-supplied DLC ambassador.
//
// INSTALLATION. See Task AD-2 wire-in and the `// TODO(AD): wire in connect()`
// marker inside cppsdk/src/dlc/RTIambassadorImpl.cpp — Agent AA owns
// connect() and will use this bridge as `m17_fed_ref_` for the M17 impl.
//
// NAMESPACE. Lives in `gorti::dlc` — it is an internal glue class, not part
// of the spec-exposed rti1516e API surface.

#pragma once

// MUST be first — the shim rewrites `rti1516e` to `rti1516e_m17` for the M17
// headers only, then restores the macro so DLC headers below use the real
// `rti1516e` namespace. See FederateAmbassadorBridge_m17_shim.h for why.
#include "FederateAmbassadorBridge_m17_shim.h"

#include <RTI/FederateAmbassador.h>
#include <RTI/Handle.h>
#include <RTI/Typedefs.h>
#include <RTI/VariableLengthData.h>

#include <optional>
#include <set>
#include <string>
#include <vector>

namespace gorti {
namespace dlc {

// ===== FR-DLC-14 — callback re-entrancy witness (M36 Agent DA) =====
//
// True while a DLC callback dispatch is on THIS thread's stack. Set by
// the RAII CallbackScope around every bridge dispatch below; checked by
// DLCRTIambassadorImpl's bridge()/bridgeR() helpers, which throw the
// spec-mandated CallNotAllowedFromWithinCallback (catalogue 17.2) when a
// federate re-enters the ambassador from inside a callback. §10.4-exempt
// services (evoke*/enable/disableCallbacks) bypass the check.
extern thread_local bool tls_in_callback;

// RAII guard: saves/restores so nested dispatch (spec-illegal but
// defensive) unwinds correctly.
class CallbackScope {
 public:
  CallbackScope() : prev_(tls_in_callback) { tls_in_callback = true; }
  ~CallbackScope() { tls_in_callback = prev_; }
  CallbackScope(const CallbackScope&) = delete;
  CallbackScope& operator=(const CallbackScope&) = delete;

 private:
  bool prev_;
};

// Bridge from M17-shaped callbacks to DLC-shaped callbacks.
//
// Subclasses M17's FederateAmbassador (rti1516e_m17::FederateAmbassador after
// the shim) so it can be installed at M17 as the callback sink. Each override
// converts M17 arguments to DLC arguments and delegates to `dlc_fed_`.
//
// Ownership: does NOT own the DLC federate pointer. The DLCRTIambassadorImpl
// stores the federate reference from connect() and passes a raw pointer into
// the bridge, then guarantees the pointer outlives the bridge (both are torn
// down by disconnect()).
//
// Null-safety: `dlc_fed_ == nullptr` makes every callback a silent no-op.
// This lets the impl construct the bridge before the federate reference is
// available; not intended for production use.
class DLCFederateAmbassadorBridge : public rti1516e_m17::FederateAmbassador {
 public:
  explicit DLCFederateAmbassadorBridge(rti1516e::FederateAmbassador* dlc_fed);
  ~DLCFederateAmbassadorBridge() override = default;

  // Accessor for tests + for AA's wire-in verification.
  rti1516e::FederateAmbassador* dlcFederate() const { return dlc_fed_; }

  // ===== §6 Object Management =====

  // §6.9 — a new object instance was registered by some federate.
  void discoverObjectInstance(
      rti1516e_m17::ObjectInstanceHandle object,
      rti1516e_m17::ObjectClassHandle object_class,
      const std::string& object_name) override;

  // §6.11 — attribute values were updated on a subscribed instance.
  // Timestamp presence selects the DLC overload (no-time vs with-time).
  void reflectAttributeValues(
      rti1516e_m17::ObjectInstanceHandle object,
      const rti1516e_m17::AttributeHandleValueMap& values,
      std::optional<double> timestamp) override;

  // §6.13 — an interaction was sent by some federate.
  void receiveInteraction(
      rti1516e_m17::InteractionClassHandle interaction_class,
      const rti1516e_m17::ParameterHandleValueMap& parameters,
      std::optional<double> timestamp) override;

  // §6.15 — a subscribed instance was deleted by its owner. Timestamp
  // presence selects the DLC overload (RO 4-arg vs TSO 6-arg). M36 DA.
  void removeObjectInstance(
      rti1516e_m17::ObjectInstanceHandle object,
      std::optional<double> timestamp,
      const rti1516e_m17::VariableLengthData& tag) override;

  // §6.20 — the RTI asks this federate (an owner) for fresh values. M36 DA.
  void provideAttributeValueUpdate(
      rti1516e_m17::ObjectInstanceHandle object,
      const rti1516e_m17::AttributeHandleSet& attributes,
      const rti1516e_m17::VariableLengthData& tag) override;

  // §6.3 — a previously-requested name reservation succeeded.
  void objectInstanceNameReservationSucceeded(
      const std::string& object_name) override;

  // §6.3 — a name reservation request was rejected (already in use).
  void objectInstanceNameReservationFailed(
      const std::string& object_name) override;

  // §6.6 — an atomic batch reservation succeeded.
  void multipleObjectInstanceNameReservationSucceeded(
      const std::vector<std::string>& object_names) override;

  // §6.6 — an atomic batch reservation was rejected. Only the colliding
  // names are forwarded (DLC surface takes the requested-names set).
  void multipleObjectInstanceNameReservationFailed(
      const std::vector<std::string>& requested_names,
      const std::vector<std::string>& colliding_names) override;

  // ===== §8 Time Management =====

  // §8.13 — the manager has granted a time advance request. M17 delivers
  // as a bare double; the DLC surface wraps in an HLAfloat64Time and passes
  // by `LogicalTime const&`.
  void timeAdvanceGrant(double time) override;

  // ===== §4 Federation Management =====

  // §4.13 — a synchronization point was registered by some federate.
  void announceSynchronizationPoint(
      const std::string& label,
      const rti1516e_m17::VariableLengthData& tag) override;

  // §4.15 — federation is synchronized at `label`. The DLC callback takes a
  // FederateHandleSet of federates that failed to sync; the legacy 1-arg M17
  // slot does not carry that data so the bridge passes an empty set.
  void federationSynchronized(const std::string& label) override;

  // §4.15 — M37 Agent EC-1: the failed-set-carrying M17 slot (M37 Agent EA
  // wire). Converts the raw failed_to_sync vector into the DLC
  // FederateHandleSet per the divergence catalogue §4.15 failedToSyncSet
  // parameter.
  void federationSynchronized(
      const std::string& label,
      const std::vector<rti1516e_m17::FederateHandle>& failed_to_sync)
      override;

  // ===== §4.12 sync-point registration acks (M37 Agent EC-1) =====

  // §4.12 — the sync-point registration THIS federate issued was accepted.
  void synchronizationPointRegistrationSucceeded(
      const std::string& label) override;

  // §4.12 — the registration was rejected. The M17 SyncPointFailureReason
  // maps onto the DLC SynchronizationPointFailureReason enum; kUnspecified
  // folds to SYNCHRONIZATION_POINT_LABEL_NOT_UNIQUE (the DLC enum has no
  // unspecified value — documented mapping, see conv::to_dlc_sync_fail).
  void synchronizationPointRegistrationFailed(
      const std::string& label, SyncPointFailureReason reason) override;

  // ===== §5.10-13 declaration advisories (M37 Agent EC-1) =====

  void startRegistrationForObjectClass(
      rti1516e_m17::ObjectClassHandle object_class) override;
  void stopRegistrationForObjectClass(
      rti1516e_m17::ObjectClassHandle object_class) override;
  void turnInteractionsOn(
      rti1516e_m17::InteractionClassHandle interaction_class) override;
  void turnInteractionsOff(
      rti1516e_m17::InteractionClassHandle interaction_class) override;

  // ===== §6.17-18 DDM scope advisories (M37 Agent EC-1) =====

  void attributesInScope(
      rti1516e_m17::ObjectInstanceHandle object,
      const rti1516e_m17::AttributeHandleSet& attributes) override;
  void attributesOutOfScope(
      rti1516e_m17::ObjectInstanceHandle object,
      const rti1516e_m17::AttributeHandleSet& attributes) override;

  // ===== §8.22 retraction (M37 Agent EC-1) =====

  // §8.22 — the DLC callback carries only the MessageRetractionHandle; the
  // M17 sender FederateHandle is dropped (DLC signature divergence).
  void requestRetraction(
      rti1516e_m17::FederateHandle sender,
      rti1516e_m17::MessageRetractionHandle retraction_handle) override;

  // ===== §7 Ownership Management =====

  // §7.4 — an owner has begun a negotiated divestiture. DLC surface omits
  // the divesting federate handle from its callback signature.
  void requestAttributeOwnershipAssumption(
      rti1516e_m17::ObjectInstanceHandle object,
      const rti1516e_m17::AttributeHandleSet& attributes,
      rti1516e_m17::FederateHandle divesting_federate,
      const rti1516e_m17::VariableLengthData& tag) override;

  // §7.7 — this federate has acquired ownership of the listed attributes.
  // DLC signature omits `owning_federate` but adds a tag; the bridge passes
  // an empty tag since M17 does not carry one.
  void attributeOwnershipAcquisitionNotification(
      rti1516e_m17::ObjectInstanceHandle object,
      const rti1516e_m17::AttributeHandleSet& attributes,
      rti1516e_m17::FederateHandle owning_federate) override;

  // §7.5 — the divesting federate's negotiated divestiture completed.
  void requestDivestitureConfirmation(
      rti1516e_m17::ObjectInstanceHandle object,
      const rti1516e_m17::AttributeHandleSet& attributes) override;

  // §7.10 — the §7.9 acquire-if-available could not grant the listed
  // attributes; nothing was queued. M37 Agent EC-1 (real wire — replaces
  // CA's client-side synthesis).
  void attributeOwnershipUnavailable(
      rti1516e_m17::ObjectInstanceHandle object,
      const rti1516e_m17::AttributeHandleSet& attributes) override;

  // §7.11 — a peer queued an acquisition against attributes THIS federate
  // owns; tag passes through. M37 Agent EC-1.
  void requestAttributeOwnershipRelease(
      rti1516e_m17::ObjectInstanceHandle object,
      const rti1516e_m17::AttributeHandleSet& attributes,
      const rti1516e_m17::VariableLengthData& tag) override;

  // ===== §4 Save/Restore =====

  // §4.17 — begin federation save. Timestamp presence selects the DLC
  // overload (no-time vs with-time).
  void initiateFederateSave(
      const std::string& label,
      std::optional<double> save_time) override;

  // §4.20 — DLC omits label from federationSaved.
  void federationSaved(const std::string& label) override;
  // §4.20 — DLC takes a SaveFailureReason; the bridge passes
  // RTI_UNABLE_TO_SAVE (M17 does not carry a reason).
  void federationNotSaved(const std::string& label) override;

  // §4.27 — begin federate restore (legacy 2-arg M17 slot). DLC signature
  // adds a federate name (not carried on the 2-arg slot) — the bridge
  // passes an empty wstring.
  void initiateFederateRestore(
      const std::string& label,
      rti1516e_m17::FederateHandle federate_handle) override;

  // §4.26 — the name-carrying 3-arg M17 slot (M37 Agent EA wire); the
  // save-time federate name forwards for real. M37 Agent EC-1.
  void initiateFederateRestore(
      const std::string& label,
      const std::string& federate_name,
      rti1516e_m17::FederateHandle federate_handle) override;

  // §4.25 — restore-request acks. The DLC Failed signature drops the M17
  // reason string (spec carries no reason on §4.25). M37 Agent EC-1.
  void requestFederationRestoreSucceeded(const std::string& label) override;
  void requestFederationRestoreFailed(const std::string& label,
                                      const std::string& reason) override;

  // §4.26 — a federation restore is beginning. M37 Agent EC-1.
  void federationRestoreBegun() override;

  // §4.29 — DLC omits label from federation{Restored,NotRestored}.
  void federationRestored(const std::string& label) override;
  void federationNotRestored(const std::string& label) override;

 private:
  // The DLC federate ambassador we forward converted callbacks into.
  // Not owned. May be null (bridge silently no-ops).
  rti1516e::FederateAmbassador* dlc_fed_;
};

// ===== Conversion helpers exposed for tests =====
//
// These are the primitive conversions used by the bridge — split out so the
// gtest can pin the wire behaviour independently of an actual bridge run.
namespace conv {

// UTF-8 std::string -> std::wstring. Byte-widening only; no code point
// decoding (M17 already emits ASCII-safe strings for names / labels — the
// FOM XML surface is 7-bit ASCII by convention). If real UTF-8 sequences
// appear in the wild we upgrade this to a proper decode.
std::wstring s2ws(const std::string& s);

// Widen a vector<string> into a set<wstring>. DLC uses std::set on the
// batch-reservation callbacks; M17 uses std::vector. The set drops order
// and de-duplicates, which is consistent with the spec §6.6 semantics.
std::set<std::wstring> s2ws_set(const std::vector<std::string>& v);

// M17 raw uint64 handle -> DLC typed handle. Encodes the uint64 as
// big-endian 8 bytes and constructs the typed handle via its
// VariableLengthData ctor (which matches the encode/decode roundtrip in
// cppsdk/src/dlc/Handle.cpp).
template <class DLCHandle>
DLCHandle to_dlc_handle(std::uint64_t raw);

// M17 std::vector<uint8_t> -> DLC VariableLengthData. Copy semantics —
// the M17 buffer is not retained.
rti1516e::VariableLengthData to_dlc_vld(
    const std::vector<std::uint8_t>& bytes);

// M17 AttributeHandleValueMap -> DLC AttributeHandleValueMap. Iterates the
// M17 map, converts each key and value, and inserts into the DLC map.
rti1516e::AttributeHandleValueMap to_dlc_attr_map(
    const rti1516e_m17::AttributeHandleValueMap& m);

rti1516e::ParameterHandleValueMap to_dlc_param_map(
    const rti1516e_m17::ParameterHandleValueMap& m);

// M17 AttributeHandleSet (set<StrongHandle>) -> DLC AttributeHandleSet
// (set<class AttributeHandle>).
rti1516e::AttributeHandleSet to_dlc_attr_set(
    const rti1516e_m17::AttributeHandleSet& s);

// M37 Agent EC-1 — M17 failed-to-sync vector -> DLC FederateHandleSet
// (§4.15 failedToSyncSet parameter).
rti1516e::FederateHandleSet to_dlc_fed_set(
    const std::vector<rti1516e_m17::FederateHandle>& v);

// M37 Agent EC-1 — M17 SyncPointFailureReason -> DLC enum. kUnspecified
// folds to SYNCHRONIZATION_POINT_LABEL_NOT_UNIQUE (the DLC enum carries
// no unspecified value; §4.12 reasons are exactly the two spec causes).
rti1516e::SynchronizationPointFailureReason to_dlc_sync_fail(
    rti1516e_m17::FederateAmbassador::SyncPointFailureReason r);

}  // namespace conv

}  // namespace dlc
}  // namespace gorti
