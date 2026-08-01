# Operations

## Endpoint exposure

| Endpoint | Server default | Exposure at default | Purpose |
|---|---|---|---|
| Federate | `:8442` | All interfaces | HLA service and callback gRPC traffic |
| Admin | `localhost:8443` | Loopback, plaintext | Status and event stream for `rti-top` |
| Metrics | `:9090` | All interfaces, plaintext | Liveness and Prometheus metrics |

An empty `--admin-listen` value disables the admin listener. The TLS flags for
the federate listener do not apply to the separate admin or metrics listeners.
Use explicit addresses instead of relying on defaults.

For a local development server, run in the foreground with durable event-log
and save directories:

=== "Linux and macOS"

    ```bash
    ./bin/rtid \
      --listen 127.0.0.1:8442 \
      --admin-listen 127.0.0.1:8443 \
      --metrics-listen 127.0.0.1:9090 \
      --audit-replay-plugin event-journal \
      --log-dir ./eventlogs \
      --save-dir ./gorti-saves \
      --log-format text
    ```

=== "Windows PowerShell"

    ```powershell
    .\bin\rtid.exe `
      --listen 127.0.0.1:8442 `
      --admin-listen 127.0.0.1:8443 `
      --metrics-listen 127.0.0.1:9090 `
      --audit-replay-plugin event-journal `
      --log-dir .\eventlogs `
      --save-dir .\gorti-saves `
      --log-format text
    ```

Installed release binaries can be invoked as `rtid` instead. `rtid` has no
daemonization flag or bundled service definition, so a managed deployment must
supply its own process supervisor, working directory, absolute data paths, and
restart policy.

## Readiness and monitoring

Wait for the `rtid serving` log record before starting federates. It lists the
addresses that were successfully bound. The metrics listener serves a
liveness response at `/` and Prometheus text at `/metrics`.

=== "Linux and macOS"

    ```bash
    curl -fsS http://127.0.0.1:9090/
    curl -fsS http://127.0.0.1:9090/metrics
    ./bin/rti-top --rtid-addr 127.0.0.1:8443 --smoke
    ```

=== "Windows PowerShell"

    ```powershell
    Invoke-WebRequest http://127.0.0.1:9090/ | Select-Object -Expand Content
    Invoke-WebRequest http://127.0.0.1:9090/metrics | Select-Object -Expand Content
    .\bin\rti-top.exe --rtid-addr 127.0.0.1:8443 --smoke
    ```

Run `rti-top --rtid-addr 127.0.0.1:8443` without `--smoke` for the interactive
terminal view. Press `Q` or `Ctrl-C` to exit the monitor; this does not stop
`rtid`. `rti-top` is read-only unless the server is explicitly started with
`--admin-mutating=true`.

Prometheus metrics include current federation and federate counts,
`gorti_event_log_seq`, and object-handle counts. The current production
sequence adapter reports zero and must not be used as a journal-progress
alarm. The liveness endpoint proves that the metrics HTTP server is responding;
it does not prove that a particular federation or federate is healthy.

## Durable data and logs

The default server runs the `gorti-hla-core` profile. It does not construct,
encode, or store audit records. Enable the optional module explicitly:

```bash
rtid \
  --audit-replay-plugin event-journal \
  --log-dir /var/lib/gorti/eventlogs
```

`--audit-replay-plugin` accepts `none` (the default) or `event-journal`.
The plugin requires a writable `--log-dir` and stores generation-qualified
binary `.log` files below per-federation directories. Plugin recording errors
are reported through structured process logs and plugin status, but they do
not change an HLA service result. `AdminService.TailEvents` is available only
while the plugin is loaded.

Persisted event records provide bounded replay evidence for the record types
currently emitted. `replay-from-log` is supplied by the audit/replay module
and verifies log reproduction; it does not
reconstruct a complete live federation. Save and restore use structured
manager snapshots, and the current save bundle may contain a zero-length event
log extent even when the server runs in strict mode.

`--event-log-protobuf-validation` defaults to `true`. Setting it to `false`
skips only Protobuf required-field initialization checks. Event encoding,
UTF-8 checks, sequence assignment, sink writes, and write-ahead ordering remain
enabled. This diagnostic option produced no measurable end-to-end gain in the
current two-federate workload, so production deployments should retain the
default unless they have separately validated their event schema and workload.
The setting has no effect when the audit/replay plugin is not loaded.

`--save-dir` defaults to `./gorti-saves` and stores federation save bundles.
`--state-dir` optionally persists the core federation-generation epoch used to
reject stale handles across process restarts. This core state is independent
of audit logs. Relative paths are resolved from the process working directory. Managed
deployments should use absolute paths on storage writable by the service
account and include those paths in backup and retention policies.

`--log-level` accepts `debug`, `info`, `warn`, or `error`.
`--log-format` accepts `json` (the default) or `text`. Use JSON for log
collection and text for an interactive development run.

`--gc-percent` defaults to 400: at startup rtid raises the Go garbage
collector's target percentage (`runtime/debug.SetGCPercent`) above the Go
runtime default of 100, trading a larger peak heap for measurably lower
allocation-driven latency on the callback fast path. An operator-set `GOGC`
environment variable always wins over this product default, and an explicit
`--gc-percent` flag wins over both (precedence: explicit flag, then `GOGC`,
then the product default 400). Pass `--gc-percent=-1` to leave the Go runtime
completely untouched and restore Go runtime defaults. On memory-constrained
hosts, `--go-mem-limit` (a soft memory limit in bytes via
`runtime/debug.SetMemoryLimit`; the default `0` makes no limit call) bounds
the larger heap the relaxed GC target allows.

## Security boundary

The federate listener is plaintext unless both `--tls-cert` and `--tls-key`
are set. Supplying only one is a startup configuration error. TLS has a minimum
version of 1.2.

```bash
rtid \
  --listen 0.0.0.0:8442 \
  --tls-cert /etc/gorti/server-cert.pem \
  --tls-key /etc/gorti/server-key.pem \
  --admin-listen 127.0.0.1:8443 \
  --metrics-listen 127.0.0.1:9090 \
  --audit-replay-plugin event-journal \
  --log-dir /var/lib/gorti/eventlogs \
  --state-dir /var/lib/gorti/state \
  --save-dir /var/lib/gorti/saves
