# M19 — DDS / RTPS data plane adapter

Status: Phase 0 — design only, no code changes yet. Pending decisions
are marked **OPEN** and should be pinned before Phase 1 starts.

This document proposes a DDS / RTPS data plane for gorti as an
opt-in alternative to the gRPC streaming data plane. Per
`docs/srs.md` §10.4, the goal is: federates can route the data
plane (object updates + interactions) through DDS topics while the
control plane (federation lifecycle, declarations, time, sync,
ownership, save/restore, MOM) stays on gRPC.

Audience: federation operators who need the throughput / discovery /
distribution properties DDS provides; researchers studying transport
algorithms in HLA contexts; future M19 implementer agents.

---

## 1. Goal & non-goals

### Goal

Add a data-plane transport that:

1. Sends object-attribute updates and interactions over DDS topics
   instead of gRPC server streams
2. Preserves the existing IEEE 1516-2010 service contract — every
   federate API behaves identically; only the wire path differs
3. Coexists with the gRPC data plane (federate-side opt-in via
   config + per-federation negotiation)
4. Maps the `HLAreliable` / `HLAbestEffort` transportation +
   `TimeStamp` / `Receive` order types onto DDS QoS policies
5. Keeps the rtid daemon as the **control plane** authority
   (federations, declarations, MOM, sync, ownership coordination,
   save/restore) but moves data-plane fan-out into DDS

### Non-goals

- **Wire-level interoperability with commercial RTIs** (Pitch, MAK,
  Portico). Per `docs/srs.md` §1: "out of scope, forever, or far
  future". gorti's DDS layer defines its own topic naming +
  payload encoding; we do not implement IEEE 1516-3-compliant DDS
  bindings.
- **Replacing gRPC entirely.** Control plane stays gRPC. The
  research platform's strategy interfaces, the rtid-TUI's
  AdminService + MutatingService, the cut-2 service-group RPCs all
  stay where they are.
- **Distributed federation directory.** rtid remains the
  authoritative federation registry. DDS is just the data plane
  carrier.
- **Hot transport-switching mid-federation.** A federation either
  uses DDS for data plane or gRPC. The choice is pinned at federation
  creation; federates joining the federation inherit it. No mixed
  data-plane within one federation in Phase 1.
- **Replacing the Outbox machinery for gRPC federates.** The
  cut-3 perf-optimised batched-channel `multiOutbox` (commits
  e01a6d3 / 890e18a / a191bfd / b8489f3 / 7ca819d) keeps serving
  gRPC-mode federations unchanged. DDS-mode federations don't
  use multiOutbox.

---

## 2. Architecture

### 2.1 Two transport modes side by side

| Mode | Control plane | Data plane | Federate sees |
|---|---|---|---|
| `gRPC` (today; default) | gRPC RPCs to rtid | gRPC bidi streams to rtid + multiOutbox fan-out | One TCP connection per federate to rtid |
| `DDS` (this milestone) | gRPC RPCs to rtid (unchanged) | DDS DomainParticipant per federate; DataWriters publish to topics; DataReaders subscribe | One control connection to rtid + DDS multicast/discovery on the data plane |

The mode is a per-federation property recorded in the federation
record at create time. Federates see it via a new
`CreateFederationRequest.transport_mode` field and the
`JoinFederationResponse` echoes it. The federate's SDK transparently
selects the right transport.

### 2.2 Data-plane responsibility

In gRPC mode, rtid mediates every data-plane delivery:

```text
Federate A -- send_interaction --> rtid --> multiOutbox --> Federate B's stream
```

In DDS mode, rtid does **not** see data-plane traffic. Federates
publish + subscribe directly to DDS topics; rtid just records the
declared pub/sub via `declaration.Manager` so MOM counters and the
rtid-TUI's snapshot can still see what's published. (rtid does
**not** join the DDS domain itself in Phase 1; this is Model B from
§3 below.)

```text
Federate A -- DataWriter.write(GenToBuffer) --> DDS topic --> Federate B's DataReader
```

