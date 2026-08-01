# DEVStone-HLA Portico/gorti benchmark

This directory separates performance measurement from the service-semantic
checks in `verification/`. The requested name `DEVSOne` is resolved here to
DEVStone, the established synthetic benchmark for DEVS simulation engines. The
repository implements a DEVStone-HLA mapping. A deterministic graph traversal
materializes every atomic event-value delivery into a versioned binary plan;
each plan record becomes one receive-order object update and one receive-order
interaction between a producer and a consumer. The benchmark measures RTI
handling of that application workload; it is not a DEVS simulation-kernel
score.

A benchmark run is accepted only after the DEVStone-HLA semantic gate passes
for Portico and gorti. The benchmark then compares the two implementations with
the same model parameters, FOM bytes, payloads, random seed, callback policy,
choreography, and measurement boundaries. The tracked default workload is HI,
width 10, depth 10, 100 injected events, 40,600 expected interactions, and
40,600 expected object updates.

## Experiment design

The confirmed experiment contract is `environment/experiment.json`; the
LocalLRC experiment is `environment/experiment-local-lrc.json`. Portico 2.1.4
is implementation A and gorti 0.9.0 is implementation B. Each measured
schedule contains 30 matched pairs:

```text
pair 01: A then B
pair 02: B then A
...
pair 29: A then B
pair 30: B then A
```

This produces exactly 30 measured executions per implementation, with each
implementation running first in 15 pairs. Five executions per implementation
are used for warm-up and excluded from all reported samples. Pair `n` uses seed
`1516 + n - 1`; both implementations receive the same seed in that pair. A
failed execution invalidates the pair. Its replacement uses the original seed
and order and is not added as an extra observation.

Each measured execution starts fresh product and federate processes. Portico
uses two independent federate processes and its normal LRC topology. gorti uses
one RTI server process and two independent federate processes. Product startup,
federation creation, and teardown are outside the timed region. The timer starts
when the subscriber has armed its monotonic timer and both federates achieve
`VERIFY_START`. It stops when the subscriber accepts the final expected
callback. Terminal-state and delivery evidence are then checked outside the
timed region.

Completed batch time is the primary comparable endpoint. Callback throughput
is its derived rate. Caller-return latency is not a common endpoint because
Portico LRC return, gorti server confirmation, and gorti LocalLRC queue
admission provide different guarantees. The LocalLRC result therefore
publishes only completed batch time and callback throughput.

## Reproducible environment

`environment/prerequisites.json` records the supported host profiles and pinned
Go, Python, Java, Docker, and Ubuntu container versions. Stable source inputs
and known artifact hashes are tracked. Build-specific hashes and host facts are
resolved into the external manifest before measurement. The preflight verifies
these values before it starts a federate:

- DEVStone-HLA workload configuration and FOM;
- Portico `lib/portico.jar`;
- the gorti source commit and `rtid` binary; and
- the Ubuntu 24.04 container image digest.

The two environment JSON files, benchmark source, schemas, and launchers are
version-controlled. Generated manifests, measurements, analyses, and tables
remain outside the repository; their source commit is recorded in the manifest
instead of committing the generated files.

Machine-specific absolute paths are supplied through `PORTICO_HOME` and
`GORTI_BENCHMARK_OUTPUT`. They do not belong in the tracked configuration.
The runner builds the gorti binaries from the checked-out source. Both products
must run on the same physical host, in the same
container image, with the same host CPU set and total memory budget and no
concurrent measured runs.

## No execution logs

The benchmark does not derive measurements or semantic results from process
output or event logs. RTI and federate event logging is disabled. Each
participant writes one compact JSON summary containing counters, digests, and
timings; these summaries are validated and discarded before publication.
Standard output and standard error remain in memory only and are never written
to the result directory.

The output directory must be outside the Git worktree. Only these structured
artifacts may be retained:

- `manifest.json`: resolved versions, hashes, host facts, and experiment pins;
- `results.json`: the 60 completed measured executions;
- `analysis.json`: validated statistics and confidence intervals; and
- `comparison.tex`: the generated manuscript table.

Successful structured results are retained for 365 days. The analysis included
with a publication is retained with that publication record. Failed or partial
artifacts are removed immediately. Files such as `*.log`, `stdout*`, `stderr*`,
and event-log or transcript files are forbidden.

## Launcher contract

The Windows and Linux launchers have the same required inputs and produce the
same `benchmark/common/result-schema-v1.json` document. They must fail before
starting an experiment if a pin is unresolved, a hash differs, logging is
enabled, or the output path is inside the repository. The manifest records the
gorti worktree state and hashes the actual server and federate binaries used in
the measurement.

Windows invocation:

```powershell
$env:PORTICO_HOME = 'D:\workspace\gorti\gorti\.tools\portico-extracted\portico-2.1.4'
$env:GORTI_BENCHMARK_OUTPUT = 'D:\benchmark-results\devstone-hla-001'

powershell -NoProfile -ExecutionPolicy Bypass `
  -File benchmark\portico-gorti\run.ps1 `
  -PorticoHome $env:PORTICO_HOME `
  -Output $env:GORTI_BENCHMARK_OUTPUT
```

Linux invocation:

```bash
export PORTICO_HOME=/opt/portico-2.1.4
export GORTI_BENCHMARK_OUTPUT=/srv/benchmark-results/devstone-hla-001

bash benchmark/portico-gorti/run.sh \
  --portico-home "$PORTICO_HOME" \
  --output "$GORTI_BENCHMARK_OUTPUT"
```

After collection, validate and analyze the structured result without consulting
console output or logs:

```text
python benchmark/common/validate.py <output>/results.json
python benchmark/common/analyze.py <output>/results.json --output <output>/analysis.json
python benchmark/common/render_latex.py <output>/analysis.json --output <output>/comparison.tex
```
