# Internal Python IEEE 1516-2010 Assessment

Date: 2026-10-02. Scope: the advertised product Python HLA SDK, including
the asynchronous services, IEEE-shaped ambassador, handles, encodings, FOM,
transport/TLS, historical specification tests and actual-server scenarios.
This is an internal assessment, not certification or a completed normative
conformance determination. No production or test code was changed.

## Verdict

**The Python campaign does not pass.** Across 1,952 distinct selected cases,
the completed broad rerun plus explicit live run yield **1,921 passed,
30 failed, zero errors, and one skipped**. No selected cases were deselected.
Failures remain failures even where source inspection identifies an obsolete
expectation or a fixture problem. They were not converted to skips or xfails.

Two production SDK defects have direct source and executable evidence:
unauthenticated DDM bounds queries and broken interaction-advisory dispatch.
Other failures require the narrower classifications below; the failed-test
count is not a count of IEEE violations.

## Scope And Identity

The existing `pysdk/tests/hla_campaign.json` classification was used to select
HLA tests, without running its execution/freeze gate. Current discovery found
176 Python test files, of which 28 were absent from that historical manifest.
All 24 additional HLA files were included; four additional RL files remained
outside scope. The resulting selection contains **130 files**. The remaining
46 files cover RL/PPO, application-level pyjevsim adapters and cross-package
static quality. Their complete list is preserved in `excluded-files.json`.
Exclusion is a scope decision, not a passing result or a pytest deselection.

Runtime: `D:/workspace/gorti/n04-venv/Scripts/python.exe`, Python 3.14.0;
pytest 9.1.1, pytest-asyncio 1.4.0, grpcio 1.84.0 and protobuf 7.36.2.
Working directory: `D:/workspace/gorti/gorti`, an extensively dirty checkout.
Existing unrelated changes were preserved. This is source-tree execution,
not an isolated installed-wheel qualification.

The selected server was `D:/workspace/gorti/rtid-sdk-gap-fix-20261001.exe`.
Its SHA-256 was checked before and after the campaign:

`6c9cf2c18c06236a909e900d9d70e58db6fa31e1d6708e81400e44f5ab17d979`

The historical launchers build their own external `rtid.exe` rather than use
`GORTI_LIVE_RTID`. Their actual rerun executable, at
`retry-server-artifacts/gorti-legacy-sdk-gwmbh9j8/rtid.exe`, has the same hash.
The main assessment's fresh
`D:/workspace/gorti/rtid-internal-assessment-20261002.exe` was also independently
hashed and is identical. No historical pin or executable metadata was rewritten
to manufacture that equality. This does not attest an immutable release or the
source-to-binary relationship of every file in the dirty checkout.

## Completed Executions

All evidence is outside Git at
`D:/workspace/gorti/internal-python-assessment-20261002/`.

| Run | Selected cases | Passed | Failed | Errors | Skipped | Pytest elapsed |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| Original broad run, live opt-in off | 1,952 | 1,827 | 59 | 11 | 55 | 144.63 s |
| Broad rerun, fresh external Go cache | 1,952 | 1,874 | 23 | 0 | 55 | 297.56 s |
| Explicit current live selection | 83 | 76 | 7 | 0 | 0 | 45.34 s |
| Distinct cases, broad rerun plus live | 1,952 | 1,921 | 30 | 0 | 1 | Not additive |

Every pytest process completed with exit code 1. There were no reported xfails
or xpasses. The 83-case live selection includes 23 passing selector unit cases
and **60 actual-TCP cases: 53 passed, seven failed**. It executes all 54 cases
skipped by the broad run's live opt-in, plus six black-box contracts and 23
unit cases already in the broad selection. Those 29 repeated cases are not
counted twice in the distinct-case row. Original failures remain in their own
JUnit/log records rather than being overwritten by rerun outcomes.

The original 70 nonpassing cases included 42 Go-build/enumerator failures and
11 setup errors caused by inaccessible existing Go caches. The fresh-cache
rerun, with installed-runtime access, removed that infrastructure blockage.
It did not make every historical scenario pass: six formerly blocked cases
then failed their behavioral expectations. The other 17 original behavioral
failures remained failures in the rerun.

The sole remaining skip is
`test_spec_m17_7_cpp_publisher_python_subscriber`: its configured C++ publisher
does not exist at the expected build path. This is an unavailable cross-language
prerequisite, not a Python contract failure. No C++ build or vendor substitution
was used to label it verified.

## Failure Classification

The following disjoint groups account for all **30 distinct failures**.
Classification does not change their recorded pytest outcome.

