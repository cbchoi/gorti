# rti1516e — Python SDK for the gorti RTI

Python federate SDK for the [gorti](https://github.com/cbchoi/gorti) Run-Time
Infrastructure (IEEE 1516-2010 / HLA Evolved). Plus a `pyjevsim_bridge` adapter
that lets pyjevsim coupled models join HLA federations.

This package is M4 of the gorti project. See `docs/agent-c-pysdk.md` for the
SDK contract and `docs/M4_DISPATCH_PLAN.md` for the wave model driving the
implementation.

## Layout

- `rti1516e/encoding/` — IEEE 1516.2 encoding rules (byte-identical to Go side)
- `rti1516e/fom/` — FOM XML parser + dataclass model
- `rti1516e/connection.py`, `declaration.py`, `object.py`, `interaction.py`,
  `events.py`, `errors.py` — Layer 1 idiomatic asyncio API
- `rti1516e/standard.py` — Layer 2 1516-2010-shaped ambassador adapter
- `rti1516e/_generated/` — generated gRPC stubs (gitignored; `make py-codegen`)
- `pyjevsim_bridge/` — DEVS↔HLA bridge (port mapping, time advance, select)
- `tests/spec/m4/` — orchestrator-frozen specification tests
- `tests/test_*.py` — agent-owned unit tests

## Asynchronous OM extension

`Rti1516eAmbassador` keeps its synchronous IEEE-shaped methods unchanged and
also exposes an opt-in gorti extension for pipelined object management:

```python
amb.setAsyncOperationLimit(16)
update = amb.updateAttributeValuesAsync(object_handle, attributes, timestamp=t)
interaction = amb.sendInteractionAsync(interaction_class, parameters, timestamp=t)
amb.flushAsyncOperations()

update.result()
interaction.result()
amb.timeAdvanceRequest(t)
```

Paired federates can opt into non-blocking TAR transport admission:

```python
producer_tar = producer.timeAdvanceRequestAsync(t)
consumer_tar = consumer.timeAdvanceRequestAsync(t)
producer_tar.result()
consumer_tar.result()
producer.flushAsyncOperations()
consumer.flushAsyncOperations()
```

This only overlaps transport admission. The normal `timeAdvanceGrant` callback
still determines when logical-time advance completes. Repeated A/B measurements
showed lower TAR admission latency but no material end-to-end grant improvement,
so the synchronous IEEE-shaped method remains the default.

The limit bounds accepted but unfinished RPCs. `flushAsyncOperations()` is a
submission barrier: it waits for every operation in order, observes all
results, and raises the first translated exception. Canceling the returned
observer Future does not cancel a mutating RPC whose commit status may be
unknown.

All time-advance primitives and synchronous OM lifecycle operations flush the
current generation before issuing their dependent request. Separate unary
RPCs may still complete in a different order, so use a limit of `1` for
repeated updates to the same object, update/delete dependencies, or
byte-deterministic event-log ordering.

For immediate-callback workloads, call `setDirectCallbackDelivery(True)`
before joining to bypass the intermediate asyncio queue and pump task. Stream
order and callback enable/disable semantics are preserved; unsupported
transports retain queued delivery. Because a slow callback then directly
backpressures stream reads, queued delivery remains the default.

## Pre-dispatch state

This is the orchestrator-frozen pre-work. All public-API stubs raise
`NotImplementedError`. Spec tests fail RED until Agent C wires the M4 waves.
