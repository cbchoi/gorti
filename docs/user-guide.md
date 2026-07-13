# User guide

## Runtime model

`rtid` owns federation state and exposes HLA service groups over gRPC. Each
federate is an independent process that joins a named federation, declares the
data it publishes or subscribes to, and receives callbacks on an event stream.

The supported deployment is one `rtid` process with any number of local or
remote federates. Use a unique federation name for independent runs and resign
all federates before destroying the federation.

## Federation management

A normal lifecycle is:

1. Create a federation with an ordered set of FOM modules.
2. Join each federate with a unique name and type.
3. Register synchronization points when startup barriers are required.
4. Publish and subscribe before sending application data.
5. Resign every federate and destroy the federation.

FOM module order and bytes are part of the run identity. Keep them under
version control for reproducible experiments.

## Object and interaction management

Object attributes represent persistent distributed state. Register an object
instance, update its owned attributes, and delete the instance at the end of
its lifetime. Interactions represent transient events and do not create
persistent instances.

Receive-order messages are deliverable immediately. Timestamp-order messages
enter the time-management path and are delivered before the corresponding
time-advance grant. A successful synchronous `SendInteraction` return includes
the server ACK boundary documented by the Go and Python SDKs.

## Go SDK

The exported package is `rti/pkg/federate`. It resolves FOM names to handles,
wraps generated gRPC clients, and exposes typed callbacks through
`Federate.Events()`.

```go
conn, err := federate.Connect(ctx, "127.0.0.1:8442")
if err != nil {
    return err
}
defer conn.Close()

fed, err := conn.JoinFederation(ctx, spec, "publisher")
if err != nil {
    return err
}
defer fed.Resign(ctx)
```

Complete executable federates are under `examples/go-tar-wait` and
`verification/gorti-go-fair`.

## Python SDK

The `rti1516e` package provides an asyncio API and a 1516-shaped ambassador
adapter. Use `grpc://host:port` for plaintext and `grpcs://host:port` for TLS.
The SDK README contains the complete API examples.

## C++ SDK

`cppsdk` implements the IEEE 1516.1-2010 DLC header surface for Linux and
macOS. Compile-time lockfiles and runtime exception tests protect API shape.
Windows C++ support remains on the roadmap.

## Next steps

- [Time management](time-management.md)
- [Operations and security](operations.md)
- [Verification and interoperability](verification.md)