### 2.3 Topic naming

OPEN decision (§7.3); proposed default convention:

```
gorti/<federation_name>/interaction/<interaction_class_handle>
gorti/<federation_name>/object/<object_class_handle>/<attribute_handle>
```

- One topic per `(federation, interaction class)` for
  ReceiveInteraction
- One topic per `(federation, object class, attribute)` for
  ReflectAttributeValues
- Topic name uses HANDLES (uint64), not class NAMES, so renames in
  the FOM don't break the wire path
- Federation name in the prefix scopes a DDS domain to multiple
  gorti federations safely

Each topic carries a single DDS type derived from the proto
`ReceiveInteraction` / `ReflectAttributeValues` messages — IDL
generated from a thin `.idl` shim, OR (proposal) reuse the existing
proto bytes as opaque DDS payloads. The latter is much simpler to
implement and still gives DDS QoS + discovery; it costs us
DDS-native types but those don't matter for gorti's purposes.

### 2.4 QoS profile mapping

| HLA semantic | DDS QoS |
|---|---|
| `HLAreliable` transportation | `RELIABLE` reliability + `KEEP_ALL` history + `MAX_BLOCKING_TIME` configurable |
| `HLAbestEffort` transportation | `BEST_EFFORT` reliability + `KEEP_LAST(1)` history |
| `TimeStamp` order | DDS `BY_SOURCE_TIMESTAMP` destination order; timestamp carried in the payload (DDS clock isn't HLA's logical clock) |
| `Receive` order | DDS `BY_RECEPTION_TIMESTAMP` destination order |
| Discovery | DDS native participant + endpoint discovery (no gorti work) |

Phase 1 ships only the four core QoS combinations
(`HLAreliable+TimeStamp`, `HLAreliable+Receive`,
`HLAbestEffort+TimeStamp`, `HLAbestEffort+Receive`). Other QoS
properties (history depth tuning, deadlines, ownership strength)
are deferred to Phase 3.

### 2.5 Discovery + federate identity

DDS has native participant discovery — every federate's
DomainParticipant announces itself. We layer gorti's federate
identity on top:

- DomainParticipant user data carries `(federation, federate_handle,
  federate_name)` as a JSON-encoded string
- rtid is NOT a DDS participant in Phase 1; federate-to-federate
  discovery happens entirely within the DDS domain
- The federation's `transport_mode == DDS` tag tells SDK clients
  "ignore the gRPC stream; subscribe to DDS topics"

---

## 3. Library + integration choices

### 3.1 DDS library — OPEN

| Option | Pros | Cons |
|---|---|---|
| **Cyclone DDS** (Eclipse Foundation) | Most-used OSS DDS; mature; mainstream Python bindings (`cyclonedds-python`); EPL-2.0 license | C library; needs system install or Docker layer |
| **OpenDDS** (OCI) | Stable; more control over wire format; BSD license | Heavier C++ runtime; smaller Python binding ecosystem |
| **Fast-DDS** (eProsima) | Modern; Apache-2.0; ROS2 default | Smaller Python binding ecosystem; relatively newer |
| **RTI Connext** | Most mature; commercial-grade tooling | Commercial license — disqualifies for OSS by default |

**Recommendation**: **Cyclone DDS**. Largest OSS HLA-adjacent
ecosystem; first-party Python bindings; standard apt-get on
Ubuntu/Debian; Eclipse-licensed.

### 3.2 Go binding — OPEN

| Option | State |
|---|---|
| `github.com/eclipse-cyclonedds/cyclonedds` Go bindings | Deprecated as of upstream's recent reorganisation |
| `github.com/madara-engineering/godds-cyclonedds` | Unmaintained but functional |
| Hand-rolled CGo bindings against `<dds/dds.h>` | Most work but full control + future-proof |

**Recommendation**: **Hand-rolled minimal CGo bindings** for the
small subset of Cyclone DDS we need (DomainParticipant create,
Topic create, DataWriter/DataReader create, write/take).
~300-500 lines of C interop code. Maintainable; deps survive
upstream churn.

