// M17Bridge — opaque delegate to gorti's M17 rti1516e::RTIambassador.
//
// M34 (§4 Federation Management).
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
// The bridge initially covers the §4 Federation Management surface.
// Other service groups extend it with their M17 delegation methods rather
// than re-solving the header collision.

#ifndef GORTI_DLC_M17_BRIDGE_H
#define GORTI_DLC_M17_BRIDGE_H

#include <cstdint>
#include <map>
#include <memory>
#include <optional>
#include <string>
#include <vector>


namespace rti1516e_m17 {
class FederateAmbassador;
}  // namespace rti1516e_m17

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
  // §4.14 — this federate has achieved the sync point. `successfully`
  // (M37) rides M17's flag-carrying overload; false still
  // counts toward completion but lands the federate in the §4.15
  // failed-to-sync set.
  void synchronizationPointAchieved(const std::string& label,
                                    bool successfully = true);

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

  // ---------- §5 Declaration Management (M35) --------------------
  //
  // IEEE 1516.1-2010 API shape (M17): (ObjectClassHandle cls, AttributeHandleSet attrs) — the
  // handles are pre-resolved via §10.2 support services. The bridge accepts
  // raw uint64 handles so DLC's typed handles (which store a VLD blob rather
  // than a raw integer) can be adapted via the file-bottom Friend shims in
  // RTIambassadorImpl.cpp (see raw{ObjectClass,Attribute,InteractionClass}
  // Handle helpers). Empty attrs vector is spec-legal per M17 header comment
  // — the manager records publish/subscribe intent without attribute bindings.
  //
  // The DLC-only extras `bool active` (row 11.9) and `wstring updateRate`
  // (row 11.9/11.11) are NOT modeled on the M17 wire (M17 Cut-1 does not
  // support passive subscription or per-subscription update-rate policies).
  // The DLC caller strips them before invoking the bridge; the divergence is
  // documented in docs/DLC_DIVERGENCE_CATALOGUE.md §11 rows 11.9 / 11.11.
  //
  // Throws std::runtime_error on M17 failure (see guard() prefix vocabulary).
  void publishObjectClassAttributes(
      std::uint64_t cls, const std::vector<std::uint64_t>& attrs);
  void unpublishObjectClassAttributes(
      std::uint64_t cls, const std::vector<std::uint64_t>& attrs);
  void subscribeObjectClassAttributes(
      std::uint64_t cls, const std::vector<std::uint64_t>& attrs);
  void unsubscribeObjectClassAttributes(
      std::uint64_t cls, const std::vector<std::uint64_t>& attrs);

  void publishInteractionClass(std::uint64_t cls);
  void unpublishInteractionClass(std::uint64_t cls);
  void subscribeInteractionClass(std::uint64_t cls);
  void unsubscribeInteractionClass(std::uint64_t cls);


  // ===== §6 Object Management (M35) =====
  void reserveObjectInstanceName(const std::string& name);
  void reserveMultipleObjectInstanceNames(const std::vector<std::string>& names);
  void releaseObjectInstanceName(const std::string& name);
  void releaseMultipleObjectInstanceNames(const std::vector<std::string>& names);
  std::uint64_t registerObjectInstance(std::uint64_t object_class,
                                       const std::string& instance_name);
  void updateAttributeValues(
      std::uint64_t object,
      const std::map<std::uint64_t, std::vector<std::uint8_t>>& values,
      const std::vector<std::uint8_t>& tag);
  void sendInteraction(
      std::uint64_t interaction_class,
      const std::map<std::uint64_t, std::vector<std::uint8_t>>& params,
      const std::vector<std::uint8_t>& tag);
  void deleteObjectInstance(std::uint64_t object,
                            const std::vector<std::uint8_t>& tag);
  void localDeleteObjectInstance(std::uint64_t object);
  void requestAttributeValueUpdate(
      std::uint64_t object,
      const std::vector<std::uint64_t>& attrs,
      const std::vector<std::uint8_t>& tag);
  // §6.19 class-scoped variant — every owner of any instance of the
  // class receives provideAttributeValueUpdate. M36.
  void requestClassAttributeValueUpdate(
      std::uint64_t object_class,
      const std::vector<std::uint64_t>& attrs,
      const std::vector<std::uint8_t>& tag);

  // ---------- §6 TSO (timed) variants (M36) ------------------------
  //
  // `logical_time` is the HLAfloat64Time double the DLC layer narrowed
  // from the spec-abstract LogicalTime. The M17 client sets the proto's
  // `optional double logical_time`, engaging the server TSO gate. Tag
  // remains a wire divergence on update/send (dropped — same as the RO
  // paths); delete carries it end-to-end.
  void updateAttributeValuesTimed(
      std::uint64_t object,
      const std::map<std::uint64_t, std::vector<std::uint8_t>>& values,
      const std::vector<std::uint8_t>& tag,
      double logical_time);
  void sendInteractionTimed(
      std::uint64_t interaction_class,
      const std::map<std::uint64_t, std::vector<std::uint8_t>>& params,
      const std::vector<std::uint8_t>& tag,
      double logical_time);
  void deleteObjectInstanceTimed(std::uint64_t object,
                                 const std::vector<std::uint8_t>& tag,
                                 double logical_time);

  // ---------- §8.21/§8.22 retractable TSO sends (M37) -----------
  //
  // Same shape as the *Timed variants but routed through M17's retractable
  // overloads, which allocate and return a MessageRetractionHandle (raw
  // uint64; per-federate monotonic counter). Pass the value to retract()
  // to cancel while the message is still buffered server-side. Tag remains
  // the documented §11 wire divergence (dropped).
  std::uint64_t updateAttributeValuesRetractable(
      std::uint64_t object,
      const std::map<std::uint64_t, std::vector<std::uint8_t>>& values,
      const std::vector<std::uint8_t>& tag,
      double logical_time);
  std::uint64_t sendInteractionRetractable(
      std::uint64_t interaction_class,
      const std::map<std::uint64_t, std::vector<std::uint8_t>>& params,
      const std::vector<std::uint8_t>& tag,
      double logical_time);

  // ---------- §7 Ownership Management (M36) ---------------------
  //
  // Straight uint64/std shims over M17's Cut-3 ownership surface (M17.15).
  // AttributeHandleSet flattens to vector<uint64>; the DLC-mandatory tag on
  // attributeOwnershipAcquisition is dropped at the DLC layer (documented
  // divergence, catalogue §12 row 12.2) — M17's wire only carries a tag on
  // the negotiated divestiture.
  void unconditionalAttributeOwnershipDivestiture(
      std::uint64_t object, const std::vector<std::uint64_t>& attrs);
  // §7.3 — `two_phase` (M37): true runs the real §7.3/§7.6
  // protocol — the divester gets requestDivestitureConfirmation and the
  // transfer completes only on confirmDivestiture(). false keeps the
  // pre-M37 one-phase behavior.
  void negotiatedAttributeOwnershipDivestiture(
      std::uint64_t object, const std::vector<std::uint64_t>& attrs,
      const std::vector<std::uint8_t>& tag, bool two_phase = false);
  // §7.6 — complete a two-phase negotiated divestiture (M37;
  // real ConfirmDivestiture RPC — replaces the documented DLC no-op).
  void confirmDivestiture(std::uint64_t object,
                          const std::vector<std::uint64_t>& attrs);
  void attributeOwnershipAcquisition(
      std::uint64_t object, const std::vector<std::uint64_t>& attrs);
  // §7.9 — acquire ONLY the currently-available attributes; the server
  // emits AttributeOwnershipUnavailable (§7.10) for the owned remainder
  // and queues nothing. M37 (replaces CA's query-then-acquire
  // emulation).
  void attributeOwnershipAcquisitionIfAvailable(
      std::uint64_t object, const std::vector<std::uint64_t>& attrs);
  void cancelNegotiatedAttributeOwnershipDivestiture(
      std::uint64_t object, const std::vector<std::uint64_t>& attrs);
  void cancelAttributeOwnershipAcquisition(
      std::uint64_t object, const std::vector<std::uint64_t>& attrs);
  void attributeOwnershipDivestitureIfWanted(
      std::uint64_t object, const std::vector<std::uint64_t>& attrs);
  // §7.17 — synchronous on the M17 wire. `owner` is the raw FederateHandle
  // (0 = unowned / mid-transfer); `owned` mirrors M17's result validity.
  // The DLC layer converts this into the spec's §7.18 callback delivery.
  struct OwnershipQuery {
    std::uint64_t owner;
    bool owned;
  };
  OwnershipQuery queryAttributeOwnership(std::uint64_t object,
                                         std::uint64_t attribute);
  bool isAttributeOwnedByFederate(std::uint64_t object,
                                  std::uint64_t attribute);

  // ---------- §8 Time Management (M36) --------------------------
  //
  // M17 speaks double for logical time (HLAfloat64Time wire shape). The DLC
  // layer narrows LogicalTime/LogicalTimeInterval via dynamic_cast to the
  // HLAfloat64 concretes before calling these.
  void enableTimeRegulation(double lookahead);
  void disableTimeRegulation();
  void enableTimeConstrained();
  void disableTimeConstrained();
  void modifyLookahead(double lookahead);
  void timeAdvanceRequest(double time);
  void timeAdvanceRequestAvailable(double time);
  void nextMessageRequest(double time);
  void nextMessageRequestAvailable(double time);
  void flushQueueRequest(double time);
  double queryLogicalTime();
  double queryLookahead();
  // §8.19/§8.20 — {value, finite}. finite=false means the quantity is
  // undefined (no other regulating federate / no buffered TSO message);
  // the DLC layer returns false from queryGALT/queryLITS in that case.
  struct TimeQuery {
    double time;
    bool finite;
  };
  TimeQuery queryGALT();
  TimeQuery queryLITS();
  void enableAsynchronousDelivery();
  void disableAsynchronousDelivery();
  // §8.21 — retract a not-yet-delivered TSO message by raw handle.
  void retract(std::uint64_t retraction_handle);

  // ---------- §9 Data Distribution Management (M36) -------------
  //
  // M17's DDM is HLA 1.3-shaped (routing spaces). gorti's FOM parser drops
  // every 1516e <dimension> into the implicit routing space "default"
  // (handle 1) — see rti/internal/ddm/state.go populateFromFOM. The DLC
  // layer resolves that space once and threads its handle through
  // createRegion/getDimensionHandle.
  std::uint64_t getRoutingSpaceHandle(const std::string& name);
  std::uint64_t getDimensionHandle(std::uint64_t routing_space,
                                   const std::string& name);
  std::uint64_t createRegion(std::uint64_t routing_space,
                             const std::vector<std::uint64_t>& dimensions);
  void setRangeBounds(std::uint64_t region, std::uint64_t dimension,
                      std::uint64_t lower, std::uint64_t upper);
  // §9.5 query — {lower, upper, found}. found=false when the region has no
  // committed bounds for the dimension.
  struct RegionBounds {
    std::uint64_t lower;
    std::uint64_t upper;
    bool found;
  };
  RegionBounds queryRangeBounds(std::uint64_t region, std::uint64_t dimension);
  void commitRegionModifications(const std::vector<std::uint64_t>& regions);
  void deleteRegion(std::uint64_t region);

  void subscribeObjectClassAttributesWithRegions(
      std::uint64_t cls, const std::vector<std::uint64_t>& attrs,
      const std::vector<std::uint64_t>& regions);
  void unsubscribeObjectClassAttributesWithRegions(
      std::uint64_t cls, const std::vector<std::uint64_t>& attrs,
      const std::vector<std::uint64_t>& regions);
  void subscribeInteractionClassWithRegions(
      std::uint64_t cls, const std::vector<std::uint64_t>& regions);
  void unsubscribeInteractionClassWithRegions(
      std::uint64_t cls, const std::vector<std::uint64_t>& regions);

  // §9.6 — per-attribute region bindings as map<attr, regions>.
  std::uint64_t registerObjectInstanceWithRegions(
      std::uint64_t cls,
      const std::map<std::uint64_t, std::vector<std::uint64_t>>&
          attribute_regions,
      const std::string& object_name);
  void associateRegionsForUpdates(
      std::uint64_t object,
      const std::map<std::uint64_t, std::vector<std::uint64_t>>&
          attribute_regions);
  void unassociateRegionsForUpdates(
      std::uint64_t object,
      const std::map<std::uint64_t, std::vector<std::uint64_t>>&
          attribute_regions);

  void sendInteractionWithRegions(
      std::uint64_t interaction_class,
      const std::map<std::uint64_t, std::vector<std::uint8_t>>& params,
      const std::vector<std::uint64_t>& regions,
      std::optional<double> logical_time);
  void requestAttributeValueUpdateWithRegions(
      std::uint64_t object_class, const std::vector<std::uint64_t>& attrs,
      const std::vector<std::uint64_t>& regions,
      const std::vector<std::uint8_t>& tag);

  // ---------- §4.8 listFederationExecutions (M36) ---------------
  //
  // The rti.v1.FederationService wire has ListFederations but M17's Cut-4
  // ambassador never surfaced a client method for it. Rather than touch the
  // M17 impl (outside this wave's scope), the bridge dials its own throwaway
  // channel to the URL recorded at connect() and calls the stub directly.
  // Returns federation execution names; the DLC layer synthesizes the
  // §4.9 reportFederationExecutions callback from them (spec-legal —
  // callback delivery mechanics are RTI-defined).
  std::vector<std::string> listFederationExecutions();

  // ---------- M35 — §10 Support Services + callback bind ----------
  void bind_federate_ambassador(rti1516e_m17::FederateAmbassador* fed);
  std::uint64_t getObjectClassHandle(const std::string& name);
  std::string   getObjectClassName(std::uint64_t handle);
  std::uint64_t getAttributeHandle(std::uint64_t cls, const std::string& name);
  std::string   getAttributeName(std::uint64_t cls, std::uint64_t attr);
  std::uint64_t getInteractionClassHandle(const std::string& name);
  std::string   getInteractionClassName(std::uint64_t handle);
  std::uint64_t getParameterHandle(std::uint64_t cls, const std::string& name);
  std::string   getParameterName(std::uint64_t cls, std::uint64_t param);
  // §10.24/§10.25 — federate name <-> handle via the M24
  // ListFederationMembers RPC (M36). Throws (NameNotFound
  // prefix) when no joined federate matches.
  std::uint64_t getFederateHandle(const std::string& federate_name);
  std::string   getFederateName(std::uint64_t handle);
  void enableCallbacks();
  void disableCallbacks();

  // §10.41/§10.42 — callback evocation. Delegates to M17's tickCallback
  // machinery (M17 buffers events; the caller's thread drains them).
  // evokeCallback dispatches at most one; evokeMultipleCallbacks drains
  // within the [min,max] window. Both return true iff a callback fired.
  bool evokeCallback(double approx_min_time, double approx_max_time);
  bool evokeMultipleCallbacks(double approx_min_time, double approx_max_time);

 private:
  // Full defn lives in M17Bridge.cpp; the M17 rti1516e::RTIambassador
  // member is stored here so no M17 type leaks into the header.
  struct Impl;
  std::unique_ptr<Impl> impl_;
};

}  // namespace rti1516e

#endif  // GORTI_DLC_M17_BRIDGE_H
