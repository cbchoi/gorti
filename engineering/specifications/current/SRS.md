# Software Requirements Specification

Document version: 4.5
Status: Current
Updated: 2026-10-18

## 1. Purpose

gorti shall provide an open-source Run-Time Infrastructure for interoperable
distributed simulation using the IEEE 1516-2010 High Level Architecture. The
implementation shall make federation behavior, logical-time decisions, and
verification evidence inspectable and reproducible.

## 2. Supported product scope

- A standalone Go `rtid` server for the supported single-node deployment.
- Go and Python federate APIs and the Java `hla.rti1516e` mapping.
- IEEE 1516-2010 FOM parsing and HLA basic-data encoding.
- Federation, declaration, object, time, ownership, data-distribution,
  synchronization, save/restore, and management-object services.
- Optional event journaling and replay through the `auditreplay` plugin.
- Go LocalLRC and confirmed-stream optimizations that preserve ordered
  server processing and callback behavior while exposing distinct local-
  admission and confirmed-completion boundaries.

HLA 1.3, IEEE 1516-2000, formal IVCT certification, and
production multi-node failover are outside the current supported scope.

Per-federation Float64 and Integer64 execution is implemented. The
[requirements inventory](../../verification/requirements2010/README.md) links
reviewed clauses to implementation and tests. The
[4 October service run](../../verification/ieee2010-full-service-rerun-post-sync-20261004.md)
records passing executions for all 639 applicable records and 25 unresolved
interpretations. C++ code is retained as legacy material outside this product
scope.

## 3. Functional requirements

### 3.1 Federation Management

- **FR-FM-1** The RTI shall create and destroy named federation executions.
- **FR-FM-2** A federate shall join with a unique handle and resign using a
  defined resign action.
- **FR-FM-3** Destruction shall fail while members remain and shall remove all
  service state after the final member leaves.
- **FR-FM-4** Synchronization points shall be registered, announced, achieved,
  and completed for the participating set.
- **FR-FM-5** Save and restore shall use a versioned bundle containing FOM
  provenance and supported structured manager snapshots. Event-journal extent
  data may be absent.

### 3.2 Declaration and Object Management

- **FR-DM-1** Federates shall publish and subscribe to object attributes and
  interaction classes defined by the joined federation's FOM.
- **FR-DM-2** Invalid handles and declarations shall be rejected before a send
  is acknowledged.
- **FR-OM-1** Published object instances shall be registered, discovered,
  updated, queried, and removed through the standard lifecycle.
- **FR-OM-2** Interactions shall preserve class, parameters, tag, sender, order,
  transportation, and logical timestamp where applicable.
- **FR-OM-3** Recipient fanout shall be admitted atomically; a confirmed
  synchronous success shall not hide a rejected or dropped required delivery.
- **FR-OM-4** Receive-order and timestamp-order callbacks shall be distinguishable
  and deterministic for the same input sequence.
- **FR-OM-ORDER-1** FOM preferred order shall apply independently of federation
  profile. Explicit attribute-instance and publisher interaction-class overrides
  shall take precedence. Mixed-order attribute updates shall preserve each
  attribute's effective order, and deletion shall use the effective order of
  `HLAprivilegeToDeleteObject`. An untimestamped send shall remain RO; an original
  timestamp retained on an RO callback shall not become its delivery bound.
- **FR-OM-KNOWLEDGE-1** Deletion shall distinguish an unknown instance from a
  known instance whose deletion privilege is not held by the caller. Internal
  MOM cleanup shall not create a public bypass of membership or ownership.

### 3.3 Time Management

- **FR-TM-1** Federates shall enable and disable time regulation and time
  constrained modes with non-negative lookahead.
- **FR-TM-2** NER, TAR, TARA, NMRA, and flush-queue requests shall share a
  consistent pending-advance state machine.
- **FR-TM-3** A federate shall receive exactly one Time Advance Grant for an
  accepted request unless the federation is torn down.
- **FR-TM-4** Eligible timestamp-order callbacks shall become visible before
  the corresponding grant.
- **FR-TM-5** A grant shall not become visible until logical time and pending
  request state have been committed.
- **FR-TM-6** Grant calculation shall account for regulating federates,
  lookahead, pending requests, queued timestamped events, and federation
  generation.
- **FR-TM-FQR-1** FQR shall release all currently applicable queued user messages
  and grant the minimum of the requested time, applicable GALT and TSO timestamps
  actually delivered for that request. A partial delivery shall retain that
  minimum across retry and restore; an unchanged-time grant shall be permitted.
- **FR-TM-ACTIVATION-1** Time-mode activation shall remain pending until its safe
  logical time and enabled callback can be committed. Constrained regulation
  activation shall apply implicit TARA semantics, including timed-save ordering.
