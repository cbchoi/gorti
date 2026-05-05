# gorti as an HLA/RTI research platform — design

Status: DRAFT. Phase 0 of the research-platform refactor — design only,
no code changes yet. Pending decisions are marked **OPEN**.

This document maps the existing modularity, lists every extension point
worth opening up for research, and proposes a staged path to get there
without breaking M0..M12 conformance.

Audience: researchers who want to swap algorithms (time advance, DDM
filtering, ownership protocols, …) and run the same conformance + perf
suite against their variants.

---

## 1. Goal

gorti today is a production-shaped IEEE 1516-2010 RTI: one
implementation per service, hand-wired in `cmd/rtid/main.go`. The goal
of this refactor is to let a researcher:

1. Replace one or more services or algorithms with their own variant.
2. Wire the variant into rtid via a small composition change (or
   ideally a config setting).
3. Run the existing `go test ./...` + cross-language pysdk suite + perf
   harness against the variant unchanged — same conformance bar, same
   throughput baseline.

Out of scope for this design: hot-swap of running implementations,
out-of-process / WebAssembly / RPC plugins, dynamic loading via
`plugin` package. The platform is compile-time-extensible.

---

## 2. Current modularity audit

### 2.1 Already pluggable (interface in `core/`, multiple impls)

| Interface | Default impl | Other impls in tree |
|---|---|---|
| `core.Outbox` | `cmd/rtid.multiOutbox` | `perf.perfOutbox`, `cmd/rtid.syncOutbox`, `transport/grpc.soakOutbox`, `transport/grpc.fakeSubscribableOutbox` |
| `core.Clock` | `core.realClock` | `core.fakeClock`, `core.monotonicClock` |
| `core.EventLog` | `eventlog.MultiplexWriter` | nil-log (silent drop), in-memory factory |
| `core.FOMRepository` | production parser-backed | permissive (cmd/rtid pingpong), test fixtures |
| `core.Codec` / `CodecFactory` | `pkg/encoding` | researcher-provided |
| `core.TimeManager` | `time.Manager` | (interface exists but only one impl today) |
| `core.FederationStore` | `federation.Manager` | (interface exists but only one impl today) |
| `core.ObjectRegistry` | `object.Registry` | (interface exists but only one impl today) |
| `object.DDMFilter` | `ddm.Manager` | nil (no-DDM path) |

### 2.2 Concrete-only (no interface; consumers bind to the type)

| Service | Concrete type | Consumers |
|---|---|---|
| Sync points | `sync.Manager` | rtid composition root, gRPC handler, MOM hooks |
| Ownership | `ownership.Manager` | object.Registry's OnRegister, gRPC handler |
| MOM runtime | `mom.Manager` | every other manager via OnXxxSuccess hooks |
| DDM (full surface) | `ddm.Manager` | gRPC handler; only the `DDMFilter` slice is interfaced |
| Save/restore | `savepoint.Manager` | gRPC handler |
| Declaration | `declaration.Manager` | object.Registry, gRPC handler. Per `docs/idd.md` §3 this is "pure local component", no abstraction layer; revisit whether to expose for research |

### 2.3 Algorithm-level swap points (single hardcoded impl inside the service)

| Algorithm | Where | Swap value |
|---|---|---|
| LBTS calculation | `rti/internal/time/lbts.go` exported `LBTS([]RegulatingFederate)` | conservative-min variants, lookahead policies, hierarchical LBTS |
| Time advance grant decision | `rti/internal/time/advance.go` `decideGrant` | optimistic variants, lazy advance, predictive grants |
| Stall detection | `rti/internal/time/stall.go` | adaptive timeout, federation-aware backoff |
| DDM region overlap | `rti/internal/ddm/overlap.go` (the file already documents "Cut-2 algorithm: O(P*S*D); interval-tree optimization tracked as M10 W2 follow-up") | interval trees, dimension-indexed buckets, hash-based prefilter |
| DDM subscriber resolution under regions | `rti/internal/ddm/manager.go` `SubscribersForUpdate` | cached fan-out sets, eager indexing |
| Ownership negotiation | `rti/internal/ownership/manager.go` divest/acquire/cancel methods | optimistic / market-based / authority-based protocols |
| Save bundle format | `rti/internal/savepoint/manifest.go` | binary, snapshot-based, per-manager snapshots |
| Replay strategy | `rti/internal/eventlog/replayer.go` | parallel replay, partial replay, replay against alternative manager state |
| Fanout strategy | `rti/internal/object/{interaction,update}.go` | sharded fanout, per-recipient async pumps |
| FOM merge | `rti/internal/fom/mim/merge.go` | alternative MIMs, FOM hot-reload |

