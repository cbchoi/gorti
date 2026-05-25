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
#include <optional>
#include <string>
#include <vector>

#include "Exceptions.h"
#include "Types.h"

namespace rti1516e {

// Forward-declare so the public header doesn't require the
// FederateAmbassador.h include for callers that don't subclass it.
class FederateAmbassador;

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

  // §6.30 / §6.31 — runtime instance-handle services. Unlike the
  // FOM-driven handles above, these query the live object registry
  // — they let a late-joining federate resolve "car-7" to its
  // ObjectInstanceHandle without waiting for the Discover callback.
  //
  // Errors: NameNotFound if the instance hasn't been registered.
  // The result is NOT cached client-side: instances come and go
  // during a federation's lifetime, so a query should always reflect
  // current state.
  ObjectInstanceHandle getObjectInstanceHandle(const std::string& name);
  std::string getObjectInstanceName(ObjectInstanceHandle handle);

  // §6.1-5 — object instance name reservation flow. Pitch federates
  // that pre-reserve names before calling registerObjectInstance use
  // these. Result is delivered as a callback on the bound
  // FederateAmbassador (objectInstanceNameReservationSucceeded /
  // Failed); the RPCs themselves return promptly.
  //
  // After Succeeded, the federate may call
  // registerObjectInstance(class_handle, name) with that name.
  void reserveObjectInstanceName(const std::string& name);

  // §6.5 — atomic batch reservation. All names succeed or none do;
  // the Failed callback's colliding_names lists the specific names
  // that caused the batch to fail.
  void reserveMultipleObjectInstanceNames(
      const std::vector<std::string>& names);

  // §6.4 — release a previously-reserved name. Synchronous error if
  // the federate doesn't hold the reservation.
  void releaseObjectInstanceName(const std::string& name);

  // §5 — publish / subscribe declarations.
  //
  // Pitch shape: handles only. The federate must have resolved the
  // class/attribute handles via the §10.2 services first. Empty
  // AttributeHandleSet is allowed — the manager records the
  // publish/subscribe intent without any attribute bindings.
  //
  // All four methods require the ambassador to be joined to a
  // federation; calling pre-join throws FederateNotExecutionMember.

  void publishObjectClassAttributes(ObjectClassHandle cls,
                                    const AttributeHandleSet& attributes);
  void unpublishObjectClassAttributes(ObjectClassHandle cls,
                                      const AttributeHandleSet& attributes);
  void subscribeObjectClassAttributes(ObjectClassHandle cls,
                                      const AttributeHandleSet& attributes);
  void unsubscribeObjectClassAttributes(ObjectClassHandle cls,
                                        const AttributeHandleSet& attributes);

  void publishInteractionClass(InteractionClassHandle cls);
  void unpublishInteractionClass(InteractionClassHandle cls);
  void subscribeInteractionClass(InteractionClassHandle cls);
  void unsubscribeInteractionClass(InteractionClassHandle cls);

  // §6 — object / interaction surface.
  //
  // Cut-1 ships the RO (no logical time) variants. Time-managed
  // overloads land with §8 in Cut-2. Tag support is captured by the
  // wire request shape but exposed only via the no-tag default to
  // keep Cut-1 surface small.

  // §6.4 — register an object instance of ``cls``. ``instance_name``
  // is optional; an empty string asks rtid to generate one. Returns
  // the assigned ObjectInstanceHandle.
  ObjectInstanceHandle registerObjectInstance(
      ObjectClassHandle cls,
      const std::string& instance_name = "");

  // §6.10 — update attribute values on a registered instance. Empty
  // map is rejected by rtid as a malformed update.
  void updateAttributeValues(ObjectInstanceHandle obj,
                             const AttributeHandleValueMap& values);

  // §6.12 — send an interaction. Empty parameter map is allowed.
  void sendInteraction(InteractionClassHandle cls,
                       const ParameterHandleValueMap& parameters);

  // §8.21 (M20.2) — retract a previously-sent TSO message that has
  // not yet been delivered. ``handle`` is the federate-allocated
  // MessageRetractionHandle passed in the original send. Returns OK
  // whether or not a buffered message matched (the message may have
  // already been delivered). The Cut-1/2/3 C++ ambassador only ships
  // RO send/update so this surface is forward-looking — federates
  // driving TSO over the wire directly can already exercise it.
  void retract(MessageRetractionHandle handle);

