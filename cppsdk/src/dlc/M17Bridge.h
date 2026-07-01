// M17Bridge — opaque delegate to gorti's M17 rti1516e::RTIambassador.
//
// M34 Agent AA (§4 Federation Management).
//
// Purpose: keep M17's <rti1516e/RtiAmbassador.h> OUT of the DLC translation
// unit. The DLC spec header <RTI/RTIambassador.h> and M17's
// <rti1516e/RtiAmbassador.h> BOTH declare `class RTIambassador` inside
// `namespace rti1516e`, which is a redefinition error if both are included
// in the same TU. The same collision applies to
// `rti1516e::FederateAmbassador`, `rti1516e::NotConnected`,
// `rti1516e::AlreadyConnected`, etc.
//
// Design: this header exposes ONLY std types + std::uint64_t handles, so it
// can be #included freely alongside <RTI/RTIambassador.h>. The M17 include
// happens inside M17Bridge.cpp, which does NOT include <RTI/RTIambassador.h>.
// The two dialects of `rti1516e::RTIambassador` never coexist in one TU.
//
// The bridge covers ONLY the §4 Federation Management surface Agent AA owns.
// Other M34 agents (AC §5, AB §8, AD callback dispatch) will extend the
// bridge as their surfaces need M17 delegation — each new agent adds their
// own methods here rather than re-solving the header collision.

#ifndef GORTI_DLC_M17_BRIDGE_H
#define GORTI_DLC_M17_BRIDGE_H

#include <cstdint>
#include <map>
#include <memory>
#include <optional>
#include <string>
#include <vector>

namespace rti1516e {

// SaveState / RestoreState mirror rti1516e::RTIambassador::SaveState /
// RestoreState from M17's <rti1516e/RtiAmbassador.h>. Duplicated here as
// plain enums so DLC callers can read save/restore progress without
// including M17's header.
enum class M17SaveState : int {
  Unspecified = 0, Idle = 1, Initiated = 2, Saved = 3, NotSaved = 4,
};
enum class M17RestoreState : int {
  Unspecified = 0, Idle = 1, Loading = 2, Initiated = 3,
  Completed = 4, Failed = 5,
};

// Opaque bridge to the M17 concrete rti1516e::RTIambassador.
//
// Thread safety: matches M17 — synchronous calls are thread-safe; the
// bridge itself is a thin forwarding shim (no additional locking).
class M17Bridge {
 public:
  M17Bridge();
  ~M17Bridge();
  M17Bridge(const M17Bridge&) = delete;
  M17Bridge& operator=(const M17Bridge&) = delete;

  // §4.2 — dial the RTI. `url` accepts "grpc://host:port" (or "grpcs://").
  // Throws std::runtime_error on failure. The DLC caller re-throws as the
  // appropriate <RTI/Exception.h> type.
  void connect(const std::string& url);
  // §4.3 — close the channel.
  void disconnect();
  // True between connect and disconnect.
  bool isConnected() const noexcept;

  // §4.5 — create a federation execution. `fom_modules` are file paths.
  void createFederationExecution(const std::string& name,
                                 const std::vector<std::string>& fom_modules);
  // §4.6 — destroy a federation execution.
  void destroyFederationExecution(const std::string& name);

  // §4.9 — join a federation. Returns raw FederateHandle value (uint64;
  // 0 means invalid). Callers wrap it in the DLC's rti1516e::FederateHandle.
  std::uint64_t joinFederationExecution(const std::string& federate_name,
                                         const std::string& federation_name);
  // §4.10 — resign. Cut-1 M17 has no ResignAction parameter (implicit
  // UNCONDITIONALLY_DIVEST_ATTRIBUTES); the DLC caller accepts the wider
  // ResignAction and translates to the M17 default.
  void resignFederationExecution();

  // §4.11 — register a federation sync point.
  void registerFederationSynchronizationPoint(
      const std::string& label,
      const std::vector<std::uint8_t>& tag,
      const std::vector<std::uint64_t>& required_federates);
  // §4.14 — this federate has achieved the sync point.
  void synchronizationPointAchieved(const std::string& label);

  // §4.16 — request a federation save. When `save_time` has a value it
  // pins the save to that logical time; otherwise "save now".
  void requestFederationSave(const std::string& label,
                             std::optional<double> save_time);
  // §4.17 — this federate has completed its serialization phase.
  void federateSaveComplete();
  // §4.17 — this federate failed to serialize; the save will abort.
  void federateSaveNotComplete();
  // §4.28 — abort an in-progress save federation-wide.
  void abortFederationSave();
  // §4.15 — current save state for `label`.
  M17SaveState querySaveState(const std::string& label);

  // §4.24 — request a federation restore.
  void requestFederationRestore(const std::string& label);
  // §4.26 — this federate completed its restore phase.
  void federateRestoreComplete();
  // §4.30 — abort an in-progress restore federation-wide.
  void abortFederationRestore();
  // §4.15 — current restore state for `label`.
  M17RestoreState queryRestoreState(const std::string& label);

  // ===== §6 Object Management (M35 Agent BC) =====
  //
  // Every §6 wire call requires the federate to be joined; M17 raises
  // FederateNotExecutionMember pre-join. The bridge re-raises via
  // std::runtime_error("FederateNotExecutionMember: ...") so the DLC
  // caller translates through translateBridgeError.

  // §6.5 — async reserve. Result delivered via the
  //   objectInstanceNameReservation{Succeeded,Failed}(name) callback on
  //   the bound FederateAmbassador; the RPC itself returns promptly.
  void reserveObjectInstanceName(const std::string& name);
  // §6.5 (multi) — atomic batch reservation. All names succeed or none
  //   do. Result via multipleObjectInstanceNameReservation callbacks.
  void reserveMultipleObjectInstanceNames(
      const std::vector<std::string>& names);
  // §6.6 — synchronous release of a previously reserved name.
  void releaseObjectInstanceName(const std::string& name);

  // §6.8 — register an instance. Empty `instance_name` asks the RTI to
  //   generate one. `object_class` is a raw ObjectClassHandle uint64.
  //   Returns raw ObjectInstanceHandle uint64 (0 == invalid).
  std::uint64_t registerObjectInstance(std::uint64_t object_class,
                                       const std::string& instance_name);

  // §6.10 RO — update attribute values. `values` maps raw
  //   AttributeHandle uint64 → VLD bytes. `tag` is the DLC-mandatory
  //   userSuppliedTag; M17 Cut-1 does not carry the tag on the wire
  //   (documented divergence, DLC catalogue §11 row "updateAttributeValues
  //   /tag"). Kept in the bridge signature so the semantic is visible
  //   at the DLC/M17 boundary; the bytes are dropped internally.
  void updateAttributeValues(
      std::uint64_t object,
      const std::map<std::uint64_t, std::vector<std::uint8_t>>& values,
      const std::vector<std::uint8_t>& tag);

  // §6.12 RO — send an interaction. `parameters` maps raw
  //   ParameterHandle uint64 → VLD bytes. Same tag caveat as
  //   updateAttributeValues.
  void sendInteraction(
      std::uint64_t interaction_class,
      const std::map<std::uint64_t, std::vector<std::uint8_t>>& parameters,
      const std::vector<std::uint8_t>& tag);

 private:
  // Full defn lives in M17Bridge.cpp; the M17 rti1516e::RTIambassador
  // member is stored here so no M17 type leaks into the header.
  struct Impl;
  std::unique_ptr<Impl> impl_;
};

}  // namespace rti1516e

#endif  // GORTI_DLC_M17_BRIDGE_H
