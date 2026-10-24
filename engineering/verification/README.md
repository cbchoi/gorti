# Verification Records

The [IEEE 1516-2010 test-case guide](ieee1516-2010-test-case-guide.md) explains
requirements-based case design, evidence classes, the retained Linux results,
and model/table reproduction. It also maps the SRS/SDD/IDD/STD documents to
educational exercises. Its purpose is reusable tests for conformance assessment,
not a certification verdict.

The [2026-10-19 Linux publication validation](linux-publication-validation-20261019.md)
records the Go/Python/Java service results on a main-based candidate that
excludes separate unpublished RL work, the timed-example startup fix, and
the Go lint cleanup. It distinguishes local Linux validation from a hosted
GitHub Actions run.

The [2026-10-08 Portico comparison after lazy callback report signals](portico-report-signal-20261008.md)
records 100 accepted crossover trials and a 1.608x gorti/Portico aggregate
throughput ratio, with a block-bootstrap interval favoring gorti. Both RTIs show
within-seed variation; the reversal from the preceding session is not an isolated
measure of the SDK optimization or a general RTI ranking.

The [2026-10-07 Portico comparison after static FOM indexing](portico-fom-static-metadata-20261007.md)
records 100 accepted crossover trials. Portico leads the observed mean and median;
its long observations widen the mean-ratio interval to include one. No consistent
gorti superiority or whole-standard conformance claim follows.

The [2026-10-07 static FOM metadata report](fom-static-metadata-20261007.md)
records immutable name/ancestry/default indexes, 978/978 required cases in both
profiles and a 3.92% crossover throughput gain. The same-binary control also
shows execution-position effects; broad consistency remains unestablished.

The [2026-10-07 same-seed callback crossover study](callback-crossover-20261007.md)
records a fixed A/A control and M1/M2 comparison: all 160 trials passed, with
a 4.12% aggregate throughput gain in the comparison and a whole-seed-block
confidence interval. Cross-session consistency and Portico ranking remain open.

The [2026-10-07 delivery-side callback batch report](callback-delivery-batch-20261007.md)
records bounded shared accounting, late-Send evidence preservation, full prepared
regression, lower allocations and inconclusive final throughput improvement.

The [2026-10-07 callback payload comparison report](callback-payload-view-20261007.md)
records removal of comparison-only payload copies, differential tests, full
prepared IEEE-service regression, and an 8.08% paired aggregate throughput gain.

The [2026-10-06/07 Portico comparison after copy and lookup optimization](portico-copy-lookup-20261006.md)
records a fresh 20-pair comparison. gorti wins the campaign mean, while Portico's
five fast observations and large between-campaign variation preclude a blanket ranking.

The [2026-10-06 profile-guided callback and MOM report](callback-copy-lookup-20261006.md)
records elimination of repeated service-catalog parsing and duplicate callback
copies, regression evidence and a paired comparison with unchanged SDK binaries.

The [2026-10-06 Portico comparison after server batching](portico-server-callback-batch-20261006.md)
records a fresh paired campaign using the preceding gorti server, identical
workloads, runtime-default CPU settings and two independent federates per RTI.

The [2026-10-06 server callback batch report](server-callback-batch-20261006.md)
records authenticated report-prefix processing, preserved MOM and receipt
semantics, whole-service regression results and a paired server-only comparison.

The [2026-10-06 LocalLRC callback pipeline report](local-lrc-callback-pipeline-20261006.md)
records bounded completion workers, pipelined report frames, completed-payload
release, and the actual-server recovery regression found and repaired during
whole-service validation.

The [2026-10-06 completed-receipt retention report](completed-receipt-retention-20261006.md)
records reduced ledger-copying cost, preserved recovery behavior and a paired
81,200-callback comparison against the preceding current implementation.

The [2026-10-06 LocalLRC backpressure report](local-lrc-backpressure-20261006.md)
records whole-fanout receipt credits, lease-free capacity waits, and successful
81,200-callback paired experiments. It separates recovered workload completion
from throughput improvement and documents current IEEE service regression.

The [2026-10-04 implementation and linkage increment](ieee1516-2010-applicable-fixes-20261004.md)
records corrected object, callback, checkpoint, MOM, FDD and Java behavior.
All 639 currently reviewed applicable records are linked to explicit evidence;
diagnostics and architecture-boundary tests are not semantic completion.
Full RTI conformance remains unestablished.