  // §8 — Time Management.
  //
  // M17.11 (Cut-2). The federate opts into time policies (regulating,
  // constrained) and then drives advance requests (TAR / TARA / NER /
  // NMRA / FQR). The manager arbitrates and emits a single
  // TimeAdvanceGrant callback per request.
  //
  // Wire compat: the gorti manager uses double for logical time —
  // matches IEEE 1516.1 HLAfloat64Time. Lookahead is non-negative
  // and finite. Calling any of these pre-join throws
  // FederateNotExecutionMember.

  // §8.2 — opt into time regulation. ``lookahead`` is the federate's
  // minimum time-stamp delta; must be >= 0 and finite. Manager
  // rejects already-regulating federates synchronously.
  void enableTimeRegulation(double lookahead);
  // §8.4 — opt out of time regulation.
  void disableTimeRegulation();
  // §8.5 — opt into time constraint.
  void enableTimeConstrained();
  // §8.7 — opt out of time constraint.
  void disableTimeConstrained();
  // §8.13 — modify lookahead while regulating. Synchronous error if
  // the federate isn't regulating.
  void modifyLookahead(double lookahead);

  // §8.8 — request advance to ``time`` (TAR). The manager fires
  // timeAdvanceGrant once the federate is eligible.
  void timeAdvanceRequest(double time);
  // §8.9 — TARA: same as TAR but the manager may grant a time
  // earlier than ``time`` if a TSO message is queued.
  void timeAdvanceRequestAvailable(double time);
  // §8.10 — Next Message Request. Grant fires at the time of the
  // next TSO message at or below ``time``, or at ``time`` if none.
  void nextMessageRequest(double time);
  // §8.11 — NMRA: NMR with the "available" relaxation.
  void nextMessageRequestAvailable(double time);
  // §8.12 — Flush Queue Request. Empties the TSO queue up to
  // ``time``; manager grants ``time`` immediately.
  void flushQueueRequest(double time);

  // §8.14 — query the federate's current logical time.
  double queryLogicalTime();
  // §8.15 — query the federate's current lookahead. Synchronous
  // error if the federate isn't regulating.
  double queryLookahead();
  // §8.16 — query LBTS (lower bound time stamp) across the
  // regulating set. ``finite`` indicates whether LBTS is bounded;
  // when no federate is regulating, ``finite`` is false and the
  // returned value is unspecified.
  struct LBTSResult { double time; bool finite; };
  LBTSResult queryLBTS();

  // §8.19 — Greatest Available Logical Time for this federate:
  // min(currentTime + lookahead) over all OTHER regulating
  // federates. ``finite`` is false when no other federate is
  // regulating. M20.1.
  struct GALTResult { double time; bool finite; };
  GALTResult queryGALT();

  // §8.20 — Least Incoming Time Stamp: smallest timestamp of any
  // TSO message currently buffered for this federate. ``finite``
  // is false when no message is buffered. Async-on federates
  // see finite=false always (TSO bypasses the buffer). M20.1.
  struct LITSResult { double time; bool finite; };
  LITSResult queryLITS();

  // §8.17 — opt into asynchronous (unbuffered) delivery. RO
  // callbacks fire immediately rather than at the next grant.
  void enableAsynchronousDelivery();
  // §8.18 — opt out of asynchronous delivery.
  void disableAsynchronousDelivery();

  // §9 — Data Distribution Management.
  //
  // M17.17 (Cut-3). Regions partition the value space of one or more
  // dimensions so subscribers can express interest in narrower
  // slices of a publisher's updates / interactions. Workflow:
  //   1. Look up the FOM-declared routing space + dimensions by name.
  //   2. Create a region in that routing space; set per-dimension
  //      range bounds; commit.
  //   3. Subscribe object class attributes / interaction classes
  //      with the region; updates / interactions outside the bounds
  //      don't reach this federate.
  //   4. Publishers can also register objects bound to regions and
  //      associate per-attribute regions for fine-grained filtering.

  // §9.2 — look up a FOM-declared routing space by name. Returns an
  // invalid handle if the name is unknown.
  RoutingSpaceHandle getRoutingSpaceHandle(const std::string& name);
  // §9.2 — look up a dimension by name within a routing space.
  DimensionHandle getDimensionHandle(RoutingSpaceHandle routing_space,
                                     const std::string& name);

