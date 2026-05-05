# gorti as an HLA/RTI research platform — design

Status: Phase 0 — design only, no code changes yet. The five decisions
flagged in §10 of the original draft are now PINNED (see §3); Phase 1
work can be dispatched once this is reviewed.

Decisions pinned 2026-05-05:
- (b) in-tree alternatives selected by TOML config
- determinism contract is a config knob with default "per-impl opt-in"
- Phase 2 focus: time + ownership
- declaration management is in Phase 1 (not deferred)
- config format: TOML

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

## 3. Design decisions — PINNED

### 3.1 Researcher-friendliness level — **(b) in-tree alternatives**

Multiple impls live in tree under `rti/internal/<service>/alt_*.go`;
researchers contribute new impls alongside the defaults. Selection is
config-driven (§7.2). Out-of-process plugins are deferred indefinitely.

### 3.2 Determinism contract — **per-impl opt-in (default), config-selectable**

The contract itself is a runtime knob with three settings; the default
is **per-impl opt-in**.

| Setting | Rule | Spec-test consequence |
|---|---|---|
| `determinism = "strict"` | Every wired impl must satisfy `DeterminismPreserving() == true`; the rtid composition root rejects mixes that include any non-preserving impl. Current M3/M4 replay tests apply unchanged | Conservative; rules out optimistic / market-based research variants |
| `determinism = "per-impl-opt-in"` (default) | Each impl declares its own `DeterminismPreserving() bool`. Replay tests filter by the active impls' flags: skip with reason when any wired impl is non-preserving; run normally when all are | Most flexible; the conformance baseline is preserved without locking out non-deterministic research |
| `determinism = "off"` | Replay tests are unconditionally skipped. Researchers manage determinism manually | Pure exploratory work; conformance comparisons become per-researcher responsibility |

Mechanism: the TOML research config carries a top-level
`determinism = "..."` setting that defaults to `"per-impl-opt-in"`
when absent. Replay test fixtures read it (via env var
`GORTI_DETERMINISM=...` or by reflecting on the active rtid config)
and gate accordingly.

### 3.3 Research focus order — **time + ownership**

Phase 2 carves algorithm-level strategies in this order:

1. **Time advance** — LBTS variants, grant decision strategies. Touches
   `time/` and `core.TimeManager`. (See §6.1.)
2. **Ownership protocols** — negotiation / divest / acquire strategies.
   Touches `ownership/`. (See §6.3.)

DDM overlap, replay/save, and fanout strategy are deferred to a later
phase; the §6 sketches for them stay in this document as a roadmap
but no Phase 2 work targets them.

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

**Included in Phase 1** (decision §3 / 2026-05-05).

`docs/idd.md` §3 currently calls this "pure local component, no
abstraction layer". The Phase 1 commit adds `core.DeclarationManagement`
and updates that doc note: research-platform reachability outweighs the
purity argument. The interface exposes the publish / subscribe / lookup
methods plus the InteractionPublishersFor / InteractionSubscribersFor
slice (the only ones currently consumed by `object.Registry` and the
gRPC declaration handler).

Risk: low. Touching the doc note in `docs/idd.md` is a small
secondary edit.

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

### 6.2 DDM package — DEFERRED past Phase 2

(Sketch retained as roadmap.) The current `regionsOverlap` would
become the default behind an `OverlapStrategy` interface; researchers
would add `alt_intervaltree.go`, `alt_dimensionhash.go`, etc. Not in
Phase 2 per §3.3; revisit when a researcher signals demand.

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

### 7.2 Phase 3: registry + TOML config

After Phase 2, add a small registry:

```go
// rti/internal/research/registry.go
type Registry struct {
    lbtsStrategies        map[string]time.LBTSStrategy
    grantStrategies       map[string]time.GrantStrategy
    negotiationStrategies map[string]ownership.NegotiationStrategy
    // ...
}

func (r *Registry) Register(category, name string, impl any) error
func (r *Registry) Lookup(category, name string) (any, bool)
```

`cmd/rtid/main.go` reads `--research-config <file.toml>` (or env vars
for one-off overrides). Flag absent → all defaults apply, behavior
identical to today.

**Config format: TOML** (decision §3 / 2026-05-05). Sample:

```toml
# Top-level determinism contract: "strict" | "per-impl-opt-in" | "off".
# Default when absent: "per-impl-opt-in".
determinism = "per-impl-opt-in"

[time]
lbts = "default"             # alternates: "naive-min", "lookahead-pinned"
grant = "default"            # alternates: "optimistic-tara", "lazy"

[ownership]
negotiation = "default"      # alternates: "market-based", "bidding"
```

