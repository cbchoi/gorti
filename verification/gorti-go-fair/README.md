# Native Go fair-comparison verifier

This arm mirrors the Pitch IEEE 1516-2010 Java verifier with two independent
OS processes named `publisher` and `subscriber`. It imports only the public
`rti/pkg/federate` SDK and reaches `rtid` over its TCP address.

## Run

From the repository root, supply the same FOM, seed, and count used for Pitch:

```powershell
.\verification\gorti-go-fair\run.ps1 `
  -Fom .\verification\pitch\fom\PitchVerifier.xml `
  -Seed 1516 `
  -Count 100 `
  -ServerEventLog off
```

`-Fom`, `-Seed`, and `-Count` are mandatory. The canonical comparison module is
`verification\pitch\fom\PitchVerifier.xml`; both processes read and submit
those caller-selected bytes. The launcher builds the Go client once, launches
one subscriber process and one publisher process, and starts a local `rtid`
when the configured address is not already listening. Use `-NoStartRtid` to
require an existing server, or `-RtidPath` to select the server executable.
Server event logging is fixed to `off` for the fair arm; the launcher passes
`--log-dir=` and records `server_event_log=off` in workload metadata.

The executable can also be launched directly. Each role requires the same
`--address`, `--federation`, `--fom`, `--seed`, `--count`, and `--output`
values. Coordination uses HLA synchronization points, not files: the subscriber
registers `VERIFY_READY` after DM/TM setup and the publisher registers
`VERIFY_DONE` after its final grant.

## Fair choreography

The publisher reserves `PitchVerifierEntity`, requires the matching successful
reservation callback, and registers the named object. For every index it then
invokes, sequentially:

1. `UpdateAttributeValues` at logical time `index + 1`.
2. `SendInteraction` at the same logical time.
3. `TimeAdvanceRequest` for that logical time and waits for its exact grant.

The subscriber arms the index in its callback validator, starts the delivery
batch clock immediately before its `TimeAdvanceRequest`, and stops the clock at
the later of the matching attribute and interaction callback timestamps. Both
federates request and verify grants `1..count+1`; the final time carries the
timestamped object deletion/removal.

Each process has exactly one goroutine reading `Federate.Events()`. That
immediate pump validates synchronization labels and participants, reservation,
class and object identity, exact field sets, HLA sequence and payload bytes,
logical timestamps, duplicates, removal, and grants before notifying the role
workflow. The HLA encodings match Pitch's `HLAinteger32BE` and
`HLAASCIIstring` encoders.

## Artifacts

The output directory retains:

- `canonical.ndjson`: fixed publisher-then-subscriber semantic merge using the
  Pitch actor and event vocabulary.
- `projected-canonical.ndjson`: deterministic four-record FM/DM/OM/TM summary
  with both sync labels, all payloads, and grants `1..count+1`.
- `metrics.ndjson`: raw Pitch-envelope metrics with `kind`, `service`,
  `metric`, `unit`, and `value`.
- `samples.ndjson`: the exact five-samples-per-iteration fair stream:
  publisher update/send/TAR and subscriber TAR/completed-delivery batch.
- `benchmark.json`: `gorti.production-benchmark/v1` artifact with raw samples,
  summaries, comparison metadata, and complete delivery accounting.
- Per-role semantic, metric, sample, stdout, and stderr files, plus server logs
  when the launcher starts `rtid`.

A passing run requires `expected_fanout = delivered + explicitly_rejected +
dropped`, every expected delivery accepted, no unexpected callbacks, both HLA
synchronizations, and contiguous passing semantic logs for both actors.

## Tests

```powershell
$env:GOCACHE = (Join-Path $PWD '.tools\go-cache')
go test ./verification/gorti-go-fair
```

The tests cover required CLI inputs, Pitch-compatible encodings, deterministic
payloads, strict callback and delivery accounting, fixed-name reservation,
synchronization participant validation, projected canonical output, and the
publisher's sequential API choreography.
