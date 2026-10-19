# IEEE 1516-2010 Service Implementation Checkpoint

For subsequent live FOM, checkpoint, rate, MOM, and SDK work, see the
[live-services checkpoint](ieee1516-2010-live-services-20260920.md).
The remaining-work table below describes the earlier checkpoint.

Date: 2026-09-20. Target: the 2010 editions of IEEE 1516, IEEE 1516.1,
and IEEE 1516.2. This checkpoint extends the existing implementation; it is
not a release or a declaration that every RTI service is complete.

## Implemented Behavior

| Area | Change | Executable boundary |
|---|---|---|
| Object management | Local deletion removes only the caller's knowledge and scope state. Ownership and pending acquisition prevent deletion. Continuing subscriptions can rediscover. | Unit contracts and a real server with four clients exercise peer isolation, rejection, query visibility, and rediscovery. |
| Object callbacks | Knowledge exists before discovery becomes observable. Missing discovery joins the same reservation as scope/reflect callbacks. Failed admission or commit does not leave fabricated knowledge. | Callback-time lookup, overflow, failed commit, retry, and concurrent cross-object reservation tests. |
| Support | Caller-scoped object name/handle queries and `GetKnownObjectClassHandle`. | Actual RPC plus Go, Python, and C++ forwarding tests. Legacy caller-zero lookups retain the previous global-read behavior. |
| Declaration | Active/passive options, named update-rate validation, initial FOM advisory metadata, switch RPCs, and inherited publication/subscription interest. | Passive subscriptions still receive matching data; remote active interests control registration/interaction advisories. Options survive snapshots. |
| Advisory admission | Ordinary and regional subscription changes reserve the complete advisory set before committing state. | Full recipient queues reject the change without partial delivery; draining and retrying produces the required edge once. |
| Scope advisories | The attribute-scope switch controls InScope/OutOfScope callbacks without suppressing reflections or losing scope state. | Real-server transitions with disable, region exit/reentry, enable, and subsequent scope changes. |
| DDM | Per-call region interaction routing, region-filtered update requests, current attribute-owner routing, creator/context checks, and partial unsubscribe. | Production RPCs cover mixed owners, inherited attributes, nonowners, foreign regions, malformed pairs, and independent region sets. |
| MOM | Actual per-class reliable traffic counters, explicit target handling, standard-MIM-shaped count reports, publication reports, and MIM data. | Registry traffic hooks through dispatcher, emitter, and stream; durable state, overflow, malformed input, and unavailable legacy history. |
| Save/restore | A bounded same-roster logical-time target-ceiling checkpoint. | Actual server progress, participant resignation, callback failure, abort, generation changes, pending requests, TSO queues, and the configured time horizon. |
| FOM | Update rates, initial advisory switches, and class ancestry survive production loading. A nonmutating extension preparation step identifies handle/cache changes. | Invalid/conflicting metadata rejection and unchanged live state after rejected extension preparation. |
| C++ | Immediate callback dispatch, callback disabling/resuming, exception propagation, joined Float64 factory, support queries, local deletion, and declaration/DDM option forwarding. | Dedicated runtime executable suite; lifecycle review and failure-path tests are recorded below. |

The counter boundary is RTI acceptance and admitted recipient fanout, not
successful network delivery or execution of the federate's callback. The
current data path is reliable; requested transport overrides are not used to
invent best-effort traffic statistics. Missing legacy counter history and
overflow produce explicit unavailable/error results rather than fabricated zero.

## Compatibility

- Existing RPCs and fields remain. Support caller identity, declaration/DDM
  options, the advisory switch RPC, and known-class query are additive.
- An additive-projection test removes only the reviewed additions and reproduces
  the previous descriptor and RPC hashes. The current full golden remains a
  separate guard. Passing it alone is not IEEE interface conformance.
- Declaration/DDM options and MOM traffic state are persisted. Legacy snapshots
  retain documented defaults; unknown historical detailed counts are not
  reconstructed from aggregate totals.
- Raw logs and experiment output stay outside the repository. No automatic CI
  policy changes, commit, push, or benchmark comparison were made in this work.

## Plan, Do, Review, Reflect

### Plan

Split DDM, declaration, MOM, scheduled checkpoint, FOM, and C++ work by file
ownership. Keep production composition, local object knowledge, compatibility,
and combined verification in the integration work. Preserve the existing
working tree and use failing behavioral assertions before implementation.

### Do

Connect service implementations to the production server rather than adding
test-only execution paths. Include RPC and callback observations as well as
state tests. Use the existing ports for outbox reservations, time evaluation,
checkpoint admission, and snapshots.

### Review

Independent review exposed failures beyond the initial happy paths:

1. Discovery was observable before caller-scoped lookup recognized the object.
   A failed rediscovery could then be followed by a reflection for an unknown
   object. Per-object delivery serialization and transactional discovery
   admission correct those cases.
2. Combining new knowledge publication with the old scope lock created a
   cross-object lock inversion between registry state and recipient queues.
   Scope snapshots now use short state-lock sections; per-object delivery
   serialization protects the operation across admission and commit.
3. Failed regional advisories originally left both subscription tables changed,
   so retrying had no edge to emit. Declaration admission and DDM rollback now
   preserve retry behavior. Resignation is different: mandatory cleanup must
   complete even if a surviving publisher's queue is full.
4. New snapshot fields exposed fixtures that assumed one JSON version or treated
   the existing MOM framed envelope as plain JSON. Tests now preserve framing
   and continue to reject malformed snapshots.
