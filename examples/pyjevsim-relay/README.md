# pyjevsim relay example — Generator → Buffer → Processor

Three-federate DEVS pipeline running on the gorti `pyjevsim_bridge`:

```text
Generator ──GenToBuffer──▶ Buffer ──BufferToProc──▶ Processor
```

This is the next step up from the two-federate `examples/pyjevsim/`
producer/consumer demo. It exercises **the same federate as both
subscriber and publisher** — the buffer — which is the case where
DEVS↔HLA semantics actually have to compose.

## Federates

### Generator (DEVS Source)

Emits one `GenToBuffer` interaction per logical tick with a monotonic
sequence number, until it has emitted `stop_after` messages. After
that it stays idle (`output_handler` returns `{}`) so the runner's
drain phase can flush whatever's still queued in the buffer.

### Buffer (DEVS bounded-FIFO Queue)

Receives `GenToBuffer` on `in_msg`, holds messages in a
fixed-capacity FIFO, and emits the head of the queue as
`BufferToProc` on `out_msg` every `service_period` ticks. New
arrivals while the queue is at capacity are **dropped** silently and
the dropped seq is recorded in `Buffer.dropped` for verification.

### Processor (DEVS Sink)

Pure subscriber. Each `BufferToProc` arrival decodes the seq number
and appends to `received`. No output, no internal scheduling.

## Wire shape

Two interaction classes in `relay-fom.xml`. Two distinct classes
(rather than one with two pub/sub pairs) is intentional — the two
pipeline edges are visually distinct in the event log, which matters
when you start studying interleaving + drops in the wire log.

```xml
<interactionClass>
  <name>GenToBuffer</name>
  <parameter><name>seq</name><dataType>HLAinteger32BE</dataType></parameter>
</interactionClass>
<interactionClass>
  <name>BufferToProc</name>
  <parameter><name>seq</name><dataType>HLAinteger32BE</dataType></parameter>
</interactionClass>
```

## Run it

```bash
# From the repo root
python3 examples/pyjevsim-relay/runner.py

# Knobs (defaults shown)
python3 examples/pyjevsim-relay/runner.py \
    --gen-messages 50 \
    --capacity 5 \
    --service-period 2 \
    --drain-ticks 30 \
    --verbose
```

Default-config output:

```text
runner: published=50  forwarded=29  dropped=21  received=29  residual=0  verify=ok
```

The verification asserts:

1. **Accounting closes**: `published == forwarded ∪ dropped ∪ residual`
   (no seq is silently lost between the federates).
2. **No double-counting**: `forwarded ∩ dropped == ∅`.
3. **Pipeline integrity**: `processor.received == buffer.forwarded`
   (every seq the buffer released arrived at the processor).

## Why two `step_once` calls per buffer tick?

The bridge's cycle semantics (`pysdk/pyjevsim_bridge/time_advance.py`
§4.4: *"external arrived earlier than ta → no internal cycle this
round"*) means a federate that has pending external arrivals drains
them and returns **without** running `output_handler`. That works
fine for pure-subscriber sinks, but a federate that is both
subscriber AND publisher (the buffer) needs both halves:

- 1st `step_once`: drain externals → `external_transition` → return
- 2nd `step_once`: no pending externals → `output_handler` →
  `internal_transition`

The runner does this explicitly inside its outer tick loop. If you
build your own queue-shaped federate, follow the same pattern.

## Tuning the drop rate

The default `service_period=2` halves the buffer's emission rate
relative to the generator's arrival rate, so the queue saturates
around tick 8 and starts dropping. Knobs:

| Knob | Effect |
|---|---|
| `--service-period 1` | line-rate emit; no drops in steady state |
| `--service-period 2` | half-rate emit; ~40% drop rate at default capacity |
| `--service-period 4` | quarter-rate emit; ~75% drop rate |
| `--capacity 20` | larger queue, fewer drops, more residual at end of producing |
| `--gen-messages 200` | longer producing phase, same drop ratio |

## What this demonstrates

- **DEVS↔HLA composition** with a federate that both publishes and
  subscribes (the queue).
- **Drop-on-overflow** as a wire-level fault model that researchers
  studying QoS / fairness algorithms can vary.
- **End-to-end accounting** — the verification step is the hook to
  build alternative-strategy comparison runs (swap an
  `ownership.NegotiationStrategy` or a `time.LBTSStrategy` per the
  [research platform how-to](../../docs/research-platform-howto.md)
  and re-run; the same accounting checks apply).

## What's deferred

- **Cross-process variant** — this example is in-process via
  `InProcessTransport`. A subprocess-spawned `rtid` + 3 separate
  Python processes is the same wiring with `grpc://` URLs and a real
  rtid binary; pattern is in `pysdk/tests/spec/m5/test_spec_m5_cross_language.py`.
- **Service-time pacing on the processor** — currently instant
  consume. Adding a service period to the processor turns it into a
  classic G-Q-S server; the same `Buffer` model code handles that.
- **Real `pyjevsim` `StructuralModel`** — the federate models in
  this directory implement the bridge's `CoupledModelProtocol` via
  duck typing (matching `examples/pyjevsim/`); a structural pyjevsim
  model adapter is `pysdk/pyjevsim_bridge/_real_pyjevsim_adapter.py`
  but isn't exercised here.