  // §9.5 — create a region in ``routing_space`` covering the given
  // dimensions. Initial bounds are unset; use setRangeBounds + commit
  // before the region is usable.
  RegionHandle createRegion(RoutingSpaceHandle routing_space,
                            const std::vector<DimensionHandle>& dimensions);
  // §9.5 — set bounds for one dimension of a region.
  void setRangeBounds(RegionHandle region,
                      DimensionHandle dimension,
                      const DimensionRange& bounds);
  // §9.5 — commit pending range-bound changes to the federation. The
  // region is only visible to subscribers / publishers AFTER commit.
  void commitRegionModifications(const std::vector<RegionHandle>& regions);
  // §9.5 — delete a region; subscribers / publishers using it are
  // automatically un-bound.
  void deleteRegion(RegionHandle region);
  // §9.5 — query the current bounds of one dimension of a region.
  // Returns a {DimensionRange, found} pair.
  struct QueryBoundsResult { DimensionRange bounds; bool found; };
  QueryBoundsResult queryBounds(RegionHandle region, DimensionHandle dimension);

  // §9.6 — region-aware publish/subscribe.
  void subscribeObjectClassAttributesWithRegions(
      ObjectClassHandle object_class,
      const AttributeHandleSet& attributes,
      const RegionHandleSet& regions);
  void subscribeInteractionClassWithRegions(
      InteractionClassHandle interaction_class,
      const RegionHandleSet& regions);
  void unsubscribeObjectClassAttributesWithRegions(
      ObjectClassHandle object_class,
      const AttributeHandleSet& attributes,
      const RegionHandleSet& regions);
  void unsubscribeInteractionClassWithRegions(
      InteractionClassHandle interaction_class,
      const RegionHandleSet& regions);

  // §9.6 — register an object with per-attribute region bindings.
  // ``object_name`` may be empty for auto-generated. Returns the
  // assigned object handle + final name (server-generated when
  // ``object_name`` was empty).
  struct RegisterWithRegionsResult {
    ObjectInstanceHandle object;
    std::string object_name;
  };
  RegisterWithRegionsResult registerObjectInstanceWithRegions(
      ObjectClassHandle object_class,
      const AttributeRegionMap& attribute_regions,
      const std::string& object_name = "");

  // §9.6 — associate / unassociate per-attribute regions on an
  // existing object. Empty map on Unassociate drops all bindings.
  void associateRegionsForUpdates(
      ObjectInstanceHandle object,
      const AttributeRegionMap& attribute_regions);
  void unassociateRegionsForUpdates(
      ObjectInstanceHandle object,
      const AttributeRegionMap& attribute_regions);

  // §9.12 — send an interaction filtered by region overlap.
  void sendInteractionWithRegions(
      InteractionClassHandle interaction_class,
      const ParameterHandleValueMap& parameters,
      const RegionHandleSet& regions,
      std::optional<double> logical_time = std::nullopt);
  // §9.13 — pull-style attribute update request, filtered by region
  // overlap. ``tag`` is opaque user data.
  void requestAttributeValueUpdateWithRegions(
      ObjectClassHandle object_class,
      const AttributeHandleSet& attributes,
      const RegionHandleSet& regions,
      const VariableLengthData& tag = {});

  // §4.8-15 — Save / Restore.
  //
  // M17.16 (Cut-3). Two phases:
  //   1. Save: requester calls requestFederationSave; manager
  //      broadcasts InitiateFederateSave to every federate; each
  //      federate serializes its state and reports back via
  //      federateSaveComplete or federateSaveNotComplete; manager
  //      broadcasts FederationSaved or FederationNotSaved.
  //   2. Restore: requester calls requestFederationRestore; manager
  //      loads from a prior labeled save. Restore callbacks are NOT
  //      yet wired on the gorti server side — federates can drive
  //      the RPCs and poll queryRestoreState but won't receive
  //      initiateFederateRestore / federationRestored events. See
  //      stream.proto's comment near InitiateFederateSave.

  // §4.8 — request a federation save. ``save_time`` is the optional
  // logical-time pin; pass std::nullopt for "save now".
  void requestFederationSave(
      const std::string& label,
      std::optional<double> save_time = std::nullopt);
  // §4.9 — this federate has completed its serialization phase.
  void federateSaveComplete();
  // §4.9 — this federate failed to serialize; the save will abort.
  void federateSaveNotComplete();
  // §4.28 — abort an in-progress save federation-wide.
  void abortFederationSave();
  // §4.15 — current save state for ``label`` (queue/idle/initiated/
  // saved/failed).
  enum class SaveState {
    Unspecified = 0, Idle = 1, Initiated = 2, Saved = 3, NotSaved = 4,
  };
  SaveState querySaveState(const std::string& label);

