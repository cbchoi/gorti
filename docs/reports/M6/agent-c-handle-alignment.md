# Agent C M6 W1A — Cross-language handle alignment

Post-MVP follow-up dispatched per the M5 close + MVP gate (`995e097`)
to align Python's FOM parser MIM merge against the canonical Go-side
`rti/pkg/fom/mim/standard-mim.xml`. Unblocks the deferred M5 spec test
`pysdk/tests/spec/m5/test_spec_m5_modes.py::test_spec_m5_best_effort_attribute_delivers_ro`.

## Outcome

**Python-side alignment: COMPLETE.** Python and Go now assign the same
numeric handle to every class name in every FOM exercised in this work,
proven pairwise by the new agent-owned regression
`pysdk/tests/test_handle_alignment.py` (3/3 tests PASS, including the
exact M5 inline ModesProbe FOM).

**M5 test 1: STILL SKIPS, with updated reason — secondary blocker
discovered.** Handle alignment is necessary but not sufficient: a
production-wiring gap on the Go side (rtid never calls
`fomRepository.RememberFor` post-`CreateFederation`) keeps the
end-to-end RO delivery test from passing. That gap is out of W1A scope
(`rti/*` is read-only per the dispatch); see "Secondary blocker" below
for the next agent dispatch.

## Root cause of the handle disagreement (now fixed)

The IEEE 1516.1-2010 standard MIM (`rti/pkg/fom/mim/standard-mim.xml`)
nests several interaction class names twice under different parents:

| Class name | First parent | Second parent |
|---|---|---|
| `HLAadjust` | `HLAmanager.HLAfederate` | `HLAmanager.HLAfederation` |
| `HLArequest` | `HLAmanager.HLAfederate` | `HLAmanager.HLAfederation` |
| `HLAreport` | `HLAmanager.HLAfederate` | `HLAmanager.HLAfederation` |
| `HLAreportFOMmoduleData` | `HLAreport` (under `HLAfederate`) | `HLAreport` (under `HLAfederation`) |
| `HLArequestFOMmoduleData` | `HLArequest` (under `HLAfederate`) | `HLArequest` (under `HLAfederation`) |
| `HLAsetSwitches` | `HLAadjust` (under `HLAfederate`) | `HLAadjust` (under `HLAfederation`) |

Go's `model.NewFOM` keeps every flat entry — the duplicates appear as
sibling slots in the name-sorted slice — and
`*fomHandle.LookupInteractionClass(leaf)` returns 1-based position
within that slice. For the M5 inline FOM that places `ModesProbe` at
**handle 86** on the Go side.

Python's pre-W1A `_load_mim` deduplicated by name across both the
single-MIM-file walk and the cross-MIM-file accumulator, collapsing the
six duplicates and dropping every later class six slots. `ModesProbe`
landed at **handle 80** on the Python side.

When the Python publisher sent `ModesProbe` over real gRPC, the wire
carried interaction-class-handle 80; Go's `OrderForInteraction(80)`
resolved to a different (or out-of-range) class, returned the TSO
default, and the timestamp survived — defeating the per-class FOM-order
contract.

## Fix landed

`pysdk/rti1516e/fom/parser.py::_load_mim` no longer dedupes
object-class / interaction-class entries when reading the MIM XMLs. It
appends every flat entry verbatim, mirroring Go's `flattenMIM*` +
`model.NewFOM` semantics. Data types remain name-deduped (IEEE schema
guarantees uniqueness; Go's MIM corpus is a single file so the
cross-file dedup is a no-op for it).

