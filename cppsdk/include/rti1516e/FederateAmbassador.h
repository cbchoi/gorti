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

namespace rti1516e {

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
};

}  // namespace rti1516e
