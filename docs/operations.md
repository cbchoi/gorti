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

### Object instance names

To register an object with an application-supplied name, reserve the name and
wait for the successful reservation callback before registering. An explicit
name without a reservation held by the caller is rejected with
`ObjectInstanceNameNotReserved`. Omitting the name requests an RTI-generated
name and does not require a reservation. This rule also applies to registration
with regions.

Reservation calls reject the `HLA` prefix; multiple-name reservation also
rejects an empty set. A name collision produces the reservation-failed callback.
If the callback cannot be admitted to the outbound queue, the call returns an
error without retaining new reservations or advancing its callback sequence.

Registration and deletion do not release an explicit reservation. Release it
with the name-release service when it is no longer needed; resign also removes
the federate's reservations. A released name can be reserved by another
federate while its original object is still live, but registering a second
object with that name remains prohibited until the first object is deleted.
Checkpoints preserve reservation rights separately from registered objects.

### Region-scoped object registration

Use matching server and Go/Python SDK versions for
`RegisterObjectInstanceWithRegions`. The operation now registers one object
with validated region associations before discovery. Older Go/Python clients
perform a preliminary ordinary registration and must be upgraded with the server.
Resolve dimension handles by name; standard-MIM dimensions are now included.
This registration profile currently requires the region's dimension set to
match the attribute's declared set; region-template expansion is not implemented.

The HLA core profile supports this operation. The current audit/replay codec
cannot preserve region-scoped registration. The optional production audit
observer warns and records a recording failure without blocking the HLA
operation; its journal is incomplete for DDM replay. A directly injected strict
writer rejects the operation before creating an object. Operational application
logging is separate. Checkpoints retain the object and its region associations.
Cross-version DDM handle-table restoration is not established.

### Save and restore boundary

Label-only restore selects the current execution's committed checkpoint when
available, or an unambiguous committed checkpoint from a previous execution.
An ambiguous label across executions fails; `saved_federation_generation`
explicitly selects the source execution. Keep the original storage directory,
including the committed-save history and bundle, when restarting the server.
Participants are matched by saved names. Same-generation restoration can recover
saved handles; cross-generation restoration assigns fresh handles in the target
execution and rewrites saved references. The server saves
the composed FOM and its handle bindings, object instances, name reservations,
object knowledge and transport settings, publication/subscription tables,
logical-time state and queued TSO callbacks, synchronization, ownership, DDM,
and MOM state. Simulation model memory and current attribute values belong to
the federates; the object registry routes values and does not cache them.

Pause application service calls, drain acknowledged sends,
and close LocalLRC/confirmed mutation streams before starting a checkpoint.
Keep callback-consumption streams open. During the protocol, ordinary
federation-scoped RPCs are rejected with `SaveInProgress` or `RestoreInProgress`;
SavepointService requests and callback consumption remain available. Wait for
the final callback before resuming model execution. The SDK refreshes FOM
bindings and changes the active handle only on successful restoration; do not
cache the pre-restore handle in application requests. Supported SDK runtime
images preserve recognized pending callbacks, known unreported outcomes and
typed local query notifications. The server reconciles them with its invocation
ledger; unread bytes and arbitrary application closures are not snapshots.
Checkpoint export rejects executing callbacks or unknown results. Go initially
disables LocalLRC after restore and uses confirmed calls. With application sends
paused and callback consumption running, call `ReinitializeLocalLRC(ctx)` to
rebuild the local validation state. An ordered callback marker controls when the
snapshot is installed. Failure leaves the confirmed path active; it does not
resubmit unresolved work from an old stream.

Restore validates all required snapshots before notifying participants and
applies them only after every participant reports successful restoration and
space for every completion callback has been reserved.
Abort or participant failure before that commit leaves RTI state unchanged.
Completion-queue admission failure also leaves state unchanged. An unexpected
adapter failure after commit is reported as an error with state already
committed; it is not described as a rollback.

Scheduled saves preserve advance requests that cross the checkpoint, deliver
eligible timestamped callbacks before save initiation, and resume pending
requests after completion. Tests cover positive and zero lookahead. Restoration
also resumes time requests retained in the checkpoint. An ambiguous source
execution, incompatible source model, or legacy bundle missing required state
is rejected rather than reported as restored. Legacy framing remains
readable, but this does not make every old bundle sufficient for a current
runtime restore. In particular, restoration requires complete accepted
message-retraction ID history; a missing history is not treated as empty.
The extended MOM snapshot format is not readable by older
server versions. The bundle manifest, including encoded snapshots, is limited
to 16 MiB; larger snapshots fail saving instead of creating an unreadable
bundle. This checkpoint profile is not a declaration of full IEEE
1516-2010 Save/Restore conformance.

### Authenticated automatic resignation

Joining issues a private 32-byte session token. Current SDKs retain it across
restore handle changes, replace it on a fresh join, and use it for automatic
resign settings and callback accounting. Do not log or persist this token.
Use TLS outside a trusted local environment. This capability protects the new
session-scoped controls; it is not a replacement for the server's existing
network access controls and OIDC configuration.

