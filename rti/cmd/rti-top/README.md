# rti-top — top-style live federation observability

`rti-top` is a terminal UI for `rtid` (gorti's runtime infrastructure
daemon). It dials `rtid`'s admin gRPC listener, polls `Snapshot` at
the configured cadence, and renders five live views of every
federation hosted by the running daemon.

This is **Phase 2** of the design in [`docs/rtid-tui.md`](../../../docs/rtid-tui.md).
Phase 1 (the `AdminService` proto + handler + per-Manager
`Snapshot()` methods + `--admin-listen` flag) is the wire surface
this binary consumes; Phase 2 adds the bubbletea TUI on top.

`rti-top` is **read-only**. It exposes no keybindings or RPCs that
mutate federation state — see §1 + §7.5 of the design doc. Mutating
control-plane operations are deferred to a separately-scoped Phase 5+.

## Build

```sh
go build -o /tmp/rti-top ./rti/cmd/rti-top
```

## Run

Start `rtid` with the admin listener enabled:

```sh
rtid --listen :8442 --admin-listen :8443 --metrics-listen :9090
```

In another terminal, attach `rti-top`:

```sh
rti-top --rtid-addr localhost:8443
```

The admin listener defaults to `localhost` for safety; bind to
`0.0.0.0` only when the operator explicitly wants remote rti-top
attachment (and adds their own ACL via mTLS or a reverse proxy —
mTLS for the admin endpoint is a cut-3 backlog item).

### Flags

| Flag | Default | Description |
|---|---|---|
| `--rtid-addr` | `localhost:8443` | AdminService listener address on rtid (host:port). |
| `--refresh` | `1s` | Snapshot polling interval. Clamped at boot to `[100ms, 60s]`. Cycled at runtime via the `r` keybinding. |
| `--smoke` | `false` | Smoke-test mode: dial + call Status + print the response, then exit. Used by CI; does not enter the TUI. |

If `--refresh` is outside the PINNED `[100ms, 60s]` range the binary
exits with a clear usage error and exit code 2.

## Key bindings

| Key | Effect |
|---|---|
| `F` | Federations view (landing). In the Events view, `F` enters the filter input. |
| `T` | Time-advance view for the selected federation. |
| `W` | Wire-stats view (cross-federation top-style table). |
| `O` | Drill into the highlighted federation. |
| `I` | Open the event-log tail for the selected federation. |
| `Enter` | Drill in (Federations → Drilldown → FederateDetail). |
| `Esc` | Pop back one view level. |
| `↑` / `↓` (or `k` / `j`) | Move selection / scroll. |
| `R` | Cycle refresh interval through `100ms, 500ms, 1s, 2s, 5s, 10s, 30s, 60s`. |
| `S` | In Wire view: cycle the sort column. |
| `P` | In Events view: pause / resume the live tail. |
| `/` | Open the federation filter input. |
| `Q` / `Ctrl-C` | Quit. |

## Views

### 1. Federations (`F`, default landing)

```
 gorti rtid rtid-cut2 — uptime 3h23m — 2 federations — 8 federates — refresh 1s
 [F]ederations  [T]ime  [W]ire  [O]bjects  [I]nteractions  [Q]uit
   FEDERATION             MODE           FEDERATES  PUB CLASSES  OBJECTS   TPS
 ▶ demo                   verbose        3          5            12        75.0
   benchmark              best-effort    5          0            0         0.0

 ↑↓ select  Enter drill-down  R refresh-rate  / filter  Q quit
```

### 2. Drilldown (`Enter` on a federation; matches §3.2 PINNED)

```
 Federation: demo — verbose — federates_joined=3
   FEDERATE           HANDLE  TIME      LOOKAHEAD  ROLE           TPS     Q     DROP    AGE
 ▶ generator          1       5.00      1.00       regulator      50      0     0       12m3s
   buffer             2       5.00      0.50       reg+const      25      4     6       12m3s
   processor          3       5.00      1.00       constrained    0       0     0       11m48s

 LBTS: 5.50    Pending grants: [h=1 @ 6.00]
 Sync points: [start_simulation: ✓ achieved]
 Save state:  IDLE
 Region count: 0 (no DDM activity)

 Esc back  ↑↓ select federate  Enter inspect  T time view  W wire view  I events
```