The [2026-10-02 completion check](ieee1516-2010-completion-check-20261002.md)
records newly reproduced Python/FM, pending-deletion and FOM-name defects,
passing DDM/MOM/time-boundary checks, and updated requirement evidence.
Internal completion is not passed; external assessment is not the only remaining work.

The [2026-10-02 whole-source requirement review](ieee1516-2010-requirements-review-20261002.md)
replaces seed-only coverage arguments with a candidate full-source census,
service-specific decompositions, inspected oracles and linked execution records.
It records remaining internal work and does not declare full conformance.

The [2026-10-02 defect corrections](ieee1516-2010-defect-corrections-20261002.md)
record fixes and fresh regression evidence following the internal assessment.
The earlier failures remain archived; this increment is not a full-conformance
or certification verdict.

The [2026-10-02 internal whole-RTI assessment](ieee1516-2010-internal-assessment-20261002.md)
records fresh broad Go/race, Python and official-Java execution, reproduced
behavioral failures, retained obsolete/ambiguous oracles and the remaining
normative coverage. The full internal verdict is not passed; this is not an
external assessment or certificate.

The [requirement traceability and independent-check increment](ieee1516-2010-traceability-20261001.md)
records source-located atomic obligations, fresh bounded Go execution,
independently authored public Python contracts and retained support/lifecycle
failures. Its record-validation result is not a full-conformance verdict.

The [control callback and unreported result recovery campaign](ieee1516-2010-control-outcomes-20261001.md)
records invocation-phase recovery, fresh authorization, SDK local notifications,
restore readiness and the separately assessed source/server evidence. Older
records retain the scope and limitations of their own checkpoints.

The [DDM, callback knowledge and typed-time increment](ieee1516-2010-ddm-epochs-20260930.md)
records inactive association lifecycle and restore validation, cancellable
ownership-assumption epochs, and exact Integer64 primitives. It explicitly
distinguishes these from full-server Integer64 and terminal callback receipt.

The [2026-09-29 service completion increment](ieee1516-2010-service-completion-20260929.md)
records the complete Java method map, atomic grouped DDM calls, support and
ownership corrections, persisted Java runtime state and the remaining normative work.

The [Java stateful-services increment](ieee1516-2010-java-stateful-20260929.md)
records ownership, regional routing, same-session save/restore, encoding helpers
and the remaining Java/backend constraints.

The [flush, activation and retraction batch](ieee1516-2010-flush-enable-retract-20260922.md)
records FQR, safe activation callbacks, legal retraction and saved-state behavior.

The [preferred-order and advance batch](ieee1516-2010-order-advance-20260922.md)
records FOM order defaults, unengaged advancement and same-time TSO eligibility.

The [RO queue implementation](ieee1516-2010-ro-queue-20260922.md) records message
eligibility, release ordering, versioned save/restore and recovery validation.

The [third conformance implementation batch](ieee1516-2010-third-batch-20260922.md)
tracks pending GALT, strict range admission and the remaining RO delivery work.

The [second conformance implementation batch](ieee1516-2010-second-batch-20260921.md)
records atomic outgoing admission, zero-lookahead grant history, DDM and
ownership-confirmation corrections and their Windows/Linux regression evidence.

The [first conformance implementation batch](ieee1516-2010-first-batch-20260921.md)
records the 2026-09-21 W1/W3/W4 changes, fresh Go/Python execution, review
corrections and remaining obligations from the conformance execution plan.

The normative test contract is the
[Software Test Description](../specifications/current/STD.md). Executable
fixtures live under `tests/` and `verification/`; generated benchmark output is
kept outside version control unless it is archived as release or publication
evidence.

Public summaries are available in `docs/verification.md`,
`docs/reproducibility.md`, and `docs/performance.md`. Historical acceptance
scope is summarized in the versioned STD documents and linked executable
evidence.

The [performance milestone record](performance-milestones.md) documents each
optimization's plan, implementation, review, measurement, and promotion
decision.

The [IEEE 1516-2010 conformance assessment](ieee1516-2010-conformance-assessment.md)
records observed API and semantic gaps, the remaining normative audit, and
milestones for establishing an RTI conformance claim.