### 3.3 Python binding — PROPOSED PIN

`cyclonedds-python` (`pip install cyclonedds`) — first-party,
maintained, supports the full Cyclone DDS API. No alternative
worth considering. Mark this PINNED if §3.1 lands at Cyclone DDS.

### 3.4 Mixed gRPC+DDS in one federation — OPEN

Do we allow a federation to have some federates using gRPC for the
data plane and others using DDS within the same federation?

| Option | Pros | Cons |
|---|---|---|
| **Pure-mode federations** (proposal) | Simpler; predictable performance; each federation's wire path is uniform | Migration friction — to switch a production federation to DDS, every federate has to rebuild |
| **Mixed federations** | Smooth migration story; rtid bridges between the two transports | rtid joins DDS domain (defeats the "rtid not on data plane" goal of M19); two transport paths run concurrently; harder to debug; weakens the determinism story |

**Recommendation**: **Pure-mode federations Phase 1**. Mixed-mode
is Phase 4+ if real demand emerges. The migration story is "create
a parallel federation in DDS mode and swap consumers".

### 3.5 Build / distribution — OPEN

| Question | Default proposal |
|---|---|
| Build dependency | Cyclone DDS C library + headers required at build time. `apt install libcdds-dev` on Debian/Ubuntu; `brew install cyclonedds` on macOS; vendored build from source on others |
| CGo presence in main `rtid` binary | NO — DDS support lives in a build-tagged subpackage (`go build -tags dds`). Default `rtid` binary is CGo-free + DDS-free |
| `rti-top` / docs site / examples | Unaffected. They use the gRPC plane regardless |
| CI | Add a separate `ci-dds` target that runs the DDS-tagged tests when the runner has Cyclone DDS available |
| Distribution | Two binaries: `rtid` (default, gRPC-only, CGo-free) and `rtid-dds` (DDS-capable). Federation operators pick at deploy time |

This keeps the no-DDS-toolchain user experience exactly as today
and isolates the C interop blast radius behind a build tag.

---

## 4. Plumbing surface

### 4.1 Proto changes (Phase 1)

Append-only:

- `CreateFederationRequest.transport_mode` (new optional enum
  `TransportMode { TRANSPORT_MODE_UNSPECIFIED=0, GRPC=1, DDS=2 }`;
  unspecified → GRPC for backward-compat)
- `JoinFederationResponse.transport_mode` (echoes the federation's
  mode + a `dds_domain_id` int32 if mode is DDS)
- `Federation.dds_domain_id` recorded in the federation manager's
  state

No other proto changes in Phase 1. The
`ObjectService.UpdateAttributeValues` / `SendInteraction` RPCs
still exist; in DDS mode they're simply not invoked (the SDK
publishes to DDS instead).

### 4.2 Go-side packages

```
rti/internal/transport/dds/   (NEW; build-tag: dds)
  doc.go
  participant.go    DomainParticipant lifecycle
  topic.go          topic naming + creation
  writer.go         DataWriter wrapping
  reader.go         DataReader + take loop
  qos.go            HLA → DDS QoS mapping
  cgo_dds.go        thin CGo wrapper around <dds/dds.h>
  smoke_test.go     end-to-end with Cyclone DDS available
```

`object.Registry` and `multiOutbox` are unchanged — when a
federation is in DDS mode, the registry's fanout calls are simply
no-ops (the SDK publishes directly; rtid doesn't see data-plane
traffic).

### 4.3 Python-side

```
pysdk/rti1516e/_dds_transport.py   (NEW)
  Mirrors the existing _transport.py + _inprocess.py contract.
  Selected automatically when the JoinFederationResponse reports
  transport_mode == DDS.
```

The bridge (`pyjevsim_bridge`) is unchanged — federates that use
the bridge inherit DDS automatically when the federation is in DDS
mode. Same `CoupledModelProtocol`, same `step_once`, just a
different wire underneath.