The PINNED column set is: `name`, `handle`, `current_time`,
`lookahead`, `role`, `tps`, `queue_depth`, `drops_total`, `age`.
`age` is computed client-side as `now - join_unix_seconds` and
rendered with a magnitude-appropriate unit (`5s`, `12m3s`, `2h15m`,
`3d4h`). A zero `join_unix_seconds` (legacy data path or pre-
Phase-3 daemon) renders as `-` so the row still aligns.

### 3. Federate detail (`Enter` on a federate row)

```
 Federate: buffer  (handle=2)  Federation: demo
 IDENTITY
   name=buffer  handle=2  role=reg+const
 TIME
   current=5.000  lookahead=0.500  pending=—  contribution=5.50
 PUB / SUB
   pub object classes:       (none)
   sub object classes:       (none)
   pub interaction classes:  (none)
   sub interaction classes:  (none)
 WIRE STATS
   updates_sent=25  interactions_sent=0
   reflections_received=50  interactions_received=0
   outbox queue=4/8192  drops_total=6
 SYNC
   start_simulation: achieved  required=0  achieved=0
```

The optional `SAVE`, `DDM`, and `SYNC` sections collapse when empty
per §3.2 PINNED — only sections with active state appear.

### 4. Time advance (`T`)

```
 Time advance — Federation: demo
 FEDERATE         CURRENT   PENDING   LOOKAHEAD  CONTRIBUTION  STATE
 generator        5.00      6.00      1.00       6.00          awaiting LBTS≥6.00
 buffer           5.00      —         0.50       5.50          idle (no request)
 processor        5.00      —         1.00       —             constrained-only

 LBTS: 5.50   (= min over regulators of current+lookahead)

 Time history (last 30 ticks, normalized):
   generator      ▁▁▂▃▄▅▆▇████████████████████
   buffer         ▁▁▂▂▃▃▄▄▅▅▆▆▇▇████████████
   processor      ▁▁▁▂▂▃▃▄▄▅▅▆▆▇▇████████████

 Esc back  W wire view  R refresh-rate  Q quit
```

The sparkline tracks each federate's `current_time` over the last 30
snapshot ticks (normalized to per-federate min/max). Samples persist
in-memory across view switches — entering and leaving the Time view
does not reset the history.

### 5. Wire stats (`W`)

```
 Wire stats — totals since federate join — sort: FEDERATION
 FEDERATION       FEDERATE       SENDS     RECVS     DROPS    Q-DEPTH   Q-MAX
 demo             buffer         25        50        6        4         8192
 demo             generator      50        0         0        0         8192
 demo             processor      0         25        0        0         8192

 Total: 75 sends  75 recvs  6 drops
 Outbox utilization (max q across federates): ░░░░░░░░ 0%
   note: Phase 2 reports cumulative totals; rate windows (1s|5s|1m) deferred to Phase 3.

 S sort  R refresh-rate  Esc back  Q quit
```

`S` cycles the sort column (federation, federate, sends, recvs,
drops, q-depth). The 1s|5s|1m rate-window selector from the design
doc's §3.4 mockup is **deferred to Phase 3** — the proto exposes
"since federate join" totals only, and computing rate windows
requires either a delta-capable proto (a Phase-3 proto bump) or
client-side delta tracking which is unreliable across reconnects.

### 6. Event log tail (`I`)

```
 Event log — federation: demo — tail
   seq=1  ts=0  payload_bytes=0
   seq=2  ts=0  payload_bytes=0

   Phase 2 limitation: TailEventsResponse.payload is empty in this cut;
   the view shows seq + timestamp only. Richer event detail lands in Phase 3.

 F filter  P pause/resume  Esc back  Q quit
```