```

Clients must use the matching TLS transport: `ConnectWithOptions` with a
`tls.Config` in Go, or a `grpcs://` URL in Python. Protect private keys with
filesystem permissions appropriate to the service account.

The current C++ profile does not implement TLS, mTLS, or bearer-token client
transport. Do not use a `grpcs://` C++ URL as evidence that the channel is
encrypted; run C++ federates only on a trusted network boundary until secure
transport is implemented.

For mTLS, add `--tls-client-ca` with a PEM CA bundle. Every federate must then
present a certificate signed by that CA. The optional
`--tls-client-cn-allow` comma-separated list further restricts accepted client
certificate Common Names.

OIDC bearer-token verification is enabled with a pinned public-key PEM through
`--oidc-jwks-pem`; `--oidc-audience` and `--oidc-issuer` constrain the expected
claims. In the current implementation, issuer discovery is not implemented:
setting `--oidc-issuer` without `--oidc-jwks-pem` fails at startup. Pair bearer
tokens with TLS on an untrusted network.

TLS protects the channel and authenticates the server. mTLS or OIDC can also
authenticate the connecting client, but none of these mechanisms provides
complete per-federate authorization. Normal service requests carry
a federation name and numeric federate handle without a session credential
cryptographically bound to that handle. Treat endpoint access as trusted and
do not expose it to mutually untrusted federate clients.

The admin listener remains plaintext and has no built-in operator
authentication, even when the federate listener uses TLS, mTLS, or OIDC. Keep
admin on loopback. Mutating admin operations can forcibly resign federates or
destroy federations; they are disabled by default and should remain so unless
there is a specific operational need. The metrics listener is also plaintext;
bind it to loopback or place it behind an appropriate monitoring boundary.

## Graceful shutdown

Shut down in this order:

1. Stop accepting new application work and new HLA sends.
2. Complete or cancel application workflows, drain required callbacks, and
   flush or await LocalLRC work when its completion boundary matters.
3. Resign every federate with a fresh bounded cleanup context, then close SDK
   connections.
4. Destroy the empty federation if the application owns that lifecycle step.
5. Press `Ctrl-C` in the `rtid` terminal. A Unix process supervisor can send
   `SIGTERM`.
6. Wait for the `rtid shutting down` record and process exit before rotating,
   copying, or replacing active plugin-log and save directories.

On process shutdown, `rtid` gracefully stops the federate and admin gRPC
servers, gives the metrics HTTP server up to five seconds to shut down, and
closes loaded plugin resources. Avoid terminating the process forcibly during
normal operation.

## Troubleshooting

Start with `rtid --help`, then rerun with `--log-level debug --log-format text`
when more context is needed.

| Symptom | Likely cause and action |
|---|---|
| Startup fails with `address already in use` or a bind error | Another process owns one of the federate, admin, or metrics ports. Inspect all three addresses or select unused ports. |
| Remote federates cannot connect | Confirm `--listen` is not loopback-only, the client uses the same host and port, and the host firewall permits the connection. Configure TLS before exposing the port to an untrusted network. |
| `rti-top` reports a dial or status failure | Confirm `rtid` started with a non-empty `--admin-listen`, use that exact address, and remember that `rti-top` expects the admin connection to be plaintext. |
| `/metrics` works but federates cannot join | The metrics listener is a separate HTTP server. Check the federate gRPC address and `rtid` logs. |
| No event-log files appear | Confirm `--audit-replay-plugin=event-journal` and set a writable `--log-dir`. The default HLA core profile creates no journal. |
| Startup fails while creating generation, log, or save data | Check that the configured directories and their parents are writable by the process account. Prefer absolute paths under a supervisor. |
| TLS clients report an unknown authority or hostname error | Trust the CA that signed the server certificate and connect using a hostname present in that certificate. Do not disable certificate verification. |
| The server rejects TLS configuration at startup | Supply `--tls-cert` and `--tls-key` together; mTLS also requires both plus a valid PEM `--tls-client-ca`. |
| Authenticated calls return `Unauthenticated` | Confirm the bearer token is present, signed by the pinned key, and matches the configured issuer and audience claims. |
| A time advance remains pending | Check every regulating federate's current time, lookahead, pending request, and callback consumption. Use `rti-top` and the [time-management guide](time-management.md). |
| A federate stops receiving callbacks | Keep the SDK event stream draining and avoid long blocking work in the callback consumer. Inspect stream closure and federation-halted events. |

## Configuration records

For repeatable runs, record the binary SHA-256, Git commit or release tag, FOM
SHA-256 and module order, complete command line, relevant environment
overrides, CPU and operating system, endpoint exposure, TLS/authentication
mode, and logging mode. The fair-comparison runner captures these fields for
its own workloads.
