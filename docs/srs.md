# Software Requirements Specification — Go HLA Evolved RTI (MVP)

Status: draft, locked-by-conversation 2026-04-28.
Standard target: IEEE 1516-2010 (HLA Evolved).
License: MIT.

---

## 1. Purpose & Scope

This SRS defines the requirements for an open-source IEEE 1516-2010 (HLA Evolved) Run-Time Infrastructure (RTI) implemented in Go, with a Python federate SDK that is interoperable with the [pyjevsim](https://github.com/) DEVS framework.

**Cut 1 — Minimum Viable Product (MVP)** [DONE per tag `mvp`, M0..M5]: the "walking skeleton" — the smallest end-to-end system that demonstrates a real HLA federation. Federation lifecycle, publish/subscribe, object/interaction exchange, time-managed advancement (NER only), deterministic replay, one Python federate built on a DEVS coupled model.

**Cut 2 — Production-grade RTI** [in flight, M6..M11]: completes the IEEE 1516-2010 service surface so gorti is a real alternative to commercial RTIs (Pitch, MAK, Portico) for non-DDS workloads. Adds: TAR/TARA/FQR time advance primitives, federation save/restore, full Data Distribution Management (DDM) with regions and routing spaces, Ownership Management proper, synchronization points, runtime Management Object Model (MOM), TLS hardening, cross-language handle parity hardening.

Out of scope (forever, or far future): DDS/RTPS data plane (gRPC remains the wire), mTLS + OIDC + federation-level access control, distributed RTI / hot standby / multi-process RTI, interoperability with commercial RTIs at the wire level (other vendors implement different non-standard wire formats), federate SDKs in C++/Java/C#/Rust (Python only through cut 2), FOM editor / GUI tooling.

## 2. References

- IEEE Std 1516-2010 — Framework and Rules.
- IEEE Std 1516.1-2010 — Federate Interface Specification.
- IEEE Std 1516.2-2010 — Object Model Template (OMT) Specification.
- Portico RTI (Java, CDDL) — primary reference implementation for time management and FOM parsing.
- pyjevsim (latest) — Python DEVS framework; first federate SDK compatibility target.

## 3. Definitions & Acronyms

| Term | Meaning |
|---|---|
| RTI | Run-Time Infrastructure — central HLA service |
| Federate | A simulation participant that joins a federation |
| Federation | A set of federates collaborating under a shared FOM |
| FOM | Federation Object Model — XML-defined types/classes/interactions |
| MIM | Management and Initialization Model — base FOM always loaded |
| TSO | Time-Stamp Order delivery |
| RO | Receive Order delivery |
| LBTS | Lower Bound on Time Stamp |
| NER | Next Message Request |
| TAR | Time Advance Request |
| DEVS | Discrete Event System Specification |

## 4. System Overview

### 4.1 Architecture

Two-plane architecture (target). MVP collapses both planes onto gRPC; the data-plane interface is shaped so a DDS adapter can replace it in v2 without touching core services.

```
+------------------+         gRPC bidi-stream         +------------------+
|  Federate (any   | <------------------------------> |  RTI (Go)        |
|  language SDK)   |     control + data plane         |                  |
+------------------+                                  | - federation     |
                                                      | - declaration    |
                                                      | - object         |
                                                      | - time           |
                                                      | - event log      |
                                                      | - FOM repo       |
                                                      +------------------+
```

### 4.2 Components

- **`rti/`** — Go RTI server (this is the product).
- **`rti/pkg/fom/`** — FOM XML parser, MIM, validation.
- **`rti/pkg/encoding/`** — HLA Evolved encoding rules (binary codec).
- **`rti/internal/core/`** — frozen interfaces (`Transport`, `FederationStore`, `TimeManager`, `EventLog`, etc.).
- **`pysdk/`** — Python federate SDK + pyjevsim bridge.
- **`proto/rti/v1/`** — gRPC contracts (frozen; orchestrator-owned).

### 4.3 Deployment

Single RTI process, supports multiple federations concurrently, federates discover the RTI via static config (URL in env or config file). Distributed RTI is post-MVP.

---

## 5. Functional Requirements

### 5.1 Federation Management (FR-FM-*)

- **FR-FM-1** — RTI shall implement `createFederationExecution(name, fomModules[])` with strict 1516-2010 DIF XML validation; non-conformant FOMs are rejected with diagnostic.
- **FR-FM-2** — RTI shall implement `destroyFederationExecution(name)`; rejected if any federate is still joined.
- **FR-FM-3** — RTI shall implement `joinFederationExecution(federateName, federationName)` returning a deterministic federate handle (assigned in join order, stable across replay).
- **FR-FM-4** — RTI shall implement `resignFederationExecution(action)` with `UNCONDITIONALLY_DIVEST_ATTRIBUTES` semantics in MVP; other resign actions deferred.
- **FR-FM-5** — Federate handles, object handles, attribute handles, interaction handles shall be deterministic functions of join/declaration order.
- **FR-FM-6** *(deferred from MVP cut 1; in cut 2)* — Synchronization point announce / achieve / federation-synchronized.

### 5.2 Declaration Management (FR-DM-*)

- **FR-DM-1** — `publishObjectClassAttributes(class, attrs[])`.
- **FR-DM-2** — `subscribeObjectClassAttributes(class, attrs[])`.
- **FR-DM-3** — `publishInteractionClass(class)` / `subscribeInteractionClass(class)`.
- **FR-DM-4** — Region-based variants are deferred (DDM out of scope for MVP).

### 5.3 Object Management (FR-OM-*)

- **FR-OM-1** — `registerObjectInstance(class, name?)` returns deterministic object handle.
- **FR-OM-2** — `discoverObjectInstance` callback fires on subscribers in deterministic order.
- **FR-OM-3** — `updateAttributeValues(obj, values, time?)` and `reflectAttributeValues` callback.
- **FR-OM-4** — `sendInteraction(class, params, time?)` and `receiveInteraction` callback.
- **FR-OM-5** — In cut 1, object death is implicit: federate resign destroys all objects it owned. Explicit `deleteObjectInstance` lifecycle moves to cut 2.

### 5.4 Time Management (FR-TM-*)

- **FR-TM-1** — `enableTimeRegulation(lookahead)` / `enableTimeConstrained()` / disable variants.
- **FR-TM-2** — `nextMessageRequest(time)` (NER) in cut 1. Cut 2 adds: `timeAdvanceRequest(time)` (TAR), `timeAdvanceRequestAvailable(time)` (TARA), `flushQueueRequest(time)` (FQR), `nextMessageRequestAvailable(time)` (NMRA). All four primitives MUST share LBTS computation + grant-emission machinery from cut 1.
- **FR-TM-3** — RTI shall compute LBTS via deterministic all-reduce across regulating federates; tie-break on (federate handle → object handle → attribute handle).
- **FR-TM-4** — Lookahead enforcement shall not depend on wall-clock.
- **FR-TM-5** — `timeAdvanceGrant(time)` callback fires when LBTS allows; deterministic ordering across federates.
- **FR-TM-6** — A stalled federate shall trigger a timeout diagnostic (configurable, default 60s); federation halts with clear cause, never silent deadlock.

### 5.5 FOM Handling (FR-FOM-*)

- **FR-FOM-1** — Parse 1516-2010 DIF XML modules; reject anything non-conformant.
- **FR-FOM-2** — Standard MIM and `HLAstandardMIM` are embedded in the RTI binary; always loaded.
- **FR-FOM-3** — Single user FOM module supported in cut 1; multi-module merge in cut 2.
- **FR-FOM-4** — FOM model is immutable after federation create.

### 5.6 Encoding Rules (FR-ENC-*)

- **FR-ENC-1** — Implement HLA Evolved encoding rules: `HLAfixedRecord`, `HLAvariantRecord`, `HLAfixedArray`, `HLAvariableArray`, `HLAopaqueData`, all primitive types defined in 1516.2-2010.
- **FR-ENC-2** — Encoder output for any FOM-defined type must be byte-identical between Go (`rti/pkg/encoding`) and Python (`pysdk/rti1516e/encoding`); enforced via golden vectors at `tests/conformance/encoding_vectors.json`.

### 5.7 Event Log (FR-EVT-*)

- **FR-EVT-1** — RTI writes every federation-crossing message in TSO with monotonic sequence numbers to a binary event log.
- **FR-EVT-2** — Replay reader can re-feed the log to the RTI; output log of a replayed run shall be byte-identical to the original (when all federates are deterministic w.r.t. their inputs).
- **FR-EVT-3** — Log format is forward-compatible (length-prefixed Protobuf records, magic header, version field).

### 5.8 pyjevsim Bridge (FR-PYJ-*)

- **FR-PYJ-1** — `HLAFederate(coupled_model)` adapter wraps a pyjevsim coupled model as one HLA federate. Internal atomics stay in-process under native pyjevsim scheduling.
- **FR-PYJ-2** — pyjevsim message ports map to FOM **interaction classes** by default; attributes reserved for state observable between messages.
- **FR-PYJ-3** — `ta()` from the coupled model maps to `nextMessageRequest`; on grant, the bridge runs the model's internal cycle, drains output ports, sends interactions, then re-requests.
- **FR-PYJ-4** — Simultaneous-event tie-break preserves pyjevsim's `select()` ordering exactly at the HLA boundary.

### 5.9 Synchronization Management (FR-SYN-*) — cut 2

- **FR-SYN-1** — `registerFederationSynchronizationPoint(label, tag, [federates])` per IEEE 1516.1-2010 §4.6. Optional federate set (nil = all joined federates).
- **FR-SYN-2** — `synchronizationPointAchieved(label)` per §4.7; RTI tracks per-(label, federate) achievement state.
- **FR-SYN-3** — `announceSynchronizationPoint` callback fires when registration succeeds; `federationSynchronized` callback fires when all required federates have achieved.
- **FR-SYN-4** — Sync-point state recorded in the event log so replay reproduces the announce/achieve order byte-identically (NFR-DET-1, NFR-DET-2).

### 5.10 Ownership Management (FR-OWN-*) — cut 2

- **FR-OWN-1** — `unconditionalAttributeOwnershipDivestiture(obj, attrs)` per §7.2 (cut 1 already has this via resign).
- **FR-OWN-2** — `negotiatedAttributeOwnershipDivestiture(obj, attrs, tag)` + `attributeOwnershipAcquisition(obj, attrs, tag)` per §7.3-7.4. Two-phase protocol with RTI as broker.
- **FR-OWN-3** — `cancelNegotiatedAttributeOwnershipDivestiture` / `cancelAttributeOwnershipAcquisition` per §7.5-7.6.
- **FR-OWN-4** — `attributeOwnershipDivestitureIfWanted(obj, attrs)` per §7.7.
- **FR-OWN-5** — `queryAttributeOwnership(obj, attr)` + `isAttributeOwnedByFederate(obj, attr)` query services per §7.8-7.9.
- **FR-OWN-6** — Ownership transitions recorded in the event log; deterministic replay of multi-phase protocols (NFR-DET-1).

### 5.11 Management Object Model — Runtime (FR-MOM-*) — cut 2

- **FR-MOM-1** — Standard MIM defines `HLAmanager.HLAfederate` and `HLAmanager.HLAfederation` object classes; cut 2 wires these as runtime-registered MOM objects whose attributes the RTI populates and updates per §10 of 1516.1-2010.
- **FR-MOM-2** — Federates may subscribe to MOM attributes via the standard pub/sub APIs (no special calls); RTI emits attribute updates on lifecycle events (federate join/resign, attribute publish/subscribe, sync-point register/achieve).
- **FR-MOM-3** — Cut 2 implements the read-only MOM. MOM-driven control services (`HLAsetSwitches`, `HLArequestFederationSave`, etc. invoked AS interactions) deferred to cut 3.

### 5.12 Federation Save/Restore (FR-SR-*) — cut 2

- **FR-SR-1** — `requestFederationSave(label[, time])` per §4.8. Save coordinator broadcasts `initiateFederateSave` to all joined federates at a synchronized point.
- **FR-SR-2** — `federateSaveComplete()` / `federateSaveNotComplete()` per §4.9. RTI aggregates federate responses and emits `federationSaved` / `federationNotSaved`.
- **FR-SR-3** — `requestFederationRestore(label)` + `initiateFederateRestore` + `federateRestoreComplete` per §4.10-4.12. RTI replays the saved event log to bring federation state back.
- **FR-SR-4** — Save artifact format: a sealed bundle (tar.gz) of (a) FOM modules, (b) federation manifest (federates, declarations, registered objects, attribute ownerships, sync-point state), (c) the event log up to the save point. Format is documented in `docs/sdd.md` §X.
- **FR-SR-5** — Restore is byte-deterministic with the original run (NFR-DET-2 extends to the save/restore cycle).

### 5.13 Data Distribution Management (FR-DDM-*) — cut 2

- **FR-DDM-1** — Routing space declarations parsed from FOM XML (1516.2-2010 Annex A `<dimensions>` + `<dimension>` elements) and made queryable via `getDimensionHandle`/`getDimensionName`.
- **FR-DDM-2** — `createRegion(routingSpace, dimensions[])` returns a `RegionHandle`; per §6.5. Per-region range bounds settable via `commitRegionModifications(regions[])`.
- **FR-DDM-3** — `subscribeObjectClassAttributesWithRegions(class, attrs, regions)` + `subscribeInteractionClassWithRegions(class, regions)` per §6.6. Subscriptions are scoped to the subscribed regions.
- **FR-DDM-4** — `registerObjectInstanceWithRegions(class, attrToRegion[])` + `updateAttributeValuesWithRegions(...)` per §6.7. Attribute updates fan out only to subscribers whose regions overlap the publisher's.
- **FR-DDM-5** — Region overlap detection: deterministic interval-tree check across all dimensions of the routing space; tie-break on `RegionHandle` ascending.
- **FR-DDM-6** — Performance: DDM filtering MUST NOT make non-DDM workloads slower (zero-cost when no regions are in play). DDM workload baseline at federation size 25 with 100 regions in `docs/reports/M10/agent-a.md`.

---

## 6. Non-Functional Requirements

### 6.1 Determinism (NFR-DET-*)

- **NFR-DET-1** — Given identical inputs (FOM, federate join order, message timestamps, RNG seeds), two runs of the same federation in verbose mode shall produce byte-identical event logs.
- **NFR-DET-2** — Replay from event log shall be byte-identical to original log for deterministic federates.

### 6.2 Performance (NFR-PERF-*)

- **NFR-PERF-1** — **Verbose mode** baseline: ≥100 msg/s/federate sustained on commodity hardware (4-core, 16GB) at federation size 5; full TSO + structured event log enabled.
- **NFR-PERF-2** — **Best-effort mode** baseline: ≥1,000 msg/s/federate sustained on the same hardware with RO permitted per-attribute and minimal logging.
- **NFR-PERF-3** — Time-advance grant p99 latency < 50 ms in verbose mode at federation size 5; baseline only, no optimization required for MVP.
- **NFR-PERF-4** — Mode is selected per-federation at create time; declared per-attribute/per-interaction in the FOM.

### 6.3 Scalability (NFR-SCALE-*)

- **NFR-SCALE-1** — No accidental O(N²) algorithms in core services. RTI shall be measurable up to federation size 100; not optimized for that scale, but not artificially limited.
- **NFR-SCALE-2** — Performance baseline (NFR-PERF-*) is recorded across federation sizes 2, 5, 25, 100 at M5.

### 6.4 Reliability & Crash Recovery (NFR-CRASH-*)

- **NFR-CRASH-1** — Federate death shall halt the federation with a diagnostic identifying the federate; no silent stall.
- **NFR-CRASH-2** — RTI death is "game over" in MVP — no hot standby. Manual resume via replay log.
- **NFR-CRASH-3** — Reliability per attribute/interaction: both reliable+TSO and best-effort+RO supported (declared in FOM); reliable+TSO is default.

### 6.5 Security (NFR-SEC-*)

- **NFR-SEC-1** — TLS optional via flag (`--tls-cert`, `--tls-key`); no authentication or authorization in MVP. Trusted-network assumption documented.

### 6.6 Observability (NFR-OPS-*)

- **NFR-OPS-1** — Structured logging via `log/slog` (Go) and Python `logging` with JSON formatter; all events include `federation`, `federate_handle`, `seq`, `phase`.
- **NFR-OPS-2** — Prometheus metrics exposed on configurable HTTP port: counters (messages by type), histograms (advance grant latency, message size), gauges (joined federates, objects per class).
- **NFR-OPS-3** — No distributed tracing in MVP.

### 6.7 Deployment (NFR-DEPLOY-*)

- **NFR-DEPLOY-1** — Single RTI binary; multiple federations supported per process.
- **NFR-DEPLOY-2** — Federate discovery via static config (env var `RTI_URL` or `--rti-url` flag).

---

## 7. External Interface Requirements

### 7.1 Wire Protocol (IR-PROTO-*)

- **IR-PROTO-1** — Control + data plane defined in `proto/rti/v1/*.proto` (gRPC + Protobuf). Frozen; orchestrator-owned.
- **IR-PROTO-2** — Data plane uses bidi-streams for attribute updates and interactions. The Go `Transport` interface in `rti/internal/core/` allows future replacement with DDS without touching higher-level services.
- **IR-PROTO-3** — Wire encoding of FOM-defined types follows HLA Evolved encoding rules (FR-ENC-*); message envelopes are Protobuf.

### 7.2 FOM Format (IR-FOM-*)

- **IR-FOM-1** — 1516-2010 DIF XML only. No legacy 1.3 OMT support, no proprietary formats.

### 7.3 Federate API Shape (IR-PYAPI-*)

- **IR-PYAPI-1** — Hybrid: idiomatic Python API (`async def`, context managers, dataclasses) plus a thin standard-shaped adapter (`Rti1516eAmbassador`-style) for users porting from existing RTIs.

---

## 8. Constraints

- **C-1** — Solo developer; lean MVP approach.
- **C-2** — License: MIT.
- **C-3** — RTI implementation language: Go (stable toolchain). Federate SDKs: separate per-language libraries; Python first (pyjevsim-compatible).
- **C-4** — Standard target: IEEE 1516-2010 (HLA Evolved) only — not 1516-2000, not 1.3, not 1516-2025.
- **C-5** — Development uses three sandboxed coding agents (claude-sandbox, codex-sandbox, gemini-sandbox) running with auto-approve. Architectural and contract decisions go through the orchestrator (Claude in conversation), not the agents. See `docs/AGENTS.md` for guardrails.

---

## 9. Out of Scope (Explicit)

These are intentionally excluded from cuts 1–2 (post-cut-2 backlog or forever-out):

- DDS/RTPS data plane (gRPC remains the wire through cut 2).
- mTLS, OIDC, federation-level access control (TLS server-side only via `--tls-cert/--tls-key` per NFR-SEC-1).
- Distributed RTI / hot standby / multi-process RTI.
- Interoperability with other HLA stacks at the wire level (Pitch, MAK, Portico use proprietary wire formats; only FOM XML interop is in scope).
- Federate SDKs in C++, Java, C#, Rust (Python only through cut 2).
- FOM editor / GUI tooling.
- MOM-driven control services (cut 3 — see FR-MOM-3).
- Optimistic time advance variants beyond TARA (cut 3).

---

## 10. Verification Approach & Milestone Exit Criteria

### 10.1 Verification levels

1. **Unit** — `go test`, `pytest`. Required ≥80% coverage on `rti/pkg/fom`, `rti/pkg/encoding`, `rti/internal/time`, `pysdk/rti1516e/encoding`.
2. **Conformance** — golden encoding vectors run in both Go and Python; byte-diff = 0.
3. **Determinism** — replay test: run a federation N times, assert identical event logs; replay event log, assert byte-identical second log.
4. **Adversarial** — at each milestone gate, agents fuzz / misbehave-test each other's components.
5. **End-to-end** — cross-language smoke test (Go federate ↔ Python federate ↔ pyjevsim federate).

**Methodology**: all production code is developed test-first per `docs/TDD.md`. For each milestone M1..M5, the orchestrator pre-writes specification tests under `tests/spec/M<x>/` that encode the milestone's exit criteria (§10.2). Agents may not weaken these tests; their passing is necessary for milestone advancement.

### 10.2 Milestones — Cut 1 (walking skeleton; DONE per tag `mvp`)

| ID | Owner | Deliverable | Exit Criteria |
|---|---|---|---|
| **M0** | Orchestrator | `proto/`, `rti/internal/core/` interfaces, `docs/AGENTS.md`, CI scaffold | All three sandboxes pass `make verify` on no-op branch; agents pass conventions quiz |
| **M1** | Agent B | FOM parser, MIM, encoding rules | Strict-rejection of 10 malformed FOMs; encoder round-trips all types; matches golden vectors; coverage ≥80% on encoding |
| **M2** | Agent A | Federation/Declaration/Object Mgmt + event log + gRPC handlers | `examples/go-pingpong/` deterministic across 10 runs; replay byte-identical |
| **M3** | Agent A | Time Mgmt (NER + LBTS) | `examples/go-timed/` (3 federates, different lookaheads) deterministic across 20 randomized scenarios; stall timeout fires correctly |
| **M4** | Agent C | Python SDK + pyjevsim bridge | `examples/pyjevsim/` (2 coupled models, 1 RTI) deterministic across 10 runs; Python encoder passes 100% of golden vectors; `mypy --strict` clean |
| **M5** | Orchestrator + all | End-to-end + both modes + perf baseline | Cross-language federation works; verbose & best-effort modes both functional; baseline measured at sizes 2/5/25/100 |

Verification activities at each gate are detailed in the per-agent briefs (`agent-a-rti-core.md`, `agent-b-fom-encoding.md`, `agent-c-pysdk.md`).

### 10.3 Milestones — Cut 2 (production-grade RTI)

| ID | Owner | Deliverable | Exit Criteria |
|---|---|---|---|
| **M6** | Orchestrator + all | Hardening + cross-language handle alignment + TLS + real-pyjevsim adapter + Python `grpcs://` + replay path + production in-process driver | Last skipped spec test flips to PASS; race-clean concurrency; TLS handshake works server-side + client-side; bidirectional Python+Go cross-language smoke functional |
| **M7** | Agent A | TAR + TARA + FQR + NMRA (complete time-advance primitives) | All four primitives invokable; share LBTS computation with NER; spec tests cover scenario matrix (single-regulator-grants, peer-blocked, FQR-cancels-pending); deterministic across 20 randomized scenarios |
| **M8** | Agent A | Synchronization Management + Ownership Management proper | `registerFederationSynchronizationPoint` + `synchronizationPointAchieved` + `announceSynchronizationPoint`/`federationSynchronized` callbacks. Negotiated divest/acquire two-phase protocol. All transitions in event log; replay byte-identical |
| **M11** | Agent A | MOM runtime (HLAfederate/HLAfederation reflection) | Federates can subscribe to `HLAmanager.HLAfederate` attributes via standard pub/sub; RTI emits attribute updates on lifecycle events |
| **M10** | Agent A + B | DDM (regions + routing spaces) | Routing space declarations parsed from FOM; region creation + commit; `subscribeWithRegions` + `updateAttributeValuesWithRegions` filtering; performance baseline at size 25 with 100 regions; zero-cost when no regions are in play |
| **M9** | Agent A | Federation save/restore | `requestFederationSave` + `initiateFederateSave` aggregation + `federationSaved`. Restore replays the saved event log to byte-identical state. Save artifact format documented in `docs/sdd.md` |

Cut 2 dispatch order rationale: M7 first (smallest, completes the time-management surface that's most user-visible). M8 next (foundational for federations that coordinate startup or hand off attributes). M11 third (small, gives observability — quick win). M10 fourth (biggest single absence for "real RTI" claim; do before save/restore because users feel its absence directly). M9 last (most expensive; touches every existing service group).

### 10.4 Milestones — Cut 3 (production hardening + reach)

| ID | Owner | Deliverable | Exit Criteria |
|---|---|---|---|
| **M12** | All agents | gRPC handler + Python SDK exposure for cut-2 service groups | All cut-2 internal APIs reachable via gRPC; Python SDK exposes them via Layer 1 (idiomatic asyncio) + Layer 2 (1516-shaped ambassador); cross-language spec tests for each service group |
| **M13** | Agent A | Per-manager state snapshots in save manifest + federation.Manager.MembersOf accessor + HLAfederateType plumbing | M9 save bundle includes structured snapshots (sync state, ownership state, MOM state, DDM regions); restore byte-identical without sole reliance on event-log replay; production rtid uses MembersOf for sync/savepoint required-set resolution |
| **M14** | Agent A + C | mTLS + OIDC client authentication | Server requires + verifies client cert (or OIDC bearer token) before accepting any RPC; Python SDK passes credentials; spec test exercises mTLS round-trip. **DONE 2026-05-09** — see `docs/M14_DISPATCH_PLAN.md` and `CHANGELOG-MASTERPLAN.md`. M14 ships RS256 JWT verification against `--oidc-jwks-pem`; OIDC discovery (`--oidc-issuer` JWKS HTTP fetch) deferred to M-future. |
| **M15** | Agent A | Distributed RTI: multi-process federation hosting | Federation can span N rtid processes that gossip via cluster protocol; federate transparently routes to the rtid hosting its federation; failover deferred to M16. **CUT-1 DONE 2026-05-09** — `ClusterService` surface + single-node-correct manager. **CUT-2 (multi-node consensus) deferred** — see `docs/M15_DISPATCH_PLAN.md` §0 for honest scope. |
| **M16** | Agent A | Hot standby + replay-driven RTI failover | Standby rtid replicates event log + can take over on primary failure; spec test simulates primary kill mid-federation. **DEFERRED** — depends on M15 cut-2. Plan at `docs/M16_DISPATCH_PLAN.md`. |
| **M17** | new owner (C++) | C++ federate SDK | C++ SDK passes the same conformance vectors + cross-language spec tests as Python SDK; pkg-config / CMake distribution |
| **M18** | new owner (Java) | Java federate SDK | Same shape as M17 for Java; Maven Central distribution |
| **M19** | Agent A | DDS/RTPS data plane adapter | Federate-side opt-in: data plane (object/interaction fan-out) goes via DDS instead of gRPC streams; control plane stays gRPC; spec test verifies wire-level DDS interop |
| **M20** | Agent A | MOM-driven control services + optimistic time variants | HLAsetSwitches, HLArequestFederationSave etc. invokable AS interactions per IEEE 1516.1 §10. Time variants beyond cut-2's TAR/TARA/FQR/NMRA |
| **M21** | Agent A + C | Complete TimeService gRPC wiring (cross-process time advance) | All 5 cut-2 advance primitives (NER, TAR, TARA, NMRA, FQR) + 3 queries (QueryLogicalTime, QueryLookahead, QueryLBTS) + ModifyLookahead reachable cross-process; pysdk time RPCs flip from no-op to real; `examples/go-timed/` and `examples/pyjevsim-time-advance/` restored as cross-process showcases; cut-3 README "no time-managed variant" caveats struck. **DONE 2026-05-07** — see `docs/M21_DISPATCH_PLAN.md` and `CHANGELOG-MASTERPLAN.md` |
| **M22** | Agent A + C | TimeService completion (close M21 carryovers) | Pysdk Federate exposes all 15 time methods + 1516e ambassador parity; `enable/disableAsynchronousDelivery` reachable cross-process with TSO buffer/release semantics per IEEE 1516.1 §8.16-8.17 (default OFF per spec); NER+forced-grant race diagnosed as SDK-side semantics (forced grants leave federate in time-advancing state); `waitForFullGrant` lands in `examples/go-timed/` + `examples/pyjevsim-time-advance/`, M21-era workarounds (TAR fallback + 5 ms settle delay + retry-on-TimeAdvancingState backoff) removed. **DONE 2026-05-09** — see `docs/M22_DISPATCH_PLAN.md` and `CHANGELOG-MASTERPLAN.md` |
| **M23** | Agent A + C | ObjectManagement (§6) + DDM (§9) completion | §6: `delete_object_instance` + `RemoveObjectInstance` callback wired end-to-end (proto slot was orphan since M0); `local_delete_object_instance`; `request_attribute_value_update` + `ProvideAttributeValueUpdate` callback (instance + class variants — late-joiner pull pattern); `change_attribute_transportation_type` + `change_interaction_transportation_type` (record-only, wire switching deferred). §9: DDM Go SDK gains all 16 methods (was zero before M23); 6 missing wire RPCs added (`AssociateRegionsForUpdates`, `UnassociateRegionsForUpdates`, `UnsubscribeObjectClassAttributesWithRegions`, `UnsubscribeInteractionClassWithRegions`, `SendInteractionWithRegions`, `RequestAttributeValueUpdateWithRegions`). **DONE 2026-05-09** — see `docs/M23_DISPATCH_PLAN.md` and `CHANGELOG-MASTERPLAN.md` |
| **M24** | Agent A + C | FederationManagement (§4) completion + Resign correctness | `UNCONDITIONALLY_DIVEST_ATTRIBUTES` actually divests (pre-M24 was a no-op — manager removed federate from roster but ownership records stayed stale); all 6 ResignAction values accepted (pre-M24 was 1); `ownership.Manager.ReleaseAllOwnedBy` + `CancelPendingFor` (NEW); cmd/rtid resigning-dispatch wires per-action cleanup (release / delete / cancel) BEFORE the roster mutation. §4.8 `ListFederationMembers` + §4.28 `AbortFederationSave` + §4.30 `AbortFederationRestore` wired end-to-end; SDKs gain explicit-action `ResignWithAction` + 3 new methods. **DONE 2026-05-09** — see `docs/M24_DISPATCH_PLAN.md` and `CHANGELOG-MASTERPLAN.md` |

Cut-3 dispatch order rationale: M12 first (closes the biggest user-visible gap — cut-2 service groups exist but federates can't reach them via the network). M13 next (closes cut-2's documented snapshot deferrals). M14 third (production-deployable security; needed before any real-world deployment). M15+ (distributed) and M17-M18 (non-Python SDKs) are larger but have natural follow-up shape. M19 (DDS) is the biggest architectural change. M20 closes long-tail compliance. M21 was inserted late in cut-3 to close the cut-1 / cut-2 time-service gap (NER was the only primitive with a wire path; all other advance primitives + queries returned `Unimplemented`); deliberately kept narrow (no new semantics, only the wire adapter + SDK + showcases).

### 10.5 Cut 4+ (open-ended backlog)

- Federation save/restore across distributed rtid topology
- Live FOM module hot-reload
- Federate hot-restart (federate process dies + reconnects without federation halt)
- HLA-Evolved interoperability with commercial RTIs at the wire level (Pitch HLA Evolved wire format reverse-engineering; out of scope for OSS without spec-vendor cooperation)
- FOM editor / GUI tooling
- Time-warped optimistic time advance (Jefferson-style rollback)
