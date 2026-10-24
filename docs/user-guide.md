# User guide

## Runtime model

`rtid` owns the authoritative state for each running federation and exposes HLA
service groups over gRPC. A **federation** is one coordinated simulation run. A
**federate** is an independently running participant. A **FOM module** defines
the object classes, attributes, interactions, parameters, and data types shared
by those participants.

The supported deployment is one `rtid` process with multiple local or
remote federates. Use a unique federation name for independent runs. Cluster
and failover flags exist for experiments, but multi-node operation is not the
supported deployment baseline.

Each federate opens a connection, joins by a unique name, issues service calls,
and drains callbacks from its event stream. Leaving the callback stream unread
can apply backpressure and prevent the application from making progress.

## Federation lifecycle

A normal lifecycle is:

1. Start `rtid` and wait until its listeners are ready.
2. Create a federation with an ordered set of FOM modules, or join the existing
   federation using the same model. The Go SDK's `JoinFederation` helper
   performs create-or-join.
3. Join every federate with a unique name and start consuming callbacks.
4. Register synchronization points when startup barriers are required.
5. Publish and subscribe before registering objects, updating attributes, or
   sending interactions.
6. Stop new sends, complete required time advances, and drain expected
   callbacks during application shutdown.
7. Resign every federate, then destroy the empty federation when the
   application requires explicit cleanup.
8. Close SDK connections, then stop `rtid` with `Ctrl-C` or `SIGTERM`.

Closing a Go SDK `Connection` does not automatically resign its federates, and
cancelling a request context is not a substitute for `Resign`. Give cleanup a
fresh, bounded context if the main operation context may already have expired.

FOM module order and bytes are part of the run identity. Keep FOM files under
version control and record their hashes, including modules added by joining
participants, for reproducible experiments.

Additional FOM modules supplied at join may introduce new classes or subclasses.
They must not add attributes or parameters to an already complete class; use a
new subclass for such extensions. Compatible repeated definitions may differ in
descriptive text, but operational conflicts still reject the join without
changing the federation's model. Existing handles remain stable after a
successful extension.

### Selecting a MIM

The server uses its embedded MOM and Initialization Module (MIM) by default. To select
a file explicitly, pass it separately from the FOM module list:

| SDK | Input |
|---|---|
| Go | `federate.LoadMIMModule(path)`, then `FederationSpec.MIMModule` |
| Python async | `FederationSpec(..., mim_module=path)` |
| Python ambassador | `createFederationExecution(name, fom_modules, mim_module=path)` |
| Java ambassador | The `createFederationExecution` overload with a separate MIM URL |

Only explicitly selected file contents are uploaded; the server does not open
that path on its own host. Keep the same designator and bytes when supplying a
join expectation. The server checks it before extending the FOM or adding the
federate. An older server rejects this request rather than silently dropping it.

The current selection profile accepts the embedded MIM's structure and
definitions with differences in formatting, comments and existing `semantics`
descriptions. It does not accept arbitrary replacement definitions. Omitting
the selection preserves the existing embedded-MIM behavior.

For the federation's next-save label, gorti follows IEEE 1516.1-2010 Clause 11.6
table precedence: Table 6 names the attribute `HLAAnnexSaveName`, while the
distributed Annex G XML uses `HLAnextSaveName`. Effective FDDs, MOM metric keys
and attribute-name responses use `HLAAnnexSaveName`. Support-service lookups
accept the old spelling as an alias for the same handle; no second attribute
is added. Use the table spelling in new federates and name-keyed SDK calls.
Both spellings are accepted in selected MIMs, and old FOM snapshots retain
their handles on restore. Original MIM bytes and source digests are preserved,
including MIM source reports; those reports are not rewritten effective FDDs.

## Object and interaction management

Object attributes represent persistent distributed state. Register an object
instance, update its owned attributes, and delete the instance at the end of
its lifetime. Interactions represent transient events and do not create
persistent instances.

