# Callback reporting comparison

This experiment compares two Go builds from the same source tree. Both preserve
authenticated callback entry, outcome recording, receipt identity, and retry
handling. It does not disable IEEE service checks or callback bookkeeping.

| Build tag | Entry | Ordinary RO data outcome | Other outcomes |
| --- | --- | --- | --- |
| `callback_sync` (also the default) | Unary RPC, wait | Unary RPC, wait | Unary RPC, wait |
| `callback_async` | Negotiated local RO admission; otherwise ordered stream, wait | Bounded queue, no per-callback wait | Ordered stream, wait |

The async build negotiates local entry for ordinary, nonregional, untimed RO
data callbacks. The server advertises the capability only for its mandatory
receipt-backed data path; the SDK activates it after an authenticated report
has succeeded. Eligibility is retained from the server envelope, not mutable
application metadata. Local admission records invocation before executing the
body and queues ENTRY before OUTCOME. It does not pre-count buffered callbacks.
Timestamped traffic, callbacks carrying regions, recovery and control callbacks
keep confirmed entry. Unknown capability versions, older peers and disconnected
callback streams also retain confirmed entry. Python and Java continue to use
the existing unary reporting API; they do not select this transport automatically.

The stream batches up to 64 reports and acknowledges the successful prefix.
An error stops the batch before later reports are applied. The client queue is
bounded at 256 reports. Opaque receipts retain idempotence after a lost ACK.
Only unsupported-service negotiation falls back to the old unary API.

`DispatchEvent` may return before data entry and outcome are acknowledged in the async
build. This changes the gorti-specific report-error timing, not the intended HLA
callback order. `FlushCallbackInvocationReports` establishes the remote outcome
boundary and retries retained entry reports followed by results without invoking
the application again. Local-entry and outcome errors are tracked separately.
Background errors are retained and surfaced on a later report operation.
Subsequent outbound SDK service calls cannot overtake preceding report ACKs.
An asynchronous failure keeps that service boundary closed until the failed
reports are successfully retried; merely observing the error does not clear it.
Resignation drains completed outcomes before closing the reporting transport.
Applications should finish dispatch before resigning, as in the benchmark.
`CallbackReportTransportStats().LocalEntries` distinguishes negotiated local
admission from an outcome-only peer running the same build tag. Historical
results for `callback_async` must therefore be identified by binary hashes and
transport counters, not by the tag alone.

## Build and run

From the repository root on Windows:

```powershell
./benchmark/callback-reporting/build.ps1 -Output ../benchmark-results/callback-report-builds
python ./benchmark/callback-reporting/compare.py `
  --sync-server ../benchmark-results/callback-report-builds/rtid-sync.exe `
  --async-server ../benchmark-results/callback-report-builds/rtid-async.exe `
  --sync-client ../benchmark-results/callback-report-builds/gorti-go-fair-sync.exe `
  --async-client ../benchmark-results/callback-report-builds/gorti-go-fair-async.exe `
  --output ../benchmark-results/callback-report-comparison
```

On Linux, run `bash benchmark/callback-reporting/build.sh`, then use the same
Python command without `.exe` suffixes and with shell line continuations.
The `callback_sync` and `callback_async` tags are mutually exclusive. Build both
the server and the Go federate with the selected tag. An async client can still
connect to a server without the optional reporting service.

## Comparison protocol

- Two independent Go federate processes and a fresh server per trial.
- Identical FOM bytes, seed, DEVStone-HLA plan, confirmed producer calls,
  callback representation, and compact-summary logging in both builds.
- HI width 10, depth 10, five injected events: 4,060 measured data callbacks.
- Five warm-up pairs and 20 measured pairs, alternating AB/BA order, plus
  128 warm-up operations per channel within each trial.
- Duration from the subscriber's timing arm before VERIFY_START to the receive
  timestamp of its last subsequently validated callback. That timestamp is
  captured when the event pump dequeues the event, before DispatchEvent entry
  admission and the callback body. Report drain is checked before teardown,
  outside that interval; this is not a remote-outcome-settled latency metric.
- Exact callback counts and payload/order hashes must agree for each pair.
  Stream counters also confirm that the async build exercised its new path.
- Results and binary hashes are written outside the repository. The runner
  retains failed trials and does not save stdout/stderr execution logs.

The comparison reports paired completion-time speedup and an order-stratified
bootstrap 95% interval. It does not establish general RTI superiority, measure
TM performance, or replace the service conformance tests. Portico is not rerun
by this experiment.

## LocalLRC and before/after comparisons

`compare_transports.py` uses the same workload and measurement boundary to
compare confirmed sends with LocalLRC. It records actual LocalLRC counters and
requires all admitted operations to be acknowledged, with no failures.

```powershell
python benchmark/callback-reporting/compare_transports.py `
    --server ../benchmark-results/callback-report-builds/rtid-async.exe `
    --client ../benchmark-results/callback-report-builds/gorti-go-fair-async.exe `
    --output ../benchmark-results/local-lrc-comparison
```