### 4.4 cmd/rtid changes

- New flag `--dds-domain-id` (int) sets the default DDS domain ID
  for federations created in DDS mode. Default 0.
- New flag `--enable-dds` (bool) — when false (default), rtid
  rejects `CreateFederation` requests with `transport_mode = DDS`
  with a clear "this rtid build was not compiled with DDS support"
  error. When true, federations can be created in either mode.
- The build-tag-gated `rtid-dds` binary has `--enable-dds=true` as
  default; the no-DDS `rtid` binary has it forced false at compile
  time.

---

## 5. Phasing

| Phase | Deliverable | Risk | Effort |
|---|---|---|---|
| 0 | This doc + decisions pinned | none | DONE pending review |
| 1 | Hand-rolled minimal CGo bindings to Cyclone DDS + proto extensions + DDS-mode plumbing in cmd/rtid (build-tagged) + Go-side smoke test that publishes one interaction to a topic and reads it back | high (first CGo work in the codebase + first system dep beyond Go toolchain + first build-tag separation) | ~weeks |
| 2 | Object-class fan-out + QoS mapping for the four core HLA combos + Python-side `_dds_transport.py` | medium-high | ~weeks |
| 3 | Cross-language M5/M12-shape conformance tests against DDS; mixed cgo-build CI; documented migration recipe | medium | ~week |
| 4 | (Optional) Mixed-mode federations: rtid joins DDS domain as bridge participant; gRPC + DDS federates coexist | high | ~weeks |
| 5 | (Optional) Tunable QoS beyond the four core combos: per-class deadlines, ownership strength, history depth, durability | low (additive) | rolling |

Each phase is independently shippable. Phase 1 alone gives "you
can run a DDS federation with one type of interaction, end-to-end,
under a build tag" — enough to validate the architecture and decide
whether to invest in Phase 2.

---

## 6. Decisions

1. **DDS library (§3.1)**: PINNED 2026-05-06 — **Cyclone DDS**.
   Eclipse Foundation; mainstream Python bindings; standard
   `apt-get install libcyclonedds-dev`; EPL-2.0 license.
2. **Go binding strategy (§3.2)**: PINNED 2026-05-06 — **hand-rolled
   minimal CGo** for the four primitives (DomainParticipant +
   Topic + DataWriter/DataReader create + write/take). ~500 LoC of
   C interop; insulated from upstream binding churn.
3. **Python binding (§3.3)**: PINNED 2026-05-06 (consequential on
   §6.1) — **`cyclonedds-python`**.
4. **Mixed federations (§3.4)**: OPEN. Default: **pure-mode
   Phase 1**, mixed-mode Phase 4+ if needed. Phase-2/3 setting.
5. **Build / distribution (§3.5)**: PINNED 2026-05-06 —
   **build-tag-gated subpackage**. Default `rtid` binary stays
   CGo-free + DDS-free; `rtid-dds` is the DDS-capable variant.
   `make build` (no DDS toolchain needed) produces today's binary
   unchanged; `make build-dds` opts in.
6. **Topic naming (§2.3)**: OPEN. Default: **handle-based +
   proto-bytes**. Phase-2/3 setting (Phase 1 ships one topic of
   either shape).
7. **Determinism contract**: OPEN. Default: **per-impl opt-in**
   matching research-platform §3.2. Replay tests SKIP in DDS-mode
   with clear reason.
8. **Federation transport mode at create time** vs. **at federate
   join time**: OPEN. Default: **federation-wide** — pinned at
   create time, federates inherit.

§6.1 + §6.2 + §6.5 are now pinned. Phase 1 is dispatchable. §6.4 /
§6.6 / §6.7 / §6.8 affect Phase 2-3 and can be pinned later.

---

## 7. What this is NOT

To prevent the same scope creep §3.4 references:

- Not a replacement for gRPC. Control plane stays gRPC forever.
- Not an IEEE 1516-3 DDS-bindings implementation. gorti's DDS is
  its own wire layer; spec interop is explicitly out of scope.
