# IEEE 1516-2010 Feature Inventory

Date: 2026-09-20. Status: baseline discovery and explicit triage, not feature,
whole-standard, binding/ABI, release, or certification completion.

The machine-readable ledger is
[ieee1516-2010-feature-inventory.json](ieee1516-2010-feature-inventory.json).
This work implements the inventory/checking part of M0. It does **not** close
M0's exhaustive per-state/per-overload mapping: support-header member audits,
independent semantic trace review, and normative applicability remain open.

## Current Batch Handoff

Main's latest 2026-09-20 update supersedes the baseline **work-assignment**
status below, not its recorded source hashes or closure evidence. These updates
are reported, not independently verified by this worker:

- M1 host, last-save history and currentFDD implementations are underway with
  user approval. The four baseline gaps are not asserted still absent today.
- M2 elapsed-time subset is ready for integration review. The complete RO/TSO
  delivery ledger remains **unassigned**.
- The official C++ probe found actual float64 constructor ABI and named-factory
  defects; Parfit reports fixes. Strict official-header ownership/exception
  incompatibilities remain open, and integer64 execution remains unsupported.
- The newly inventoried full Python campaign contains 1,069 cases: 1,062 passed,
  two failed and five skipped. Fixes are underway, including four placeholder
  cross-language tests. This is not a full-campaign pass and must not be confused
  with the older selected 676-case result.

The same update is machine-readable as `current_batch_reported`. No row is
promoted to `fixed-tested` without fresh source-bound evidence and review.
Independent integration review is the next available work for this worker.

## Evidence Boundary

The ledger is anchored to Main's captured
`D:/workspace/gorti/feature-completion-baseline-20260920-195349/source`, not the
moving shared working tree. It includes tracked and relevant untracked inputs
from that capture. The manifest SHA-256 is
`eb82812afeb9dd6fb9f9193e30bc436a5d4844d2d898d0e79ba5281cd49d1c2a`;
the source ZIP SHA-256 is
`a4949ce0cd2d4785072c6716f76b511596c6637d1c4ea75d7fabe7b8d28cdd08`.
Warnings about inaccessible legacy `.qa`/pytest directories and unavailable
tracked paths are **scope exclusions to review**, not positive source proof.

Official supplementary headers are read from the existing downloaded 2010
archive, not repository replacement headers. Their file hashes are pinned.
The archive and official-MIM hashes/provenance are recorded in JSON. The
supplemental material carries this attribution:
Reprinted with permission from IEEE 1516.1(TM)-2010.
Authorized full normative texts and applicable corrections remain unavailable.

The [remaining-services report](ieee1516-2010-remaining-services-20260920.md)
supersedes earlier implementation findings. Its campaigns are **historical**,
not freshly executed evidence for this inventory or later parallel changes.
All 35 C/N/F rows are reconciled individually. Thirty-four record bounded
historical fixes; C10 remains a partial fix plus time-profile question. None is
promoted to fresh `fixed-tested` merely because an old report passed.

## Discovered Surface

| Catalog | Entries | Interpretation |
|---|---:|---|
| Official C++ RTI services | 151 overloads / 140 method names | Parameter declarations, return types and declared exceptions retained. |
| Official C++ callbacks | 60 overloads | Distinct overload IDs; no inferred callback behavior. |
| Product C++ ambassador declarations | 151 services + 60 callbacks | Separate IDs from official headers; signatures are not assumed equivalent. |
| C++ support headers | 80 | Official and product factories, handles, encodings and types; member-level audit remains explicit. |
| MIM object classes / attributes | 4 / 45 | Qualified IDs, inheritance and declared data types. |
| MIM interaction classes / parameters | 85 / 101 | Includes structural/abstract classes; parameters are declared, not inherited duplicates. |
| MIM data types | 53 | Structural representation, enumerators and record/array definitions. |
| MIM dimensions / transportations | 2 / 2 | Named declarations and structural properties. |
| Wire RPCs / messages / enums | 140 / 304 / 16 | Field numbers/types/presence/oneofs and streaming shape from structured descriptors. |
| Go SDK callables / types / values | 177 / 75 / 74 | Exported declarations via Go AST, including callback/event types. |
| Python SDK callables / types | 420 / 211 | Public declarations via Python AST, including annotated fields. |
| Python package export manifests | 3 | Explicit exports and origins, including exports from private implementation modules. |
| Audit / profile / baseline implementation gap / review gap | 45 / 10 / 8 / 8 | Explicit dispositions and blocking dependencies. |
| Total | 2,285 | Counts are discovery counts, never conformance percentages. |

Each row has a stable ID, source anchor/hash, profile, applicability, rationale,
component trace, oracle/test requirement, reviewer field, evidence references,
and blocking dependencies. Eight facets are explicit: inputs, exceptions,
lifecycle, post-state, callbacks, failure/concurrency, persistence and binding/
encoding. Trace groups provide concrete component paths, stateful oracle
requirements and links to all 110 historical gate groups with exact case names.
These are candidates for review, not an assertion that a group covers every row.