- **FR-TM-RETRACT-1** Retraction shall require the original sender, a valid TSO
  designator, current regulation and a timestamp strictly beyond the applicable
  current/requested time plus actual current lookahead. Queue cancellation and
  callback admission shall be atomic; legal deletion retraction shall restore
  original attribute owners and affected recipients' object knowledge.
- **FR-TM-FILTER-1** Eligible RO and TSO messages shall use current subscriptions
  in both DSE settings. Discarded messages shall not set a FQR/NMR/NMRA grant
  target. NMR/NMRA shall not discard later unsafe timestamp groups prematurely.
- **FR-TM-ADVANCE-1** A joined federate shall be permitted to request finite TAR,
  TARA, NMR, NMRA and FQR with neither regulation nor constraint enabled. GALT
  shall not bound an unconstrained requester's advance, and its incoming messages
  shall not select an earlier next-message target. Target, pending-request and
  scheduled-save checks shall remain applicable. Production membership guards
  shall reject unknown or resigned callers before creating time state.
- **FR-TM-TSO-1** A constrained federate in the time-granted state shall retain
  same-current-time TSO even with asynchronous delivery enabled. NMR/NMRA target
  selection shall include queued TSO at current time; required delivery shall
  precede the resulting grant.
- **FR-TM-OUTGOING-1** A regulating sender's pending advance shall use requested
  logical time for the outgoing timestamp constraint; an early grant shall
  release that constraint to the actual granted time. Zero-lookahead strictness
  and effective lookahead remain additional requirements, not exceptions.
- **FR-TM-OUTGOING-2** Final outgoing timestamp validation and delivery admission
  shall be atomic with respect to changes to the sender's time boundary,
  including an empty recipient set. Rejection shall not consume a retraction ID.
- **FR-TM-ZERO-1** Strict zero-lookahead TAR/NMR bounds shall survive a same-time
  Available grant. Save/restore shall retain the history needed to enforce that
  bound, without replacing the logical-time representation's rules with an
  arbitrary comparison tolerance.
- **FR-TM-GALT-1** GALT shall account for other regulating federates' committed
  pending advances, actual lookahead and possible early grants. The querying
  federate shall not directly bound itself. LITS shall additionally account for
  its queued incoming TSO messages. Query services and MOM shall observe a
  coherent time state without publishing an uncommitted admission.
- **FR-TM-RO-1** Reflections, interactions and removals classified as receive
  order shall wait in a separate per-federate queue unless the receiver is
  nonconstrained, advancing or has asynchronous delivery enabled. An original
  timestamp shall remain in the callback without becoming an RO eligibility
  bound. Discovery and control callbacks shall not be held by this gate.
- **FR-TM-RO-2** Every advance mode shall release all still-applicable queued RO
  messages before TAG. A pending advance shall permit RO release even while
  another federate prevents the grant. Release shall recheck current reflection
  and interaction subscriptions; removal shall preserve prior-discovery semantics.
- **FR-TM-RO-3** An accepted backlog exceeding transport capacity shall make
  progress through bounded-prefix release. Failure shall retain unsent messages
  and pending advances; no TAG may overtake required messages. Application-owned
  retries shall not depend on a duplicate advance request or repeat successful
  deliveries, and shall respect checkpoint and federation-generation admission.
- **FR-TM-RO-4** Under transport-capacity pressure, enabling asynchronous
  delivery or disabling constraint shall accept the state change and continue
  release in the background. Other drain errors shall restore the prior flag
  and retain unsent messages. Disabling asynchronous delivery shall not recall
  messages already released.

### 3.4 Ownership, DDM, MOM, and persistence

- **FR-OWN-1** Attribute ownership acquisition, divestiture, cancellation, and
  resign cleanup shall preserve a single authoritative owner.
- **FR-OWN-REPEAT-1** Repeated ordinary acquisition shall preserve an existing
  pending request without duplicate callbacks while processing new attributes.
  If-available acquisition shall reject an already-pending ordinary request.
- **FR-OWN-CONFIRM-1** Confirm Divestiture shall distinguish a confirmation not
  requested from an acquisition no longer pending. The latter shall return the
  affected attributes to waiting without transferring ownership. Save/restore
  shall preserve the state needed to make this distinction.
- **FR-DDM-1** Region and dimension operations shall filter applicable object
  and interaction delivery without changing non-DDM semantics.
- **FR-DDM-RANGE-1** Set Range Bounds shall require
  `0 <= lower < upper <= dimension upper bound`. An invalid request shall not
  replace previously pending or committed bounds and shall report
  `InvalidRangeBound` through the Python service interfaces.
- **FR-DDM-EMPTY-1** An empty regional object-subscription set shall not replace
  existing regional subscriptions or alter their active/passive settings.
  An empty region set for object unsubscription shall likewise leave existing
  subscriptions unchanged after the required validation.
