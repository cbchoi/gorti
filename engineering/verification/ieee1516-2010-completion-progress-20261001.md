# IEEE 1516 2010 Implementation and Verification Progress

Updated: 2026-10-01. Working branch: `codex/ieee2010-feature-completion`.

This record separates implemented behavior, executed acceptance tests and the
remaining work needed for a whole-RTI assessment. Full IEEE 1516-2010 conformance
has not been established. The evidence manifest retains
`normative.coverage_complete: false`; passing its supported-profile campaign is
not a whole-standard verdict.

## Implemented Behavior

| Area | Current implementation | Acceptance boundary |
|---|---|---|
| Logical time | Per-federation Float64 and Integer64 services, callbacks, MOM and checkpoint composition | Exact adjacent values above 2^53, finite MaxInt64, five advance modes and rejected mixed representations |
| Unpublication | Explicit empty versus omitted sets, owned-attribute relinquishment, pending acquisition guard, atomic callback reservation | Domain, wire and actual server scenarios; retained recipient projection |
| Synchronization | Registration outcomes, participant lifecycle, authenticated callback entry, terminal invocation, durable callback obligations and guarded background retry | Actual TCP and Go SDK; callback queueing is not treated as invocation |
| DDM | Template first-commit rules, contextual defaults and Excluded values, detached sender realizations, receiver-specific copied-region query leases | Portable value codecs and callback lifetime; final server integration is assessed separately |
| Timed save | Requester validation, pending schedule replacement and participant-specific initiation | Both time families, all five advance modes and message retention |
| Restore selection | Standard label-only lookup of a committed checkpoint from a recreated execution | Empty private SDK state only; incompatible source or target fails before mutation |
| MOM services | Typed service catalog, extension dispatch, authenticated managing-federate admission and deferred transportation confirmation | Service-provider contracts and selected actual server scenarios, not the complete 32-service live matrix |
| MOM regional attributes | Subject-only HLAfederate points, ordinary/regional union and postcommit late-subscription publication | Six actual server cases, both time families; copied-point query lifetime remains separate |
| Copied-region checkpoints | Exact geometry and receiver leases in additive DDM snapshot field 7 | Both typed restore graphs, generation rebind, atomic malformed rejection and legacy readers; not already-ready client queue recovery |
| MOM incoming counters | Class and runtime incoming counters advance at callback entry; outcome does not count twice | Optional measurement failure marks coverage unavailable without blocking the callback body |
| Queue measurement | Data remains counted across ready, transport and SDK stages until authenticated callback entry | The evaluator lease protects the waiting-to-ready observation; control callbacks are excluded |
| Language interfaces | Go, Python and Java callback admission, retry and transportation confirmation support | Separately executed binding campaigns; not inferred from generated declarations |

The queue measurement uses a narrow domain port, `core.ReadyDataQueue`. Its
transport implementation counts independently admitted opaque receipts, not
message payloads or possibly repeated event sequence numbers. Observations hold
the same time evaluator lease used by delivery transfers and release time-state
locks before reading the ready source. The disjoint-stage invariant requires
concurrency tests; summing unrelated queue-depth samples would be insufficient.

Copied DDM regions are prepared before outbound admission, become queryable at
callback entry and are released after callback outcome. Authentication checks
bind range and dimension queries to the invoking federate. This mechanism is an
implementation choice, not an IEEE requirement to use gRPC acknowledgments.

## Executed Evidence

Raw logs and executable artifacts are stored outside the repository under
`D:/workspace/gorti`. Results below retain their actual execution scope and are
not combined into a release-wide test total.

