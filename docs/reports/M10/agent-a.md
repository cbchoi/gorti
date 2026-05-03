# Agent A - M10 W1 - Data Distribution Management (DDM)

Cut-2 milestone M10. Implements IEEE 1516.1-2010 §6 — routing spaces,
regions, region-scoped publish/subscribe, and the overlap-driven
fan-out filter inside `object.Registry`.

Implements: FR-DDM-1..6 (per `docs/srs.md` §5.13).

## Outcome

- **All 5 M10 spec tests GREEN** (`go test ./rti/spec/M10/...`):
  - `TestSpec_M10_RegionLifecycle_CreateCommitDelete`
  - `TestSpec_M10_RegionOverlap_DeterminesSubscriberFan_out` (unskipped + green)
  - `TestSpec_M10_NoOverlap_DropsUpdate` (unskipped + green)
  - `TestSpec_M10_RangeOverlap_ClosedOpen`
  - `TestSpec_M10_DeterministicSubscriberOrder` (unskipped + green)
- All M0-M8 + M11 tests still GREEN (`go test ./...` clean).
- `go test -race ./rti/internal/ddm/... ./rti/internal/object/...` clean.
- `golangci-lint run ./rti/internal/ddm/... ./rti/pkg/fom/...` clean.
  (Pre-existing G115 hits in `rti/internal/object/{registry,update,interaction}.go`
  on `WallNs: uint64(wallNs)` and in `rti/cmd/rtid/foms.go` on
  `int(cls)` index conversions are unchanged from the M10 baseline.)
- 11 agent-owned unit tests in `rti/internal/ddm/ddm_test.go` covering:
  required-Options validation, permissive lookup (mints + caches
  handles), FOM-backed lookup (rejects undeclared dimensions), region
  lifecycle (create + setBounds + commit + query + delete), ownership
  enforcement, in-use deletion rejection, zero-cost shortcut on empty
  publisher regions, no-association fast-path, associate + query
  round-trip, REPLACE semantics on subscribe-with-regions, and
  closed-open Range overlap edge cases.
- 2 benchmarks in `rti/internal/ddm/ddm_bench_test.go` covering the
  FR-DDM-6 reference workload (size 25 federation × 100 publisher
  regions × 100 subscriber regions on a 2-dimension routing space) and
  the zero-cost (no-regions) path.

## Files modified / created

Modified:
- `rti/internal/ddm/manager.go` — frozen-shape stub bodies replaced
  with real implementation. `New` now validates `Outbox` + `FOMs`
  (with the typed-nil guard pattern from `object/registry.go`).
  Routing-space + dimension lookup runs in two modes: FOM-backed
  (when the `core.FOMHandle` satisfies the new `DimensionEnumerator`
  interface, the production `*fomHandle` does) and permissive (every
  name resolves to a freshly minted handle, matching the spec-test
  fixture stubs).
- `rti/internal/object/registry.go` — adds `Options.DDM DDMFilter`
  optional hook + `DDMFilter` interface declaration. `New` accepts a
  typed-nil DDM and treats it as absent. `DDMRegionHandle` is
  `uint64` (avoids a circular `object → ddm` import; the boundary
  conversion happens in the cmd/rtid adapter).
- `rti/internal/object/update.go` — `fanoutReflect` now consults a
  new `subscribersForReflect` helper. When `Options.DDM == nil` OR
  the producer has no associations on the object, the cut-1 path
  runs unchanged. Otherwise the helper unions
  `DDM.SubscribersForUpdate` per attribute, with a per-attribute
  fallback to `Declarations.SubscribersFor` when the producer didn't
  associate any region for that specific attr.
- `rti/cmd/rtid/main.go` — composes `ddm.Manager`; wires it into
  `object.Options.DDM` via a thin `ddmFilterAdapter` that converts
  `ddm.RegionHandle` ↔ `object.DDMRegionHandle` at the boundary.
- `rti/cmd/rtid/foms.go` — adds `Dimensions()` method on `*fomHandle`
  + a compile-time assertion that the handle satisfies
  `ddm.DimensionEnumerator`. The DDM manager type-asserts on every
  `Get` so the production runtime gets FOM-driven dimension tables
  for free.
