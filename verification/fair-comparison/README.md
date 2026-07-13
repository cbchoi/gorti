# Fair Pitch/Go comparison contract

This directory defines the fail-closed boundary for performance claims between
Pitch pRTI and gorti's Go path. Its owned adapters invoke the actual
`verification/pitch/Run.ps1` and `verification/gorti-go-fair/run.ps1`
launchers, then convert their `benchmark.json` and `canonical.ndjson` outputs
to the shared result contract.

## Fixed workload

Every arm receives the same resolved path to
`verification/pitch/fom/PitchVerifier.xml`. The orchestrator does not copy or
rewrite that FOM. It hashes the source bytes once, passes the exact path to both
arms, and requires both result artifacts to echo that SHA-256.

The workload contract is otherwise fixed except for `count`:

```text
seed                 = 1516
two_process          = true
choreography         = sequential_update_send_then_tar
delivery_boundary    = subscriber_pre_tar_to_both_callbacks
callback             = immediate
server_event_log     = file
expected_fanout      = delivered = 2 * count
rejected/dropped     = 0
duplicates/invalid   = 0
```

The delivery duration starts immediately before the subscriber TAR and ends
only after both reflection and interaction callbacks have completed. Payload
construction, encoding, and other setup must be outside the raw timed sample.
For caller API samples, both implementations resolve FOM handles and construct
handle-keyed value maps before timing. Go also creates its timeout context
before starting the counter. The measured interval starts immediately before
the SDK call and ends after that call returns, so request construction,
serialization, transport, server processing, and the response remain included.
All raw metric values are non-negative integer nanoseconds. A launcher must use
the supplied workload JSON as its source of truth and emit one
`gorti.fair-comparison/launcher-result-v1` artifact.

## Semantic parity

Each launcher normalizes its semantic transcript to exactly four records in
FM, DM, OM, TM order. Each projected item has this shape:

```json
{"service": "FM", "record": {"...": "canonical semantic fields"}}
```

The projection SHA-256 is computed over UTF-8 bytes of compact JSON with ASCII
escaping, lexicographically sorted object keys, and no trailing newline. The
Python validator recomputes this digest. Analysis rejects a session unless the
entire four-record projection and digest are identical across Pitch and Go in
every measured pair. Matching event counts alone is never sufficient. Warmup
artifacts are also validated and must share the session semantic identity.

## AB/BA execution

`run-comparison.ps1` defaults to five warmup pairs followed by twenty measured
pairs. Every pair runs both arms serially. Measured order contains exactly ten
AB pairs (Pitch then Go) and ten BA pairs (Go then Pitch), strictly alternating
from an orientation chosen by the recorded order seed. Warmup order is also
alternating and balanced to within one pair. This limits monotonic session
drift from accumulating in one order stratum. Warmups are never included in
statistics.

Before each process starts, `manifest.json` records its global position, phase,
pair and slot, AB/BA order, resolved executable and SHA-256, exact arguments,
working directory, environment overlay, output path, and start time. Completion
adds duration, exit status, result path, and result SHA-256. The manifest also
captures repository state, PowerShell/Python, CPU, active Windows power scheme,
`GOMAXPROCS`, config hash, and canonical FOM provenance.

Command templates must carry these shared tokens in both arms:

```text
{fom} {seed} {count} {server_event_log} {output} {run_id} {workload_file}
```

Optional tokens are `{repo}`, `{fom_sha256}`, `{phase}`, `{pair}`, and `{slot}`.
Templates are argument arrays and are never evaluated as shell source.

`launchers.local.json` is ready for Windows PowerShell and does not assume
`pwsh` is installed. The adapters use these defaults when the corresponding
environment variable is not set:

```text
PRTI1516E_HOME       = C:\Program Files\prti1516e
GORTI_FAIR_RTID_PATH = {repo}\verification\out\fair-comparison\bin\rtid-fair.exe
GORTI_FAIR_GO        = go
GORTI_FAIR_PYTHON    = python
```

The adapter parameters `-PRTIHome`, `-RtidPath`, `-Go`, and `-Python` can also
be added directly to the corresponding argument arrays.

These are adapter defaults, not environment overlays in the JSON, so caller
environment values override them without editing the ready config.

Run a claim-grade session from the repository root. The persistent wrapper
keeps one Go RTID alive across warmup and measured arms, mirroring the
persistent-server shape of the externally managed Pitch endpoint:

```powershell
.\verification\fair-comparison\run-persistent-comparison.ps1 `
  -ConfigPath .\verification\fair-comparison\launchers.local.json `
  -OutputDirectory .\verification\out\fair-comparison\claim `
  -Count 100 -ServerEventLog file
```

The persistent wrapper starts one selected Go RTID with a session-level file
log directory, and every Go arm invokes the real launcher with `-NoStartRtid`
and a unique federation name. An online Pitch RTIexec can be bound to the same
session-persistent lifecycle with these process environment values:

```powershell
$env:GORTI_FAIR_PITCH_CRC_PID = '<RTIexec PID>'
$env:GORTI_FAIR_PITCH_CRC_ADDRESS = '127.0.0.1:8989'
$env:GORTI_FAIR_PITCH_EVENT_LOG_DIR = 'C:\Program Files\prti1516e\logs'
```

In this mode the Pitch adapter verifies the live PID, executable path, start
time, and executable hash. Each arm seals only the CRC event-log bytes appended
by its federation into an immutable run-local artifact. Both adapters require
server event-log evidence, application verifier logs, and immediate callback
handling. Go results record both RTID and generated client binary hashes. If no
persistent Pitch PID is supplied, the adapter retains its isolated per-arm CRC
path.

`WarmupPairs` and `MeasuredPairs` can be lowered for orchestrator smoke tests,
but only the defaults satisfy this comparison protocol.

## Analysis

For every run and metric/dimension identity, `analysis.json` reports the sample
count, median, nearest-rank p95, and nearest-rank p99. For each statistic it
reports all Pitch and Go run values, the paired Go/Pitch ratio for every pair,
the median paired ratio, and a deterministic paired-bootstrap 95% confidence
interval (10,000 resamples by default).

Order effects are stratified by AB and BA. The reported
`go_second_over_go_first_ratio` is the median Go/Pitch ratio in AB pairs divided
by the median ratio in BA pairs, with an independently resampled 95% interval.
A value above one means Go's relative value was higher when it ran second.
Accounting totals are reported separately for each implementation.

The analyzer rejects before producing ratios when any of these differ or fail:

- workload field, count, fixed seed, FOM hash, or event-log mode;
- canonical semantic projection, projection hash, or pass status;
- expected/delivered fanout or any rejection, drop, duplicate, or invalid event;
- metric name, dimensions, unit, direction, or sample scope;
- result file hash, run identity, arm identity, pair completeness, or AB/BA balance;
- positive denominator needed for a paired ratio.

The JSON Schemas in `schemas/` document the wire shape. `check_contract.py`
adds cross-field checks JSON Schema cannot express, including the fixed workload,
projection digest, exact `2 * count` accounting, schedule/slot consistency, and
result-to-orchestrator workload equality.

## Focused validation

```powershell
python -m pytest verification/fair-comparison/tests
python -m ruff check verification/fair-comparison
```
