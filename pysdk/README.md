# gorti Python SDK

The Python SDK contains an asynchronous federate API, an IEEE-shaped ambassador
adapter, HLA encodings, typed handles and collections, and a pyjevsim bridge.

## Layout

- `rti1516e/connection.py` and service modules: idiomatic asynchronous API
- `rti1516e/standard.py`: IEEE-shaped `Rti1516eAmbassador`
- `rti1516e/encoding/`: HLA basic and composite encodings
- `rti1516e/fom/`: FOM XML parser and data model
- `rti1516e/_generated/`: generated gRPC bindings
- `pyjevsim_bridge/`: DEVS-to-HLA model adapter
- `tests/`: specification, unit, integration, and regression tests

## Asynchronous object management extension

The ambassador keeps synchronous IEEE-shaped methods and provides an optional
bounded, pipelined extension:

```python
amb.setAsyncOperationLimit(16)
update = amb.updateAttributeValuesAsync(object_handle, attributes, timestamp=t)
interaction = amb.sendInteractionAsync(interaction_class, parameters, timestamp=t)
amb.flushAsyncOperations()

update.result()
interaction.result()
amb.timeAdvanceRequest(t)
```

`flushAsyncOperations()` waits for accepted operations in order and raises the
first translated failure. Time-advance and synchronous lifecycle calls flush
earlier dependent work before crossing their ordering boundary. Use an
in-flight limit of `1` when repeated operations on the same object require
strict submission order.

Paired federates can submit TAR transport calls concurrently, but logical-time
completion is still determined by `timeAdvanceGrant` callbacks.

## Direct callback delivery

Immediate-callback workloads can call `setDirectCallbackDelivery(True)` before
joining. This bypasses the intermediate asyncio queue while preserving stream
order and callback enable/disable behavior. A slow callback directly
backpressures stream reads, so queued delivery remains the default.

## Verification

```bash
ruff check pysdk
cd pysdk
mypy --strict rti1516e pyjevsim_bridge
pytest --maxfail=1
```

The current language-profile contract is summarized in
`engineering/specifications/current/IDD.md` and the test acceptance rules are
in `engineering/specifications/current/STD.md`.
