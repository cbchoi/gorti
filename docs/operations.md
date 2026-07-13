# Operations

## Endpoints

| Endpoint | Default | Purpose |
|---|---|---|
| Federate | `:8442` | HLA service and callback gRPC traffic |
| Admin | `localhost:8443` | Read-only status and event stream for `rti-top` |
| Metrics | `:9090` | Prometheus metrics |

Start a development server with durable event and save directories:

```bash
rtid --listen 127.0.0.1:8442 \
  --admin-listen 127.0.0.1:8443 \
  --metrics-listen 127.0.0.1:9090 \
  --log-dir ./eventlogs \
  --save-dir ./gorti-saves
```

## Security boundary

Plaintext listeners are intended for a trusted network. Use TLS and the
documented authentication options across an untrusted network. Keep the admin
listener on loopback unless an external access-control layer protects it.
Mutating admin operations are disabled by default.

## Observability

`rti-top` displays federation membership, logical time, wire rates, and event
records from the admin endpoint. The Prometheus endpoint supports long-running
monitoring. Server event logs are also evidence inputs for the verification
harness.

## Shutdown

Federates should stop new sends, drain expected callbacks, resign, and close
their event stream. Destroy the federation only after all members resign. A
process-level shutdown cancels remaining streams and closes event-log and save
resources.

## Configuration records

For repeatable runs, record the binary SHA-256, FOM SHA-256, complete command
line, environment overrides, CPU and operating system, and active logging
mode. The fair-comparison orchestrator captures these fields automatically.