Researchers add a TOML file, point rtid at it, get an alternative
configuration. Adding a new alternative impl = ship a `alt_*.go` file
+ add a `Register(...)` call in the package's `init()` (or a small
manual registration in main).

---

## 8. Determinism contract

Pinning the rule from §3.2:

1. Every algorithm strategy interface declares
   `DeterminismPreserving() bool`.
2. The active rtid carries a determinism mode read from the TOML
   research config (or env `GORTI_DETERMINISM`); default is
   `"per-impl-opt-in"`.
3. Replay tests (`rti/spec/M3/replay_test.go`,
   `examples/go-timed/replay_test.go`,
   `pysdk/tests/spec/m4/test_spec_m4_replay.py`) gate based on the
   active mode AND the wired impls' flags:
   - `strict`: composition root rejects any non-preserving impl at
     boot; replay tests run unchanged.
   - `per-impl-opt-in`: replay tests SKIP with reason if any wired
     impl reports `DeterminismPreserving() == false`; otherwise run
     unchanged.
   - `off`: replay tests skip unconditionally.
4. Behavioral conformance tests (M0..M12 except replay) run against
   every wired impl regardless of mode.
5. Researchers writing non-determinism-preserving impls add a
   regression suite under `rti/research/<name>/` validating their
   own correctness.

This makes determinism a property of the active rtid configuration,
not of the codebase.

---

## 9. Phasing

Repeated from earlier with refinement.

| Phase | Deliverable | Concrete output | Risk | Effort |
|---|---|---|---|---|
| 0 | This doc + agreement | `docs/research-platform.md` | none | DONE 2026-05-05 |
| 1 | Service-level interfaces for the 6 concrete-only managers (sync, ownership, MOM, DDM, savepoint, declaration) | 6 commits, one per service: extract interface in `core/`, switch consumers | low; no behavior change | medium |
| 2a | Time strategies | `time/strategy.go` with `LBTSStrategy` + `GrantStrategy`; defaults extracted unchanged; no alt impls yet | medium; touches hot path | medium |
| 2b | Ownership negotiation strategy | `ownership/strategy.go` with `NegotiationStrategy`; defaults extracted unchanged | medium | medium-large |
| 3 | Module registry + TOML config-driven assembly | `internal/research/` registry + `--research-config <file.toml>` flag in cmd/rtid; determinism mode honored | medium; new infra | medium |
| 4 | One reference alternative impl per Phase-2 service | e.g. `time/alt_optimistic.go`, `ownership/alt_market.go`; opt-in via TOML | low (additive) | medium |
| Future | DDM overlap, replay/save, fanout strategies; out-of-process plugins | (deferred — see §6.2, §6.4, §6.5, §11) | n/a | n/a |

Each phase keeps M0..M12 spec tests green. Per-phase commits are
revertable.

---

## 10. Phase 1 dispatch plan

Decisions pinned. Phase 1 is six small mechanical commits, one per
concrete-only manager. Each commit:

1. Adds a `core.<Name>` interface in `rti/internal/core/`.
2. Asserts the existing concrete `*Manager` satisfies it (compile-time
   `var _ core.<Name> = (*Manager)(nil)`).
3. Switches consumers (gRPC handlers, composition root in
   `cmd/rtid/main.go`, related test fakes) from `*Manager` to the
   interface.
4. Updates `docs/idd.md` if the service had an explicit "no
   abstraction" note (declaration is the only one).
5. Verifies `go test -race ./...` clean + cross-language pysdk tests
   green.

Per-commit subjects:

1. `refactor(sync): extract core.SyncCoordinator interface`
2. `refactor(ownership): extract core.OwnershipCoordinator interface`
3. `refactor(mom): extract core.ManagementObjectModel interface`
4. `refactor(ddm): extract core.DataDistributionManagement interface`
5. `refactor(savepoint): extract core.SavepointCoordinator interface`
6. `refactor(declaration): extract core.DeclarationManagement interface`

Each is independently revertable.

---

## 11. Non-goals (stated to prevent scope creep)

- Hot-swapping running implementations.
- Out-of-process plugin loading (`plugin` package, gRPC plugins, WebAssembly).
- A dependency-injection framework. The existing `Options{}` pattern is
  enough.
- Backward-compatibility with non-1516-2010 RTIs.
- Web UI / GUI for configuring research runs.
- Distributed RTI (cut-3 backlog item M15).
