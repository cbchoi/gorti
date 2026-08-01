# Verification

The release verification tree contains executable service checks and
cross-implementation semantic comparison tools. Generated results are written below
ignored `verification/out/` directories and are not source artifacts.

## Local gorti checks

- `gorti/` exercises FM, DM, OM, and TM through the public Python ambassador.
- `gorti-go/` records production-shaped Go SDK latency and delivery accounting.
- `gorti-go-fair/` runs the two-process Go arm used by the fair comparison.
- `common/` validates canonical logs, payloads, provenance, and statistics.

```powershell
python -m pytest verification/common verification/gorti
go test ./verification/gorti-go/... ./verification/gorti-go-fair/...
```

## Cross-implementation comparison

- `fair-comparison/` defines fixed FOM, seed, process, choreography,
  measurement, logging, warm-up, measured-run, and AB/BA requirements.
- `commercial-rti/` is a vendor-neutral IEEE 1516e Java adapter. Licensed API
  and runtime files are caller-supplied and remain outside the repository.
- `portico/` provides the reproducible Portico/gorti comparison harness.

The default comparison protocol uses two independent federate processes, five
warm-up pairs, thirty measured pairs, and alternating AB/BA order. A result is
accepted only after complete semantic projection, callback ordering, logical
grant, delivery accounting, FOM hash, and process provenance checks pass.

```powershell
python -m pytest verification/fair-comparison/tests verification/portico/tests
powershell -NoProfile -ExecutionPolicy Bypass `
  -File verification/portico/RunComparison.ps1
```

Performance values are reported separately from semantic acceptance. LocalLRC
queue admission and server-confirmed calls have different completion boundaries
and must not be compared as equivalent latency samples.

The 30-run publication performance experiment is maintained under
[`benchmark/`](../benchmark/). Verification remains the semantic gate; the
benchmark owns workload generation, environment pins, balanced execution,
structured measurements, statistics, and manuscript tables.