  // §4.10 — request a federation restore from a prior labeled save.
  void requestFederationRestore(const std::string& label);
  // §4.11 — this federate completed its restore phase. (Restore
  // callbacks aren't yet emitted server-side, but the RPCs are
  // accepted so federates can drive the protocol manually.)
  void federateRestoreComplete();
  // §4.30 — abort an in-progress restore federation-wide.
  void abortFederationRestore();
  // §4.15 — current restore state for ``label``.
  enum class RestoreState {
    Unspecified = 0, Idle = 1, Loading = 2, Initiated = 3,
    Completed = 4, Failed = 5,
  };
  RestoreState queryRestoreState(const std::string& label);

  // §7 — Ownership Management.
  //
  // M17.15 (Cut-3). Negotiated transfer: an owner divests, candidate
  // subscribers may acquire. Sync transfer: unconditional divest +
  // immediate acquire. All RPCs return Empty synchronously; the
  // transfer outcomes arrive via FederateAmbassador callbacks
  // (requestAttributeOwnershipAssumption / Notification /
  // requestDivestitureConfirmation).

  // §7.2 — drop ownership without waiting for an acquirer. Subscribers
  // see an attribute with no owner until somebody acquires it.
  void unconditionalAttributeOwnershipDivestiture(
      ObjectInstanceHandle object,
      const AttributeHandleSet& attributes);

  // §7.3 — offer ownership to subscribers. ``tag`` is opaque user
  // data echoed in the announce callback on each subscriber.
  void negotiatedAttributeOwnershipDivestiture(
      ObjectInstanceHandle object,
      const AttributeHandleSet& attributes,
      const VariableLengthData& tag);

  // §7.4 — request to acquire attributes from the current owner.
  void attributeOwnershipAcquisition(
      ObjectInstanceHandle object,
      const AttributeHandleSet& attributes);

  // §7.5 — withdraw a previously-issued negotiated divest.
  void cancelNegotiatedAttributeOwnershipDivestiture(
      ObjectInstanceHandle object,
      const AttributeHandleSet& attributes);

  // §7.5 — withdraw a pending acquisition.
  void cancelAttributeOwnershipAcquisition(
      ObjectInstanceHandle object,
      const AttributeHandleSet& attributes);

  // §7.5 — drop ownership IF some acquirer wants it; no-op otherwise.
  void attributeOwnershipDivestitureIfWanted(
      ObjectInstanceHandle object,
      const AttributeHandleSet& attributes);

  // §7.7 — query the current owner of an attribute. Returns the
  // FederateHandle (raw==0 indicates mid-transfer / unowned).
  struct OwnershipQueryResult {
    FederateHandle owner;
    bool owned;
  };
  OwnershipQueryResult queryAttributeOwnership(
      ObjectInstanceHandle object,
      AttributeHandle attribute);

  // §7.7 — is the named attribute owned by THIS federate?
  bool isAttributeOwnedByFederate(
      ObjectInstanceHandle object,
      AttributeHandle attribute);

  // §4.7 — Federation synchronization points.
  //
  // M17.14 (Cut-3). A federate registers a sync point with an
  // optional ``required_federates`` set (empty = "all currently
  // joined"). The manager broadcasts SynchronizationPointAnnounced
  // to every required federate; each calls
  // synchronizationPointAchieved when its local condition is met.
  // Once all required federates have achieved, the manager fires
  // FederationSynchronized on every required federate.

  // §4.7 — register a federation sync point. ``tag`` is opaque user
  // data echoed in the announce callback. Pass an empty
  // ``required_federates`` to default to "all currently joined".
  void registerFederationSynchronizationPoint(
      const std::string& label,
      const VariableLengthData& tag,
      const std::vector<FederateHandle>& required_federates = {});

  // §4.7 — this federate has achieved the sync point. Idempotent
  // server-side; double-achieve doesn't error.
  void synchronizationPointAchieved(const std::string& label);