| Cases | Group | Evidence and assessment |
| ---: | --- | --- |
| 5 | DDM bounds query authentication | Four M10 async/ambassador range cases and the M12 DDM case reach `DDMClient.query_bounds`, then receive `UNAUTHENTICATED: valid session token is required`. `pysdk/rti1516e/ddm.py:224-231` constructs/calls `QueryBounds` without a federate handle or session metadata; the DDM client constructor also retains no token. The server's participant-scoped query at `rti/internal/adapters/grpc/ddm.go:233` requires authenticated identity. **Confirmed product SDK integration defect**, not a missing executable. The remaining range-bound assertions are not verified by these failed cases. |
| 2 | Interaction advisory callback dispatch | M39's all-event dispatcher and M27's actual-server cross-federate ambassador both raise `UnboundLocalError` for `InteractionClassHandle`. The branch-local import at `pysdk/rti1516e/standard.py:2071` makes the name local throughout `_invoke_event`, but `TurnInteractionsOn/Off` use it at lines 2210/2212 without entering that branch. **Confirmed production Python defect**; the two tests demonstrate synthetic and real-server manifestations. |
| 2 | Obsolete Integer64 rejection expectations | M26 facade lifecycle and `test_live_time_selection_metadata_and_rejection` expect creation with `HLAinteger64Time` to fail. The same candidate passes all six exact Integer64 production cases, including all five advance modes and adjacent TSO timestamps above `2^53`. These tests' Float64-only rejection oracle is obsolete for this candidate. Later assertions in the failed tests are not credited as executed. |
| 2 | Obsolete pending data-metric availability expectation | The two evoked callback variants expect `HLAinteractionsReceived` to be absent while data callbacks are queued. The candidate supplies encoded zero, with two callbacks pending. Current `data_entry_metrics_pending_test.go` explicitly expects available zero before ENTRY and increments at actual ENTRY. This is a stale metric-availability oracle relative to the current entry-accounting profile. Immediate callback variants pass; the failed evoked cases stop before their later delivery/count assertions. |
| 2 | Historical synchronization callback participation | M12 sync scenarios fail with `SynchronizationPointLabelNotAnnounced`. They use raw asynchronous event consumption/direct achieve calls without reporting actual callback invocation. The current server distinguishes callback delivery from application invocation. Fixture participation/receipt compatibility needs review; this result alone is not a confirmed synchronization implementation defect. |
| 1 | Historical restore participation | M12 save/restore fails with `RestoreInProgress` while querying immediately after restore request and before processing the validated restore stage. Modern fresh-client/restart restoration scenarios pass. This historical sequencing/guard mismatch remains failed, with its later completion assertions unverified. |
| 2 | Restored same-time grant ordering assertion | NMR/NMRA tests report message index 23 versus grant index 17. They search by dataclass equality starting before restore; `TimeAdvanceGrant` equality compares only `time`, so an older/recovered grant at time five can be selected instead of the newly awaited grant at five. The actual message is delivered and has expected metadata. The index oracle is ambiguous; **no ordering-regression verdict** is inferred without event-identity discrimination. Original assertions were not changed. |
| 10 | RO queue metric/raw-consumer receipt mismatch | Nine malformed-timestamp cases stop at their initial `HLAROlength == 0` precondition with value one, before attempting the invalid timestamp. Another RO queue case reads three after domain delivery, where it expects zero. `LiveCallbacks` only collects raw events; it does not dispatch/report invocation of the current receipt-bearing callbacks. Queue/callback accounting and the oracle boundary need reconciliation. These are not nine demonstrated timestamp-admission defects, and that admission behavior remains unverified by these executions. |
| 1 | DDM no-shared-dimension delivery | The `no-shared-axis` case receives both `subset:y-only` and the expected `subset:x-hit`, rather than only the latter. Current DDM overlap source describes no common dimensions as nonoverlap. **Unresolved observable candidate/SDK routing mismatch**; no passing claim or normative exemption is assigned. Other proper-subset cases pass. |
| 2 | Raw ownership callbacks over restore | Requested-confirmation restore gets `INTERNAL: federate outbox unavailable`; waiting-offer restore observes a second ownership-assumption event. Both recur in the two broad runs. These raw callback observers do not acknowledge actual invocation, while restart recovery intentionally replays uninvoked controls. That can affect duplicate-event expectations; the outbox failure still requires diagnosis. **Unresolved restore/receipt integration**, not silently dismissed as fixture-only. |
| 1 | In-process event pump/thread boundary | `test_ambassador_callbacks_dispatch_from_event_pump` observes no callbacks after its one-second wait. It pushes into `asyncio.Queue` from the caller thread while the ambassador waits on a background loop; `InProcessTransport.push_event` uses direct `put_nowait` without a thread-safe loop wakeup. **Source-supported thread-boundary diagnosis**, not a demonstrated real-TCP delivery failure. No wakeup patch or weakened wait was used. |

