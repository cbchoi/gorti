# Native Go fair-comparison verifier

This arm mirrors the reference RTI Java verifier with two independent
processes named `publisher` and `subscriber`. It imports only the public
`rti/pkg/federate` SDK and connects to `rtid` over TCP.

## Run

From the repository root, supply the same FOM, seed, and count used for the
reference RTI:

```powershell
.\verification\gorti-go-fair\run.ps1 `
  -Fom .\verification\commercial-rti\fom\CommercialRtiVerifier.xml `
  -Seed 1516 `
  -Count 100 `
  -ServerEventLog off
```

`-Fom`, `-Seed`, and `-Count` are mandatory. The canonical comparison module is
`verification\commercial-rti\fom\CommercialRtiVerifier.xml`; both processes read and submit
those caller-selected bytes. The launcher builds the Go client once, launches
one subscriber process and one publisher process, and starts a local `rtid`
when the configured address is not already listening. Use `-NoStartRtid` to
require an existing server, or `-RtidPath` to select the server executable.
The fair arm uses the default HLA core profile; the launcher passes
`--audit-replay-plugin=none`, leaves `--log-dir` empty, and records
`server_event_log=off` in workload metadata.

The executable can also be launched directly. Each role requires the same
`--address`, `--federation`, `--fom`, `--seed`, `--count`, and `--output`
values. Coordination uses HLA synchronization points, not files: the subscriber
registers `VERIFY_READY` after DM/TM setup and the publisher registers
`VERIFY_DONE` after its final grant.

## Fair choreography

The publisher reserves `CommercialRtiVerifierEntity`, requires the matching successful
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
workflow. The HLA encodings match reference RTI's `HLAinteger32BE` and
`HLAASCIIstring` encoders.

## Receive-order transport modes

Use `-ReceiveOrder` for the pipelined LocalLRC arm. This is the default
receive-order transport and keeps the same two processes, FOM bytes, seed,
update-then-interaction order, payloads, and callback accounting. It records
local queue admission and one final cumulative flush separately.

Add `-Confirmed` when every receive-order update and interaction must wait for
the server result. `-LocalLRC` remains accepted as an explicit spelling of the
default. The two options are mutually exclusive because caller latency from
LocalLRC and confirmed mode has a different completion boundary.

The same selection can be loaded from a tracked JSON profile:

```powershell
.\verification\gorti-go-fair\run.ps1 `
  -Fom .\verification\commercial-rti\fom\CommercialRtiVerifier.xml `
  -Seed 1516 -Count 100 -ReceiveOrder `
  -TransportConfig .\verification\profiles\local-lrc.json
```

The direct executable accepts
`--transport-config=verification/profiles/local-lrc.json`. Explicit
`--local-lrc`, `--confirmed`, queue, ACK, batch, and callback options override
the corresponding profile values.

`-LocalLRCBatchSize` requests 32, 64, 128, or 256 operations per stream frame;
the default is 32 and the accepted summary records the negotiated value.
`-CallbackRepresentation handles` selects the same handle-map callback shape
used by the Portico verifier. Name callbacks remain the normal SDK default.
The launcher also exposes the bounded server outbox capacity, batch size, and
flush interval so a standalone verification run can reproduce the tracked
comparison contract.

## DEVStone workload plan

The executable accepts `--workload-plan=<path>` with `--receive-order=true`.
The binary plan begins with the eight bytes `DVSHLA1\0`, followed by a
big-endian `uint32` record count, a big-endian `uint64` seed, a 32-byte
topology digest, and fixed 32-byte records. Each record contains four
big-endian `uint32` values (`index`, event sequence, target ordinal, and
occurrence ordinal), an eight-byte attribute payload, and an eight-byte
interaction payload. Count, seed, length, canonical record order, and trailing
bytes are checked before either federate joins.

Plan payloads are sent as 16-character lowercase hexadecimal strings. Warm-up
payloads retain the normal deterministic encoding. In compact receive-order
runs, the publisher registers its object before `VERIFY_READY` and the
subscriber enters that synchronization only after validating discovery of the
named object. Warm-up traffic starts after the federation completes
`VERIFY_READY`. Plan runs retain the four labels `VERIFY_READY`,
`VERIFY_MEASURE`, `VERIFY_START`, and `VERIFY_DONE`; the subscriber arms its
batch timer after the START announcement and immediately before achieving the
point, while the publisher waits for federation synchronization before sending
measured traffic.

Add `--compact-summary=true` to disable all semantic, metric, and sample
NDJSON streams. This mode requires a plan and its externally validated
lowercase SHA-256 through `--workload-plan-sha256=<digest>`. The comparator
gives each role a separate output directory, which
must be empty when the verifier starts. The verifier accumulates call
durations, callback accounting, and separate attribute/interaction
arrival-order digests in memory. A successful review writes only
`publisher-summary.json` or `subscriber-summary.json`, using schema
`gorti.devstone.participant-summary/v1`; a failed run writes no accepted
summary. Compact mode supports both confirmed and LocalLRC receive-order
profiles; the summary identifies the selected transport and its completion
boundary.

## Artifacts

The output directory retains:

- `canonical.ndjson`: fixed publisher-then-subscriber semantic merge using the
  reference RTI actor and event vocabulary.
- `projected-canonical.ndjson`: deterministic four-record FM/DM/OM/TM summary
  with both sync labels, all payloads, and grants `1..count+1`.
- `metrics.ndjson`: raw reference-RTI-compatible metrics with `kind`, `service`,
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

The tests cover required CLI inputs, IEEE 1516e-compatible encodings, deterministic
payloads, strict callback and delivery accounting, fixed-name reservation,
synchronization participant validation, projected canonical output, and the
publisher's sequential API choreography.