Roughly 11 algorithm hot points worth opening up. Not all need to be
done at once.

---

## 3. Design decisions — OPEN

These three pin the rest of the doc and the implementation work.

### 3.1 Researcher-friendliness level — OPEN

| Option | What it means | Cost | Recommendation |
|---|---|---|---|
| (a) Interface refactor only | Researchers fork the repo, swap an impl, recompile rtid | smallest; just phases 0+1+2 | start here |
| (b) In-tree alternatives | Multiple impls live in tree, selected by config; researchers add alongside | medium; adds phases 3+4 | **proposed default** |
| (c) Out-of-tree plugins | Go `plugin` package / WebAssembly / RPC | high; determinism + version-pinning headache | not recommended |

Default proposal: **(b)**. Lets researchers contribute their alt
implementations upstream without needing fork maintenance. Keeps
determinism review centralized. Doesn't preclude (c) later.

### 3.2 Determinism contract — OPEN

The cut-1 master plan promised byte-identical event-log replay across
runs. Alternative algorithm implementations may or may not preserve
that:

| Mode | Rule | Spec-test consequence |
|---|---|---|
| **strict** | Every alt impl MUST produce a byte-identical event log to the default impl on the same inputs | Hardest bar; rules out non-deterministic algorithms; current spec tests apply unchanged |
| **per-impl opt-in** | Each impl declares whether it is determinism-preserving; the M3/M4 replay tests run only against impls that opt in | Most flexible; researchers can study non-deterministic optimistic variants without breaking the suite |
| **off** | Determinism is researcher-managed; replay tests skip when an alt is selected | Loosest; complicates conformance comparisons |

Default proposal: **per-impl opt-in**. Each `Module` declares
`DeterminismPreserving() bool`. The replay test suite filters by this
flag; conformance behavior tests (M0..M12 service correctness) run
against all impls.

### 3.3 Research focus order — OPEN

Phase 2 extracts algorithms. Order matters because each algorithm
extraction is a real refactor of a working service. Common research
foci:

1. **Time advance** — the richest research area; LBTS variants,
   optimistic vs conservative, lookahead policies. Touches: `time/`,
   `core.TimeManager`.
2. **DDM filtering** — overlap algorithms are well-studied; this
   already has a TODO for interval trees (M10 W2 follow-up). Touches:
   `ddm/`.
3. **Ownership protocols** — negotiation strategies (market-based,
   bidding, etc.). Touches: `ownership/`.
4. **Replay / save** — partial replay, snapshot-based save. Touches:
   `savepoint/`, `eventlog/`.
5. **Fanout strategy** — already well-optimized post-perf-pass; less
   research-relevant unless studying scaling.

Default proposal: pick **2 of the top 3** for Phase 2 (time + DDM, or
time + ownership). Defer the rest.

---

## 4. Module conventions

Convention to apply across all extension points after Phase 1.

### 4.1 Naming

- The interface lives in `rti/internal/core/` if it's cross-service, or
  in the owning service package if it's a per-service strategy.
- Default implementation is `<service>.Default<Service>` or just
  `<service>.New(...)` returning the default. Alternatives sit in
  sibling files: `<service>/alt_<name>.go`.
- Files named `alt_*.go` are reserved for alternative implementations
  (gives a grep-friendly handle).

### 4.2 Module shape

Every module follows the existing `Options{} → New(opts) → *Manager`
pattern. The `Options` struct gains an optional `Strategy <Iface>`
field; nil means "use the package default". This preserves backward
compatibility — existing call sites that don't set `Strategy` keep
working.

```go
// time/manager.go
type Options struct {
    // existing fields...
    LBTSStrategy LBTSStrategy   // nil → default DefaultLBTS
    GrantStrategy GrantStrategy // nil → default DefaultGrant
}
```