To isolate a code change, supply `--baseline-server` and `--baseline-client`
from the earlier build, and set both `--baseline-mode` and `--candidate-mode`
to the same transport (`confirmed` or `local-lrc`). Both builds must use the
same `--callback-profile`. The manifest identifies both binary pairs and the
two completion modes. Default counts remain five warm-up pairs and twenty
measured pairs; failures stop the campaign and remain in the result data.

### Seed-block crossover

For a before/after change whose apparent gain varies with execution order, use
`--design crossover`. Every seed is run four times as ABBA or BAAB, so both AB
and BA are observed for that seed. Here A means baseline and B means candidate.
The sequence allocation is randomized reproducibly with `--order-seed` and
balanced separately within warm-up and measurement (within one block for an odd
count). Workload seeds remain `1516 + block index`; the order RNG is independent.

```powershell
python benchmark/callback-reporting/compare_transports.py `
    --baseline-server ../benchmark-results/before/rtid-async.exe `
    --baseline-client ../benchmark-results/before/gorti-go-fair-async.exe `
    --server ../benchmark-results/after/rtid-async.exe `
    --client ../benchmark-results/after/gorti-go-fair-async.exe `
    --baseline-mode local-lrc --candidate-mode local-lrc `
    --callback-profile async --batch-size 32 --external-events 100 `
    --design crossover --order-seed 20261007 --warmup 5 --runs 20 `
    --output ../benchmark-results/crossover-comparison
```

On Linux use the same Python command, shell line continuations and executable
paths without `.exe`. In crossover mode, `--warmup 5 --runs 20` means five
warm-up **seed blocks** and twenty measured seed blocks: 100 trials in total,
including 40 measured trials per build. It does not mean 20 independent pairs
per order. Both participants and the server are restarted for every trial.

The schedule is saved before execution and its hash is recorded in the manifest.
All four trials of each seed must have identical normalized semantic evidence.
Missing, duplicated, failed or mismatched trials invalidate the campaign;
they are not replaced. Results include sequence, position, UTC start and total
trial wall time. The latter includes startup and teardown and is not the primary
completion metric. The primary timing and ACK-drain boundaries are unchanged.

The completion speedup is the baseline arithmetic mean divided by the candidate
arithmetic mean. At equal delivered counts this is also the aggregate throughput
ratio. Its 95% percentile bootstrap interval resamples **whole four-trial seed
blocks**, stratified by ABBA/BAAB sequence, with 10,000 replicates and bootstrap
seed 1516. Repeated trials of the same seed are not counted as independent samples.
Order-specific means and a second/first-in-pair ratio help diagnose order effects;
they do not identify a CPU, cache, GC or scheduling mechanism by themselves.

Run an A/A control first by supplying the same server and client binaries for
both conditions, keeping the modes identical. Fix both campaign sizes before
starting and retain all outcomes. A control interval containing one is not an
equivalence proof, and its point estimate should not simply be subtracted from
the A/B gain. This design separates seed from execution order; long-lived
federation and control/delivery/settlement timing studies are separate work.

The schedule, analysis and failure retention have focused tests:

```powershell
python -m unittest discover -s benchmark/callback-reporting -p test_crossover.py -v
```

For the Go SDK's pending-callback admission cost alone:

```powershell
go test ./rti/pkg/federate -run '^TestRuntimeCallbackAdmissionRetainsRecoveryIdentity$' `
    -bench '^BenchmarkRuntimeCallbackAdmission$' -benchmem -count=3
```

This microbenchmark excludes network delivery and application callback time.
Use the paired workload above for end-to-end conclusions. The runner records
`GOMAXPROCS`; apply the same value to both conditions and do not run regression
tests concurrently with performance measurements.

## Preserved release reference

The July 27 results used a longer HI workload: 100 external events, 40,600
plan records and 81,200 data callbacks. Commit `5cf718a` (August 1 release) is
the nearest preserved source, not an attested copy of the July 27 executable.
Restore and build it outside the checkout without resetting local changes:

```powershell
python benchmark/callback-reporting/restore_reference.py --output ../benchmark-results/legacy-reference
$env:GOMAXPROCS = '2'
python benchmark/callback-reporting/compare_transports.py `
  --baseline-server ../benchmark-results/legacy-reference/rtid.exe `
  --baseline-client ../benchmark-results/legacy-reference/gorti-go-fair.exe `
  --server ../benchmark-results/callback-report-builds/rtid-async.exe `
  --client ../benchmark-results/callback-report-builds/gorti-go-fair-async.exe `
  --baseline-mode local-lrc --candidate-mode local-lrc --legacy-reference `
  --external-events 5 --output ../benchmark-results/legacy-reference-comparison
