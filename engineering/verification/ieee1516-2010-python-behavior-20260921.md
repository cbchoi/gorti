# Python-First IEEE 1516-2010 Behavioral Corrections

Date: 2026-09-21. Branch: `codex/ieee2010-feature-completion`.

## Scope

This pass prioritizes the Python SDK and observable Python federate behavior.
It corrects reproduced server and FOM defects from the
[source-based review](ieee1516-2010-normative-review-20260921.md). No C++ source,
Protobuf schema, generated binding or public RPC signature was changed in this
pass. Existing unrelated working-tree changes were preserved.

The supplied source identities and hashes remain in that review. Each clause
used below was checked against the rendered active 2010 text; deleted redline
text was excluded. Page numbers are one-based PDF pages. Original publications,
extractions, rendered pages and raw execution logs remain outside the repository.

## Corrections

| Area | Source | Corrected behavior |
|---|---|---|
| Incoming time order | 1516.1, 8.1.2 Table 2 and 8.1.7; PDF 183, 189 | Enabling asynchronous delivery no longer releases future TSO callbacks or relabels constrained reception as RO. A regulating-only receiver receives timestamped messages as RO without requesting time advancement. |
| Constraint disable | 1516.1, 8.7; PDF 198-199 | Disabling constraint releases eligible queued messages as RO. Failed admission preserves a retryable constrained state and the undelivered queue. Production outboxes reserve the complete drain before publication. |
| LITS | 1516.1, 8.1.5 and 8.18; PDF 185, 212-213 | Query and MOM snapshots consider both the current GALT calculation and the buffered incoming minimum; an empty queue need not produce an undefined LITS. |
| Callback metadata | 1516.1, 8.1.2 Table 2; PDF 183 | A message sent as TSO retains its retraction designator when received as RO. Interaction, attribute and removal creation paths now retain it for immediate delivery as well as queued delivery. |
| Positive update rates | 1516.2, 4.11.2 and 6.2.11(2); PDF 60, 84 | Checked Go/Python model, module, merge and export paths reject zero and negative rates. Positivity and equality use exact decimals before runtime float conversion. Python no longer silently discards the update-rate table. |
| Additive regional object subscriptions | 1516.1, 9.8 narrative and (c-e); PDF 238-239 | New region triples accumulate without deleting previous regions. Resubscription changes only the requested triples' active/passive and update-rate options. Invalid calls roll back without partial mutation. |
| Regional persistence | Same subscription requirement; existing save/restore contract | Disjoint per-region option groups round-trip through the existing snapshot format. Duplicate overlapping triples remain invalid. Ordinary subscriptions remain independent. |

These are bounded behavioral claims, not closure of every requirement in each
referenced clause. In particular, correcting future-TSO safety does not establish
complete idle receive-order eligibility.

## Implementation Boundaries

- `rti/internal/hla/time`: incoming classification, TSO release, constraint
  transitions, LITS/MOM observation and scheduled-save resume.
- `rti/internal/hla/object`: retraction metadata on immediate and queued callbacks.
- `rti/internal/hla/ddm`: disjoint region option groups and transactional rollback.
- `rti/internal/adapters/savebundle`: validate regional snapshot uniqueness at the
  region-triple level while preserving the serialized schema.
- `rti/pkg/fom/model`, `parser`, `exporter`: exact positivity and admission/export
  rejection with positive precision controls and independent XSD/semantic oracles.
- `pysdk/rti1516e/fom`: immutable exact-decimal update-rate values, module/MIM
  retention and non-mutating merge; existing service signatures remain unchanged.

Python documentation now distinguishes Python `async`/`await` from HLA
asynchronous delivery. The latter never removes timestamp-order constraints.

## Plan, Do, Review, Reflect

1. Plan: assign independent time-live, update-rate and DDM work; retain the
   time-manager implementation and shared integration in the main work stream.
   Establish clause-specific negative tests before production changes.
2. Do: reproduce future-TSO release, regulating-only buffering, empty-queue LITS,
   missing immediate retraction metadata, accepted nonpositive rates and lost
   regional subscriptions. Implement the smallest owning-layer corrections.
3. Review: check actual Python callbacks, independent FDD validation, grouped
   save/restore and concurrency. Independent review caught three additional time
   boundary problems: an internal deletion marker lagging its grant, a failed
   constraint-disable that could not be retried, and unflushed deletion hooks on
   save-resume failure. Each received a reproducer and correction.
4. Reflect: old green tests sometimes encoded incorrect assumptions. Their
   expectations now follow the located requirement while retaining failure,
   queue-preservation and round-trip assertions. No failing case was changed to
   a skip. Integration also replaced invalid zero/negative successful-rate
   fixtures with valid equivalence controls and explicit rejection tests.

