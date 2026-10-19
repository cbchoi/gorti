# IEEE 1516-2010 Feature Completion Checkpoint

Date: 2026-09-20. Branch: `codex/ieee2010-feature-completion`.
Status: first parallel implementation batch reviewed and verified; not whole-plan completion.

This records the first execution batch of the
[feature completion plan](../design/ieee1516-2010-feature-completion-plan.md).
Earlier dirty changes were preserved. No history rewrite, commit, push, CI
policy change or publication was performed by this batch. Raw results remain
outside Git under `D:/workspace/gorti/`.

## Baseline And Scope

Before implementation, 2,049 source/configuration files were archived in
`feature-completion-baseline-20260920-195349/source.zip` outside the repository.
The manifest SHA-256 is
`eb82812afeb9dd6fb9f9193e30bc436a5d4844d2d898d0e79ba5281cd49d1c2a`.
All 114 unavailable tracked paths were confirmed as tracked deletions; no
additional missing tracked input was found. Inaccessible legacy Python `.qa`
directories and runtime caches were excluded, not silently counted as captured.
The four-root Go suite passed against the extracted baseline.

The [inventory](ieee1516-2010-feature-inventory.md) discovers 2,285 entries and
reconciles 45 historical audit IDs. These are API/schema/source entries, not
2,285 missing functions or a conformance percentage. Its recorded baseline is
immutable; subsequent fixes need separate source-bound disposition evidence.

## Implemented Boundaries

| Area | Implemented and tested boundary | Still open |
|---|---|---|
| Host | Go/Python/C++ opt-in `GORTI_FEDERATE_HOST`; validated join metadata; live membership, generation and restore-handle fencing. | Normative withholding/default policy; no automatic host discovery. |
| Current FDD | Deterministic export from one committed merged model; precise decimals, enumerators and switches retained; query and actual MOM reflection path. | Upstream metadata fidelity and implicit/default FOM profiles; incomplete models remain unavailable. |
| Last save | Separate versioned completed-decision journal, exact generation identity, fresh/unknown/immediate/timed-zero distinctions, restart and restore behavior. | Predecision orphan bundles require inspection; power-loss durability beyond existing filesystem guarantees is not established. |
| Elapsed time | Server granted/advancing state intervals, per-attribute publication acknowledgement, fractional carry, failure preservation, restore/rejoin fencing. | Complete RO/TSO delivery-stage ledger and normative measurement-boundary review. |
| Creation | Fallible history setup before publication; failed generation never reused; committed validated model reused without a second parse. | Existing best-effort MOM/cluster operational boundaries remain documented separately. |
| Python SDK and verification | Missing ownership exception mapped and exported; internal barrier excluded from HLA callbacks; independent-process Go/Python time tests; explicit inventory and source-drift checks. | Full campaign result and cross-language prerequisites below; no waiver of skipped tests. |
| C++ verification | Official supplementary headers verified independently of repository headers; compiled clients exercise real library/server paths. | Strict binding/ownership/exception compatibility, integer64 execution and a clean TSAN campaign. |

The public Protobuf/RPC schema was not changed by this batch. A baseline/current
comparison of 174 protocol and generated Go/Python/C++ source files found no
added, removed or changed file within those roots. FDD transmission
was explicitly approved for federation MOM clients; verification used only
local test models and local servers. The existing deployment authentication
boundary is unchanged. A federation name is not an access-control credential.

## Plan, Do, Review, Reflect

Independent workstreams implemented separate roots, then cross-reviewed the
integration. Behavioral failing tests preceded the host source, FDD retention,
completed-save history, elapsed publication and fixture fixes. Subsequent
review found additional defects instead of treating first-pass tests as closure:

- Missing/null JSON decision fields could fabricate an empty-label or immediate
  save. Required field presence/type validation now preserves explicit valid
  empty/null values while rejecting corrupted history.
- Cancellation during history preparation could leave a created execution
  without the FOM/MOM publication. Checked creation now aborts before publication;
  committed local setup no longer depends on the caller's cancellation.
- Uncancelled local setup needs a bounded peer-notification context. A stalled
  cluster peer must not hold creation admission indefinitely.
- Expanded Python discovery exposed a missing ownership exception mapping and
  an internal protocol marker incorrectly classified as an HLA callback.
- Independent official-header compilation exposed float64 constructor/factory
  ABI differences that repository-header compilation did not detect.
- Live MOM review found that dynamic FOM joins and completed saves updated
  queries without publishing the changed attributes to subscribers. Lifecycle
  hooks now publish those values after the corresponding successful operation.
  A separate late-subscription test exposed missing host/FDD reflections;
  application composition now refreshes the subscribed MOM class after discovery.
  These tests do not manually refresh metrics to make the asserted operation pass.

The reflection is to retain these concrete regressions in the manual gates,
not to rename unsupported behavior as a completed feature. The expanded Go
manifest covers explicit engineering contracts; it is not a normative inventory.

## Verification

Completed integration results are recorded below. Historical or intermediate
results must not be used as frozen-release evidence.

After the callback fixes, the four-root Windows suite passed 72 packages and
2,075 top-level tests, with eight explicit legacy/platform skips and no failures.
The external official FDD XSD oracle executed successfully. The same four-root
Linux/WSL race suite passed 72 packages and 2,076 top-level tests, with seven
explicit skips, no failures and no data-race warnings. Counts exclude subtests.
The Windows-only FDD oracle was skipped on Linux; its Windows result is separate
evidence, not a claim that it executed in the race campaign.

The remaining skips are the old halt-time and resign-while-pending scenarios,
M2 registration/replay scenarios, M3 deterministic-LBTS scenario and M5 Python
join scenario. Windows additionally skips the terminal snapshot and Go timed
end-to-end tests; those two execute on Linux. A zero-exit suite is not evidence
that these skipped cases passed.