`_merge_user_onto_mim` was simplified to use a name-keyed set check
against the MIM (matching Go's `mim.mergeNoCollision` behavior) and to
not impose a name dedup among user classes, again preserving Go's
handle-position semantics for any user FOM with sibling-name classes.

The `_diag_mim_redefinition` (FOM-101) path remains name-keyed and
PASSES the existing `test_each_bad_fixture_emits_its_code[FOM-101-...]`
fixture without modification.

## Verification

- `pysdk/tests/test_handle_alignment.py` (NEW, agent-owned, 3 tests):
  - `test_python_and_go_agree_on_handles[minimal.xml]` — PASS
  - `test_python_and_go_agree_on_handles[pyjevsim-bridge.xml]` — PASS
  - `test_python_modes_probe_lands_at_same_handle_as_go` — PASS
  - Each test compiles + runs an inline Go enumerator program against
    the production `rti/pkg/fom/parser` + `rti/pkg/fom/mim` packages,
    then asserts every (kind, handle) pair matches between Python's
    `parse(...)` and Go's `mim.Merge(StandardMIM, parsed)`.
- `pysdk/tests/spec/m5/test_spec_m5_cross_language.py` — PASS (no regression).
- `pysdk/tests/spec/m5/test_spec_m5_modes.py::test_spec_m5_verbose_attribute_delivers_tso` — PASS (no regression).
- `pysdk/tests/spec/m5/test_spec_m5_modes.py::test_spec_m5_best_effort_attribute_delivers_ro` — SKIPS (updated reason; see below).
- All other M0–M5 spec + unit tests — GREEN (480 passed, 2 skipped overall).
- `mypy --strict pysdk/` — clean (75 source files, 0 errors).
- `ruff check pysdk/` — clean.
- `go test ./...` — all packages PASS, no regression.

## Secondary blocker — out-of-scope for W1A, owed to next dispatch

End-to-end testing surfaced a second wiring gap that prevents the M5
test from passing even with handle alignment fixed:

- `rti/cmd/rtid/main.go` constructs `fomRepository` (the FOMRepository
  implementation passed to the federation manager) but never calls
  `fomRepository.RememberFor(name, handle)` after a successful
  `CreateFederation`.
- `FOMRepoOrderLookup.InteractionOrder` (in
  `rti/internal/transport/grpc/best_effort.go`) resolves
  `Repo.Get(fed)` to `(nil, ErrFederationNotFound)`, hits the
  `if err != nil || h == nil { return OrderTimeStamp, false }` branch,
  and returns the TSO default for every interaction.
- `Registry.deliveryTimestampForInteraction` then preserves the
  publisher's timestamp regardless of the FOM's `<order>Receive</order>`.

The Go-side spec test `rti/spec/M5/best_effort_test.go` does NOT
exercise this path (it injects an `orderTable` fixture directly as
`object.Options.Orders`, bypassing `FOMRepoOrderLookup` entirely),
which is why the production wiring gap was previously invisible.

The fix is ~3 lines in `rti/cmd/rtid/main.go`: after the
federation-create RPC succeeds, call `foms.RememberFor(name, handle)`
so the per-federation handle map is populated. (Or: route
`FOMRepoOrderLookup` through the federation manager's
`federationState.fom`.) Either approach plus a fresh Go-side spec test
exercising the `FOMRepoOrderLookup` end-to-end would close the M5
test 1 loop.

W1A scope (per the M6 dispatch) is Python-only, `rti/*` read-only.
This finding is captured in the updated `pytest.skip` reason on the
M5 test so the next dispatch can target it precisely.

## Files changed

Modified:
- `pysdk/rti1516e/fom/parser.py` — `_load_mim` no longer dedupes
  OC/IC by name; `_merge_user_onto_mim` simplified to name-vs-MIM
  collision check only. Both paths gain explanatory docstrings citing
  the cross-language handle-alignment contract.
- `pysdk/tests/spec/m5/test_spec_m5_modes.py` — module docstring +
  `test_spec_m5_best_effort_attribute_delivers_ro` updated to
  document the now-resolved handle-alignment blocker AND the newly
  surfaced Go-side wiring gap. Skip block retained with the updated
  reason naming the secondary blocker.

Created:
- `pysdk/tests/test_handle_alignment.py` — agent-owned regression
  proving Python and Go agree on handles for the minimal, bridge, and
  M5-modes inline FOMs.
- `docs/reports/M6/agent-c-handle-alignment.md` — this report.

## Acceptance vs. M6 dispatch

| Acceptance criterion | Status | Notes |
|---|---|---|
| `pysdk/tests/test_handle_alignment.py` PASSES | PASS | 3/3 tests, all foms covered |
| `test_spec_m5_best_effort_attribute_delivers_ro` PASSES | SKIP | Updated reason; secondary Go-side wiring gap blocks |
| `test_spec_m5_verbose_attribute_delivers_tso` PASSES | PASS | No regression |
| `test_spec_m5_cross_language.py` PASSES | PASS | No regression |
| All M0–M5 spec tests GREEN | PASS | 480 passed, 2 skipped (one pre-existing replay defer, one updated) |
| `mypy --strict pysdk/` clean | PASS | 0 errors, 75 files |
| `ruff check pysdk/` clean | PASS | All checks passed |
| `go test ./...` all green | PASS | All packages |

The Python alignment work is complete and verified. The M5 test 1
acceptance line cannot be satisfied without a separate Go-side wiring
fix that is explicitly out of W1A scope.
