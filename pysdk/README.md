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

## Logical time selection

`FederationSpec(..., logical_time_implementation_name="HLAfloat64Time")`
selects Float64 time; use `"HLAinteger64Time"` for exact integer time. The asynchronous
`create_federation(spec, logical_time_implementation_name=...)` keyword overrides
the spec without modifying it; `createFederationExecution` accepts the same
keyword. Empty selects Float64. Create continues to return `None`.

Inspect a joined federate's `logical_time_implementation_name`, or call the
ambassador's SDK accessor `getLogicalTimeImplementationName()`. Federation-list
rows expose `logical_time_implementation_name` without changing the tuple return
shape. List metadata reports the server's name even when this SDK cannot join it.

Absent metadata on legacy servers means `HLAfloat64Time` only. The SDK also accepts
`HLAinteger64Time` and sends its times and lookahead as exact signed int64 wire
values. Use Python `int` in that federation, including callback/query values above
`2^53`; floats, booleans and out-of-range integers fail before time-service RPCs.
No rounded legacy double is written alongside an Integer64 value. Custom factory
names are rejected. The single-server gRPC backend enables both factories by
default; `--enable-integer64=false` disables explicit Integer64 selection.
Integer64 uses exact unary service calls rather than Float64 optimized request
streams. Legacy Float64 `+Inf` final times use the old wire field alone.

## Ownership callback receipts

The standard ambassador negotiates mandatory terminal invocation receipts before
starting its callback pump. Acquisition, unavailability and cancellation callbacks
are acknowledged when the application handler is entered, including a throwing
handler, not when queued. Retry attempts with the same callback ID do not invoke
the handler again. Object-knowledge epochs discard stale buffered discovery and
ownership assumptions after local deletion, without dropping terminal outcomes.

Only a server returning `UNIMPLEMENTED` falls back to delivery-only compatibility;
test transports without a federation generation also use that legacy profile.
The ambassador remains both the service facade and the default overridable callback
target, so `confirmAttributeOwnershipAcquisitionCancellation` is a callback slot,
not an RTI service. Raw async consumers must call
`enable_ownership_callback_receipts()` and then use `dispatch_event(event, handler)`,
or report an actual manual invocation with `report_ownership_callback_invocation`.
Mandatory receipts do not depend on optional MOM callback accounting.

Failed local join validation uses the returned session capability for resignation.
If cleanup also fails, both errors remain in the join failure and the connection
retains the exact cleanup identity. `close()` (including
`recover_commit_unknown()`) retries that resignation before closing the channel.
A failed retry leaves the transport attached for another cleanup attempt; it
does not expose the rejected federate for normal services.

A successful create response that reports an unsupported type instead raises
`FederationCreationResponseError`, a subclass of `RTIinternalError`. The message
states that the server already created the execution, and the exception retains
`federation_name`, `federation_generation`, `effective_seed`, and the reported
`logical_time_implementation_name`. This is not a clean factory rejection.
Nothing is automatically destroyed; after inspecting the evidence, callers can
explicitly destroy the execution using the connection's retained generation.

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

From the repository root, install the development tools and generate the
protocol bindings:

```bash
cd pysdk
python -m pip install -e '.[dev]'
python -m rti1516e._proto
python -m ruff check .
python -m mypy --strict rti1516e pyjevsim_bridge
cd ..
python scripts/validate_sdks.py python
```

These commands also work in PowerShell. Type checking uses the active Python
version so that dependency stubs match the installed interpreter.
The code-generation step refreshes the ignored local bindings from `proto/`.
The validation runner starts a local RTI server and runs the inventoried HLA
tests. Use `python -m pytest <test-path>` for a targeted regression. The optional
`pyjevsim` extra is required by examples that use the upstream simulator.

The [IDD](../engineering/specifications/current/IDD.md) describes the language
interfaces; the [STD](../engineering/specifications/current/STD.md) defines test
selection and acceptance. [Verification](../docs/verification.md) summarizes
the recorded HLA service runs.
