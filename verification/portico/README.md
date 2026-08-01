# Portico comparison

The Portico/gorti comparison compiles the vendor-neutral IEEE 1516e Java
verifier against Portico and compares it with the native Go verifier.

The measured workload uses receive-order Object Management traffic. A strict
timestamp-order test is retained as functional evidence, but its performance
is not compared until both implementations satisfy the same
callback-before-grant contract.

## Prerequisites

- Portico 2.1.4 Linux distribution extracted inside the repository or passed with `-PorticoHome`
- Docker Desktop with the `ubuntu:24.04` image
- Go and a JDK on `PATH`
- Python 3.9 or newer

The harness derives a small classpath override from Portico's own JGroups configuration. It changes
only UDP/PING to TCP/TCPPING so that Docker Desktop can provide deterministic peer discovery; the
remaining ordering and reliability protocols are preserved.

## Run

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File verification/portico/RunComparison.ps1
```

Select a tracked transport profile with:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass `
  -File verification/portico/RunComparison.ps1 `
  -TransportConfig verification/profiles/local-lrc.json
```

Use `verification/profiles/confirmed.json` for the server-confirmed profile.
`-Config` is accepted as a short alias. An explicit `-GortiTransport` value
overrides the selected JSON profile.

Defaults are 5 warm-up pairs and 30 measured pairs with alternating AB/BA
order. gorti uses LocalLRC by default; pass
`-GortiTransport confirmed` to run the stronger server-completion profile.
The tracked DEVStone-HLA workload defines the callback count; select a
different checked workload with `-Workload`, not `-Count`. Both products use
the same FOM and plan bytes, paired seed, sequential update/interaction
choreography, immediate callbacks, controlled runtime profiles, Ubuntu image,
and two independent federate containers. gorti additionally runs its required
RTI server container; Portico coordinates its peer LRCs through JGroups.

The harness distinguishes diagnostic logging from gorti's optional
audit/replay module. Portico runs with diagnostic logging and its JGroups
auditor off. gorti runs with diagnostics restricted to errors and uses the
default `gorti-hla-core` profile. `-GortiAuditReplayPlugin event-journal`
selects the separately labeled `gorti-audit-replay` profile. Results from the
core and plugin-enabled profiles must not be pooled.

## Audit plugin ablation

Use the gorti-only runner to measure the event-journal cost without repeatedly
starting Portico:

```powershell
python verification/portico/compare_gorti_journal_profiles.py `
  --repo . `
  --fom verification/commercial-rti/fom/CommercialRtiVerifier.xml `
  --workload benchmark/devstone/workload/workload.json `
  --output verification/out/gorti-audit-plugin-ablation
```

```bash
python verification/portico/compare_gorti_journal_profiles.py \
  --repo . \
  --fom verification/commercial-rti/fom/CommercialRtiVerifier.xml \
  --workload benchmark/devstone/workload/workload.json \
  --output verification/out/gorti-audit-plugin-ablation
```

The defaults are five warm-up and 30 measured pairs. Each pair reuses the same
binary, FOM, plan, and seed, alternates profile order, and requires exact
callback and terminal-state evidence equality before accepting the timing.
Only `comparison.json` is retained.

The generated `comparison.json` is stored below ignored `verification/out/`.
Process transcripts, semantic logs, and sample logs are not written. Compact
participant summaries and binary plans are validated in transient storage and
discarded. Each pair compares the complete ordered attribute and interaction
payload projections before accepting any performance sample.