In particular, passing source/domain/race checks elsewhere do not erase the
DDM authentication or Python advisory failures, and a failing historical test
does not automatically establish a normative service violation.

## Passing Live Evidence

The explicit run passes exact Integer64 advances and adjacent timestamps,
GALT pending-request/retraction scenarios, RO/TSO delivery scenarios, FOM
admission/extension, DDM association lifecycle and region subscription options,
timed save, retraction history, and default terminal ownership invocation.
Both time families pass fresh-runtime restoration and actual server restart
with fresh Python clients, including control/outcome recovery and mixed local
notifications. TLS and Go/Python handle alignment also pass in the broad rerun.

The six separately authored black-box contracts pass in all three completed
runs: duplicate creation, joined-member destruction rejection, unknown/nonowner
deletion controls, FOM contribution persistence, `NameNotFound` lookup and rejoin
after remote recreation. This is six distinct cases, not 18 distinct requirements.
The main assessor's additional six TCP passes and reported Go/race counts
are integration context only; they are not included in this Python tally.

## Missing Coverage And Claim Boundary

The selected test inventory covers the advertised Python behavioral families,
but is not an exhaustive method/state/exception/callback obligation matrix.
Some tests assert surface presence, translation or mocked behavior, rather
than production semantics. Their passes cannot close an entire IEEE clause.

The normative inventory consulted during this Python assessment has
`coverage_complete: false` and 20 seed requirements. The separate
[normative audit](ieee1516-2010-internal-normative-audit-20261002.md) completed
its bounded coverage/applicability review but explicitly leaves G0/G3 open;
it did not complete the full normative inventory. That audit and the main
core campaigns were not independently rerun by this Python campaign.
Integration must associate the actual Python outcomes with reviewed applicable
obligations; these executions alone do not establish all FOM/OMT conditions, qualify arbitrary
federate/federation responsibilities, or prove every permitted concurrent
schedule and platform profile.

Python is a product binding, not the selected standard Java mapping. Standard
Java API qualification, complete C++ binding/ABI qualification, official IVCT,
cross-vendor interoperability and organizationally independent certification
are outside this report. The separate 35-case Python IVCT-inspired directory
was not run: its fixture hard-codes `bin/rtid` and does not expose the selected
external binary/artifact configuration. No fixture was edited or replaced to
manufacture coverage. RL/application/static-quality exclusions and installed
wheel/platform qualification also remain outside this execution scope.

## Reproduction And Evidence

The executions use `-B -m pytest`, `-c pysdk/pyproject.toml`,
`-p pytest_asyncio.plugin -p no:cacheprovider -o addopts= -ra --capture=sys`,
external `--basetemp` and `--junitxml`, and `--durations=20`. Full selections
are saved in `selected-files.json` and `live-selected-files.json`.

Environment common to the runs:

```text
PYTHONPATH=D:/workspace/gorti/ieee2010-schema-control-20261001;D:/workspace/gorti/gorti/pysdk
PYTEST_DISABLE_PLUGIN_AUTOLOAD=1
PYTHONDONTWRITEBYTECODE=1
GORTI_LIVE_RTID=D:/workspace/gorti/rtid-sdk-gap-fix-20261001.exe
GORTI_RTID=D:/workspace/gorti/rtid-sdk-gap-fix-20261001.exe
GORTI_RTID_SHA256=6c9cf2c18c06236a909e900d9d70e58db6fa31e1d6708e81400e44f5ab17d979
GORTI_REQUIRE_SUPPORT_NAME_TRAILER=1
```

`GORTI_RUN_LIVE_E2E` is zero for the broad runs and one only for the explicit
16-file live selection. Server/FOM/save artifacts use separate external
`GORTI_LIVE_ARTIFACTS` and `GORTI_INDEPENDENT_EVIDENCE` directories per run.
The retry uses external `go-cache` and `os-tmp` directories and the existing
`D:/workspace/gorti/.tools/gomodcache`; only runtime access was escalated.
No blanket obsolete-test opt-in or assessment gate was used as passing proof.

Evidence files: `baseline.log/xml`, `retry.log/xml`, `live.log/xml`, the three
`*-outcomes.json` files, `run-summary.json`, `combined-outcomes.json`,
selection/exclusion lists, server artifact directories and
`cleanup-and-binaries.json`. JSON outcomes are derived from completed JUnit
records; they are not authenticated external attestations.

## Cleanup

All three pytest sessions ended. At the final process inspection on
2026-10-02 11:08:28 +09:00, **zero servers associated with this Python evidence
directory remained**. The independent fixtures also retain per-server closure
records. An unrelated Java-assessment server remained and was deliberately
untouched. The selected and rebuilt server hashes were unchanged and identical.
The only authored repository artifact is this report.
