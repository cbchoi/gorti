# User guide

## Runtime model

`rtid` owns the authoritative state for each running federation and exposes HLA
service groups over gRPC. A **federation** is one coordinated simulation run. A
**federate** is an independently running participant. A **FOM module** defines
the object classes, attributes, interactions, parameters, and data types shared
by those participants.

The supported deployment is one `rtid` process with any number of local or
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
version control, use the same module set for every participant, and record
their hashes for reproducible experiments.

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
custom-dial connections use the confirmed fallback because the current
LocalLRC stream does not carry authenticated stream identity.

## SDK capability boundaries

The SDKs share the same RTI server but do not expose an identical surface.

| SDK profile | Principal surface | Important limits |
|---|---|---|
| Go | Lifecycle, declarations, OM, TM, synchronization, DDM, callbacks, confirmed calls, and LocalLRC | No equivalent top-level ownership, save/restore, MOM, or Support clients |
| Python async | Lifecycle and dedicated service clients, including ownership, save/restore, DDM, MOM, and Support | Some GALT/LITS and retraction operations are not exposed uniformly |
| Python ambassador | Synchronous IEEE-shaped adapter and typed handle wrappers | Typed handles are still integer values without federation provenance |
| C++ DLC-shaped profile | Broad IEEE 1516-2010 headers, callbacks, exceptions, and encodings | The verified subset is narrower than the header surface; some order/transport changes are compatibility no-ops |

The schema and the current
[Interface Design Description](https://github.com/cbchoi/gorti/blob/main/engineering/specifications/current/IDD.md)
are authoritative. Do not assume source or behavioral compatibility for an
operation that is not covered by the relevant SDK tests and conformance fixture.

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

## C++ SDK

`cppsdk` implements the IEEE 1516.1-2010 DLC header surface for Linux and
macOS. Generate bindings with Buf before configuring CMake. Compile-time
lockfiles, runtime exception tests, callback tests, and encoding round-trips
protect the API shape. Windows C++ support remains on the roadmap. See the
[C++ SDK README](https://github.com/cbchoi/gorti/blob/main/cppsdk/README.md) for
dependency and build options.

The current C++ transport is plaintext and does not implement TLS, mTLS, or
OIDC. Keep C++ federation traffic inside a trusted network boundary.

## Related guides

- [Time management](time-management.md)
- [LocalLRC fast path](local-lrc.md)
- [Operations and security](operations.md)
- [Verification and interoperability](verification.md)