Receive-order messages are deliverable immediately. In the Go SDK, standard
receive-order updates and interactions return after bounded LocalLRC admission;
use `AwaitLocalLRC`, `FlushLocalLRC`, or an explicit `...Confirmed` method when
the application needs the server completion boundary. Timestamp-order messages
remain on the confirmed time-management path and are delivered before the
corresponding time-advance grant.

## Time and callbacks

A time-regulating federate promises not to send timestamp-order data earlier
than its current logical time plus lookahead. A time-constrained federate asks
the RTI to deliver timestamp-order data in logical-time order. A time-advance
request can remain pending until the federation-wide lower bound permits the
grant; the [quickstart](quickstart.md) demonstrates that wait.

An omitted selector uses `HLAfloat64Time`. The single-server gRPC backend also
supports explicit `HLAinteger64Time` selection. The factory is fixed for each
execution; Go, Python and Java expose it through their federation interfaces.
Integer64 uses exact unary requests and callbacks, not the Float64-only optimized
request streams. See [time management](time-management.md) for configuration.

Treat callbacks as part of the service contract, not as optional logging.
Applications should continuously consume `Federate.Events()` (Go) or the
equivalent SDK callback mechanism, handle federation-halted and stream-closure
conditions, and avoid blocking the callback consumer with long application
work.

Go applications receive FOM-name maps by default. A handle-oriented
application may set `ConnectOptions.CallbackRepresentation` to
`CallbackRepresentationHandles`; reflection and interaction events then expose
numeric handle maps without an extra name projection. This is an opt-in data
representation only. It does not change callback order, delivery, or ownership.

### Ownership callback completion

Strict terminal ownership receipts are a negotiated capability, independent of
optional MOM callback measurements. Raw Go dispatch clients call
`EnableOwnershipCallbackReceipts(ctx)` before acquisition work and use
`DispatchEvent`, or call `ReportOwnershipCallbackInvocation` only after actually
invoking their handler. Reading `Events()` alone is not invocation. A handler that
throws has still been invoked; retrying `FlushOwnershipCallbackReceipts` retries
the acknowledgement, not application code. `RetryOwnershipCallbacks` requests
redelivery of unresolved outcomes.

The server authenticates receipt reports and checks federation generation,
callback epoch, callback ID and delivery attempt. Acquired, unavailable and
acquisition-cancellation outcomes retain their acquisition/local-delete obligation
through failed sends until actual invocation is reported. Same-epoch replay is
deduplicated and re-acknowledged. After restore, an unentered callback invokes
under fresh authorization; a saved, known finished result is reported without
invoking the handler again. An already accepted result is reconciled with a
completion marker. Unknown or still-executing results cannot be checkpointed.
Do not reuse old-scope receipts.

Wire-visible object knowledge epochs and fenced local deletion let participating
SDKs reject buffered obsolete ownership offers after rediscovery. Server dispatch
guards alone cannot protect advice after Send starts or while it is SDK-buffered.
Python and Java ambassadors now attempt automatic receipt negotiation on a modern
nonzero-generation join. An unimplemented service retains delivery-only behavior;
other negotiation failures do not silently downgrade. Selected Python and Java
localhost tests cover this negotiation. Raw Go/Python
dispatch still requires explicit negotiation and actual-invocation reporting.
Legacy raw consumers are not silently placed in strict mode.
Unpublication uses the same terminal invocation ledger and relinquishes ownership
only for the published attributes being removed. Explicit empty attribute sets
are no-ops; omitted sets select the whole class.

### Synchronization callback processing

The server distinguishes callback entry from callback completion. Announcement
entry allows `SynchronizationPointAchieved` inside the announcement handler.
A label remains active until the required `FederationSynchronized` handlers have
completed. Transport send alone does not establish either boundary.

Go clients should invoke handlers through `DispatchEvent`, which reports both
boundaries and retains failed reports for retry without rerunning application
code. Raw consumers must call `ReportCallbackEntry` before the handler and
`ReportCallbackInvocation` after it. Receiving an event on a channel is not a
callback invocation. Use `FlushCallbackInvocationReports` to retry retained
dispatch outcomes.

