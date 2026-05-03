# Software Design Document — Go HLA Evolved RTI

Status: draft, locked-by-conversation 2026-04-28.
Trace: this SDD realizes the requirements in `docs/srs.md`. Every component design here cites the FR/NFR/IR IDs it satisfies.
Companion document: `docs/idd.md` — Interface Design Document. The SDD describes structure and dynamics; the IDD specifies the interfaces in detail.

---

## 1. Architectural Overview

### 1.1 Style

Layered, with **explicit interfaces at each layer boundary**. Layers (top to bottom):

1. **Transport** — gRPC server (cut 1) / DDS adapter (cut 2). Receives federate requests, dispatches to core services. Agent A.
2. **Core services** — Federation, Declaration, Object, Time. Stateful, per-federation. Agent A.
3. **Persistence** — Event log (write-ahead, TSO). Agent A.
4. **Data layer** — FOM model + parser, encoding rules. Pure libraries, no state. Agent B.
5. **Federate SDK** (separate process) — Python idiomatic API + standard adapter + pyjevsim bridge. Agent C.

Why layered: each agent owns roughly one band, with clear seams between. The seams are the `core` interfaces in `rti/internal/core/` (frozen by orchestrator).

### 1.2 Component diagram (high level)

```
+-----------------------------------------------------------+
|                  RTI server process (Go)                  |
|                                                           |
|  +------------------+   +------------------+              |
|  |   gRPC Transport |   |  Metrics (HTTP)  |              |
|  +---------+--------+   +------------------+              |
|            |                                              |
|            v                                              |
|  +-------------------------------------------------+      |
|  |              Service router                     |      |
|  |  (FederationSvc, DeclarationSvc, ObjectSvc,     |      |
|  |   TimeSvc, StreamSvc)                           |      |
|  +-+-----------+-----------+-----------+-----------+      |
|    |           |           |           |                  |
|    v           v           v           v                  |
|  +------+  +------+  +--------+  +--------+               |
|  |Feder.|  |Decl. |  |Object  |  |Time    |               |
|  |Mgr   |  |Mgr   |  |Registry|  |Manager |               |
|  +--+---+  +--+---+  +---+----+  +---+----+               |
|     \         \         /            /                    |
|      \         \       /            /                     |
|       v         v     v             v                     |
|       +----------------+     +-------------+              |
|       |   Event Log    |<----| Clock (inj) |              |
|       +-------+--------+     +-------------+              |
|               |                                           |
|               v                                           |
|       +----------------+                                  |
|       |  FOM Repo      |  (uses pkg/fom + pkg/encoding)   |
|       +----------------+                                  |
|                                                           |
+-----------------------------------------------------------+

   Wire (gRPC bidi-stream)            Wire (gRPC bidi-stream)
        ^                                       ^
        |                                       |
+---------------+                       +---------------+
| Python SDK    |                       | Python SDK    |
| + pyjevsim    |                       | + pyjevsim    |
| bridge        |                       | bridge        |
+---------------+                       +---------------+
   Federate 1                              Federate 2
```

### 1.3 Process / deployment model

(Implements C-3, NFR-DEPLOY-1, NFR-DEPLOY-2.)

- One RTI binary, one OS process, one listening port for gRPC, one for metrics.
- Multiple federations served from one process.
- Federate processes are independent; discover the RTI via static URL (`RTI_URL` env / `--rti-url` flag).
- No clustering / hot standby in MVP; RTI death halts every federation.

### 1.4 Concurrency model

(Cross-cuts D-3, NFR-DET-1.)

Three classes of goroutines:

1. **One transport goroutine per federate-connected stream.** Reads inbound messages, posts to the core via channels. Writes outbound messages drained from a per-federate outbox channel.
2. **One serialization goroutine per federation.** Holds the federation's mutable state. Reads from a single inbound channel of `FederationCommand`. This is where determinism is guaranteed: a single goroutine ordering all state-mutating operations, with stable tie-break on equal timestamps.
3. **One event-log writer goroutine per federation.** Receives `Event` values from the federation goroutine over a buffered channel; writes to disk in TSO order, fsyncs per configurable batch.

