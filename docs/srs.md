# Software Requirements Specification — Go HLA Evolved RTI (MVP)

Status: draft, locked-by-conversation 2026-04-28.
Standard target: IEEE 1516-2010 (HLA Evolved).
License: MIT.

---

## 1. Purpose & Scope

This SRS defines the requirements for the **Minimum Viable Product (MVP)** of an open-source IEEE 1516-2010 (HLA Evolved) Run-Time Infrastructure (RTI) implemented in Go, with a Python federate SDK that is interoperable with the [pyjevsim](https://github.com/) DEVS framework.

The MVP is the "walking skeleton" — the smallest end-to-end system that demonstrates a real HLA federation: federation lifecycle, publish/subscribe, object/interaction exchange, time-managed advancement, deterministic replay, and one Python federate built on a DEVS coupled model.

Out of scope for MVP (deferred): Ownership Management, Data Distribution Management (DDM), Save/Restore, full Management Object Model (MOM), optimistic time advance, DDS data plane, mTLS authentication, distributed RTI topology, interoperability with commercial RTIs (Pitch / MAK / Portico federates).

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
- **FR-TM-2** — `nextMessageRequest(time)` (NER) implemented in cut 1; `timeAdvanceRequest` (TAR) added in cut 2.
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

These are intentionally excluded from MVP and shall not be implemented in cuts 1–2:

- Ownership Management (full set of divest/acquire services).
- Data Distribution Management (regions, routing spaces).
- Save/Restore (federation save, federate save, restore protocol).
- Full Management Object Model (only read-only federate list in MVP).
- Optimistic time advance (`flushQueueRequest`, `timeAdvanceRequestAvailable`).
- DDS/RTPS data plane.
- mTLS, OIDC, federation-level access control.
- Distributed RTI / hot standby / multi-process RTI.
- Interoperability with commercial RTIs (Pitch, MAK) or other HLA stacks.
- Federate SDKs in C++, Java, C#, Rust (Python only in MVP).
- FOM editor / GUI tooling.

---

## 10. Verification Approach & Milestone Exit Criteria

### 10.1 Verification levels

1. **Unit** — `go test`, `pytest`. Required ≥80% coverage on `rti/pkg/fom`, `rti/pkg/encoding`, `rti/internal/time`, `pysdk/rti1516e/encoding`.
2. **Conformance** — golden encoding vectors run in both Go and Python; byte-diff = 0.
3. **Determinism** — replay test: run a federation N times, assert identical event logs; replay event log, assert byte-identical second log.
4. **Adversarial** — at each milestone gate, agents fuzz / misbehave-test each other's components.
5. **End-to-end** — cross-language smoke test (Go federate ↔ Python federate ↔ pyjevsim federate).

**Methodology**: all production code is developed test-first per `docs/TDD.md`. For each milestone M1..M5, the orchestrator pre-writes specification tests under `tests/spec/M<x>/` that encode the milestone's exit criteria (§10.2). Agents may not weaken these tests; their passing is necessary for milestone advancement.

### 10.2 Milestones (cut 1 = walking skeleton)

| ID | Owner | Deliverable | Exit Criteria |
|---|---|---|---|
| **M0** | Orchestrator | `proto/`, `rti/internal/core/` interfaces, `docs/AGENTS.md`, CI scaffold | All three sandboxes pass `make verify` on no-op branch; agents pass conventions quiz |
| **M1** | Agent B | FOM parser, MIM, encoding rules | Strict-rejection of 10 malformed FOMs; encoder round-trips all types; matches golden vectors; coverage ≥80% on encoding |
| **M2** | Agent A | Federation/Declaration/Object Mgmt + event log + gRPC handlers | `examples/go-pingpong/` deterministic across 10 runs; replay byte-identical |
| **M3** | Agent A | Time Mgmt (NER + LBTS) | `examples/go-timed/` (3 federates, different lookaheads) deterministic across 20 randomized scenarios; stall timeout fires correctly |
| **M4** | Agent C | Python SDK + pyjevsim bridge | `examples/pyjevsim/` (2 coupled models, 1 RTI) deterministic across 10 runs; Python encoder passes 100% of golden vectors; `mypy --strict` clean |
| **M5** | Orchestrator + all | End-to-end + both modes + perf baseline | Cross-language federation works; verbose & best-effort modes both functional; baseline measured at sizes 2/5/25/100 |

Verification activities at each gate are detailed in the per-agent briefs (`agent-a-rti-core.md`, `agent-b-fom-encoding.md`, `agent-c-pysdk.md`).

### 10.3 Cut 2 (post-MVP, same architecture, additive)

- TAR (in addition to NER), sync points, explicit object delete lifecycle, multi-module FOM merge, save/restore, basic MOM, DDS data plane adapter (replaces gRPC streams for attribute updates), C++/Java federate SDKs.
