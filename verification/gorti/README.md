# gorti Python verifier

This verifier drives two real `Rti1516eAmbassador` federates against a gorti
`rtid`. The producer and consumer exercise federation management (FM),
declaration management (DM), object management (OM), and time management (TM).
No transport or ambassador methods are mocked in the live run.

Run from PowerShell:

```powershell
.\verification\gorti\run.ps1
```

The runner starts `bin\rtid.exe` when available and otherwise uses
`go run ./rti/cmd/rtid`. It waits for the federate port, runs the verifier, and
always stops the server. Useful options include `-Count`, `-Seed`,
`-OutputDirectory`, `-Python`, `-RtidPath`, `-OutboxBatchSize`,
`-TarTransport`, `-CallbackTransport`, and `-DisableEventLog`.
`-TarTransport threaded` is the default IEEE-facade path; `async` exercises
the opt-in TAR Future extension. `-CallbackTransport direct` bypasses the
ambassador's intermediate event queue; `queue` remains the default.

Each production-shaped run emits four separate artifacts:

- `canonical.ndjson`: deterministic semantic evidence;
- `metrics.ndjson`: backward-compatible aggregate metrics;
- `run-metadata.json`: source, binary/runtime hashes and versions, exact argv,
  logging arm, host, timestamps, and outcome;
- `benchmark.json`: raw integer-nanosecond samples, median/p95/p99 summaries,
  and complete delivered/rejected/dropped accounting.

Callbacks are dispatched by the ambassador's immediate background pump. The
verifier waits on a shared callback signal instead of invoking callbacks in a
polling loop, so the measurement does not repeatedly occupy the ambassador's
asyncio loop.

## Comparison contract

`canonical.ndjson` is the canonical cross-RTI transcript. Every row has
only these top-level fields:

```text
kind, seq, service, event, actor, data
```

The `data.phase` value is one of `plan`, `do`, `review`, or `reflect`.
Runtime handles, server addresses, process IDs, and wall-clock timestamps are
never semantic data. Callback observations are sorted by workload index before
they enter the review phase, so RTI-permitted receive-order variation does not
change the transcript.

Payloads are lowercase hex strings computed as the first 16 characters of
SHA-256 over UTF-8 `seed:channel:index`. The channel names are `attribute` and
`interaction`.

`metrics.ndjson` is intentionally non-canonical. Its rows use the
Pitch-compatible metric envelope:

```text
kind, service, metric, unit, value
```

It reports aggregate ambassador-call durations and OM callback latency without
mixing those measurements into semantic output.

Both federates enable regulation and constrained mode. `VERIFY_READY` and
`VERIFY_DONE` synchronization barriers bracket the workload. Each object update
and interaction is sent TSO at `index + 1` before both federates request that
time. The producer reserves `verifier-entity-1`, registers it, and deletes it
TSO after the workload; the consumer must observe discovery and removal.

The gorti strict FOM parser rejects the optional DIF `<time>` element. The FOM
therefore declares `TimeStamp` order but omits `<time>`; gorti uses its
`HLAfloat64Time` federation default, matching Pitch's explicit create argument.

Run the focused tests with:

```powershell
python -m pytest verification\gorti\test_verifier.py -q
```

After collecting at least 20 measured `benchmark.json` files, produce a
run-level analysis with deterministic bootstrap confidence intervals:

```powershell
python -m verification.common.analyze_benchmarks `
  <run-01\benchmark.json> <run-02\benchmark.json> `
  --output analysis.json --min-runs 20 --seed 1516
```

The analyzer rejects mixed binaries, mixed workloads, incomplete accounting,
and any drop in a no-drop workload.