Why this shape:
- Single-writer per federation = no lock-based ordering ambiguity, easy to reason about, easy to make deterministic.
- Channels (not mutexes) for command handoff = no lock ordering issues, easy to fuzz.
- Backpressure: bounded channels; if a federate overruns, we drop the federate (per NFR-CRASH-1) rather than the federation.

```
        ┌─ stream goroutine (in)  ──────►  cmdCh  ──┐
fed 1 ──┤                                            ├──►  federation goroutine (single writer)
        └─ stream goroutine (out) ◄─────  outCh  ───┘             │
                                                                  ├── eventCh ──► eventlog goroutine
                                                                  │
                                                                  └── outCh of subscriber federates
```

---

## 2. Core Components (Agent A)

### 2.1 FederationManager

Implements: FR-FM-1..5.

- Holds a map `federations: map[FederationName]*Federation`.
- `Federation` holds: name, FOM, federate roster, declaration matrices, object registry, time state, event log writer, command channel, eventlog channel.
- Federate handles assigned monotonically per federation (1, 2, 3...) at join, recorded in event log so replay reproduces them.
- Resign with `UNCONDITIONALLY_DIVEST_ATTRIBUTES`: drop owned objects, remove from declaration matrices, append `FederateResigned` event.

### 2.2 DeclarationManager

Implements: FR-DM-1..3.

Per-federation state:
- `pubObjAttrs: map[ObjectClassHandle]map[FederateHandle]set[AttributeHandle]`
- `subObjAttrs: map[ObjectClassHandle]map[FederateHandle]set[AttributeHandle]`
- `pubInter: map[InteractionClassHandle]set[FederateHandle]`
- `subInter: map[InteractionClassHandle]set[FederateHandle]`

Iteration always sorted by handle (D-2). Subscription matching for an update is O(subscribers); not optimized in MVP.

### 2.3 ObjectRegistry

Implements: FR-OM-1..5.

- `objects: map[ObjectHandle]*ObjectInstance`. Object handles assigned monotonically.
- On `RegisterObject`: append `ObjectRegistered` event, fan-out `Discover` to subscribers in deterministic order.
- On `UpdateAttributeValues`: encode via `pkg/encoding`, append `AttributeUpdated` event with timestamp, route to subscribers (each receives via their `outCh`).
- On `SendInteraction`: append `InteractionSent` event, route to subscribers symmetrically.

### 2.4 TimeManager

Implements: FR-TM-1..6, NFR-DET-1.

State per federation:
- `regulating: map[FederateHandle]Time` — lookahead per regulating federate. Constrained set is a separate `map[FederateHandle]bool`.
- `pendingNER: map[FederateHandle]Time` — outstanding next-message-request times.
- `currentTime: map[FederateHandle]Time` — last granted time per federate.

Algorithm — NER grant cycle:

```
on every state change (NER request, federate join/resign while regulating, etc.):
    LBTS = +Inf
    for h in sortedKeys(regulating):
        LBTS = min(LBTS, currentTime[h] + lookahead[h])
    for h in sortedKeys(pendingNER):
        if currentTime[h] < pendingNER[h] and (not constrained[h] or pendingNER[h] <= LBTS):
            grant(h, pendingNER[h])
            currentTime[h] = pendingNER[h]
            delete(pendingNER, h)
            // append TimeAdvanceGranted event
```

Tie-break per D-4: when multiple federates are eligible at the same grant time, grant in order of federate handle (lowest first), then re-evaluate after each grant (a grant changes `currentTime[h]` and may unlock others).

Stall detection (FR-TM-6): a configurable timer (default 60s wall-clock — *only* used for stall detection, never for ordering) tracks `last_advance_request[h]`. If a regulating federate hasn't requested in the timeout window, halt the federation with a diagnostic naming that federate.

