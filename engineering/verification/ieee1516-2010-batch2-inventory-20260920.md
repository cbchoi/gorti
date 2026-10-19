# Batch 2 Candidate Inventory

Date: 2026-09-20. Scope: R0 discovery/reconciliation and bounded R9 tooling.
Status: interim source-bound discovery, not a frozen release, executed RTI
campaign, completed feature assessment, conformance assessment or certification.

## Artifacts And Source Identity

- Candidate: [machine-readable discovery](ieee1516-2010-batch2-inventory-20260920.json).
- New generator/checker: `scripts/ieee2010_inventory_candidate.py`.
- New tests: `scripts/tests/test_ieee2010_inventory_candidate.py`.
- The original 2,285-row ledger and existing evidence manifest were not edited.
  The original ledger SHA-256 remains
  `fb780ef397f5874ce22d3897333aa6cf0d9331b7c937768144c8edb570345635`.

The parent before-capture ZIP was checked member-by-member against all 2,378
manifest digests. The manifest SHA-256 is
`63401bbef06a5a64caf5d55f5d4232d5db0b58d62391d7289819cc31c5d34aaa`;
the ZIP SHA-256 is
`9667dab713a92c907984652021ccc5d031dbe62eaf0d2d28396ec08686304247`.
Its exclusions and unavailable paths remain explicit, not satisfied inputs.

The candidate records 2,438 repository source/configuration/test/tool files and
30 official supplementary headers. The discovery source identity is
`e97e5cd54b793917df35eea33a6a96c09936c14ad98b89156878c191e73626a8`.
This is a bounded capture identity, not a full-release source commitment.
Official header hashes must match the original ledger's pins; no new full
normative texts or corrections were obtained or inferred.

## Discovery And Reconciliation

The candidate contains 2,296 rows: 2,284 `unverified` and 12
`normativeunknown`. All facets remain open, and all row evidence/reviewer
fields remain empty. Historical fixes are not absent implementations, and
historical passes are not fresh candidate evidence.

Against the original ledger:

| Change | Count | Meaning |
|---|---:|---|
| Unchanged contract | 2,266 | Source or implementation may still differ; no disposition transfer. |
| Historical classification | 16 | Eight original implementation-gap and eight review-gap rows become historical follow-ups, retaining IDs. |
| Same-ID contract change | 3 | Python exports, `createFederationExecution` signature and `joinFederationExecution` signature. |
| Added | 11 | `OwnershipAcquisitionPending` Python type and ten project work-order rows R0-R9. |
| Removed | 0 | The checker would retain removed IDs as unresolved reconciliation obligations. |

Each work order has an owner, concrete current implementation anchors, a
proposed independent oracle and named blocking dependencies. These are project
IDs, not IEEE clause locators. Component/name associations and test candidates
are discovery aids, not per-overload semantic trace review.

The first-batch checkpoint supersedes old host/FDD/history/elapsed absence
claims. Current R4 caller/lifecycle work and replay-verifier work also require
their own exact-source executed evidence and review; this inventory does not
reuse an older runtime pass count to close them. `unavailable` is never a pass.

## Plan, Do, Review, Reflect

Plan: preserve the immutable baseline; reuse its structured Go/Python/protobuf/
XML and bounded C++ extractors; independently reconcile current contracts and
source hashes; forbid automatic closure promotion.

Do: the same five synthetic checks were first run through the existing
schema-1 checker. The valid open inventory passed; four candidate safeguards
failed because the legacy path accepted a self-rewritten missing-row catalog,
a same-ID contract edit, a changed baseline reference and a closed facet under
an open row. These are tooling boundary reproductions, not RTI or normative
violations. The baseline-reference mutation also demonstrates that the legacy
checker does not interpret the new candidate envelope.

The new checker requires exact fresh rediscovery, pins the original ledger,
rejects every closure disposition in discovery, detects changed contracts and
source hashes, preserves removed IDs, verifies the parent ZIP and refuses to
overwrite any existing output. All five initial tests then passed.

Review: additional tests cover stale/removed/added source, duplicate IDs,
nonsemantic C++ line moves, historical disposition resets, malformed inputs,
cross-platform path escapes, existing output preservation and ZIP tampering.
The new 22 tests plus the existing 56 inventory/evidence-gate tests all passed:
**78 tests, zero failures/skips**. This is workstream self-review; independent
parent review remains required. No shared verification runner was modified.

Reflect: a discovery run actually rejected concurrent source drift. The
versioned artifact was generated during a later stable scan, but subsequent
fresh checking rejected it when parallel edits changed these captured files:
`examples/go-timed/runner_test.go`, `rti/cmd/rti-top/integration_test.go`,
`rti/internal/adapters/grpc/time_test.go`, `rti/pkg/federate/time_test.go`.
That rejection is retained; the artifact is not overwritten to hide drift.
`--require-completion` also rejects interim discovery by design. A final
candidate must be newly versioned after writers settle and the real release
campaign is source-bound. No RTI runtime tests were executed by this tool.

## Reproduction And Limits

Raw transcripts are outside the repository under `D:/workspace/gorti/`:

- `ieee2010-batch2-inventory-red-20260920.log`: four RED safeguard failures.
- `ieee2010-batch2-inventory-green-initial-20260920.log`: initial five GREEN.
- `ieee2010-batch2-inventory-green-review-20260920.log`: all 78 GREEN.
- `ieee2010-batch2-inventory-green-final-20260920.log`: final repeat, all 78 GREEN.
- `ieee2010-batch2-inventory-artifact-hashes-20260920.json`: exact tool/test,
  baseline/evidence/candidate and RED/final-GREEN transcript hashes.
- `ieee2010-batch2-inventory-discovery-reviewed-20260920.log`: concurrent drift rejection.
- `ieee2010-batch2-inventory-discovery-write-20260920.log`: artifact generation.
- `ieee2010-batch2-inventory-check-20260920.log`: later drift rejection.
- `ieee2010-batch2-inventory-completion-rejected-20260920.log`: completion rejection.

Run with the existing `D:/workspace/gorti/n04-venv/Scripts/python.exe` and
`GOCACHE=D:/workspace/gorti/.gocache`,
`GOMODCACHE=D:/workspace/gorti/.tools/gomodcache`:

```powershell
& $py -B scripts/ieee2010_inventory_candidate.py check `
  --root D:/workspace/gorti/gorti `
  --official-root C:/Users/cbchoi/AppData/Local/Temp/gorti-ieee1516-audit-20260918/cpp-api/cpp/src `
  --before-manifest D:/workspace/gorti/ieee2010-batch2-before-20260920/manifest.json `
  --candidate D:/workspace/gorti/gorti/engineering/verification/ieee1516-2010-batch2-inventory-20260920.json
& $py -B -m unittest discover -s scripts/tests -p 'test_ieee2010*.py' -v
```

For a later candidate, use `discover` with a new output filename. This is an
opt-in discovery/checking tool; it does not replace the old inventory checker
or integrate Go/Python/C++ campaign ingestion into the release gate. Support
header member/ABI review, dynamic/inherited APIs, build-profile applicability,
exhaustive state/exception matrices, normative locators and all R9 final
campaigns remain open. No commit, push, reset or new agent was used.