The [2026-10-03 applicable-requirement progress](ieee1516-2010-applicable-requirements-20261003.md)
records new assertion-level links, retained failures and an ID-level remaining backlog.

The [2026-10-03 full validation run](ieee1516-2010-full-validation-20261003.md)
records the expanded campaign, legacy IVCT failures, Java restart instability
and the distinction between passing bounded tests and whole-RTI conformance.

The [2026-10-03 defect corrections](ieee1516-2010-defect-fixes-20261003.md)
record repairs and fresh regression evidence for the six failures found in the
completion check, including same-time callback ordering and FOM error mapping.
Whole-RTI conformance remains unestablished.

The [2010 implementation progress](ieee1516-2010-implementation.md) separates
tested corrections from remaining service semantics and normative audits.

The [2026-09-20 service checkpoint](ieee1516-2010-service-completion-20260920.md)
describes object knowledge, declaration/DDM options, MOM traffic, scheduled
checkpoints, SDK changes, and the remaining implementation work.

The subsequent [live-services checkpoint](ieee1516-2010-live-services-20260920.md)
records live FOM extension, scheduled advancement and restore identity,
publisher rate advisories, MOM queries, and callback metadata integration.

The [remaining-service implementation record](ieee1516-2010-remaining-services-20260920.md)
covers authenticated automatic resign, cross-generation restart restoration,
deferred object deletion, ownership denial, LocalLRC reinitialization, SDK
callback execution, and measured MOM reporting. Consult this record before
treating older audit findings as current implementation gaps.

The [2010 requirements matrix](ieee1516-2010-requirements-matrix.md) links
observed gaps to concrete acceptance scenarios, correction checkpoints, and
questions that require the full normative standards.

The [exact-time and ownership-callback increment](ieee1516-2010-exact-time-callbacks-20260930.md)
records exact Integer64 layer migration, authenticated invocation receipts and
fresh callback scopes after restore. Production Integer64 factory selection
remains disabled pending complete server composition; these layer tests do not
establish whole-RTI conformance.

The [feature-completion checkpoint](ieee1516-2010-feature-completion-20260920.md)
records the next implementation batch and its fresh verification. The separate
[feature inventory](ieee1516-2010-feature-inventory.md) is a baseline discovery
ledger, not a percentage of standard conformance.

The [second implementation batch](ieee1516-2010-batch2-20260920.md) records
FDD metadata retention, authenticated MOM attribution, Python inputs, replay
corrections, and the separate delivery-accounting and strict-C++ foundations.
It reports remaining failures and work orders without treating partial profiles
as full conformance.

The [third implementation batch](ieee1516-2010-batch3-20260920.md) continues with
selected-MIM handling, transport/rate fidelity, real outbox diagnostics, terminal
MOM reporting and SDK work. Consult its results and remaining-work section for
the current boundary rather than treating older missing-input findings as current.

The [whole-RTI engineering audit](ieee1516-2010-full-rti-audit.md) covers the
remaining state, callback, DDM, time and SDK gaps across service groups. It
distinguishes observed defects from questions requiring normative text.

The [2010 quality gates](ieee1516-2010-quality-gates.md) define the evidence
required for a tested subset, a full conformance assessment and certification.
The local/manual gate runs the exact Go cases in
[`ieee1516-2010-evidence.json`](ieee1516-2010-evidence.json):

```sh
python -B scripts/ieee2010_quality_gate.py --run
python -B scripts/ieee2010_quality_gate.py --claim full-2010 --check
```

The retained-FDD group requires `GORTI_FDD_XSD` to identify the official
`IEEE1516-FDD-2010.xsd` from the user's local supplementary materials and
`GORTI_FDD_PYTHON` to identify a Python interpreter with `xmlschema` and `lxml`.
Missing oracle prerequisites produce a skip that fails this manual gate; they
are not waived. The Python HLA campaign has its own checked-in inventory and
runner in `scripts/verify_python_hla.py`; the Go gate does not substitute for it.

Run from the repository root with Go and Python 3.10+ on PATH. The first command
returns 0 only after the selected tests actually pass without skips; it does not
establish whole-service conformance. The second currently returns 1 because
normative coverage and independent evidence are incomplete. `--check` alone
returns 2 for a valid plan without executing tests. JSON stdout records the
commands, observations and verdict; retain execution output outside Git.