```

On Linux use the same Python scripts, omit `.exe`, and export `GOMAXPROCS=2`.
The restore script pins the commit and tree and records source/binary hashes.
The reference lacks current callback invocation receipts and transport counters.
`--legacy-reference` marks those baseline counters unavailable; it does not
relax callback count, payload/order hash, completion or candidate transport checks.
It is not evidence that the old implementation passes the current IEEE tests.
This is a contemporary, same-host reconstruction, not a reproduction of the
original container environment. The current build must pass the service suite
separately. Never use the legacy client to bypass current callback reporting.

`--external-events 100` restores the historical 81,200-callback load. Earlier
current builds failed this load at the 8,192-pending-receipt budget; a subsequent
trial also exposed callback-outbox saturation. The LocalLRC direct data path
now waits outside service leases for both limits and retries only an
uncommitted transfer. Keep those failed trials as experimental evidence. A
successful long run establishes delivery under bounded backpressure, not
recovery of the reference implementation's throughput. Compare completion time
and run the current IEEE service tests separately.

## Native Portico comparison

`compare_portico.py` compares either report profile against Portico using the
existing Java participant and the same Go participant, on one native host.
It does not run Docker, smoke tests or staging gates. Both RTIs use two
independent federate processes; gorti also uses its server process. Results,
plans, summaries, binary hashes and failures stay outside the repository.

Build the Go profiles above. Compile the existing Java participant against
Portico into the same external build directory, for example on Windows:

```powershell
$Build = [IO.Path]::GetFullPath('../benchmark-results/portico-callback-builds')
$Portico = [IO.Path]::GetFullPath('.tools/portico-extracted/portico-2.1.4/lib/portico.jar')
./benchmark/callback-reporting/build.ps1 -Output $Build
New-Item -ItemType Directory -Force "$Build/classes" | Out-Null
$Sources = @(Get-ChildItem verification/commercial-rti/src -Recurse -Filter '*.java' |
    ForEach-Object { $_.FullName })
javac --release 8 -encoding UTF-8 -cp $Portico -d "$Build/classes" @Sources
python benchmark/callback-reporting/compare_portico.py `
    --gorti-server "$Build/rtid-async.exe" `
    --gorti-client "$Build/gorti-go-fair-async.exe" `
    --gorti-profile async --java (Get-Command java).Source `
    --java-classes "$Build/classes" --portico-jar $Portico `
    --output ../benchmark-results/portico-callback-comparison `
    --warmup 5 --runs 20
```

The historical default is `--producer-mode confirmed`. To compare the LocalLRC
path, add `--producer-mode local-lrc --batch-size 32`. Add
`--external-events 100` for 81,200 measured data callbacks per trial; the default
of five external events produces 4,060 callbacks. Both RTIs receive the same
generated workload. The manifest records these choices and `GOMAXPROCS`.

The runner derives the existing TCP/TCPPING override from Portico's JGroups
configuration for deterministic loopback discovery. It preserves the other
ordering and reliability protocols. Participant callback accounting, ordered
payload hashes and normalized terminal state must match before a pair is
accepted. A failed pair is retained and stops the campaign; it is not replaced
with a successful retry.

For a same-seed order crossover, add `--design crossover --order-seed 20261007`.
Here A is Portico and B is gorti. `--warmup 5 --runs 20` then means five warm-up
and twenty measured four-trial seed blocks (ABBA/BAAB): 50 trials per RTI, of
which 40 are measured. Both AB and BA are run for every seed. The four trials
must have identical normalized evidence. Failures stop execution and remain
in `results.json`; a failed campaign is marked incomplete, not summarized as a
successful subset. The original paired default remains available.

The crossover reuses the whole-seed-block stratified bootstrap described above.
`completion_speedup` is **Portico mean / gorti mean**, so values above one favor
gorti. `gorti_over_portico_mean_completion_ratio` is its inverse and retains the
historical ratio convention below. The manifest and summary label both explicitly.
Do not treat repeated trials of one seed as independent pairs. Fresh-process
Portico startup and teardown can make wall-clock campaign time substantially
longer than the measured callback interval; both durations are retained separately.

The primary comparison is completed receive-order traffic, with the timing
boundary described above. Go timestamps the event-pump dequeue before
DispatchEvent; Java timestamps callback entry before payload validation.
API return latency is secondary metadata: gorti's confirmed mode waits for a
server result, while its LocalLRC mode completes on local admission. Neither
choice makes the other RTI's internal completion boundary equivalent. Do not
interpret their API return times as an equal-semantics speed ranking.

Each trial uses fresh processes, including JVMs, with 128 warm-up operations
per channel. This is not a long-lived-JVM steady-state, TM or multi-host scaling
experiment. The reported completion ratio is gorti/Portico: values greater
than one mean Portico completed faster.