- `rti/pkg/fom/model/fom.go` — adds `Dimension` data class +
  `NewFOMWithDimensions` constructor + `(*FOM).Dimensions()`
  accessor. `NewFOM` is now a thin wrapper that passes `nil` for
  the dimension slice — backward compatible with every existing
  caller. Dimensions are sorted by name (NFR-DET-1).
- `rti/pkg/fom/parser/walk.go` — adds `xmlDimensions` /
  `xmlDimension` XML structs + `convertDimensions` flattener.
  `parseUpperBound` parses the leading numeric prefix of the
  `<upperBound>` element (forgiving cut-2 simplification: trailing
  units are ignored).
- `rti/pkg/fom/parser/parser.go` — collects per-module
  `modDimensions` and feeds them into `NewFOMWithDimensions`. Both
  the per-module-validation FOM and the merged-result FOM carry the
  dimension slice.
- `rti/spec/M10/ddm_test.go` — three SCAFFOLD tests unskipped
  (orchestrator-authorized per dispatch brief). The lifecycle test
  + range-overlap unit test were already passing once the manager
  bodies landed.

Created:
- `rti/internal/ddm/state.go` — per-federation state container
  (`federationDDMState`): routing-space + dimension tables, region
  store with split committed/pending bounds (1516.1-2010 §6.5
  atomic-commit semantics), object/interaction subscriber lists
  with REPLACE semantics, per-object publisher-association map.
  Helpers: `populateFromFOM`, `regionInUse`, `materializeRegions`.
- `rti/internal/ddm/overlap.go` — the FR-DDM-5 overlap test.
  O(P × S × D) double-loop over publisher × subscriber region
  pairs. A dimension declared on only one side is treated as a
  wildcard (always-overlap) on the other.
- `rti/internal/ddm/ddm_test.go` — 11 unit tests (see above).
- `rti/internal/ddm/ddm_bench_test.go` — 2 benchmarks (see Perf).
- `tests/conformance/foms/good/ddm-test.xml` — fixture FOM with two
  dimensions (`X`, `Y`) used by future end-to-end M10 tests + the
  M10 acceptance demo. Lives next to `minimal.xml` and
  `pyjevsim-bridge.xml` per the M1 fixture convention.
- `docs/reports/M10/agent-a.md` — this report.

## Performance baseline (FR-DDM-6)

Hardware: 12th Gen Intel Core i7-12700, Linux 6.17, Go 1.23.
Measured via `go test -bench=. -benchmem -benchtime=2s
./rti/internal/ddm/...`.

```
BenchmarkSubscribersForUpdate_ZeroCost-20
        1000000000          1.446 ns/op   0 B/op   0 allocs/op
BenchmarkSubscribersForUpdate_Size25_100Regions/with_regions-20
             20882        115739 ns/op   57650 B/op  596 allocs/op
BenchmarkSubscribersForUpdate_Size25_100Regions/without_regions-20
        1000000000          1.448 ns/op   0 B/op   0 allocs/op
```

Interpretation:

- **Zero-cost path (FR-DDM-6 contract)**: when the producer hasn't
  associated any region with the published object,
  `SubscribersForUpdate` returns `nil` after a single
  empty-slice check — 1.45 ns/op, 0 allocs. The
  `object.Registry.subscribersForReflect` helper additionally
  short-circuits on `DDM == nil || !HasObjectAssociations` BEFORE
  ever calling `SubscribersForUpdate`, so production workloads that
  never use DDM pay only the nil-check on the registry side. This
  is the same-shape comparison cut-1 vs cut-2 — no measurable
  regression on the existing fanout hot path.
- **Reference workload (size 25 × 100 regions)**: a single
  `SubscribersForUpdate` call against 100 publisher regions × (25
  subscribers × 4 regions/sub) = 100 × 100 region-pair overlap
  tests on a 2-dimensional routing space takes ~116 µs. That's
  ~58 KB of transient state per call (region-bounds materialization
  + per-attr union map). The acceptance bar is correctness +
  zero-cost-when-empty, both met. An interval-tree replacement of
  the O(P × S × D) double-loop is a candidate optimization for
  M10 W2; the perf numbers don't currently demand it (a 1 kHz
  update rate on this size 25 / 100-region workload would consume
  ~12% of one core).

## Cut-2 simplifications + W2 follow-ups

