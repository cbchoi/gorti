// rti1516e::FederateAmbassador — virtual override slots for RTI callbacks.
//
// Mirrors Pitch's `rti1516e::FederateAmbassador`. Federate code subclasses
// this and overrides the slots it cares about; unhandled slots are no-op
// by default. The RTIambassador's tickCallback() drains buffered events
// and invokes the matching slot on the bound FederateAmbassador.
//
// Cut-1 surface: the 3 most-used callbacks (discover / reflect / receive).
// Cut-2 adds removeObjectInstance, provideAttributeValueUpdate, the sync
// + ownership + save event family.
//
// Threading: tickCallback() invokes overrides on the caller's thread.
// Federate code must NOT re-enter the same ambassador from inside a
// callback (Cut-1 holds no per-call locks; a re-entrant call would
// deadlock on the gRPC stream's internal mutex).

#pragma once

#include <optional>
#include <string>
#include <vector>

#include "Types.h"

// M35 (Agent BF-2) — M17 shim deprecation gate. See Types.h for the pattern;
// FederateAmbassador.h re-declares locally for header independence.
#ifdef GORTI_ACCEPT_M17_SHIM
#  define GORTI_M17_SHIM_DEPRECATED_FA /* silenced */
#else
#  define GORTI_M17_SHIM_DEPRECATED_FA \
     [[deprecated("gorti M17 shim — use <RTI/...> per IEEE 1516.1-2010 DLC (M35). Define GORTI_ACCEPT_M17_SHIM to silence.")]]
#endif
namespace GORTI_M17_SHIM_DEPRECATED_FA rti1516e {

class FederateAmbassador {
 public:
  virtual ~FederateAmbassador() = default;

  // §6.5 — a new object instance was registered by some federate.
  // The class_handle / object_name pair lets the subscriber map
  // the new handle to its FOM context without an extra RPC.
  virtual void discoverObjectInstance(
      ObjectInstanceHandle /*object*/,
      ObjectClassHandle /*object_class*/,
      const std::string& /*object_name*/) {}

  // §6.11 — attribute values were updated on a subscribed instance.
  // ``timestamp`` is empty (no value) for RO delivery; present for
  // TSO delivery once §8 Time Management lands.
  virtual void reflectAttributeValues(
      ObjectInstanceHandle /*object*/,
      const AttributeHandleValueMap& /*values*/,
      std::optional<double> /*timestamp*/) {}

  // §6.13 — an interaction was sent by some federate.
  virtual void receiveInteraction(
      InteractionClassHandle /*interaction_class*/,
      const ParameterHandleValueMap& /*parameters*/,
      std::optional<double> /*timestamp*/) {}

  // §6.15 — a subscribed object instance was deleted by its owner.
  // ``timestamp`` is empty for RO delivery; present for TSO. ``tag``
  // echoes the deleter's user-supplied tag. M36 Agent DA.
  virtual void removeObjectInstance(
      ObjectInstanceHandle /*object*/,
      std::optional<double> /*timestamp*/,
      const VariableLengthData& /*tag*/) {}

  // §6.20 — a peer called requestAttributeValueUpdate for attributes
  // this federate owns; respond with updateAttributeValues carrying
  // fresh values for ``attributes``. M36 Agent DA.
  virtual void provideAttributeValueUpdate(
      ObjectInstanceHandle /*object*/,
      const AttributeHandleSet& /*attributes*/,
      const VariableLengthData& /*tag*/) {}

  // §6.2 — a previously-requested name reservation succeeded.
  // M17.10 (Cut-2). The federate may now call registerObjectInstance
  // with the reserved name.
  virtual void objectInstanceNameReservationSucceeded(
      const std::string& /*object_name*/) {}

  // §6.2 — a name reservation request was rejected (already in use).
  virtual void objectInstanceNameReservationFailed(
      const std::string& /*object_name*/) {}

  // §6.5 — an atomic batch reservation succeeded (every name in the
  // request was accepted).
  virtual void multipleObjectInstanceNameReservationSucceeded(
      const std::vector<std::string>& /*object_names*/) {}