### 4.3 Lifecycle

Modules get **no** Init/Start/Stop interface in Phase 1. The existing
`New(opts) → *Manager, error` is enough. Lifecycle hooks are deferred
unless a concrete need arises (avoid speculative complexity per
CLAUDE.md).

### 4.4 Metrics

Every algorithm-level extension point exposes a single `Metrics()` hook
that returns a small struct (counters + last-result fields). Researchers
can pluck this to compare runs. Concrete shape TBD; mirrors the existing
MOM `OnXxxSuccess` hook pattern.

---

## 5. Service-level extension points (Phase 1 detail)

For each concrete-only Manager, the proposed interface and consumers.

### 5.1 `sync.Manager` → `core.SyncCoordinator`

Methods to expose:
- `RegisterSynchronizationPoint(...)`, `SynchronizationPointAchieved(...)`,
- `MembersResolver` plumbing (currently uncalled — cut-3 backlog).

Consumers: gRPC `transport/grpc/sync.go`, MOM hooks. Switch both to
take the interface.

Risk: low. ~50 lines diff. Existing concrete keeps working.

### 5.2 `ownership.Manager` → `core.OwnershipCoordinator`

Methods to expose: all 8 §7 RPCs + `RegisterInitialOwnership`,
`fanoutAssumption`.

Consumers: gRPC `transport/grpc/ownership.go`, `object.Registry`'s
`OnRegister` hook.

Risk: low.

### 5.3 `mom.Manager` → `core.ManagementObjectModel`

Already mostly hook-shaped. Just formalize the surface as an interface.
Consumers: every other manager's `OnXxxSuccess` callback, gRPC handler.

Risk: low.

### 5.4 `ddm.Manager` → `core.DataDistributionManagement`

Already has a `DDMFilter` interface for one consumer (object.Registry).
Generalize to expose the full surface (regions, routing spaces, region-
scoped pub/sub) so researchers can swap the whole DDM not just the
filter slice.

Risk: low-medium. The gRPC handler currently binds to the concrete
type; switch to interface.

### 5.5 `savepoint.Manager` → `core.SavepointCoordinator`

Methods: 7 RPCs + `Storage` injection (already pluggable today).

Risk: low.

### 5.6 `declaration.Manager` → `core.DeclarationManagement`

Per `docs/idd.md` §3 this is intentionally concrete; revisit whether
research demand justifies an interface. **OPEN — recommend deferring
to Phase 2** if a concrete need arises (e.g., a researcher wanting to
swap subscriber resolution).

---

## 6. Algorithm-level extension points (Phase 2 detail)

Per-service, the algorithm hooks worth carving out. Each is its own
small Phase-2 commit.

### 6.1 Time package

```go
// rti/internal/time/strategy.go
type LBTSStrategy interface {
    LBTS(regulators []RegulatingFederate) core.LogicalTime
    Name() string
    DeterminismPreserving() bool
}

type GrantStrategy interface {
    DecideGrant(ctx GrantContext) GrantDecision
    Name() string
    DeterminismPreserving() bool
}
```

`time.Options` gains `LBTSStrategy` + `GrantStrategy` fields; nil →
default impls (the current code, lifted into `defaultLBTS`,
`defaultGrant` types). Existing tests untouched.

### 6.2 DDM package

```go
// rti/internal/ddm/strategy.go
type OverlapStrategy interface {
    Overlap(pub, sub []regionBounds) bool
    Name() string
    DeterminismPreserving() bool
}
```

The current `regionsOverlap` becomes the default. Researchers add
`alt_intervaltree.go`, `alt_dimensionhash.go`, etc.

### 6.3 Ownership package

```go
// rti/internal/ownership/strategy.go
type NegotiationStrategy interface {
    OnNegotiatedDivest(...) ...
    OnAcquire(...) ...
    Name() string
    DeterminismPreserving() bool
}
```

