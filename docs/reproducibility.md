# Reproducibility

## Shared comparison conditions

A comparison records the same workload and the selected measurement boundary
for both RTIs:

- identical FOM bytes and SHA-256;
- seed 1516 and the same generated payloads;
- two independent federate processes;
- the same sequential or parallel choreography;
- the same caller and completed-delivery measurement boundaries;
- the warm-up and measured-run counts specified by the selected design;
- balanced AB/BA pairs or ABBA/BAAB crossover blocks;
- matched callback checks and documented logging settings; and
- zero rejected, dropped, duplicate, or invalid deliveries.

The runner rejects mismatched semantics, delivery accounting, metric identity,
or execution order. Retain the manifest with each result.

## Choose an experiment

| Runner | Design | Primary measurement |
|---|---|---|
| `benchmark/portico-gorti/` | Five warm-up pairs, 30 measured AB/BA pairs | DEVStone-HLA receiver completion |
| `benchmark/callback-reporting/compare_portico.py` | Paired or crossover; October runs use five warm-up and 20 measured ABBA/BAAB blocks | Receiver completion with callback-report accounting |
| `verification/fair-comparison/` | Service-level reference-RTI comparison | Per-service latency and completed delivery |

Each crossover block contains two trials per RTI, so 20 measured blocks yield
40 measurements per RTI. Resample whole seed blocks within order strata when
computing its confidence intervals. Do not analyze these as independent pairs.

## Generate comparison evidence

Create a machine-local launcher configuration from the tracked example, then
run:

```powershell
Copy-Item .\verification\fair-comparison\launchers.example.json `
  .\verification\fair-comparison\launchers.local.json

.\verification\fair-comparison\run-persistent-comparison.ps1 `
  -ConfigPath .\verification\fair-comparison\launchers.local.json `
  -OutputDirectory .\verification\out\fair-comparison\claim `
  -Count 100 -ServerEventLog file
```

`launchers.local.json` is intentionally ignored because it contains local
installation paths. Raw results under `verification/out` are also ignored.

The current wrapper validates semantics, accounting, pair balance, and metric
identity, but it does not by itself enable every strict provenance check in the
contract library. Before publishing that runner's result, also check server
process identity, executable hashes, the selected logging profile, FOM identity,
and process topology against its manifest.

## Analysis

For every metric, `analysis.json` reports median, p95, p99, paired
gorti/reference RTI ratios, a deterministic paired-bootstrap 95% confidence
interval, and AB/BA order effects. Warmup data is validated but excluded from
statistics.

The service-level contract and schema are documented in
`verification/fair-comparison/README.md`. Preserve the complete output
directory when archiving evidence for a paper or release.

## Software environment

Record the gorti commit, reference RTI version, Go/Python/Java versions,
executable hashes, CPU, operating system, power scheme, FOM hash, and
server-log mode.
The generated manifest captures these values where the operating system makes
them available.

## Paired DEVStone-HLA benchmark

The tracked experiment under `benchmark/` is separate from the retained
20-pair verification comparison. It uses a DEVStone-derived HI workload with
two independent federate processes, five untimed warm-up executions, 30
measured executions per implementation, unique paired seeds, and a 15:15
AB/BA order balance.

Every accepted run carries callback accounting, synchronization outcomes, the
FOM and workload hashes, an ordered callback digest, and a terminal-state
digest. Process output and RTI event logs are neither analysis inputs nor
retained artifacts. See the [benchmark guide](benchmark.md) for the exact
Windows and Linux commands.

## Crossover callback-reporting benchmark

`benchmark/callback-reporting/compare_portico.py --design crossover` runs the
ABBA/BAAB design. The October campaigns used `--warmup 5 --runs 20`,
`--producer-mode local-lrc --batch-size 32 --gorti-profile async`, and 100
external events. This runner keeps structured trial evidence outside Git.
Its artifact policy differs from the paired container runner above.

Use the [callback-reporting instructions](https://github.com/cbchoi/gorti/blob/main/benchmark/callback-reporting/README.md)
for binaries and required arguments. The [performance page](performance.md)
describes receiver timing and the final report drain.
