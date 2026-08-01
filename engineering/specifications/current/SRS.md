# Software Requirements Specification

Document version: 4.0
Status: Current
Updated: 2026-07-23

## 1. Purpose

gorti shall provide an open-source Run-Time Infrastructure for interoperable
distributed simulation using the IEEE 1516-2010 High Level Architecture. The
implementation shall make federation behavior, logical-time decisions, and
verification evidence inspectable and reproducible.

## 2. Supported product scope

- A standalone Go `rtid` server for the supported single-node deployment.
- Go, Python, and C++ federate APIs.
- IEEE 1516-2010 FOM parsing and HLA basic-data encoding.
- Federation, declaration, object, time, ownership, data-distribution,
  synchronization, save/restore, and management-object services.
- Deterministic, bounded event journaling and replay-oriented diagnostics.
- Same-host LocalLRC and confirmed-stream optimizations that preserve ordered
  server processing and callback behavior while exposing distinct local-
  admission and confirmed-completion boundaries.

HLA 1.3, IEEE 1516-2000, a Java federate SDK, formal IVCT certification, and
production multi-node failover are outside the current supported scope.

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

### 3.4 Ownership, DDM, MOM, and persistence

- **FR-OWN-1** Attribute ownership acquisition, divestiture, cancellation, and
  resign cleanup shall preserve a single authoritative owner.
- **FR-DDM-1** Region and dimension operations shall filter applicable object
  and interaction delivery without changing non-DDM semantics.
- **FR-MOM-1** Standard management objects shall expose federation and federate
  state through the normal subscription path.
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
- **FR-ENC-1** Go, Python, and C++ encoders shall produce identical bytes for
  the common HLA basic-data types.

## 4. Interface requirements

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

`shall` identifies required product behavior, not a claim that every language
profile already satisfies every requirement. The current [IDD](IDD.md) records
known interface limitations and non-uniform completion or error boundaries.
Such gaps remain open until executable evidence satisfies this acceptance rule.
