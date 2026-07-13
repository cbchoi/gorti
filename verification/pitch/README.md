# Pitch IEEE 1516e Java verifier

This directory is a self-contained, noninteractive verifier for Pitch pRTI Free 5.5.2's IEEE 1516-2010 Java API. It launches two independent JVM federates and exercises real RTI calls in all four service groups:

- **FM:** connect, create, join, two targeted two-federate synchronization points, resign, destroy, and disconnect. The subscriber registers `VERIFY_READY` only after its DM/TM setup; the publisher registers `VERIFY_DONE` after traffic completes.
- **DM:** publish and subscribe object attributes and an interaction class.
- **OM:** reserve a name, register/discover a named object, timestamped attribute updates/reflections, timestamped interaction sends/receives, and timestamped delete/removal observation.
- **TM:** enable regulation and constrained modes, issue time advance requests, and receive grants.

## Run

From this directory:

```powershell
.\Run.ps1 -Fom .\fom\PitchVerifier.xml -Seed 1516 -Count 100
```

`-Fom` is required: both comparison arms must receive the same caller-selected
module, canonically `verification\pitch\fom\PitchVerifier.xml`. `Run.ps1` finds
pRTI at `C:\Program Files\prti1516e` unless `PRTI1516E_HOME` or `-PRTIHome`
overrides it. Logical time remains `HLAfloat64Time` through
`createFederationExecution`, so no RTI-specific `<time>` block is needed in the
FOM. The runner starts a hidden, headless CRC when the selected address is not
already listening, builds the verifier, launches the subscriber and publisher
as two independent Java processes, validates their outputs, and stops the CRC
it started. Fair adapters choose a free address and require an owned CRC; a
pre-existing CRC is marked unattested because its command and redirected logs
cannot be established by the run.

Build without running:

```powershell
.\Build.ps1
```

## Determinism and logs

Payload `index` is the first 16 lowercase hexadecimal characters of SHA-256 over the UTF-8 text `${seed}:${channel}:${index}`. Channels are `attribute` and `interaction`.

`logs\canonical.ndjson` is merged in fixed publisher/subscriber order. Each semantic line has exactly:

```json
{"kind":"semantic","seq":0,"service":"FM","event":"phase","actor":"publisher","data":{"phase":"plan","status":"complete"}}
```

Semantic data never includes runtime handles or wall-clock timestamps. Every actor records `plan`, `do`, `review`, and `reflect` phases. TSO update, interaction, delete, reflection, receive, and removal events retain their logical time.

`logs\metrics.ndjson` contains measured values separately, with exactly `kind`, `service`, `metric`, `unit`, and `value` fields. Comparable definitions are:

- `call_latency.<operation>`: elapsed nanoseconds around one synchronous RTI API invocation.
- `completed_delivery_batch_latency`: subscriber nanoseconds from issuing one time advance request until that index's attribute reflection and interaction receive callbacks are both complete.
- `sustained_throughput`: subscriber attribute-plus-interaction deliveries per second, from the first batch request through completion of the final data batch. Object discovery and removal are excluded.

`logs\benchmark.json` is the analysis artifact using
`gorti.production-benchmark/v1`. It retains every integer-nanosecond sample for
the four fair-comparison operation names:

- `updateAttributeValues`
- `sendInteraction`
- `timeAdvanceRequest`
- `completed_delivery_batch_latency`

For every index, the publisher invokes update, then interaction, then TAR.
The subscriber's completed-delivery clock starts immediately before its TAR
call and ends at the later of the attribute and interaction callback timestamps.
Both RTI ambassadors use `HLA_IMMEDIATE`. Artifact metadata records the FOM
SHA-256, `two_process=true`,
`choreography=sequential_update_send_then_tar`,
`delivery_boundary=subscriber_pre_tar_to_both_callbacks`,
`callback=HLA_IMMEDIATE`, and
`logging_mode=off`. The verifier's compatibility logs remain file-backed and
are identified separately as `verifier_logging_mode=file`.

Each run creates an isolated `pitch-home-<pid>` below its output directory,
writes verified CRC and LRC settings there, and passes that directory as
`-Duser.home` to the CRC and both Java federates. The CRC runs from the same
isolated directory so an installation-local settings file cannot take
precedence. `CRC.eventLog.enable` is checked before launch: `off` requires an
empty event-log directory and `file` requires a captured event log. The
artifact records both settings-file SHA-256 digests plus effective tracing and
TCP/UDP bundling values. Delivery accounting proves
`expected_fanout = delivered + explicitly_rejected + dropped`; duplicate and
invalid callbacks are also counted explicitly. A successful run requires zero
rejections, drops, duplicates, and invalid callbacks.

Per-actor source logs, raw sample streams, and captured process output are
retained in the same directory. `run-evidence.json` seals the observed CRC,
publisher, and subscriber process identities; exact argument vectors; Java
executable hashes; CRC, Pitch API, and verifier JAR hashes; and byte counts plus
SHA-256 for every redirected stdout/stderr artifact. Result construction
re-hashes this evidence and every referenced file. Missing or stale evidence is
rejected by the fair adapters; the shared result contract remains compatible
with older smoke artifacts that omit claim-grade fields.

## Twenty-run analysis

From the repository root:

```powershell
$Fom = (Resolve-Path .\verification\pitch\fom\PitchVerifier.xml).Path
$Artifacts = 1..20 | ForEach-Object {
    $Run = ".\verification\out\pitch-fair\run-{0:D2}" -f $_
    .\verification\pitch\Run.ps1 -Fom $Fom -Seed 1516 -Count 100 -OutputDirectory $Run
    Join-Path $Run 'benchmark.json'
}
python -m verification.common.analyze_benchmarks @Artifacts `
    --output .\verification\out\pitch-fair\analysis.json --min-runs 20 --seed 1516
```

`FairRun.ps1` is the adapter for `verification\fair-comparison`. It accepts the
orchestrator's FOM, workload contract, run ID, logging mode, and output path,
then emits and validates `result.json` using
`gorti.fair-comparison/launcher-result-v1`. Attested results return the CRC
process and server logs in the shared provenance fields and retain the sealed
verifier process, runtime, and client-log descriptors in provenance
environment metadata for audit.

## Layout

- `fom\PitchVerifier.xml`: canonical shared, time-free IEEE 1516-2010 comparison FOM.
- `src\...\PitchVerifier.java`: both noninteractive federate roles and log writers.
- `Build.ps1`: Java 8-compatible compilation and runnable JAR packaging.
- `Run.ps1`: headless CRC and two-process orchestration, validation, and stable log merge.
- `FairRun.ps1`: fail-closed adapter for balanced Pitch/Go comparison sessions.