### 2.5 EventLog

Implements: FR-EVT-1..3, NFR-DET-1, NFR-DET-2.

Binary format (one entry):

```
[4-byte BE length][protobuf-encoded Event message]
```

File header (once at file start): `KDRTI\0\1\0` (8 bytes) + `version uint32 BE` (4 bytes) + `federation_name length-prefixed UTF-8`.

`Event` is a Protobuf message with a `oneof` discriminating event types: `FederateJoined`, `FederateResigned`, `ObjectRegistered`, `ObjectDeleted`, `AttributeUpdated`, `InteractionSent`, `TimeAdvanceRequested`, `TimeAdvanceGranted`, `FederationHalted`. Each carries `seq` (monotonic), `wall_time` (informational only, NOT used for ordering), `logical_time` where applicable, and the relevant payload.

Write protocol:
- Write-ahead: every state-mutating command appends an event BEFORE applying it. (Crash mid-apply = on next start, replay rebuilds state up to last fully-applied event.)
- fsync policy: per federation, configurable batch (default every 64 events or 100 ms, whichever first). At federation destroy, final fsync.

Replay protocol:
- Replayer reads events in order, dispatches each through the same code path live operation uses (FederationManager.HandleCommand etc.). The output event log of the replay run must be byte-identical to the input log. Any divergence indicates a determinism violation.

### 2.6 FOMRepository

Implements: FR-FOM-1..4.

A thin wrapper around `pkg/fom`. Holds the parsed, validated, immutable FOM per federation. Looked up at federation create; used by Declaration/Object/Encoding for handle resolution.

---

## 3. Data Layer (Agent B)

### 3.1 FOM Parser

Implements: FR-FOM-1..4.

Pipeline:
1. **Tokenize**: `encoding/xml` decoder; reject unknown elements/attributes (strict mode).
2. **Build raw model**: tree of class/attribute/interaction/parameter declarations + dataType registry.
3. **Resolve**: every dataType reference, every parent class reference. Reject on unresolved.
4. **Validate**: semantic rules (no cycles in class hierarchy; attribute names unique within class including inherited; encoding/order/transportation identifiers valid per spec).
5. **Freeze**: return immutable `model.FOM`.

Each validation rule has a numbered diagnostic (`FOM-001` ... `FOM-NNN`); listed in `docs/idd.md` §1.2.

### 3.2 MIM Embedding

Implements: FR-FOM-2.

`//go:embed standard-mim.xml` and `//go:embed hla-standard-mim.xml`. Source files committed with provenance comment naming the IEEE/SISO publication version. Loaded automatically before any user FOM module; user modules cannot redefine MIM types/classes (rejected with `FOM-101`).

### 3.3 Encoding Rules

Implements: FR-ENC-1..2.

For each HLA Evolved type per IEEE 1516.2-2010 §4:
- A `Codec` implementation: `Encode(v any) ([]byte, error)`, `Decode(b []byte) (any, int, error)`, `OctetBoundary() int`.
- Composite codecs are constructed by `CodecFor(model.DataType) (Codec, error)` recursively.

Padding: every composite type pads to its **largest contained octet boundary** between fields and between elements. This is the most error-prone rule; tests fixture-based.

Cross-language byte equality enforced via `tests/conformance/encoding_vectors.json`. Agent B generates; Agent C must match.

---

## 4. Federate SDK (Agent C)

### 4.1 Layered Python API

Implements: FR-PYJ-*, IR-PYAPI-1.

**Layer 1 (idiomatic)**:

```
RtiConnection.connect(url) -> RtiConnection
RtiConnection.join_federation(spec, federate_name) -> Federation
Federation.publish_object_class(class_name, attributes)
Federation.subscribe_object_class(class_name, attributes)
Federation.publish_interaction_class(class_name)
Federation.subscribe_interaction_class(class_name)
Federation.register_object(class_name, name=None) -> ObjectInstance
ObjectInstance.update_attributes(values, timestamp=None)
Federation.send_interaction(class_name, parameters, timestamp=None)
Federation.next_message_request(time)  # awaits TimeAdvanceGrant
async for event in Federation.events(): ...
```

