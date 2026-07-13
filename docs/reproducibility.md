# Reproducibility

## Fair-comparison contract

A Pitch pRTI versus gorti performance claim is valid only when both arms use:

- identical FOM bytes and SHA-256;
- seed 1516 and the same generated payloads;
- two independent federate processes;
- the same sequential or parallel choreography;
- the same caller and completed-delivery measurement boundaries;
- five warmup pairs and twenty measured pairs;
- balanced, alternating AB/BA execution order;
- identical callback handling and server logging conditions; and
- zero rejected, dropped, duplicate, or invalid deliveries.

The orchestrator fails closed when provenance, semantics, accounting, metric
identity, or pair balance differs.

## Claim-grade run

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

## Analysis

For every metric, `analysis.json` reports median, p95, p99, paired gorti/Pitch
ratios, a deterministic paired-bootstrap 95% confidence interval, and AB/BA
order effects. Warmup data is validated but excluded from statistics.

The fixed contract and schema are documented in
`verification/fair-comparison/README.md`. Preserve the complete output
directory when archiving evidence for a paper or release.

## Software environment

Record the gorti commit, Pitch version, Go/Python/Java versions, executable
hashes, CPU, operating system, power scheme, FOM hash, and server-log mode.
The generated manifest captures these values where the operating system makes
them available.
