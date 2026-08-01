# rti-top

`rti-top` is a terminal interface for observing federations hosted by `rtid`.
It connects to the admin gRPC listener, polls snapshots, and streams selected
event records. Read-only operation is the default.

## Build and run

```bash
go build -o bin/rti-top ./rti/cmd/rti-top
```

Start `rtid` with an admin listener:

```bash
rtid --listen :8442 --admin-listen localhost:8443 --metrics-listen :9090
```

Then attach the terminal UI:

```bash
rti-top --rtid-addr localhost:8443
```

The admin listener should remain bound to loopback unless it is protected by
an external access-control and transport-security layer.

## Options

| Option | Default | Meaning |
|---|---|---|
| `--rtid-addr` | `localhost:8443` | `rtid` admin listener address |
| `--refresh` | `1s` | Snapshot interval in the supported `100ms` to `60s` range |
| `--smoke` | `false` | Call status, print the result, and exit without opening the UI |

## Views

- **Federations** lists executions and their aggregate activity.
- **Federate detail** shows identity, logical time, declarations, traffic,
  synchronization, save, and DDM state when available.
- **Time advance** shows current time, pending requests, lookahead, LBTS
  contribution, and recent logical-time history.
- **Wire statistics** shows sends, receives, drops, queue depth, and rates.
- **Events** tails filtered event records for the selected federation.

Common controls:

| Key | Action |
|---|---|
| `F` | Federation view; event filter while viewing events |
| `T` | Time view; rate-window selection in wire view |
| `W` | Wire-statistics view |
| `Enter` | Open the selected federation or federate |
| `I` | Open the selected federation's event stream |
| `Up` / `Down` | Move the selection or scroll |
| `R` | Cycle the refresh interval |
| `S` | Cycle the wire-view sort column |
| `C` | Choose visible wire-view columns |
| `/` | Filter the current table |
| `P` | Pause or resume event rendering |
| `Esc` | Return to the previous view or clear input |
| `Q` / `Ctrl-C` | Exit |

Rate windows contain the last 1, 5, or 60 refresh samples. Their wall-clock
duration therefore changes with the selected refresh interval. Counter resets
on a federate rejoin are detected so rates do not become negative.

## Optional mutating operations

`ForceResign` and `DestroyFederation` are hidden unless `rtid` starts with
`--admin-mutating=true` and the client can probe `MutatingService`. The `X` key
requests a forced resign and the `D` key requests federation destruction; both
require confirmation, and destruction requires two confirmations.

```bash
rtid --admin-listen localhost:8443 --admin-mutating=true
```

`rtid` refuses a non-loopback mutating listener unless
`--admin-mutating-allow-non-loopback=true` is also supplied. That override does
not add authentication or encryption. Protect any remotely reachable admin
listener with an appropriate external security policy.

## Current limits

- Event payloads are summarized rather than fully decoded in the UI.
- Event-stream backpressure can skip records; the reported skipped count makes
  this visible.
- Admin RPC fields do not provide complete per-operator authorization or audit
  identity.
- Per-attribute ownership history and stall-risk analysis are not displayed.

## Tests

The package contains render, key-handling, mutation-confirmation, rate-window,
and integration tests. The integration test starts `rtid`, connects through the
admin service, and renders a live snapshot.

```bash
go test -race -count=1 ./rti/cmd/rti-top/...
```