The expanded manual Go gate passed all 122 groups and all 533 required test
entries (1,672 passing entries including subtests). It rejects any skip among
its selected cases. The gate's own tests passed: 28 tests and 62 subtests.
Whole-standard readiness still fails as intended, with 39 prerequisite errors
and zero commands executed. Inventory checker tests passed (28 tests and 13
subtests; one external cache-write warning). Go vet and the strict MkDocs build
passed.

Raw evidence outside the repository:

- `feature-completion-go-integrated-20260920.jsonl`
- `feature-completion-race-integrated-20260920.jsonl`
- `feature-completion-gate-run-20260920.json`
- `feature-completion-full-claim-check-20260920.json`
- `feature-completion-gate-unit-20260920.log`
- `feature-completion-vet-integrated-20260920.log`
- `feature-completion-docs-final-20260920.log`

The earlier integrated run passed 2,066 top-level tests. A subsequent review run
failed four newly added cases because their fixtures omitted federation
generation. After repairing those fixtures, genuine callback-publication
failures were reproduced and fixed. These intermediate failures remain in the
external logs; the integrated runs above supersede them.

The broader Python campaign initially collected all 1,069 expected IDs and
reported 1,062 passed, two failed and five skipped. It correctly failed the
gate and detected source drift while parallel implementation was ongoing.
All six TLS cases and four explicit-host cases passed. Follow-up results must
supersede, not erase, this diagnostic run.

The frozen Windows follow-up collected all 1,078 expected IDs: 1,077 passed,
one skipped, zero failed, with no source changes or inventory discrepancies.
The remaining skip is the C++ publisher prerequisite; the campaign gate correctly
remains not passed despite pytest's zero exit. The source identity before/after
was `3a38bc3b3d523a9def48b447d62885b4d10c5dfe5a091d15216aa4f713e5c008`.
See `m6-hla-campaign-final-windows-01/summary.json` outside the repository.

The final Linux/WSL campaign passed **all 1,079 expected tests**, with zero skips,
failures, missing/unexpected/duplicate IDs, collection issues or source changes.
The C++ publisher binary also remained unchanged. Runtime was 202.76 seconds.
The before/after source identity was
`0d04895dad96958498688517fd504776ddb15e32a1b8aa79c8d7acca50488b94`.
See `m6-hla-campaign-final-wsl-03/summary.json` outside the repository.
At report close, all 2,142 files in that source manifest still matched the
current checkout. The workstream confirmed no owned test processes remained.
This is the explicit Python HLA campaign, not the unrelated RL/standalone
bridge suites or proof of whole-standard semantics.

Two Linux diagnostic runs are preserved. The first could not collect tests
because pytest's anonymous-file capture failed on the mounted Windows drive.
The runner now uses system-stream capture, with a regression test; child
process transcripts remain separately recorded. The second passed all 1,079
executed cases but the gate rejected eight absolute-path-dependent IDs.
Those cases now have explicit filename/code IDs on both operating systems;
no test or assertion was removed and no inventory mismatch was waived. The
final campaign reran every selected case after both infrastructure repairs.

Four former M21 placeholders now execute actual independent Go/Python processes:
both directional TSO delivery cases, mixed TAR/TARA/NMR/NMRA/FQR, and ten cycles
of the existing pyjevsim adapter protocol against two independent regulators.
The last case validates the adapter with a small protocol-compatible model,
not the upstream pyjevsim engine. Child identities, callback payload/order,
time grants and successful cleanup are asserted; infrastructure failures do
not become skipped success.

The C++ follow-up corrected `HLAfloat64Time(double const&)` and both named-factory
selectors for the existing float64 profile. The four focused official-header
checks passed, including relinking previously failing, unchanged client objects.
Normal source calls remain valid, but the old by-value constructor binary symbol
is replaced: existing C++ clients must be rebuilt. No binary compatibility claim
is made for the old symbol. Unsupported factory names return null, including
integer64; no float64 implementation is relabeled as integer64.

The full bounded official-header harness completed with stable C++ source hashes:
FM, OM, TM, configured/absent host reporting, float64 construction and supported
factory selectors passed. The complete harness remains **not passed** because
the C++17 profile differs in strict ownership types and exception declarations,
and integer64 factory symbols are absent. The earlier TSAN controls reproduced
an environment startup failure without the SDK; no clean SDK TSAN result is
available. Existing C++ support tests passed (15 cases); harness safeguards
passed (nine cases). A final run against a fresh server built after the Go freeze
reproduced the same 14 passed/three failed result, with all five live-service/host
cases passing and clean owned-server shutdown. Evidence is in
`feature-completion-cpp-final-20260920/evidence.json` and
`official2010-float64-green-20260920/evidence.json` outside the repository.
The final server SHA-256 is
`97147eac5bd4a5402aef258b2fecec245e9145b3c13a72a3b68a3feb7caf682a`.

## Remaining Milestones

M0's exhaustive state/overload mapping remains open. M1 has real source and
delivery paths, but complete FDD fidelity is not closed. M2's elapsed subset is
implemented; RO/TSO accounting remains open. M3's lifecycle-aware MOM reporting,
M4's complete state/exception oracles and M5's binding/time-profile work remain.
M6's bounded Python HLA campaign is now green on Linux. The complete C++ binding,
sanitizer, fault, scaling and paired performance campaigns remain open; a formal
performance campaign was not executed in this batch. M7 requires same-release
reviewed evidence.

Authorized IEEE 1516-2010, 1516.1-2010 and 1516.2-2010 texts and applicability
review remain prerequisites for a full conformance decision. No full-standard
conformance, certification or new performance ranking is claimed here.
