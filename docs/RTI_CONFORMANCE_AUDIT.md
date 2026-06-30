# RTI Conformance Audit — IEEE 1516.1-2010 RTI-side semantics

**Status:** DRAFT. Owner: orchestrator. Sister track to `docs/DLC_COMPLIANCE_PROGRAM.md`.

The DLC Compliance Program (M31-M35) makes gorti's **federate-side C++ API** spec-compliant. This document covers the **RTI-side behavioral semantics** — what the RTI computes when federates call those APIs. The two together are what "gorti is IEEE 1516.1-2010 compliant" actually means.

Companions: `docs/DLC_COMPLIANCE_PROGRAM.md` (federate API), `docs/srs.md` §5 (existing functional requirements FR-FM-*, FR-DM-*, FR-OM-*, FR-TM-*, FR-OWN-*, FR-DDM-*, FR-SR-*), `docs/PITCH_PARITY.md` (known divergences from Pitch).

---

## 1. Why this exists separately from the DLC program

The DLC program locks **what federate code can call**. This audit locks **what the RTI computes** when those calls land. The two are orthogonal:

- An RTI that exposes the spec API but computes GALT wrong is non-compliant.
- An RTI that computes everything correctly but uses `std::string` instead of `std::wstring` is non-compliant.

gorti's M0-M28 milestones built the RTI semantics organically — without a top-down spec-compliance audit. The audit's job: catalog where gorti's semantics deviate from §4-§11 and either fix or document.

---

## 2. Audit scope — what we check per spec section

| Spec § | Topic | Existing gorti coverage | Audit checks |
|---|---|---|---|
| §4.2-4.7 | Federation lifecycle | M2, M14, M15 | Connection state machine matches §4.2 figure 4.1; `listFederationExecutions` returns correct vector |
| §4.8-4.15 | Synchronization points | M8, M13 | Sync-point set semantics (label, tag, required set, failure-to-sync set); `synchronizationPointAchieved(label, successfully=false)` propagates `failedToSyncSet` |
| §4.16-4.32 | Save / restore | M9, M13 | Save state machine §4 figure 4.2; `federateSaveBegun` precedes `federationSaved`; restore state machine §4 figure 4.3 |
| §5 | Declaration mgmt | M2 | `subscribeObjectClassAttributes(active=false)` correctly suppresses startRegistration callbacks; `unpublishObjectClass` (whole class) vs `unpublishObjectClassAttributes` (subset) |
| §6.1-6.5 | Object name reservation | M26 F | Multiple-name reservation atomicity (all-or-nothing per §6.5); failure callback `colliding_names` set is correct |
| §6.6-6.16 | Object lifecycle | M2, M23 | DiscoverObjectInstance fan-out timing per §6.7-6.9; RemoveObjectInstance ordering (RO vs TSO) |
| §6.17-6.22 | Attribute scope & updates | (partial) | `attributesInScope` / `attributesOutOfScope` fire on region overlap changes; `provideAttributeValueUpdate` fires on `requestAttributeValueUpdate` |
| §7.3-7.6 | Negotiated divest | M8 | Two-phase: divestiture announce → assumption ack → acquisition notification → divestiture confirmation. State machine §7 figure 7.1 |
| §7.7-7.16 | Acquisition / release | M8, M24 | `acquisitionIfAvailable` immediate vs queued; release-denied callback timing |
| §8.2-8.7 | Regulation / constraint enable | M7, M22 | `enableTimeRegulation` is async; `timeRegulationEnabled` callback fires only after all OTHER regulators have ack'd |
| §8.8-8.12 | Time advance primitives | M3, M7, M21, M22 | NER/TAR/TARA/NMRA/FQR grant rules; lookahead consistency; GALT/LITS computation matches §8 algorithms |
| §8.13-8.15 | Grant delivery | M3 | `timeAdvanceGrant(t)` is delivered exactly once per outstanding advance request |
| §8.16-8.21 | Time queries | M22 | `queryGALT(out)` returns false when no regulators; `queryLogicalTime` matches federate's current logical time exactly |
| §8.22 | Message retraction | M23 (partial) | `retract(handle)` causes `requestRetraction` callback on all federates that received the message |
| §9.2-9.7 | DDM region lifecycle | M10 | createRegion / commitRegionModifications / deleteRegion state |
| §9.8-9.13 | DDM filtering | M10, M23 | An instance update reaches subscribers iff publisher's regions intersect subscriber's regions for every published attribute |
| §10.4 | Callback evocation | M27 C, M29 | HLA_IMMEDIATE vs HLA_EVOKED dispatch; enableCallbacks/disableCallbacks gating |
| §11 | MOM | M20 | HLAfederation / HLAfederate attributes update on lifecycle events; HLAreport* interactions invocable |

---

## 3. Audit method

For each spec section, three probes:

1. **State-machine trace** — instrument gorti's manager to log every state transition; compare against the spec figure's state diagram. Discrepancies = bugs to fix.
2. **Pitch parity** — run the same scenario against Pitch CRC; compare the resulting callback sequence (canonicalized per `docs/DLC_COMPLIANCE_PROGRAM.md §5.3`). Mismatches = either gorti bug, Pitch bug, or undocumented spec ambiguity.
3. **Adversarial** — drive concurrent / racy scenarios specifically chosen to expose ordering bugs.