`P` pauses live-append (the goroutine continues to consume from the
stream — incoming records are dropped while paused), `Esc` cancels
the stream and returns to the Federations list.

## Phase-2 deferrals

These are documented here so they don't get lost. None of them
require proto changes; they're all client-side ergonomic
enhancements that can land independently in Phase 3.

- ~~**`age` column on the federate row.**~~ DONE in Phase 3 —
  `join_unix_seconds` (proto field 19) is populated by the
  federation manager at JoinFederation and rendered client-side.
- **Rate windows in Wire view (1s | 5s | 1m).** §3.4 mocks them up;
  Phase 2 surfaces only cumulative totals. Implementation requires
  client-side ring buffers of `(snapshot_at, totals)` pairs — fine
  to add but adds memory pressure for federations with many
  federates, so deferred until there's a concrete user demand.
- **Rich event-log payloads.** Phase 1's `TailEventsResponse` ships
  `seq` + `unix_nanos` + `payload bytes` — but the server-side
  handler currently emits empty payloads (the `eventlog.MultiplexWriter`
  reader doesn't yet round-trip the proto-encoded `EventEntry`).
  When the eventlog encoder lands, Phase 3 can decode the payload
  in `formatEvent` and render `class + sender + seq` per the §3.5
  mockup.
- **Federate column toggle.** §3.1 keybinding row shows a `[O]bjects`
  column-toggle; we use `O` as drilldown for now. A column-toggle
  framework would let users hide / show columns at will (e.g. drop
  `AGE` once it's empty for everyone).
- **Per-attribute ownership history**, **stall-risk indicators**,
  **last-activity timestamps** — explicitly out of scope per §3.2
  PINNED. Revisit only on operator demand.

## Architecture

```
┌─────────────┐  Snapshot()    ┌────────────┐  bubbletea     ┌──────────────┐
│ rtid        │◄───────────────│ rti-top    │ MVU model    →│ terminal     │
│ AdminService│   1Hz default  │ client/    │ → views.go    │ (lipgloss    │
│ (read-only) │   TailEvents() │ model.go   │ ← keys        │  styled)     │
└─────────────┘   server-stream└────────────┘               └──────────────┘
```

- `internal/client/` wraps the gRPC stub with deadline + version-pin
  policy. Only read-only RPCs are exposed.
- `model.go` is the bubbletea Model. Init kicks off the first poll;
  Update handles `snapshotMsg` (poll result), `pollTickMsg` (timer),
  `eventTailMsg` (server-stream), `tea.KeyMsg` (input).
- `views.go` is the rendering layer — one function per view. Header
  + footer are shared. The mockups in `docs/rtid-tui.md` §3.x are
  the source of truth for column ordering and footer text.
- `events.go` owns the TailEvents server-stream goroutine and the
  in-memory line ring (cap 500).
- `time_history.go` owns the per-(federation, federate) sparkline
  ring (30-sample fixed-capacity FIFO).

## Tests

Render-layer tests in `views_test.go` substring-match the load-
bearing labels in each view's output (column headers, federate names,
LBTS / sync / save labels). The integration test in
`integration_test.go` builds `rtid`, spawns it with a free admin
port, dials AdminService, fetches Snapshot, and verifies the model
renders the live response cleanly.

```sh
go build ./...
go vet ./...
go test -race -count=1 ./...
```

## Versions

Three Charm dependencies plus their transitive deps. Pinned to
v1.x of bubbletea (rather than the v1.3 series) to keep the
top-level go.mod's Go directive at 1.22 — bubbletea v1.3+ requires
Go 1.24.

| Dep | Version |
|---|---|
| `github.com/charmbracelet/bubbletea` | v1.1.2 |
| `github.com/charmbracelet/lipgloss` | v0.13.0 |
| `github.com/charmbracelet/bubbles` | v0.20.0 |