  // §11 — Management Object Model (MOM) ambassador delegates.
  //
  // M17.13 (Cut-3). Read-only introspection of the
  // HLAfederation + per-federate HLAfederate MOM object instances.
  // No callbacks: federates poll. Returns typed result structs so
  // the caller doesn't have to thread proto types through their
  // code.

  // Per IEEE 1516.1-2010 §10.2 — HLAfederation MOM snapshot.
  struct FederationAttributes {
    std::string federation_name;
    std::vector<FederateHandle> federate_handles;
    std::vector<std::string> fom_module_names;
  };
  FederationAttributes queryFederationAttributes();

  // Per IEEE 1516.1-2010 §10.3 — HLAfederate MOM snapshot. ``found``
  // is false when the federate is no longer tracked (resigned).
  struct FederateAttributes {
    bool found;
    FederateHandle federate_handle;
    std::string federate_name;
    std::string federate_type;
    bool time_regulating;
    bool time_constrained;
    double logical_time;
    double lookahead;
    std::uint32_t interactions_sent;
    std::uint32_t interactions_received;
    std::uint32_t updates_sent;
    std::uint32_t reflections_received;
  };
  FederateAttributes queryFederateAttributes(FederateHandle federate);

  // One entry in EnumerateMomInstances. ``class_name`` is either
  // "HLAobjectRoot.HLAmanager.HLAfederation" (singleton, federate=0)
  // or "HLAobjectRoot.HLAmanager.HLAfederate" (one per joined fed).
  struct MomInstance {
    std::string class_name;
    FederateHandle federate_handle;
    std::string instance_name;
  };
  std::vector<MomInstance> enumerateMomInstances();

  // §10.4 — bind a FederateAmbassador for callback delivery. Must
  // be called BEFORE joinFederationExecution; the join triggers the
  // server-streaming Events RPC which feeds the callback queue.
  // Passing nullptr unbinds — useful for clean shutdown ordering.
  //
  // The bound FederateAmbassador's lifetime must exceed the
  // RTIambassador's last tickCallback / resignFederationExecution
  // call. The simplest pattern: stack-allocate both in the same
  // scope, RtiAmbassador second so it tears down first.
  void setFederateAmbassador(FederateAmbassador* fed);

  // §10.4 — drive callback dispatch. Yields to the event-stream
  // drain for at least ``approx_min_time`` seconds; returns early
  // once a callback fires AND that minimum elapsed, or returns at
  // ``approx_max_time`` if no callback arrived. Returns true if at
  // least one callback fired during the window.
  //
  // Cut-1 "cheap evoke" semantics: same as the Python ambassador's
  // M26 Phase E behavior. Callbacks may fire OUTSIDE the window
  // (the background stream drain doesn't pause when tickCallback
  // isn't running). Strict HLA_EVOKED buffered-drain mode is Cut-2.
  // Documented in docs/PITCH_PARITY.md.
  bool tickCallback(double approx_min_time = 0.0,
                    double approx_max_time = 0.0);

  // §10.4 — strict HLA_EVOKED at-most-one. M17.22 (Cut-4).
  //
  // Waits up to ``approx_max_time`` for at least one event (and
  // blocks until ``approx_min_time`` elapses), then dispatches
  // EXACTLY ONE buffered callback. Returns true iff a callback
  // fired AND more events remain queued — federates use this as
  // the signal to keep evoking:
  //
  //   while (amb.evokeCallback(0.0, 0.1)) {
  //     // a callback just fired and more are queued
  //   }
  //
  // Strict semantics rely on M17.21's dispatchOneEvent helper.
  // Distinct from evokeMultipleCallbacks (alias for tickCallback,
  // drains the whole queue).
  bool evokeCallback(double approx_min_time = 0.0,
                     double approx_max_time = 0.0);

  // §10.4 — Pitch-name alias for tickCallback. Same semantic as
  // tickCallback. M17.18 (Cut-3).
  bool evokeMultipleCallbacks(double approx_min_time = 0.0,
                              double approx_max_time = 0.0);

  // §10.4 — pause callback dispatch. While disabled, tickCallback /
  // evokeCallback / evokeMultipleCallbacks return immediately
  // without draining; queued events buffer. The background event-
  // stream reader continues to fill the buffer. M17.18 (Cut-3).
  void disableCallbacks();
  // §10.4 — resume callback dispatch.
  void enableCallbacks();

 private:
  std::unique_ptr<RTIambassadorImpl> impl_;
};

}  // namespace rti1516e