| Campaign | Observed result | Scope |
|---|---|---|
| Production timed save and committed-label restore | Passed repeated native and focused Linux race campaigns | Both time families; standard label-only restart with empty private SDK state |
| Mandatory callback admission and reconnect | Passed three repeated native runs and a fresh callback-focused Linux race run | Includes transient outcome retry, restored recipient identity and failed queue-commit rollback |
| Production queue entry oracle | Four cases passed after the reproduced failures | Float64/Integer64 by RO/TSO; ready state, wire receipt, foreign entry rejection, authenticated entry and duplicate completion |
| Fresh integrated data/DDM subset | Three repeated runs passed for time, server and independent object packages | Data-queue handoff, equal messages, multiple recipients, reconnect, discard/retraction, reflection/removal, waiting-state restore and copied-region query lifetime |
| Fresh authentication and CLI subset | Passed | Both time families reject unsigned copied-dimension queries; actual CLI resolves both factories |
| Go binding checkpoint | 155 SDK, 45 descriptor and 366 gRPC tests passed | Checkpoint before the final DDM authentication and queue-composition changes |
| Go DDM session update | 157 SDK tests passed | Includes session replacement and exact receiver-scoped range/dimension queries |
| Python binding checkpoint | 145 affected tests passed | One separate live case was excluded from that command; no whole Python campaign is inferred |
| Fresh Python callback/encoding subset | 152 tests passed | Eight selected files; no live server is implied by this command |
| Java binding checkpoint | 401 selected tests passed; seven live Integer64 cases passed separately | The live cases used the identified round6 executable, not a final integrated release |
| Fresh Java non-live campaign | 401 tests, no failures/errors/skips | Fresh source build; all 14 live classes consciously excluded |
| Final production language campaigns | Six Python and seven Java cases passed | The same assessed executable identified below; real Integer64 service/callback boundaries |
| Copied-state and entry-counting subset | Three repeated runs passed | MOM, server and savebundle; optional measurement failure, duplicate entry and typed copied-state codecs |
| Gate implementation checks | 78 tests and 89 subtests passed | Validates the evidence tooling, not RTI semantic conformance |
| Final integrated Go source campaign | 77 tested packages, 3,154 top-level tests and 8,319 test/subtest pass events; zero failed or skipped tests | Fresh execution of `./rti/... ./tests/... ./examples/... ./verification/...`; ten additional packages have no test files; no source edits during execution |
| Final executed evidence gate | 226/226 required command groups and 985/985 required entries passed; zero missing, failed or skipped tests | 39 packages, 978 distinct required names and 3,573 test run/pass events; a bounded supported-profile verdict |
| Final focused Linux race campaigns | Passed repeated server, MOM, callback, synchronization, queue and savebundle cases | Targeted concurrency coverage, not a whole-repository race campaign |

The earlier integrated server run found DDM query authentication/reconnect
failures, MOM regional-routing acceptance failures and an actual CLI startup
failure. These failed transcripts are retained. Fresh bounded reruns passed the
authentication, reconnect and CLI cases; MOM routing and the final integrated
campaign are recorded separately rather than retrospectively marking the
earlier run successful.

An earlier production checkpoint is `D:/workspace/gorti/rtid-ieee2010-20261001.exe`,
SHA-256 `6fb18b74f42c0a51f9a0f0741e63ed0b933bd5e85158ef1df7ba315e28de09a7`.
The final assessed executable is
`D:/workspace/gorti/rtid-ieee2010-assessed-20261001.exe`, SHA-256
`2418b107d6207f8a125fead39dfa9b8293ce5187442db49440a115b1eeb423f0`.
The final Python live campaign passed six cases in 2.27 seconds. The final Java
live campaign selected `LiveInteger64ProductionTest` and passed seven cases
without failures, errors or skips (13.95 seconds of reported test execution).
Both used the final assessed executable. Owned server processes were stopped
after the campaigns. Neither executable is an attested conforming release.

The final source result is recorded in
`D:/workspace/gorti/go-sourcecampaign-final-result-20261001.json`. The final gate
result and command transcripts are recorded in
`D:/workspace/gorti/ieee-quality-final-20261001-summary.json` and its accompanying
transcript directory. The captured and current normalized manifest hashes match:
`5513aee07cbbd82c0a59199b98434ee22f3969405779d9a1c80e717049ee7edd`.

The intermediate source campaign's six failures and the earlier gate's one
failure remain recorded in the intermediate result files. M2 fixtures lacked
lawful federation creation, joining and FOM registration; those prerequisites
were corrected without weakening fanout, ordering or no-self assertions. Timed
save fixtures now decode the mandatory wire wrapper rather than casting it to a
domain event. The MOM catalog scenario invokes authenticated callback entry
before checking the received counter. Its review also exposed a production
snapshot bug: pending outcomes masked already-authoritative entry counters.
The fix exempts only the three incoming data metrics when entry accounting is
configured, while preserving failure precedence and legacy/control behavior.
The historical admin startup failure has no proven cause; diagnostics were
improved and fresh isolated and integrated runs passed. It is not presented as
a diagnosed product defect.

## Quality Gate