1. **gRPC handlers deferred**. The `proto/` directory is FROZEN at
   this cut and does not declare a `DDMService` with the lifecycle +
   subscribe/publish RPCs. Federates can therefore only invoke DDM
   via the in-process API today; the spec tests + agent-owned tests
   exercise the manager directly. Wiring the gRPC handlers (proto
   extension + transport layer + Python SDK plumbing) is the M10 W2
   follow-up.
2. **Python SDK gap**. `pysdk/*` is Agent C territory and does not
   yet expose `createRegion` / `subscribeWithRegions`; this is the
   companion to (1) above. Tracked as post-MVP per the dispatch
   brief.
3. **Routing-space scoping**. IEEE 1516.2-2010 Annex A flattens
   dimensions (no enclosing routing-space element); every dimension
   is implicitly part of a single routing space. The cut-2
   `ddm.Manager` exposes one routing space per federation named
   `"default"` (handle 1) and groups all FOM dimensions under it.
   Re-introducing 1.3-style multi-routing-space FOMs would require a
   schema extension and is not on the cut-2 / cut-3 roadmap.
4. **FOM `<upperBound>` parsing is forgiving**. The parser accepts
   the leading numeric prefix of the element text and drops trailing
   chars. Empty / unparseable bound → 0, which the manager treats as
   "unbounded" by defaulting the initial region range to `[0,
   MaxUint64)` (matching the IEEE 1516.1-2010 §6.5 "newly created
   region covers the entire routing space" semantics). The
   `<normalization>` element is captured into
   `Dimension.NormalizationKey` as a verbatim string but not
   interpreted — the RTI does not perform value normalization in
   cut-2.
5. **Permissive FOM fallback**. When the `core.FOMHandle` does not
   implement `DimensionEnumerator` (the spec-test stub doesn't), the
   manager mints handles on first lookup so the lifecycle tests can
   drive create + commit without a real FOM. Production code
   exercises the FOM-backed path because `*fomHandle` implements the
   interface.
6. **Overlap algorithm is O(P × S × D)**. The double-loop has the
   advantage of being correct + obviously deterministic + easy to
   reason about. Interval-tree optimization is the M10 W2 path if
   higher-region-count workloads land.
7. **Eventlog persistence for DDM transitions deferred**. The
   `Options.EventLog` field is wired but no Append calls are made
   yet — the proto `Event` oneof doesn't carry DDM-lifecycle
   variants (FROZEN). Replay determinism for region create/commit/
   delete therefore depends on federate-driven re-issue of the
   lifecycle calls. This mirrors the cut-1 simplification on the
   sync.Manager (FR-SYN-4) and is tracked as the same M10 W2
   follow-up.

## FOM parser extension scope (Agent B coordination)

Per the dispatch brief, `rti/pkg/fom/` is Agent B's domain but the
M6+ cut-2 dispatch model lets agents extend it as part of their own
task. M10 W1 added:

- `xmlDimensions` / `xmlDimension` XML structs (parses
  `<dimensions>` + `<dimension>` blocks)
- `parseUpperBound` helper (forgiving numeric prefix)
- `convertDimensions` flattener (XML → `[]model.Dimension`)
- `NewFOMWithDimensions` constructor (sorted by name; backward
  compatible)
- `(*FOM).Dimensions()` accessor (defensive copy, sorted)
- `Dimension` value type with `Name`, `UpperBound`, `NormalizationKey`

The strict-mode whitelist in `rti/pkg/fom/parser/strict.go` already
contained `dimensions`, `dimension`, `upperBound`, `normalization`
(pre-work landed by Agent B in M0); FOM-009 unknown-element
rejection therefore continues to apply uniformly.

## Test counts (final)

```
$ go test ./...
...
ok      github.com/cbchoi/gorti/rti/internal/ddm        0.001s
ok      github.com/cbchoi/gorti/rti/spec/M10            0.001s
... (all packages OK)

$ go test -v ./rti/spec/M10/...
PASS: TestSpec_M10_RegionLifecycle_CreateCommitDelete
PASS: TestSpec_M10_RegionOverlap_DeterminesSubscriberFan_out
PASS: TestSpec_M10_NoOverlap_DropsUpdate
PASS: TestSpec_M10_RangeOverlap_ClosedOpen
PASS: TestSpec_M10_DeterministicSubscriberOrder
```