- Not a federation directory. rtid still owns the federation
  registry, declarations, MOM, sync, ownership, save/restore.
- Not a discovery-only adapter. Federates use DDS native discovery
  for endpoint lookup, but rtid still authorises join.
- Not a mixed-RTI bridge. gorti DDS federations don't talk to
  Pitch / MAK / Portico DDS federations.
- Not a multicast-network-engineering doc. DDS over multicast
  requires sysadmin work (IGMP, network MTU, etc.) that's
  deployment-specific. We document defaults; we don't enforce
  network topology.

---

## 8. Risks + mitigations

| Risk | Likelihood | Mitigation |
|---|---|---|
| Cyclone DDS API shifts between versions | medium | Pin to a major version in `go.mod` indirect deps + Cyclone DDS apt package. Document the supported version range. |
| CGo build complexity discourages contributors | high | Build-tag gated. Default `rtid` is CGo-free. Contributors who don't touch DDS never learn about it. |
| Multicast network configuration breaks federations on certain CI / cloud / corporate networks | medium | Default to DDS unicast discovery; document the multicast tuning recipe but don't make it default |
| QoS mismatch between gorti's HLA semantics and DDS's policies surfaces as subtle ordering bugs | high | Phase 2 ships exhaustive cross-language conformance tests; strict per-QoS-combo test matrix |
| The replay-determinism guarantee silently degrades for DDS-mode federations | high | Make determinism non-default in DDS mode (research-platform per-impl-opt-in pattern); replay tests SKIP not FAIL |
| Distribution + CI complexity (two binaries, system DDS dep) chases maintainers away | medium | Document the matrix clearly; default `make build` / `make test` produces no-DDS artifacts; explicit `make build-dds` opt-in |
| The first CGo introduction is bug-prone | high | Hand-rolled bindings stay minimal (~500 LoC, just enough for the four primitives). All higher-level logic in Go. Code review the C interop carefully. |

---

## 9. Success criteria for Phase 1

Phase 1 ships when ALL of these hold:

1. `make build-dds` produces `bin/rtid-dds` with Cyclone DDS linked;
   `make build` (default) produces `bin/rtid` unchanged from today
2. A federation created with `transport_mode = DDS` accepts joins
   and records the mode
3. A federate that publishes an interaction class via gRPC and a
   second federate that subscribes via gRPC receive each other's
   interactions END-TO-END through a DDS topic, NOT through rtid
4. The Go conformance test `rti/spec/M19/dds_smoke_test.go` runs
   under `go test -tags=dds` against a local Cyclone DDS install
   and passes
5. `rtid` (no-DDS) still passes every existing test in this repo
6. Determinism + replay tests SKIP in DDS mode with a clear reason
   in their output, NOT FAIL

Phase 1 does NOT need to ship the Python-side DDS adapter — that's
Phase 2. Phase 1 is purely the Go-side architecture verification.

---

## 10. Open questions for the user before Phase 1