## Go SDK

The exported package is `rti/pkg/federate`. It resolves FOM names to handles,
wraps generated gRPC clients, and exposes typed callbacks through
`Federate.Events()`.

The core connection and cleanup pattern is:

```go
conn, err := federate.Connect(ctx, "127.0.0.1:8442")
if err != nil {
    return err
}
defer conn.Close()

fed, err := conn.JoinFederation(ctx, federate.FederationSpec{
    Name: "training",
    FOMModules: []federate.FOMModule{
        {Path: "federation.fom.xml", XML: fomXML},
    },
}, "publisher")
if err != nil {
    return err
}
defer func() {
    cleanupCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
    defer cancel()
    _ = fed.Resign(cleanupCtx)
}()
```

The application must read `fomXML` before joining and should consume
`fed.Events()` for the lifetime of the membership. Complete executable
federates are under `examples/go-tar-wait` and
`verification/gorti-go-fair`.

To make all standard receive-order calls synchronous, connect with
`ReceiveOrderTransport: federate.ReceiveOrderTransportConfirmed`. TLS, mTLS,
and bearer-token clients use `ConnectWithOptions`; see
[operations](operations.md) for the matching server controls. Bearer-token and
OIDC connections use the confirmed fallback. The LocalLRC stream authenticates
its session with the token returned at join; custom dial options alone do not
disable it.

## SDK capability boundaries

The SDKs share the same RTI server but do not expose an identical surface.

| SDK profile | Principal surface | Important limits |
|---|---|---|
| Go | Lifecycle, declarations, OM, TM, ownership, synchronization, DDM, save/restore, MOM, Support, callbacks, confirmed calls, and LocalLRC | Channel delivery is not invocation; negotiated strict ownership receipts require dispatch or explicit actual-invocation reporting, separate from optional MOM measurement |
| Python async | Lifecycle and dedicated service clients, including ownership, save/restore, DDM, MOM, Support, GALT/LITS and retraction | The asynchronous API is distinct from the synchronous ambassador adapter |
| Python ambassador | Synchronous IEEE-shaped adapter and typed handle wrappers | Typed handles are still integer values without federation provenance |
| Java | `hla.rti1516e` provider, service clients, evoked callbacks, and encoding helpers | Supported service shapes and verification scope are listed in the Java SDK guide |

The schema and the current
[Interface Design Description](https://github.com/cbchoi/gorti/blob/main/engineering/specifications/current/IDD.md)
describe the fields and service signatures. Use the SDK-specific guide for
callback models, time representations, and connection options.

## Python SDK

The `rti1516e` package requires Python 3.11 or later and provides an asyncio API
plus a 1516-shaped ambassador adapter. Install generated bindings and the
package as described in [installation](installation.md). Use
`grpc://host:port` for plaintext and `grpcs://host:port` for TLS. TLS
connections can receive PEM CA, client certificate, client key, and bearer
token values through `RtiConnection.connect`.

Prefer `async with RtiConnection.connect(...)` so the transport closes even
when an operation fails. Resign joined federates before leaving the connection
context. The
[Python SDK README](https://github.com/cbchoi/gorti/blob/main/pysdk/README.md)
identifies the package layout and current verification commands.

## Java SDK

`javasdk` provides the `hla.rti1516e` interfaces and a gorti service provider.
It connects to the same RTI server as Go and Python federates. Applications
dispatch callbacks using the evoked callback model.

The [Java SDK guide](https://github.com/cbchoi/gorti/blob/main/javasdk/README.md)
covers installation, connection setup, service coverage, and live-server tests.

## Related guides

- [Integrating existing models](model-integration.md)
- [Development and educational use](education.md)
- [Time management](time-management.md)
- [LocalLRC transport](local-lrc.md)
- [Operations and security](operations.md)
- [Requirement-derived test cases](test-cases.md)
- [Verification results](verification.md)