- **FR-MOM-1** Standard management objects shall expose federation and federate
  state through the normal subscription path.
- **FR-MOM-RO-1** RO queue length shall count distinct pending data messages
  across time-waiting and admitted delivery stages until authenticated callback
  entry. It shall exclude TSO and control callbacks and shall not count a message
  twice during a stage transfer, replay or retry.
- **FR-MOM-TSO-1** TSO queue length shall apply the same entry boundary using the
  callback's received order. Missing measurement coverage shall be explicit,
  rather than represented as a successful zero count.
- **FR-MOM-ENTRY-1** Entry-covered incoming runtime and class counters shall
  advance once when a callback is invoked, not at generation-time fanout or
  again at completion. Optional measurement failure shall not block valid HLA
  callback execution. Legacy completion-only observation shall retain its
  explicitly identified boundary and coverage policy.
- **FR-SR-RO-1** Save/restore shall preserve the separate RO queue, original
  optional timestamps, payloads, tags, callback metadata and eligibility state.
  Restore shall validate and rebind typed federate references before replacing
  live queues, then resume delivery after the terminal restore boundary.
  Time-state version 4 shall retain RO queues; versions 1-3 shall remain readable
  without inventing absent state. Unsupported versions shall fail explicitly.
- **FR-SR-ADVANCE-1** Time-state round trips shall preserve a federate's logical
  time when neither time mode is enabled and retain held same-current-time TSO.
  Import shall not itself emit callbacks. Subsequent mode enablement shall not
  reset the restored logical time; this requirement does not establish safe
  engagement-time calculation.
- **FR-EVT-1** The default HLA core profile shall not require audit record
  construction, sequencing, encoding, or storage.
- **FR-EVT-2** The optional audit/replay plugin shall sequence supported
  records with federation-generation identity and close its resources during
  teardown.
- **FR-EVT-3** Plugin availability or recording failure shall not change an
  HLA service result. Replay evidence and event-tail administration shall be
  reported unavailable when the plugin is not loaded.

### 3.5 FOM and encoding

- **FR-FOM-1** The RTI shall parse IEEE 1516-2010 object-model modules and
  reject malformed or incompatible modules with stable diagnostics.
- **FR-FOM-2** Joined modules shall produce deterministic class, attribute,
  interaction, parameter, dimension, and data-type handles.
- **FR-FOM-MIM-1** An explicitly selected MIM shall be retained separately from
  ordinary FOM modules through creation, join, model queries and save/restore.
  Unsupported selections shall fail without publishing partial state. A join's
  explicit MIM expectation shall be checked before FOM extension or membership
  allocation. Clients shall not silently downgrade a rejected selection.
  Explicit creation shall reject the exact reserved designator `HLAstandardMIM`.
- **FR-FOM-FIDELITY-1** Model export and persistence shall retain declared
  transportation metadata, optional descriptions and exact decimal update rates.
  Export shall reject missing required declarations rather than invent values.
- **FR-FOM-FDDMERGE-1** RTI transportation and update-rate combination shall use
  their operational FDD fields. Descriptive differences alone shall not reject
  equal name/reliability or equal name/maximum-rate declarations.
- **FR-ENC-1** Go, Python, and Java encoders shall produce identical bytes for
  the common HLA basic-data types.

## 4. Interface requirements

The remaining-service acceptance scope additionally requires:

- **FR-FM-SESSION-1** Session-scoped automatic resign controls shall require the
  capability issued to that live membership. Rejoin shall rotate the capability;
  restore shall preserve the live incarnation. Disconnection cleanup shall allow
  a reconnection grace period and shall not affect a replacement membership.
- **FR-SR-GENERATION-1** Explicit cross-generation restore shall validate a
  complete saved state graph, pin the target execution, and remap every typed
  federate reference to valid target identities before publication.
- **FR-SR-CALLBACK-1** Saved unentered discovery, data and server-streamed control callbacks shall retain
  their admission order, multiplicity, payload and exact logical time. Restore
  shall reserve fresh receiver capabilities before terminal-first publication;
  executed callback bodies shall not be repeated. Known finished results shall
  be restored as outcome-only reports; already-applied results shall reconcile
  without a second report. Unknown or executing results shall fail before
  opening a save writer or allocating restore identities. Separate ownership
  authority and synchronization obligations shall remain consistent with replay.
- **FR-SR-LOCAL-1** Pending SDK notifications derived from completed RPC results
  shall retain typed immutable payloads, local FIFO order and multiplicity.
  Recovery shall follow the restore terminal without repeating the original
  RPC or creating server invocation authority. Unsupported function closures
  and pending activation work shall reject export rather than disappear.