`SetAutomaticResignDirective` and `GetAutomaticResignDirective` require the
calling membership's token. The initial directive comes from the FOM's
`automaticResignAction`; the product default for an absent switch is `NoAction`.
An authenticated callback-stream disconnect starts `--auto-resign-grace`
(default `5s`). A reconnect within that interval cancels the pending cleanup.
Legacy streams without session metadata do not enable this mechanism.
Cleanup uses the configured action and its ordinary ownership preconditions;
it does not silently upgrade `NoAction` to a destructive policy. A failure is
reported in the server log. Active checkpoint protocols defer cleanup.

Explicit resign requests carrying session metadata are authenticated and bound
to the live membership identity, not only its numeric handle. Restore may change
that handle; an old token cannot resign a replacement membership. Requests
without session metadata retain the legacy behavior and still require the
deployment's access controls.

### Callback measurements

MOM queries expose MIM-encoded `attribute_values` and explicit
`unavailable_attributes`. Callback counts use authenticated receipts reported
after SDK callback invocation, not successful socket writes. Outstanding,
expired, or unsupported reports make affected totals unavailable. Existing
top-level RPC counters keep their older server-admission/fanout interpretation.
Keep callback accounting conditions identical when comparing performance.

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

## Service options and checkpoint limits

Subscriptions can be active or passive. Passive subscriptions still receive
matching object updates and interactions; they do not contribute active demand
for registration or interaction-relevance advisories. An omitted active option
preserves the previous active behavior. A nonempty update-rate designator must
exist in the loaded FOM. Active demand, regional overlap and current ownership
determine publisher update-on/off advisories and the effective named rate.
The producer decides when to send; the RTI does not drop timestamped updates or
delay grants to enforce the rate. A default or absent numeric rate is reported
as unavailable, not zero. Advisory switches affect their notifications, not
data subscriptions.

Object lookups made through a joined SDK use that federate's known-object state.
Local deletion forgets only that federate's copy. A continuing subscription can
immediately cause rediscovery; other federates and the shared object remain
unchanged. Keep consuming callbacks after local deletion.

At least one participant must be time managed for a scheduled checkpoint.
Initiation waits for safe logical-time progress and delivery of eligible TSO
events at or below the target. Resignation or a time-mode change during the
attempt cancels it; abort and failure release the checkpoint boundary.

Joining federates may add FOM modules. Go accepts `AdditionalFOMModules` in its
federation specification; Python accepts `additional_fom_modules` as explicit
file paths in `FederationSpec` or the standard-style join method. Python sends
the selected file contents to the connected RTI, so use only intended files
and a trusted server.
Conflicting definitions reject the join without publishing the extension.
Existing bindings remain stable. Existing participants can refresh their
authoritative view (`RefreshFOM` in Go and `refresh_fom` in Python).
Restoring an older checkpoint also restores
its FOM, so classes added later no longer exist after that restoration.

For timestamped sends with a nonzero retraction ID, the server rejects an ID
already accepted or reserved by that producer. History survives delivery,
retraction, resignation and checkpoint restoration, and is removed when the
federation generation is destroyed. An unused lower ID remains valid. Go and
Python expose the restored next-ID hint without allocating an ID or rewriting
the caller's value. The hint is not a reservation. Restoration fails explicitly
if the next-ID hint would overflow.

## MOM measurement sources

Set `GORTI_FEDERATE_HOST` in a Go or Python federate process to report its
host name as `HLAfederateHost`. The SDK sends this explicit UTF-8 value when it
joins; it does not inspect the operating system for a hostname. Unset or empty
means no host is reported. Values must be at most 1,024 UTF-8 bytes and contain
no whitespace or control characters. A new join replaces the previous report;
restoring a federate handle does not replace the live process's host report.

```powershell
$env:GORTI_FEDERATE_HOST = 'simulation-node-1.example'
```

```sh
export GORTI_FEDERATE_HOST=simulation-node-1.example
```

MOM queries and object updates can include `HLAcurrentFDD`, exported from one
committed merged model rather than concatenated source files. This exposes the
model definition to clients using the federation's MOM services. Use trusted
participants and the deployment's normal network and authentication controls;
the federation name alone is not an authorization boundary. Current export
requires explicit retained FDD switches, transportation and ordering metadata.
Incomplete models remain usable where previously supported, but their FDD is
reported unavailable. Source fields not retained by the model are not recovered
by export; see the FDD exporter documentation for its current scope.

With checkpoint storage configured, `HLAlastSaveName` and `HLAlastSaveTime` come
from a separate versioned history of successful save decisions. An immediate
save has an undefined logical time, distinct from a timed save at zero. A new
execution has a known empty history; a legacy execution without evidence has
unknown history. Restore retains the target execution's history. Back up the
whole save directory, including `history/`, while the server is stopped.
A process crash before the completion decision can leave an unproven bundle;
it is not silently adopted as the last successful save. Choose another label
until that orphan has been inspected.

`HLAtimeGrantedTime` and `HLAtimeAdvancingTime` measure server time-management
states, not application callback execution. Queries do not consume intervals.
Successful MOM attribute publication advances each reported attribute's own
baseline and retains sub-millisecond remainders. Restored observations exclude
server downtime; snapshots without timing history remain explicitly unavailable.
Queue lengths are separate metrics: `HLAROlength` and `HLATSOlength` combine
waiting messages and committed callbacks through authenticated callback entry.
See [time management](time-management.md) for the counting boundary.

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