The probes live in `tests/conformance/rti/` (top-level, NOT under cppsdk because they test the RTI not the SDK).

---

## 4. Phasing

This audit is **not a single milestone** — it runs in parallel with the DLC program (M31-M35) and informs which RTI bugs need fixing alongside the SDK rewrite. Suggested cadence:

- **Phase A** (concurrent with M31 RED scaffold) — write the state-machine trace harness. ~1 week.
- **Phase B** (concurrent with M32 GREEN-flip) — audit §4 + §5 + §10 (federation lifecycle, declarations, callback evocation). ~2 weeks. Lands fixes in `rti/internal/core/`.
- **Phase C** (concurrent with M33 GREEN-flip) — audit §6 + §7 (object + ownership). ~2 weeks.
- **Phase D** (concurrent with M34 GREEN-flip) — audit §8 + §9 (time + DDM). ~2 weeks.
- **Phase E** (concurrent with M35 GREEN-flip) — audit §11 (MOM) + §4.16-32 save/restore. ~2 weeks.
- **Phase F** (acceptance) — run IVCT subset against gorti rtid. Must pass TC_0001..TC_0030. ~1 week.

Total ~10 weeks; runs in parallel with the SDK rewrite so the project elapsed time isn't 3 months + 10 weeks but ~3-4 months with both tracks.

---

## 5. Acceptance criteria

The audit is DONE when:

1. Every spec section in §2's table has a row in `tests/conformance/rti/coverage.md` marked `(N/N probes passing)`.
2. Every Pitch divergence found is either (a) fixed in gorti, (b) documented in `docs/PITCH_PARITY.md` with rationale, or (c) labeled as a Pitch deviation from spec (Pitch is non-canonical for that case).
3. **An IVCT-derived conformance subset passes** (see §6 for honest scoping). NOT "TC_0001..TC_0030" — that's a placeholder for the subset selected when integration lands.
4. `scripts/check-milestones.sh check_rti_conformance` reports `RTI conformance: DONE (5/5 phases)`.

### 5.1 Progress dashboard (parallel to DLC program GREEN counter)

Acceptance is tracked per phase by `scripts/check-milestones.sh check_rti_conformance` which probes each phase's deliverables and emits `RTI conformance: (N/5 phases)` to match the DLC track's `(N/200) GREEN` reporting style. Each phase has 4-8 internal probes (state-machine trace landed, conformance tests landed, Pitch-parity comparison runs, audit report written). The full breakdown lives in `tests/conformance/rti/coverage.md` (auto-generated, updated by each phase's commits).

---

## 6. IVCT integration — honest scoping

SISO **IVCT** (Integration Verification & Certification Tool) lives at https://github.com/IVCT-team. It's the de-facto conformance suite for IEEE 1516-2010 federates against an RTI. It's a Java application that runs Java federate code via a 1516.1 Java HLA SDK and exercises a fixed catalog of conformance assertions.

**Why integrating IVCT with gorti is non-trivial:**
- IVCT calls RTI services through Java's `hla.rti1516e.*` API. gorti has no Java SDK — only Python + C++.
- IVCT's federates connect via standard 1516 binding (typically over Pitch's wire protocol). gorti's wire is gRPC; IVCT cannot speak gRPC.
- IVCT test cases (`TestCases/*.java`) are full Java programs with state machines, callback handlers, and assertion logic — they cannot simply be transliterated to Python.

**Three real integration paths (cost honest):**

| Option | Approach | Cost | Risk |
|---|---|---|---|
| **(a) Java SDK** | Build gorti's missing Java federate SDK (1516.1 HSL) so IVCT can directly call gorti. Then run IVCT verbatim. | ~2-3 months — equivalent to a full M18-style milestone | High — Java SDK is a separate compliance surface |
| **(b) Federate-protocol bridge** | Write a small Java↔gRPC bridge that exposes a 1516.1 Java API and proxies to gorti rtid. IVCT runs against the bridge. | ~3-4 weeks if pRTI 6's `FedProClient` (BSD-licensed Java federate protocol client at github.com/Pitch-Technologies/FedProClient) can be adapted | Medium — federate protocol may not cover all 1516 services gorti needs |
| **(c) IVCT-inspired Python suite** | Read IVCT test cases as specification; re-implement equivalent assertions in Python against pysdk. NOT IVCT integration — a parallel suite informed by IVCT. | ~3-4 weeks for a 30-test subset | Low scope risk; but the suite isn't IVCT — claiming "IVCT compliance" would be misleading. |

**Recommendation: (c) for M35**, renamed appropriately. The plan should claim "passes an IVCT-derived conformance subset" rather than "passes IVCT" until path (a) or (b) lands. Path (b) is the right long-term answer — track as a deferrable post-M35 milestone.

---

## 7. Open questions

- **Pitch as ground truth.** When gorti and Pitch differ and the spec is ambiguous, who wins? Per `docs/DLC_COMPLIANCE_PROGRAM.md §5.3.2` tie-breaker rule: **spec wins**. Pitch deviations go into `docs/PITCH_PARITY.md` "Pitch deviations from spec" section, parity tests skip those rows.
- **Real RTIs disagree all the time.** Realistic expectation: a few Pitch deviations from spec will surface in this audit. Treat as bugs in Pitch, not gorti. (Pitch has a closed-source codebase; we can't fix theirs, just document.)