5. C++ review found callback-origin destruction and concurrent join/disconnect
   lifetime hazards, including a final lock-guard lifetime window. Worker state
   and lifecycle locks now have retained shared ownership; lifecycle entrypoints
   are serialized and callback reentry is rejected before lifecycle locking.
   Tests cover destruction from both synthesized and wire callbacks, including
   a callback queued before join returns. The final independent source review
   found no remaining blockers in those changes; that review did not rerun tests.

The regression fixtures for update-only fast paths now explicitly seed prior
discovery. Their original reservation and callback assertions were retained;
new tests cover the previously undiscovered case separately.

### Reflect

Successful RPC return, matching method names, and synthetic callback fixtures
are insufficient completion criteria. State visibility, callback ordering,
admission failure, retry, and lifecycle cleanup need distinct evidence. This
checkpoint closes observed implementation gaps but leaves architectural work
listed below. Existing full-conformance blockers remain active.

## Verification

Final Go integration results:

| Campaign | Result | External transcript |
|---|---|---|
| Windows Go 1.26.5, uncached `./rti/... ./tests/... ./verification/... ./examples/...` | PASS | `service-completion-go-verified.log` |
| WSL Linux Go 1.22.2, uncached `-race ./rti/...` | PASS | `service-completion-race-verified.log` |
| Go static analysis, the same four Windows roots | PASS | `service-completion-vet.log` |
| Local/manual gate | PASS, 59 command groups and 306 required top-level tests; no failed or skipped required tests | `service-completion-bounded-gate.json` |
| Full-readiness prerequisite check | Expected FAIL; no runtime commands started | `service-completion-full-readiness.json` |
| Python SDK focused regression | 37 passed | `service-completion-python-final.log` |
| C++ Linux runtime fixtures, final rebuilt library | 10 executables, 161 passed; no failures, disabled cases, or skips | `cpp-ieee2010-runtime-final-summary.json` and per-suite XML/log files |
| C++ immediate/lifecycle stress, final shared-state code | 10 cases repeated 20 times, 200 passed | `cpp-ieee2010-shared-lock-repeat-final.log` |
| Gate-script regression | 28 passed | Current local execution |

The documentation build was not executed: the selected Python environment
does not contain MkDocs. Markdown and scoped whitespace checks passed.

The C++ campaign uses runtime contract fixtures. It is not a C++ campaign
against the production `rtid`, an official-header/ABI campaign, a native
Windows C++ run, or a ThreadSanitizer run. The Go race campaign does not cover
C++ memory accesses. Unexpected receive-stream termination is still an open
C++ behavior and was not reclassified as fixed by lifecycle stress results.

The manifest names the exact required tests; counts are engineering coverage,
not percentages of the IEEE requirements. The gate's normalized manifest SHA-256
is `f1e3f3154e0cb476446ecaaa8d6b6d5329a5778ad70e175776cbd81335d62d49`.
This identifies the test manifest, not an immutable implementation release.

The first full regression exposed an old M37 expectation that omitted discovery
when a previously unknown object first entered a DDM overlap. It now asserts
Discover, InScope, and Reflect in that order. The verified runs above include
that assertion and the final cross-object lock regression, not the transient
test file used while preparing the latter.

External evidence prefix: `D:/workspace/gorti/service-completion-`.
Focused RED/GREEN records also use `ddm-`, `declaration-`, `mom-`,
`scheduled-`, `join-fom-`, `discovery-`, and `cpp-ieee2010-` prefixes there.

The local/manual commands remain:

```sh
python -B scripts/ieee2010_quality_gate.py --run
python -B scripts/ieee2010_quality_gate.py --claim full-2010 --check
```

The first runs only the named Go campaign. It does not import SDK or race logs
as executed evidence. The second must continue to reject a full claim while
the reviewed normative inventory and independent evidence are absent.

## Remaining Implementation

| Work | What is still missing |
|---|---|
| Join-time FOM extension | Stable existing handles, atomic live repository publication, DDM/object cache refresh, SDK revision propagation, and persistence/replay of module revisions. Nonempty additional modules fail before joining in Python/C++; preparation is not live support. |
| Full scheduled save/restore | General advance-crossing behavior, zero-lookahead scheduling, changed participant identity restoration, and transport/callback in-flight epochs. The current checkpoint only permits participating time-managed federates to reach an agreed ceiling with positive regulator lookahead. |
| Declaration | Attribute-relevance callback production, effective update-rate reporting and rate-driven delivery, overlap-specific regional relevance, and retry of failed lifecycle advisories. A saved rate designator alone does not implement rate control. |
| DDM/object | Complete hierarchy-aware regional late discovery, closest-subscribed-ancestor known class, multi-pair SDK atomicity, and scoped-send tag/retraction/received-region metadata. |
| MOM | Remaining subscription/object-instance/FOM-module/synchronization queries, timing and attribute-state controls, complete reporting/failure/periodic-recipient semantics, and mixed-transport accounting. |
| C++ and callback contract | Remaining automatic resign, order changes, additional dimension/rate query surfaces, complete callback metadata, and propagation/recovery of unexpected receive-stream termination. |
| Normative proof | Full standard texts and applicable corrections, mandatory/conditional applicability review, official binding/ABI coverage, independent service-suite runs, and immutable release evidence. |

There are therefore known unimplemented functions in addition to missing
conformance evidence. Obtaining the standards or passing all current tests
would not by itself complete these implementations.

See the [whole-RTI audit](ieee1516-2010-full-rti-audit.md),
[FOM extension analysis](ieee1516-2010-join-fom-preparation.md), and
[quality gates](ieee1516-2010-quality-gates.md) for the broader obligations.