Official services also have name/explicit-alias links to candidate product,
Python, Go and wire declarations, plus actual C++ implementation-symbol anchors.
Such links establish discoverability, **not** equivalent overload/input/state
semantics. No method is classified missing from generated `Unimplemented*`
embeddings, optional embedded-adapter fallbacks, or historical comments.

The catalog has 2,274 `unverified` and 11 `normativeunknown` rows. This is an
explicitly triaged baseline, not a statement that 2,274 implementations are
missing. `fixed-tested` requires current executed evidence; `reproducedgap`
requires a behavioral reproduction; `justifiedNA` requires a reviewed argument.
No fresh RTI runtime campaign was run by this inventory worker.

## Reconciliation

| Earlier IDs | Current bounded disposition |
|---|---|
| C01 | Transportation service declarations/forwarding implemented; official-header executable and ABI coverage remains open. |
| C02, F04-F06 | Order/transport effects, actual callback metadata, caller-local deletion and knowledge are implemented. |
| C03, N01, N03 | Prepared resign actions and current ownership/deletion-privilege authority supersede old ignored-action and sparse-handle findings. |
| C04 | Dynamic FOM joins and identity/factory forwarding implemented; custom MIM and factory scope require separate review. |
| C05 | Tags, producer/region presence, sent/received order and retraction identity implemented; all-overload proof remains open. |
| C06, N10 | Standard switch encoding, inherited targeting and feedback prevention have bounded regressions; complete MOM lifecycle reporting is not closed. |
| C07, N05-N07, F01-F03 | Admission, complete snapshots, two-phase ownership persistence, scheduled saves, restart/generation restore and identity rebinding supersede old defects. |
| C08, F07 | Active/passive, advisory edges, regional demand and rate queries implemented. |
| C09, F08 | Real support lookups and authenticated automatic resign replace placeholders. |
| C10, F10 | Invalid-target poisoning fixed; float64-only actual execution, pending-enable semantics and independent time oracles remain open. |
| N02, N04 | Acquisition/query preconditions, binary tags and atomic divest results implemented. |
| N08-N09, F09 | Callback gating, C++ immediate dispatch and Python evoked/immediate execution implemented; exact normative scheduling/reentry remains unresolved. |
| N11-N12 | Range validation and FDD metadata/vocabulary defects have bounded fixes; not complete DDM/OMT closure. |
| F11-F13 | Regional registration, scoped routing, context checks and current-owner update requests implemented; full state/exception coverage still needs review. |
| Q01-Q10 | All remain `normativeunknown`, individually described in JSON. Supplementary artifacts cannot settle mandatory/conditional full-standard obligations. |

## Priority Backlog

These are baseline gaps; parallel worker implementations must be independently
verified before disposition changes. Source drift is not itself a defect.

| Priority | Work | Acceptance boundary |
|---|---|---|
| P0 claim | Authorized texts/corrections and Q01-Q10 | Reviewed applicability and independent oracles; Java/WSDL is neither silently waived nor automatically mandated. |
| P0 release | Freeze and reconcile the candidate | Regenerate/review discovery, verify hashes, execute exact cases on that source; missing/skip/stale evidence blocks completion. |
| P1 M1 | `HLAfederateHost`, `HLAcurrentFDD`, `HLAlastSaveName`, `HLAlastSaveTime` | Authoritative typed host, realized coherent FDD, durable completed-save history; distinguish legitimate undefined state from missing history. |
| P1 M2 | `HLAROlength`, `HLATSOlength`, `HLAtimeGrantedTime`, `HLAtimeAdvancingTime` | Complete delivery-stage accounting and monotonic intervals committed at successful attribute publication, not queue-only approximations. |
| P1 M3 | MOM caller attribution and post-resign reporting | Legitimate caller identity, lifecycle-aware observation, full inherited target/recipient/switch/periodic/error catalog. |
| P1 M4/M5 | Custom-MIM service path | Separate reviewed MIM selection/constraints/reporting and actual production behavior; see independent finding below. |
| P1 M4 | Time state/representation | Separate five-primitive and pending-enable oracles; exact integer/custom time only if applicable/claimed, never float64 wrappers above 2^53. |
| P1 M5 | SDK parity and official ABI | Explicit overload/parameter/error/callback mapping, independently compiled official-header clients and per-toolchain provenance. |
| P1 M6 | Profiles, TLS, sanitizers, full Python selection | Execute the three historically skipped TLS cases; clean C++ TSAN remains absent after startup failure and incomplete dependency instrumentation. Linux results do not prove native Windows C++ or Python Linux. |
| P1 M6/M7 | Integration/reference/performance | Frozen mixed-language N=2/4/8/16 cases, faults/restart and paired-budget comparison; no new performance claim from this inventory. |

## Independent Findings

1. **Custom MIM remains a distinct review gap.** Actual
   `DLCRTIambassadorImpl::createFederationExecutionWithMIM` prepends the MIM to
   ordinary FOM modules. `CreateFederationRequest` has no distinct MIM role;
   production `composition.Repository.Load` merges against the embedded standard
   MIM, and baseline MOM metrics report `HLAstandardMIM`. This is a concrete
   end-to-end contract concern beyond the eight attributes, not a conclusion
   drawn from the old comment. A permitted custom-MIM scenario and independent
   oracle are required before labeling a specific standard violation/reproduction.