`async with` context managers for connection and federation lifetimes; clean resign on exit.

**Layer 2 (standard adapter)**: `Rti1516eAmbassador` mirroring the 1516.1 Java/C++ ambassador. Wraps Layer 1 internally. Callback-based, synchronous-feeling. Documented as "for users porting from Pitch/Portico/MAK."

### 4.2 pyjevsim Bridge

Implements: FR-PYJ-1..4.

```
HLAFederate(coupled_model, federation_spec, federate_name, port_mapping)
```

Internal loop (per FR-PYJ-3):

```
join federation
publish/subscribe interactions per port_mapping
loop:
    drain coupled_model.output_handler() — for each output: send_interaction
    ta = coupled_model.time_advance()
    grant_time = await next_message_request(now + ta)
    if grant_time == now + ta:
        coupled_model.internal_transition()
    else:  # external event arrived first
        # interactions arriving at grant_time were already routed to coupled_model
        # via external_transition by the bridge's event handler
        pass
    now = grant_time
on cancellation: resign federation cleanly
```

Simultaneous-event handling: when multiple interactions arrive with identical timestamps, the bridge sorts them by pyjevsim's `select()` rule before delivering to `external_transition` (FR-PYJ-4). The HLA-side TSO tie-break (federate→object→attribute) sees them in pyjevsim order.

### 4.3 Encoding Mirror

Python `pysdk/rti1516e/encoding/` mirrors Agent B's Go encoding rules. Same padding logic, same primitive layouts. Validated by golden vectors: `pytest tests/conformance/test_encoding.py` reads `encoding_vectors.json` and asserts every vector encodes/decodes to the same bytes/values.

---

## 5. Key Cross-Cutting Designs

### 5.1 Deterministic handle assignment

(Implements FR-FM-5, NFR-DET-1.)

- Federate handles: monotonic, assigned by FederationManager goroutine in order of `JoinFederation` command processing. Recorded in event log as `FederateJoined{handle: N, name: ...}`.
- Object handles: monotonic per federation. Recorded.
- Attribute / interaction class / parameter handles: derived from FOM at parse time — for a given FOM module, two parses must produce identical handles. Achieved by traversing FOM in **sorted name order** and assigning monotonically.

### 5.2 Replay-from-log

(Implements FR-EVT-2, NFR-DET-2.)

```
1. Open event log file.
2. Read header; validate magic + version.
3. For each event, dispatch through FederationManager via the same command channel.
4. New event log is opened in write mode at a new path.
5. Output log must be byte-identical to input log (assuming all federates that produced the input were deterministic w.r.t. their inputs).
```

A "replay federate" is a virtual federate the replay runner instantiates to feed back the federate-side commands recorded in the input log.

### 5.3 Two-mode operation

(Implements NFR-PERF-1..4.)

- Mode is a federation-level config: `verbose | best_effort`.
- In `verbose`: every event is logged at `Info`, additionally per-message detail at `Debug`. All attribute updates and interactions delivered TSO regardless of FOM declaration.
- In `best_effort`: log only at `Warn`+ by default (configurable). Attributes/interactions declared as `BestEffort`+`Receive` in the FOM are routed RO (no LBTS gate, no event-log entry per RO message). Attributes declared `Reliable`+`TimeStamp` still go through the deterministic path.

The FOM is the source of truth for per-attribute/per-interaction reliability. Mode acts as a *master switch* that forces all to TSO-reliable in verbose, and respects FOM declarations in best-effort.

### 5.4 Stall and crash semantics