The local/manual gate requires literal test names to execute and pass. Missing
tests, skips, package failures and incomplete execution reject the gate. This
iteration added timed-save participant, committed-label selection, guarded
synchronization retry, copied-region invocation, independent delayed-subscription
and data-queue entry groups, plus the callback queue-commit failure regression.

```powershell
python -B scripts/ieee2010_quality_gate.py --run
python -B scripts/ieee2010_quality_gate.py --claim full-2010 --check
```

The second command must remain unsuccessful until the complete applicable
obligation inventory and its reviewed evidence exist. A service declaration
census, selected gap list or descriptor golden file cannot replace that inventory.
The refreshed full-2010 readiness check is recorded in
`D:/workspace/gorti/ieee-full-readiness-20261001.json`: verdict `FAIL`,
`conformance_established: false`. Its reasons include incomplete normative
coverage and traceability, unresolved applicability and missing independent
executed evidence. This is distinct from the passing 226-group execution gate.

## Remaining Work

The subsequent [callback/SDK/MOM campaign](ieee1516-2010-callback-sdk-mom-20261001.md)
closes bounded discovery/data ready/transport/client recovery, recognized GRS1
Go/Python/Java translation and the actual implemented 32-service MOM matrix.
Its settled native run passed 3,226 top-level tests and 8,990 test/subtest events;
the prior counts above remain evidence for the prior campaign only. Fresh
capabilities, copied-region lease recovery and current-session handle resolution
have separate actual-server regressions. The subsequent
[control and result recovery campaign](ieee1516-2010-control-outcomes-20261001.md)
adds pending control replay, known finished result-only reporting, completion
markers, ownership/synchronization reconciliation, fresh transportation authority
and typed SDK-local results. Its final source run passed 3,317 top-level tests and
9,764 test/subtest events; actual restarted-server SDK campaigns passed in Go,
Python and Java. These results do not establish every normative callback variant.

1. Extend the implemented control/result recovery evidence to every applicable
   normative callback condition, reentrant lifecycle boundary and supported
   legacy client. Unknown or executing results and arbitrary local closures still
   reject export; that safety policy is not a complete conformance verdict.
2. Finish the exhaustive source-derived delayed-subscription review, including
   generation/join boundaries. Current tests cover retained payloads, previously
   unknown object discovery, withdrawal, inheritance, retraction and restored
   time queues in both time families; they are not the complete requirement set.
3. Extend verified GRS1 Go/Python/Java runtime translation to every private cache
   and supported legacy state format. Unknown formats fail before target identity
   allocation. Immutable same-label storage still needs a reviewed replacement
   policy.
4. Complete fresh-process ready-receipt/client-queue conservation and the legacy
   callback-capability policy. Current bounded conservation includes reflections,
   removals, retries, filtering, retraction and time-waiting restore; it does not
   establish all checkpoint transitions.
5. Extend the passing implemented MOM catalog, Reliable/BestEffort partitions,
   standard reports and regional routing matrix to every normative clause and
   exception permutation. Copied subject-point/report lifetime and independent
   clients remain separate acceptance boundaries.
6. Complete the applicable IEEE 1516, 1516.1 and 1516.2 obligation inventory,
   including conditional triggers, exceptions and normative annexes. Review the
   chosen standard Java binding with independently authored clients.
7. Extend the completed settled source, selected binding and focused race
   campaigns to the remaining obligation inventory and full binding coverage.
   Keep exact assessed-source/binary associations and assess the standard's
   conformance-label provisions separately from internal testing.

## Review Cycle

Plan: derive each scenario from the supplied standard editions and assign file
ownership before parallel implementation. Do: reproduce semantic failures before
changing production behavior. Review: exercise actual server, SDK, failure and
concurrency paths instead of accepting callback enqueueing as execution.
Reflect: promote only the proven scope, retain failed transcripts and turn newly
found gaps into explicit work rather than weakening assertions.

The supplied redlines remain private source material. Public documentation
paraphrases requirements and records clause/page locators; it does not redistribute
licensed text. The [IEEE supplemental downloads](https://standards.ieee.org/downloads/)
and [SISO data-file catalog](https://www.sisostandards.org/page/DataFiles) provide
API, MIM and schema resources. Their existence is not complete semantic proof.
The [IVCT operation project](https://github.com/IVCTool/IVCT_Operation) describes
interoperability test suites; an IVCT subset is not treated here as whole-RTI
certification.