Items that need YOUR call (not the agent's) before Phase 1 work
begins:

1. **§6.1** library: Cyclone DDS vs other? **Default: Cyclone DDS.**
2. **§6.2** Go binding: hand-roll CGo vs adopt an existing wrapper?
   **Default: hand-roll minimal CGo (~500 LoC).**
3. **§6.5** distribution: build-tag-gated two-binary split, or
   always-on CGo? **Default: build-tag gated.**
4. **§6.4** mixed-mode federations: defer to Phase 4+, or want it
   in Phase 1? **Default: defer.**
5. **§6.6** payload format: handle-based topic names + proto bytes
   on the wire, or DDS IDL types? **Default: handle-based +
   proto bytes.**
6. **§6.7** determinism contract: replay tests skip in DDS mode?
   Confirm or override. **Default: skip with reason.**

Once 1, 2, 3 are pinned, Phase 1 dispatches. Items 4-6 can be
pinned at any point but affect Phase 2-3 specifically.

---

## 11. Effort & scope honesty

Phase 1 alone is 80–120 hours of focused engineering work split
across several agent dispatches. **It cannot be completed in one
agent session.** The first CGo introduction in this codebase is
inherently risky and needs careful review.

Phase 0 (this document) closes the design step. Phase 1+ requires
a build environment with Cyclone DDS available — **that is not the
case in the current sandbox**. The orchestrator should ensure
either:

- A development environment with `apt install libcdds-dev` (Linux)
  or `brew install cyclonedds` (macOS), OR
- A Docker layer that bundles Cyclone DDS for the agent's build

before dispatching Phase 1. Otherwise the first build will fail
on `<dds/dds.h>: file not found`.

This is a different shape of work from the Phase-1-of-anything-
in-this-session-arc dispatches. It needs deliberate environment
setup before code can land.

### 11.1 Phase 1a — foundation (LANDED)

The first M19 dispatch shipped the no-CGo foundation so the rest of
the work can land incrementally without holding the rest of the
codebase up on a Cyclone DDS apt-package availability gate.

**What landed:**

- Proto extensions (`CreateFederationRequest.transport_mode`,
  `JoinFederationResponse.transport_mode`, `JoinFederationResponse.dds_domain_id`,
  `FederationSnapshot.transport_mode`, `FederationSnapshot.dds_domain_id`);
  append-only — old federates connecting to new rtid land in the
  GRPC code path; new federates connecting to old rtid see empty
  defaults and fall back to GRPC
- New enum `TransportMode { UNSPECIFIED=0, GRPC=1, DDS=2 }`
- `core.TransportMode` mirror + `federation.Manager.TransportFor()`
  + `Snapshot()` carries the per-federation transport
- `cmd/rtid` flags `--enable-dds` (default false) +
  `--dds-domain-id` (default 0); the flag exists in EVERY rtid
  build but its EFFECTIVE behavior depends on `-tags=dds`
- `transport/grpc` `CreateFederation` rejects `transport_mode=DDS`
  with `FailedPrecondition + "this rtid was not built with DDS
  support"` when `--enable-dds` is false
- `rti/internal/transport/dds/` package skeleton:
  - `doc.go` — package docs, no build tag
  - `qos.go` — pure-Go HLA→DDS QoS mapping (real code, callable
    from Phase 1b's CGo)
  - `participant.go`, `topic.go`, `writer.go`, `reader.go` — all
    `//go:build dds`, all stubs returning `errors.ErrUnsupported`
- Stub-contract tests under `//go:build dds` document the Phase 1a
  contract; Phase 1b's first failing assertion will signal the C
  interop has landed
- `rti-top` drilldown header surfaces `transport: gRPC | DDS
  (domain N)`
- `make build-dds` + `make test-dds` Make targets

**Verified:**

- `go build ./...` (default) clean, byte-identical binary size
  versus `main`
- `go build -tags=dds ./...` clean (no Cyclone DDS dependency yet)
- `go test ./...` + `go test -tags=dds ./...` both green
- `go test -tags=dds_e2e ./...` reports `SKIPPED` for the
  Phase 1b placeholder

### 11.2 Phase 1b — CGo implementation (TODO, blocked on libcyclonedds-dev)

The next M19 dispatch lands the actual CGo bindings under
`rti/internal/transport/dds/cgo_dds.go`. Required for the dispatch
to succeed:

1. **Build environment**: `apt install libcyclonedds-dev` (Linux) or
   `brew install cyclonedds` (macOS) on the runner. Without it,
   `cgo_dds.go` won't compile.
2. **Drop in `cgo_dds.go`** with `import "C"` referencing
   `<dds/dds.h>` and the four primitives:
   - `dds_create_participant(domain_id, NULL, NULL)` →
     `defaultParticipant.dds_entity_t`
   - `dds_create_topic(participant, &type_descriptor, name, qos,
     NULL)` → `defaultTopic.dds_entity_t`
   - `dds_create_writer(publisher, topic, qos, NULL)` →
     `defaultWriter.dds_entity_t`
   - `dds_create_reader(subscriber, topic, qos, NULL)` →
     `defaultReader.dds_entity_t`
3. **Replace stub bodies** in `participant.go` / `topic.go` /
   `writer.go` / `reader.go` with calls into the CGo helpers. The
   interface contract stays unchanged — Phase 1a's stub-contract
   tests will start failing (signal that real lifecycle has
   landed) and need rewriting as real lifecycle tests.
4. **Plumb `FromHLA`** into `dds_qos_create()` plus the
   `dds_qset_reliability` / `dds_qset_history` /
   `dds_qset_destination_order` setters. The QoS mapping itself
   is already locked by Phase 1a's `qos_test.go` so the C glue
   only needs to translate value objects, not re-derive the
   mapping.
5. **Wire `defaultParticipant.Join`** to the federation runtime
   (Phase 2 — `cmd/rtid` constructs a participant per DDS-mode
   federation; in Phase 1b the test scaffold creates one directly).
6. **Land the end-to-end smoke test** (`rti/spec/M19/dds_smoke_test.go`,
   build tag `dds_e2e`) that verifies federate-to-federate samples
   flow through DDS without rtid in the data path.
7. **Replay-determinism gate**: M3/M4 byte-identical replay tests
   SKIP (not FAIL) when the federation is in DDS mode. Use the
   research-platform per-impl-opt-in pattern (§6.7 PINNED).

Estimated effort: 60–80 hours (the bulk of Phase 1's original
~80–120 hour budget; Phase 1a chipped the cheap parts off the top).

---

## 12. Mission halt state (2026-05-07)

The M19 arc was paused at end-of-Phase-1a per user direction
(Option B — clean handoff). Phases 2–5 are NOT in flight; they're
blocked on Phase 1b which is itself blocked on Cyclone DDS being
available in the build environment.

**State on `origin/main`** at halt:

- §6.1 / §6.2 / §6.5 PINNED 2026-05-06 (Cyclone DDS / hand-rolled
  CGo / build-tag-gated)
- §6.3 PINNED by consequence (`cyclonedds-python`)
- §6.4 / §6.6 / §6.7 / §6.8 OPEN (Phase-2/3 settings; not on the
  Phase-1b critical path)
- Phase 0 doc landed in commit `4d525a4`
- Phase 1a foundation landed in commit `47c9ece`:
  proto extensions, federation manager fields, `--enable-dds`
  flag, build-tag-gated package skeleton with stub
  Participant/Topic/Writer/Reader returning ErrUnsupported,
  pure-Go QoS mapping, Makefile `build-dds` / `test-dds` targets,
  rti-top transport-mode column

**To resume** (next session):

1. Provide a build environment with Cyclone DDS — `apt install
   libcyclonedds-dev` (Debian/Ubuntu) or `brew install cyclonedds`
   (macOS), OR a Docker layer that bundles it.
2. Read §11.2 ("Phase 1b — CGo implementation") for the concrete
   per-step pickup. Every interface contract Phase 1b must satisfy
   is already locked by Phase 1a's stub-contract tests under
   `//go:build dds` and the QoS mapping test under `qos_test.go`.
3. Dispatch Phase 1b. Once the smoke test
   (`rti/spec/M19/dds_smoke_test.go`, currently SKIPped under
   `dds_e2e`) flips to PASSING with two federates exchanging an
   interaction over a DDS topic without rtid in the data path,
   Phase 1b is done.
4. After Phase 1b: Phase 2 (Python adapter), Phase 3 (cross-
   language conformance), Phase 4 (mixed-mode, optional), Phase
   5 (tunable QoS, optional) become unblocked in turn.

No half-implemented Python or skip-test scaffolding was shipped —
the codebase is in a clean state where the only DDS-related code
that exists either compiles cleanly without the DDS toolchain
(default build) or compiles to a stub that returns
`errors.ErrUnsupported` (under `-tags=dds`). Future sessions don't
have to untangle partial work.