- **FR-SR-RETENTION-1** Explicitly obsolete, cancellable, unentered advisories
  shall release their invocation reservation and shall not return through
  reconnect or checkpoint replay. A queued completed-result reconciliation
  marker shall survive ordinary completed-receipt eviction and failed sends
  within the configured bounded retention capacity.
- **FR-SR-TRANSPORT-1** Pending transportation confirmations shall restore
  authority-free intent and explicit activation phase under fresh target
  authorization. Validation and capacity reservation shall precede domain
  import; installation shall not reapply an already-activated change. Lost
  attributes shall not return through reacquisition, and unconsumed
  acknowledgement authority shall remain bounded.
- **FR-SR-FAILURE-1** An unexpected terminal publication failure after domain
  import shall not be recorded as successful restoration, log success or resume
  time. The returned error shall distinguish committed state from rollback.
- **IR-SDK-CHECKPOINT-1** Go, Python and Java portable runtime state shall validate
  its version, checksum, source identity and scalar representation before target
  handle reservation. Explicit federate references shall be rebound; object,
  class, region and retraction sequence identifiers shall not be renumbered.
- **FR-MOM-TRANSPORT-1** Per-class sent and received traffic reports shall use
  the actual accepted or invoked transportation type, including Reliable and
  BestEffort. A later override or fanout recipient count shall not change the
  original sender's accepted transport partition.
- **FR-OM-RETRACT-1** Pending timestamped deletion shall remain retractable until
  irreversible removal delivery. A later delivery failure shall not turn an
  already observed removal back into a retractable operation.
- **FR-OWN-DENIAL-1** Ownership-release denial shall retain ownership and reject
  affected pending acquisitions with atomic callback admission.
- **FR-OM-NAMES-1** Multiple-name reservation release shall validate the complete
  request before changing any reservation.
- **IR-SDK-HYDRATE-1** Restored local validation state shall be installed at an
  ordered callback boundary. Old stream tickets shall not alias new operations.
- **IR-SDK-CALLBACK-1** Explicit evoked callback mode shall invoke user callbacks
  only through evoke calls. Immediate delivery shall not execute user callbacks
  on the network I/O loop.
- **NFR-OBS-CALLBACK-1** Actual callback counters shall require authenticated
  invocation reports. Missing coverage shall be reported as unavailable, not zero.
- **NFR-OBS-RESIGN-1** Terminal resign reporting shall identify the actual
  retiring membership and service outcome before its MOM subject is removed.
  Rejected signed requests shall not be attributed to a different membership
  named in the request. Telemetry shall not change the service outcome.

- **IR-WIRE-1** Network contracts are defined by `proto/rti/v1/*.proto` and
  versioned with generated bindings.
- **IR-SDK-1** SDK calls shall report invalid state, handle, membership, and
  transport failures through typed errors or standard exceptions.
- **IR-SDK-2** Synchronous Object Management calls shall return only after the
  configured confirmed boundary succeeds.
- **IR-SDK-3** Optional asynchronous or LocalLRC paths shall provide bounded
  admission, ordered completion, backpressure, and explicit failure reporting.
- **IR-CB-1** Callback delivery shall preserve per-federate order and shall not
  cross a federation-generation boundary.

## 5. Non-functional requirements

- **NFR-DET-1** Identical inputs, FOM bytes, seed, and choreography shall yield
  identical semantic projections.
- **NFR-PERF-1** Performance changes that claim semantic equivalence shall
  retain the same public behavior and report caller, server, and
  completed-delivery boundaries separately. An explicit profile that changes
  persistence or error visibility shall be identified and analyzed separately.
- **NFR-REL-1** Fanout, grant, and teardown failures shall fail closed rather
  than acknowledge incomplete work.
- **NFR-CON-1** Shared state shall be race-free under supported concurrent use.
- **NFR-SEC-1** Production network deployment shall support TLS and documented
  authentication configuration.
- **NFR-OBS-1** Logs and metrics shall identify federation, generation,
  federate, service, operation, result, and timing boundary without exposing
  credentials or proprietary runtime material.
- **NFR-PORT-1** Supported source builds shall work on Linux, macOS, and Windows
  with the documented toolchains.

## 6. Acceptance

A requirement is accepted only when it is traced to implementation and to an
automated test or reproducible scenario in the [STD](STD.md). Cross-RTI
performance evidence is accepted only after semantic equivalence and complete
delivery accounting pass for the measured workload.

`shall` identifies required product behavior. The [IDD](IDD.md) records the
language-specific service interfaces and completion/error boundaries.

Current time-management acceptance includes pending lookahead reductions,
strict timestamp floors, preferred order, all five advance requests, safe
time-mode activation, retraction, and delayed subscription evaluation.
Float64 and Integer64 service paths must retain exact time values through
callbacks and save/restore. The [STD](STD.md) identifies their test locations;
dated execution reports identify the builds on which they passed.