2. **Python standard-style parity is not complete by name or inputs.** The
   baseline class lacks `createFederationExecutionWithMIM`, `getTimeFactory`, and
   the nine same-name C++ handle-decode methods. Its creation signature omits
   the time designator; its join signature omits federate type and the call to
   `join_federation` does not forward one. Product APIs may offer alternative
   forms, so resolve intended parity/applicability instead of declaring Python
   subject to the C++ normative binding automatically.
3. **MOM reporting boundaries are visible in production composition.** Baseline
   `run.go` does not supply `ResolveCaller`; `MOMObserver.UnaryInterceptor`
   observes after the handler returns, while successful resign removes the MOM
   membership. A name-to-service table cannot establish reporting of these paths.
   Required identity/report timing still needs reviewed applicability and a
   stateful reproducer; no fabricated caller is acceptable.

## Validator And Integration

New files only; existing `ieee1516-2010-evidence.json`,
`scripts/ieee2010_quality_gate.py`, shared composition, protobuf/generated files
and SDK implementations were not edited by this worker. No server ports,
background RTI processes, global Go/race campaigns, commits or pushes are used.

Main can import `evaluate(data, root, official_root, require_completion=False)`
from `scripts/tests/ieee2010_feature_inventory.py`, or invoke its CLI. No current gate
integration is made here. Structural validation alone explicitly reports that
source verification was not requested. Supply both roots for hash checks and
run source discovery comparison as well to detect catalog omissions.

```powershell
$py = 'D:/workspace/gorti/n04-venv/Scripts/python.exe'
$baseline = 'D:/workspace/gorti/feature-completion-baseline-20260920-195349/source'
$official = 'C:/Users/cbchoi/AppData/Local/Temp/gorti-ieee1516-audit-20260918/cpp-api/cpp/src'
$env:GOCACHE = 'D:/workspace/gorti/.gocache'
$env:GOMODCACHE = 'D:/workspace/gorti/.tools/gomodcache'
& $py -B scripts/tests/ieee2010_inventory_sources.py --root $baseline --official-root $official --compare engineering/verification/ieee1516-2010-feature-inventory.json
& $py -B scripts/tests/ieee2010_feature_inventory.py --root $baseline --official-root $official
& $py -B scripts/tests/ieee2010_feature_inventory.py --root $baseline --official-root $official --require-completion
& $py -B -m unittest discover -s scripts/tests -p 'test_ieee2010*.py'
```

Discovery comparison and triage validation should exit 0; completion
prerequisites must exit 1. The scanner launches a Go AST helper, not a Go test
campaign. Its Python dependency is `google.protobuf`; the specified existing
environment provides it. No shell, SDK runtime, compiler/ABI or test result is
inferred from header parsing. The JSON output remains non-conformance evidence.

The helper's `--output` is a mechanical baseline rebuild, **not** a release
promotion or disposition-preserving merge. Do not overwrite a reviewed ledger
with it after adding closure evidence. `--compare` is read-only. A subsequent
release inventory needs a new snapshot identity and deliberate disposition
review; this helper intentionally retains the named baseline identity.

The validator rejects unknown states, duplicate/removed rows, missing C/N/F/Q
or profile rows, missing facets, source drift, broken references, unsupported
claim flags, unresolved completion, stale/skipped/unexecuted case records,
and historical evidence relabeled as current completion, reproduction or NA.
It hashes external evidence artifacts and checks their actual recorded case/
profile results. It does not authenticate reviewers or prove oracle quality;
independent human review remains required. A forged manifest and forged logs
are not made trustworthy by their mutually matching hashes.

## Plan, Do, Review, Reflect

- Plan: reconcile the latest report, pin sources, enumerate surfaces, keep
  source presence, historical fixes, fresh execution and normative truth separate.
- Do: 17 initial validator tests produced 22 failed assertions because the
  initial structural checker accepted untriaged/false-completion mutations.
  Implemented the checks and obtained a passing run. This is validator TDD,
  not an invented RTI behavioral RED.
- Review: additional tests exposed two real checker loopholes: historical
  reports could justify NA or reproduced-gap status. Both were corrected.
  Added nested C++ default-parameter parsing, source-drift, artifact-tampering,
  malformed-input and repository-catalog checks.
- Reflect: preserve every old audit ID but never copy an obsolete defect into
  the active backlog. Keep source-only findings unverified until independently
  reproduced. Reconcile concurrent source changes before any release execution.

Fresh raw checker/discovery logs are outside the repository under
`D:/workspace/gorti/m0-inventory-*`. The baseline check verifies 221 source
files. The first live-tree check detected eight changed source files, correctly
rejecting their reuse with the baseline hashes. That is expected parallel-work
drift, not eight new defects. Fresh checker results: 28 new inventory tests
plus 28 existing quality-gate tests pass (56 total). Source rediscovery matches
the saved catalog with no added/removed rows; baseline triage passes and
completion prerequisites reject the open inventory as expected. Historical RTI
campaign counts are not added to checker-test counts.