  // §6.5 — an atomic batch reservation was rejected. NONE of the
  // requested names were reserved; `colliding_names` lists which
  // specific names collided.
  virtual void multipleObjectInstanceNameReservationFailed(
      const std::vector<std::string>& /*requested_names*/,
      const std::vector<std::string>& /*colliding_names*/) {}

  // §8.8-12 — the manager has granted a time advance request. ``time``
  // is the granted logical time; per IEEE 1516.1 §8.8 this may be
  // earlier than the requested time (for NER / TARA).
  // M17.11 (Cut-2). Fires once per outstanding advance request.
  virtual void timeAdvanceGrant(double /*time*/) {}

  // §4.7 — a synchronization point was registered by some federate.
  // ``tag`` is opaque user data echoed verbatim from the register
  // call. M17.14 (Cut-3). After receiving this the federate MUST
  // eventually call synchronizationPointAchieved(label) to unblock
  // the federation.
  virtual void announceSynchronizationPoint(
      const std::string& /*label*/,
      const VariableLengthData& /*tag*/) {}

  // §4.7 — every required federate has achieved the sync point; the
  // federation is now synchronized at ``label``. M17.14 (Cut-3).
  virtual void federationSynchronized(const std::string& /*label*/) {}

  // §7.3 — an owner has called negotiatedAttributeOwnershipDivestiture
  // and the federation is asking this federate (a subscriber of the
  // attribute) whether it wants to assume ownership. To accept, call
  // attributeOwnershipAcquisition. M17.15 (Cut-3).
  virtual void requestAttributeOwnershipAssumption(
      ObjectInstanceHandle /*object*/,
      const AttributeHandleSet& /*attributes*/,
      FederateHandle /*divesting_federate*/,
      const VariableLengthData& /*tag*/) {}

  // §7.4 — this federate has just acquired ownership of the listed
  // attributes (the manager moved them here from the previous owner).
  // M17.15 (Cut-3).
  virtual void attributeOwnershipAcquisitionNotification(
      ObjectInstanceHandle /*object*/,
      const AttributeHandleSet& /*attributes*/,
      FederateHandle /*owning_federate*/) {}

  // §7.3 — a divesting federate is informed that the transfer it
  // requested via negotiatedAttributeOwnershipDivestiture has
  // completed (some acquirer took over). M17.15 (Cut-3).
  virtual void requestDivestitureConfirmation(
      ObjectInstanceHandle /*object*/,
      const AttributeHandleSet& /*attributes*/) {}

  // §4.8 — the manager has begun a federation save. Each federate
  // serializes its state and calls federateSaveComplete or
  // federateSaveNotComplete. ``save_time`` (when set) is the logical
  // time pin from the original requestFederationSave call.
  // M17.16 (Cut-3).
  virtual void initiateFederateSave(
      const std::string& /*label*/,
      std::optional<double> /*save_time*/) {}

  // §4.9 — every federate completed; the federation save is
  // finalized. M17.16 (Cut-3).
  virtual void federationSaved(const std::string& /*label*/) {}

  // §4.9 — at least one federate failed; the save was aborted.
  // M17.16 (Cut-3).
  virtual void federationNotSaved(const std::string& /*label*/) {}

  // §4.13 — the manager has begun a federation restore. The
  // federate should reload its state from the named bundle and
  // call federateRestoreComplete or federateRestoreNotComplete.
  // ``federate_handle`` is the federate's pre-save handle (may
  // differ from the current handle across rejoin); zero indicates
  // no remap. M17.25 (Cut-4).
  virtual void initiateFederateRestore(
      const std::string& /*label*/,
      FederateHandle /*federate_handle*/) {}

  // §4.14 — every federate completed; the federation restore is
  // finalized. M17.25 (Cut-4).
  virtual void federationRestored(const std::string& /*label*/) {}

  // §4.14 — at least one federate failed OR an explicit
  // abortFederationRestore landed; the restore was aborted.
  // M17.25 (Cut-4).
  virtual void federationNotRestored(const std::string& /*label*/) {}
};

}  // namespace rti1516e