- **Federate stall**: regulating federate exceeds the configured timeout (default 60s) without a NER request → halt federation with `FederationHalted{cause: stall, federate: H}`. NFR-CRASH-1.
- **Federate crash** (gRPC stream broken): treated as `UNCONDITIONALLY_DIVEST_ATTRIBUTES` resign. Federation continues. (Cut-1 simplification: if the crashed federate was regulating and other federates needed it for LBTS, they may stall — caught by stall detection.)
- **RTI crash**: process exits. Event log is fsync'd at last batch boundary. Manual recovery: replay the event log on next start.

### 5.5 Observability

(Implements NFR-OPS-1..3.)

- **Logs**: `log/slog` JSON. Always: `federation`, `seq`, `phase`. When known: `federate_handle`, `object_handle`, `class`.
- **Metrics** (Prometheus, namespace `rti_`):
  - Counters: `messages_total{type=...}`, `events_logged_total{type=...}`, `federations_created_total`.
  - Histograms: `advance_grant_latency_seconds`, `message_size_bytes{type=...}`, `event_log_batch_seconds`.
  - Gauges: `federations`, `federates{federation=...}`, `objects{federation=...,class=...}`.
- **Tracing**: not in MVP.

---

## 6. Algorithms (Pseudocode)

### 6.1 LBTS calculation

(See §2.4. Restated for clarity.)

```
function compute_LBTS(state):
    lbts = +Inf
    for h in sorted(state.regulating.keys()):
        lbts = min(lbts, state.current_time[h] + state.lookahead[h])
    return lbts
```

### 6.2 Granting NER requests

```
function try_grant(state):
    progressed = true
    while progressed:
        progressed = false
        lbts = compute_LBTS(state)
        for h in sorted(state.pending_NER.keys()):
            requested = state.pending_NER[h]
            constrained = state.constrained[h]
            if state.current_time[h] >= requested:
                continue
            if constrained and requested > lbts:
                continue
            grant_time = min(requested, lbts) if constrained else requested
            // Look for any earlier-timestamped pending message for h.
            // (Implementation: peek inbox for h, take min.)
            grant_time = min(grant_time, peek_min_inbox_ts(h))
            emit TimeAdvanceGranted{federate: h, time: grant_time}
            state.current_time[h] = grant_time
            del state.pending_NER[h]
            progressed = true
            // recompute LBTS — h's contribution may have changed
            lbts = compute_LBTS(state)
```

This terminates: each iteration either grants someone (decreasing pending_NER size) or makes no progress and exits.

### 6.3 Subscription matching for an update

```
function fan_out_update(fed, obj, attrs, ts):
    cls = obj.class
    for ancestor_cls in cls.ancestor_chain():
        for sub_handle in sorted(fed.declaration.sub_obj_attrs[ancestor_cls].keys()):
            sub_attrs = fed.declaration.sub_obj_attrs[ancestor_cls][sub_handle]
            matched = attrs.intersect(sub_attrs)
            if matched is empty: continue
            outbox(sub_handle).send(ReflectAttributeValues{obj, matched, ts})
```

Iteration over `ancestor_chain` is in subclass-to-superclass order (HLA semantics: a publisher of `Vehicle.Car` matches subscribers to `Vehicle` and `Vehicle.Car`).

---

## 7. Configuration

(See `docs/idd.md` §1.5 for the exhaustive flag list.)

Sources, in precedence order: command-line flags > env vars > config file > built-in defaults.

Selected critical configs:

- `--listen` (env `RTI_LISTEN`): gRPC server address. Default `:8442`.
- `--metrics-listen` (env `RTI_METRICS_LISTEN`): Prometheus HTTP. Default `:9090`.
- `--mode` (per federation, in create request): `verbose` | `best_effort`. No global default; must be specified at federation create.
- `--stall-timeout` (per federation, in create request): default `60s`.
- `--eventlog-dir` (env `RTI_EVENTLOG_DIR`): path. Default `./eventlogs/`.
- `--seed`: optional override for deterministic RNG. Default = hash(federation name + creation timestamp).

---

## 8. Out of Scope (Reaffirmed; see SRS §9)

This SDD does NOT design:

- Ownership Management
- Data Distribution Management
- Save/Restore protocol
- Full MOM (only read-only federate list in MVP)
- Optimistic time advance
- DDS adapter (cut 2)
- mTLS / authn / authz
- Distributed RTI / hot standby

Each of these will get its own SDD addendum when its cut is scheduled.

---

## 9. Save/Restore Bundle Format (M9 W1 — FR-SR-4)

This addendum documents the on-disk format of the federation save bundle
written by `rti/internal/savepoint.Manager`. The format is intentionally
trivial at cut-1 to maximize debuggability; a future cut may wrap the
concatenation in tar.gz per FR-SR-4's "sealed bundle" wording.

### 9.1 Layout

A single bundle file is the concatenation of:

```
[ 8 bytes ] uint64 manifestLen   (little-endian)
[ N bytes ] JSON manifest        (length = manifestLen)
[ 8 bytes ] uint64 eventLogLen   (little-endian; matches manifest.event_log_bytes)
[ M bytes ] raw event-log slice  (length = eventLogLen; may be 0)
```

The two 8-byte length prefixes frame the regions so `ReadBundle` can
stream them without seeking. `eventLogLen` MUST match the manifest's
recorded `event_log_bytes` field; mismatch is treated as
`core.ErrSaveBundleCorrupt`.

### 9.2 Manifest schema (cut-1, version = 1)

```json
{
  "version":         1,
  "federation":      "fed-name",
  "label":           "save-label",
  "save_time":       42.5,                 // optional, omitted when nil
  "federates":       [1, 2, 3],            // sorted; deterministic restore order
  "event_log_bytes": 0
}
```

- `version` — bundle format version; bumped when the layout changes
  incompatibly. `ReadBundle` rejects non-matching versions with
  `core.ErrSaveBundleCorrupt`.
- `federation` / `label` — identity tuple; matches the
  `(fed, label)` key the bundle was filed under in `Storage`.
- `save_time` — optional logical time the save was pinned at (FR-SR-1).
  `nil` means "save at current synchronization point".
- `federates` — the federate-handle list captured at save-request time,
  sorted ascending. Drives the deterministic
  `initiateFederateRestore` broadcast order on restore.
- `event_log_bytes` — byte length of the slice that follows. Cut-1
  default is 0 (see deferral below).

### 9.3 Cut-1 manifest scope

At cut-1 the manifest carries only the federation identity + federate
list. Per-manager state snapshots (declarations, ownership, sync
points, MOM, DDM) are **deferred to M9 W2**. The deferral is safe
because FR-SR-5 byte-determinism is delivered through the event-log
slice: replaying the slice through a fresh RTI reconstructs every
manager's state via the same write-ahead path the original federation
took (the same machinery as M2/M3 NFR-DET-2 replay).

### 9.4 Cut-1 event-log slice

The cut-1 manager records `event_log_bytes = 0` because the
record-oriented `core.EventLogReader` interface does not expose raw
file bytes. The on-disk per-federation `.log` file remains the source
of truth for replay; the restore path consults the same
`MultiplexWriter` and can `OpenReader` the live log to drive replay.
A follow-up patch in M9 W2 fills the slice in-bundle so saves are
self-contained (relevant for off-machine archive transport); the
framing is already in place so the layout will not change.

### 9.5 Filesystem layout

The default `FSStorage` writes one file per `(fed, label)` under
`--save-dir` (default `./gorti-saves`):

```
<save-dir>/<fed>__<label>.bundle
```

Federation/label characters that would break filenames on Windows
(`/\:*?"<>|` and ASCII control chars) are percent-escaped at the
filename layer. The escaping is one-way (the manifest carries the
canonical strings) and trivially preserves uniqueness for any printable
ASCII input.

No locking; `FSStorage` assumes a single-writer rtid process per
`--save-dir`. Multi-writer coordination (e.g. via `flock` or atomic
rename) is a follow-up if/when the production deployment story
includes hot-standby rtid replicas.
