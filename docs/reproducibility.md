# Reproducibility

## Fair-comparison contract

A reference IEEE 1516-2010 RTI and gorti can be compared only when both arms
use:

- identical FOM bytes and SHA-256;
- seed 1516 and the same generated payloads;
- two independent federate processes;
- the same sequential or parallel choreography;
- the same caller and completed-delivery measurement boundaries;
- five warmup pairs and thirty measured pairs for the publication profile;
- balanced, alternating AB/BA execution order;
- identical callback handling and server logging conditions; and
- zero rejected, dropped, duplicate, or invalid deliveries.

The comparison runner fails closed when provenance, semantics, accounting,
metric identity, or pair balance differs.

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
contract library. Treat its output as comparison evidence until server-process
identity, executable hashes, server logs, event-log provenance, FOM identity,
and process topology have also been independently verified. Only then may the
run be promoted to claim-grade evidence.

## Analysis

For every metric, `analysis.json` reports median, p95, p99, paired
gorti/reference RTI ratios, a deterministic paired-bootstrap 95% confidence
interval, and AB/BA order effects. Warmup data is validated but excluded from
statistics.

The fixed contract and schema are documented in
`verification/fair-comparison/README.md`. Preserve the complete output
directory when archiving evidence for a paper or release.

## Software environment

Record the gorti commit, reference RTI version, Go/Python/Java versions,
executable hashes, CPU, operating system, power scheme, FOM hash, and
server-log mode.
The generated manifest captures these values where the operating system makes
them available.

## DEVStone-HLA publication benchmark

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