Production queue drains are reserved atomically. A legacy outbox without a
reservation contract can publish a prefix before failure; the remainder is
retained, a retry is accepted and callbacks are not duplicated. Internal deletion
deadlines use the grant bound during advancement and current time during an RO
drain; a scheduled-save bound is not itself permission to advance time.

## Execution Evidence

Raw files use prefix `ieee2010-python-focus-20260921-` in the parent workspace,
outside the repository. Intermediate failed integration transcripts are retained.
Overlapping package and focused runs are not added as independent test totals.

| Final execution | Result | Raw evidence suffix |
|---|---|---|
| Windows full Go regression | 73 tested packages; 2,221 top-level tests / 4,485 including subtests; no failures or skipped tests | `four-root-windows-final.jsonl` |
| Linux full Go race regression | Same packages and counts; no failures, skipped tests or race warnings | `four-root-linux-race-final.jsonl` |
| Windows Python-first HLA selection, after review fixes | 1,221 passed in 159.70 seconds; no failures or skips | `python-final.log`, `python-final-junit.xml` |
| Linux new Python cases, real race-enabled RTI | 47 passed in 36.54 seconds; no failures or skips; no race warnings in server logs | `python-linux.log`, `python-linux-junit.xml`, `python-linux-live/` |
| Python inventory collection, including deferred C++ prerequisite | 1,222 collected IDs match the checked-in expectations exactly | `python-inventory.txt`; collection is not execution |
| Go manifest-to-execution cross-check | All 578 required name entries in 137 groups have passing results on both platforms | `final-go-summary.json` |
| Changed Python paths | Ruff passed | Scoped model/parser/connection/errors and the three new test files |

Windows Go was 1.26.5; Linux Go was 1.22.2. Twelve further Go packages have no
test files and are not included in the 73 tested packages. The new Go cases add
19 top-level tests relative to the earlier source-review checkpoint. Focused
package runs also exercised the official FDD XSD and semantic mutation oracles.

The first integration run exposed three old assumptions: repository fixtures
accepted nonpositive rates, M25 buffered a regulating-only receiver, and the
save/restore test equated LITS with queue occupancy. Those expectations were
corrected while preserving positive equivalence, mutation rejection, callback
ordering and actual restored-message delivery assertions. The initial full
transcripts remain under `four-root-windows.jsonl` and
`four-root-linux-race.jsonl`; they are not the final passing records.

The manifest `--check` result is `CHECK_ONLY` (exit 2, no errors), not an executed
gate PASS. `--claim full-2010 --check` remains rejected (exit 1) for incomplete
normative coverage and missing independent evidence. Neither check runs service
tests; execution evidence is the actual campaigns above. No new execution
harness, freeze-state prerequisite or automatic CI policy was introduced.

The Python inventory adds 47 cases: 5 time/live, 2 DDM/live and 40 update-rate
cases. The broader Python-first execution selects all HLA campaign files except
`spec/m17/test_cpp_python_interop.py`. That one C++ prerequisite remains in the
full campaign inventory and is not relabeled as passed. Two Python participants
use independent RTI connections to a real server in the same test interpreter;
these new tests are not described as two independent federate OS processes.
Existing Go/Python cross-process tests also execute in the broader selection.

The Go regression roots are `./rti/... ./tests/... ./examples/...
./verification/...`; Linux enables `-race`, including child builds. Both runs
explicitly select an installed Python interpreter and the local official FDD XSD.
The manual evidence manifest includes the new bounded requirements and literal
Go test names. Complete normative coverage and independent certification flags
remain unset.

## Remaining Work

1. Complete constrained idle RO eligibility, exact-current-time delivery rules,
   zero-lookahead strictness, gradual lookahead changes and pending-horizon GALT.
2. Complete exact integer64 time across protocol, queues, grants and restore.
3. Continue dynamic-FOM/MIM admission, lifetime and ownership exception review.
4. Review proper-subset DDM dimensions, additive interaction subscriptions,
   immediate late regional discovery and unnamed update-rate queries.
5. Complete pending-message accounting and remaining normative service inventory.

Python retains the backend's current float64 rate range limit: very small
positive decimals rounding to zero and values overflowing float64 are rejected
with a separate range diagnostic. Unchecked raw Go model construction is not
made into a validating constructor; checked admission/export boundaries reject
invalid values. These fixes do not establish arbitrary-decimal runtime support.

C++ mapping work is deferred by the current prioritization, not waived as an
obligation of a future standard-language conformance claim. No performance
ranking, external certification, commit or remote publication was performed.
