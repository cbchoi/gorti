# DDM Registration And Dimension Context

Date: 2026-09-19. Scope: audit F11/F12, bounded engineering corrections.
This is not a full DDM conformance assessment or a normative clause inventory.

## Plan

The region-registration RPC returned success with handle zero without creating
an object. Go/Python registered an ordinary object before calling that RPC;
the C++ standard overloads registered first and associated regions afterward.
Invalid later bindings could therefore leave an object behind, and discovery
could occur before its region associations existed.

Acceptance requires one server registration operation, real returned identity,
all-input validation before object/name mutation, region context from the FOM,
ownership initialization before discovery, correct ordinary/regional recipients,
and state preservation on validation or journal failure. Numeric range changes
must reject reversed or over-limit bounds without replacing pending bounds.

## Implementation

- All three SDK registration paths issue one existing DDM registration RPC.
  Repeated attribute entries are combined as region-set unions. A zero success
  handle is rejected rather than fabricated locally.
- The object registry captures one publication snapshot. The DDM preparation
  validates region existence, creator and dimension-set context and pins the
  referenced regions until the object/name transaction commits or fails.
  This bounded implementation accepts matching dimension sets only. The exact
  normative rules for subset/template expansion and extra dimensions remain
  Q09 work; the tests do not establish equality as a universal IEEE rule.
- Lock order is checkpoint admission, membership admission, object state, then
  DDM. Preparation does not call object, ownership or membership services.
  DDM is released before ownership initialization; the object lock is retained
  until ownership is initialized. Discovery runs after releasing these locks.
- Failed validation or journal append consumes no object ID, name reservation,
  association, ownership hook or discovery callback. Object deletion and
  resign deletion remove the corresponding region associations.
- Registration discovery uses the captured published attributes, including
  handles above eight. Production FOM enumeration replaces fixed-range probes.
- Attribute reflection combines ordinary subscriptions with overlapping region
  subscriptions. Eligible attributes without explicit publisher regions use
  the existing default-region routing policy; duplicate recipients are removed.
- The FOM model/parser retain attribute and interaction dimension references,
  including inherited and standard-MIM declarations. References and numeric
  upper bounds are checked. Returned model collections are detached copies.
- Range failures carry `InvalidRangeBound`; incompatible attribute/region
  dimensions carry `InvalidRegionContext` through the existing exception trailer.

## Review And Corrections

Independent code review found missing dimension metadata, fixed-range discovery,
ownership initialization after discovery, lost ordinary subscriptions, stale
associations after deletion and nonrepresentable audit records. These were
included in the implementation and regression tests. The review also separated
the remaining whole-DDM issues listed below from this milestone.

Follow-up review corrected duplicate modular dimension declarations that gave
different attribute/DDM handles, inconsistent unspecified-upper-bound handling,
automatically generated name collisions, zero success handles and Python range
exception conversion. The existing timer/cancellation stress test could exceed
its own queue capacity when timers emitted singleton batches. Its capacity now
accommodates all three events; production delivery code was not changed for
that test correction.

The frozen audit log format cannot preserve the new registration's region state.
The raw codec now rejects the scoped record before writing or consuming sequence
state, rather than encoding it as an ordinary registration. A directly injected
strict writer therefore rejects the operation before mutation. The production
audit plugin is deliberately a non-interfering observer: it records the failure
in its status and warning log but allows the HLA operation to continue. Such a
journal is incomplete and must not be used as DDM state-replay evidence.
The HLA core profile and same-roster snapshot path support the operation.
Completing DDM audit replay and persistent incomplete-trace rejection requires
a separate versioned persistence design; a snapshot round trip is not replay.

## Verification

Final integrated Go source and tests were frozen before these runs:

| Campaign | Result |
|---|---|
| Windows Go 1.26.5: `go test -count=1 ./rti/... ./tests/... ./verification/... ./examples/...` | PASS |
| WSL Ubuntu 24.04 / Go 1.22.2: `go test -race -count=1 ./rti/...` | PASS |
| `python -B scripts/ieee2010_quality_gate.py --run` | PASS: 37 command groups, 215 required top-level tests; no failed or skipped required tests |
| Python SDK `test_ddm_atomic_registration.py` | 19 passed; exact forwarding and error-contract tests, not a production-server campaign |
| C++ SDK, existing WSL build, direct runtime executable runs | 9/9 executables, 141/141 cases passed, including 18/18 DDM contract cases; not a fresh CTest or backend integration campaign |
| Gate implementation standard-library regression | 28 passed |
| `--claim full-2010 --check` | Expected FAIL; zero commands started, conformance not established |
| Scoped whitespace checks | PASS |

The real-server integration suite uses an owned subprocess running the actual
server composition and four federate clients over gRPC. It passed registration
routing, failure atomicity, range-bound invariance and saved-association restore.
These are four clients, not four independent federate processes.

The local/manual gate includes literal required test names for domain, object,
wire, production server, FOM, Go SDK, audit codec and non-interfering observer
tests. It does not add automatic CI work or turn these engineering tests into
normative obligations. Manifest canonical SHA256:
`d3c927e793feb236280c87d3ccf0c9b51ff074d48686c7793d220fffdbc8dcce`.

The named and unnamed C++ zero-handle regressions failed before the guard and
passed afterward. No fresh performance, native Windows C++ or independent
standards-binding campaign was performed in this milestone.

Final gate transcripts are outside Git:
`D:/workspace/gorti/ddm-evidence-20260919/bounded-gate.json` and
`D:/workspace/gorti/ddm-evidence-20260919/full-readiness.json`.
The first broad runs overlapped the audit writer's failing-test phase, and the
first race run also exposed the timer-test capacity flaw. They are not reported
as passes; the final commands above were rerun after those corrections.

TDD evidence includes direct old-handler failures (zero handle, no domain call,
ignored invalid input), domain range/preparation failures, SDK double-call
failures and FOM metadata/alias/limit failures. The production negative campaign
uses a two-file pre-fix overlay, not an untouched historic release. Raw logs are
kept outside the repository. See the production test README and the
[FOM evidence record](ieee1516-2010-ddm-fom-evidence.md).

## Compatibility

Protobuf messages, descriptors and RPC signatures are unchanged. Upgrade the
Go/Python/C++ SDKs with the server: old Go/Python clients perform a preliminary
ordinary registration and are not compatible with the corrected fused call.
Do not infer an idempotent retry from an object name.

Resolve dimension handles by name in each federation. Standard-MIM dimensions
are now retained, so numeric positions must not be hard-coded. This milestone
does not establish cross-version restore of older DDM handle tables. Existing
same-roster runtime checkpoints are tested with the current schema and model.

`model.Attribute` and `model.InteractionClass` now contain dimension slices;
attributes are no longer comparable with Go's `==`. Module comparison uses
dimension-set equality. Ordinary event-log bytes remain unchanged. A strict
raw audit writer can return `Unimplemented` through gRPC; the production audit
observer reports recording failure without changing core service results.

## Reflect And Next Work

F11 is partially corrected: region registration is implemented end to end, but
per-call regions in interaction sends and attribute-update requests remain open.
F12 is partially corrected: bounds and registration context are checked, but
the full association/subscription/unsubscription ownership/context matrix and
multi-pair operation semantics are not closed.

Other open items include late DDM discovery, concurrent discovery deduplication,
callback queue failure handling during registration, inherited/default-region
semantics, active/passive subscriptions, timed tags/retraction metadata and
complete DDM persistence validation. Existing endpoint/equal-range overlap
behavior was not changed without normative review. Explicit XML zero upper
bounds are rejected by the inspected FDD schema. An omitted bound retains the
legacy model zero sentinel, interpreted consistently as the uint64 maximum
by region creation and range validation.

The full IEEE 1516-2010 claim remains blocked on the normative texts, applicable
corrections, complete obligation mapping and independent RTI-directed evidence.
The bounded gate passing is not a certification or a percentage-complete score.