Trickier: ownership state machine is split across many methods.
Approach: extract the policy decisions (e.g., "should this acquire be
granted given current pending divests?") into the strategy; keep the
state-machine bookkeeping in `Manager`.

### 6.4 Eventlog / Savepoint

```go
// rti/internal/savepoint/strategy.go
type ManifestFormat interface {
    Encode(snapshot Bundle) ([]byte, error)
    Decode([]byte) (Bundle, error)
    Name() string
    DeterminismPreserving() bool
}
```

### 6.5 Object registry fanout

Already documented as out-of-scope for this refactor (post-perf-pass
the fanout is well-tuned). Revisit if a researcher specifically wants
to study scaling.

---

## 7. Composition

### 7.1 Phase 1: composition root only

`cmd/rtid/main.go` constructs all managers today. Phase 1 doesn't
change that — researchers fork it and swap the `New(...)` calls.

### 7.2 Phase 3: registry + config

After Phase 2, add a small registry:

```go
// rti/internal/research/registry.go
type Registry struct {
    timeStrategies   map[string]time.GrantStrategy
    lbtsStrategies   map[string]time.LBTSStrategy
    overlapStrategies map[string]ddm.OverlapStrategy
    // ...
}

func (r *Registry) Register(category, name string, impl any) error
func (r *Registry) Lookup(category, name string) (any, bool)
```

`cmd/rtid/main.go` reads a `--research-config <file>` flag (or env
vars) that names which alt impls to wire. If the flag is absent, all
defaults apply and behavior is identical to today.

OPEN: config format. Recommendation = **TOML** for human-friendly hand
editing; JSON if we want to keep the existing JSON-everywhere pattern.

---

## 8. Determinism contract

Pinning the rule from §3.2:

1. Every algorithm strategy interface declares
   `DeterminismPreserving() bool`.
2. The replay tests (`rti/spec/M3/replay_test.go`,
   `examples/go-timed/replay_test.go`,
   `pysdk/tests/spec/m4/test_spec_m4_replay.py`) check the active
   impl's flag at startup. If false, the test reports SKIP with a
   reason, and the suite continues.
3. Behavioral conformance tests (M0..M12 except replay) run against
   all impls.
4. Researchers writing non-determinism-preserving impls add a
   regression suite under `rti/research/<name>/` validating their
   own correctness.

This makes determinism a property of the chosen impl set, not of the
codebase.

---

## 9. Phasing

Repeated from earlier with refinement.

| Phase | Deliverable | Concrete output | Risk | Effort |
|---|---|---|---|---|
| 0 | This doc + agreement | `docs/research-platform.md` | none | DONE pending review |
| 1 | Service-level interfaces for the 6 concrete-only managers | 6 commits, one per service: extract interface, switch consumers | low; no behavior change | medium |
| 2a | Time strategies | `time/strategy.go` + extracted defaults; alt LBTS/Grant impls deferred | medium; touches hot path | medium |
| 2b | DDM overlap strategy | `ddm/strategy.go` + interval-tree alt as reference | medium | small |
| 2c | Ownership negotiation strategy | `ownership/strategy.go` + extraction | medium | medium-large |
| 3 | Module registry + config-driven assembly | `internal/research/` + `--research-config` flag | medium; new infra | medium |
| 4 | One reference alternative impl per Phase-2 service | e.g. `ddm/alt_intervaltree.go`, `time/alt_optimistic.go` | low (additive) | medium |

Each phase keeps M0..M12 spec tests green. Per-phase commits are
revertable.

---

## 10. Open questions before Phase 1 starts

1. **Researcher-friendliness level** (§3.1): default proposal (b)?
2. **Determinism contract** (§3.2): default proposal "per-impl opt-in"?
3. **Phase 2 focus** (§3.3): which 2 services first? Default proposal:
   time + DDM.
4. **Declaration management** (§5.6): defer or include in Phase 1?
5. **Config format** (§7.2): TOML or JSON?

Once §10.1 + §10.2 + §10.3 are pinned, Phase 1 is mechanical and can
be dispatched as 6 small commits.

---

## 11. Non-goals (stated to prevent scope creep)

- Hot-swapping running implementations.
- Out-of-process plugin loading (`plugin` package, gRPC plugins, WebAssembly).
- A dependency-injection framework. The existing `Options{}` pattern is
  enough.
- Backward-compatibility with non-1516-2010 RTIs.
- Web UI / GUI for configuring research runs.
- Distributed RTI (cut-3 backlog item M15).
