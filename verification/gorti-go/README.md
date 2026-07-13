# gorti Go production benchmark

This verifier drives two real `rti/pkg/federate` clients against a supplied
`rtid` executable. It uses the shared
`verification/gorti/federation.fom.xml` FOM and does not import RTI internals or
mock transport calls.

## Run

From the repository root:

```powershell
.\verification\gorti-go\run.ps1 -RtidPath .\bin\rtid.exe -Count 100
```

The supplied RTID must support `--outbox-batch-size` and
`--outbox-flush-interval`. Useful controls are `-Address`, `-Federation`,
`-Seed`, `-Count`, `-TimeoutSeconds`, `-OutboxBatchSize`,
`-OutboxFlushInterval`, and `-OutputDirectory`.

The launcher builds the benchmark client, starts exactly the RTID binary named
by `-RtidPath`, waits for its federate port, runs the workload, and stops the
server in a `finally` block. `benchmark.json` records the SHA-256 of that RTID,
its exact version output and argument list, the source state, Go runtime, FOM
digest, and full workload configuration.

## Measured path

All sequence and deterministic payload bytes are HLA-encoded before the hot
loop. Measured boundaries use Windows `QueryPerformanceCounter`; non-Windows
builds use Go's monotonic clock. Call timings, the post-call batch boundary,
and event-pump receive timestamps always share that counter domain, and raw
samples are converted to integer nanoseconds only when recorded. For each
logical time `index + 1`, the runner:

1. starts `updateAttributeValues` and `sendInteraction` concurrently and
   records each unary duration;
2. captures the delivery-batch boundary after both calls finish;
3. starts producer and consumer `timeAdvanceRequest` calls concurrently and
   records both unary durations;
4. consumes events through one sole-reader pump per federate;
5. validates the consumer reflection and interaction class, sequence, payload,
   and timestamp, plus exact producer and consumer grants;
6. records reflection, interaction, completed-batch, and send-to-batch delivery
   latencies.

Raw samples use integer nanoseconds and the operation names and dimensions used
by `gorti.production-benchmark/v1`:

```text
updateAttributeValues
sendInteraction
timeAdvanceRequest
reflectAttributeValues.latency
receiveInteraction.latency
completed_delivery_batch_latency
send_to_delivery_batch_latency
```

The artifact includes deterministic median, nearest-rank p95, and nearest-rank
p99 summaries. Delivery accounting is always complete with
`expected_fanout = 2 * count`; a failed run still writes the valid artifact
when setup progressed far enough to construct the benchmark.

Run-location details such as the endpoint, absolute FOM path, RTID path, and
exact server arguments are retained in environment/provenance metadata. They
are intentionally excluded from the workload fingerprint, which contains the
semantic inputs (including FOM digest, seed, count, logical-time settings, and
payload encoding). This allows repeated runs on different ports or output
directories to be analyzed as the same workload without losing traceability.

## Tests

```powershell
go test ./verification/gorti-go
```

The focused tests cover grouping and percentile summaries, artifact accounting
validation, deterministic HLA pre-encoding, strict payload/timestamp event
validation, monotonicity, and sub-100-microsecond counter resolution.
