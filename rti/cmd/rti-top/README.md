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
| `T` | Time-advance view for the selected federation. In the Wire view, `T` cycles the rate-window (1s / 5s avg / 1m avg). |
| `W` | Wire-stats view (cross-federation top-style table). |
| `O` | Drill into the highlighted federation. |
| `I` | Open the event-log tail for the selected federation. |
| `C` | In Wire view: open the column-toggle popup. Up/Down navigate, Space toggles, Esc/Enter/C closes. |
| `Enter` | Drill in (Federations → Drilldown → FederateDetail). |
| `Esc` | Pop back one view level. |
| `↑` / `↓` (or `k` / `j`) | Move selection / scroll. |
| `R` | Cycle refresh interval through `100ms, 500ms, 1s, 2s, 5s, 10s, 30s, 60s`. |
| `S` | In Wire view: cycle the sort column. |
| `P` | In Events view: pause / resume the live tail. |
| `/` | Open the filter input. Federations view filters by federation name; Drilldown filters by federate name; Wire filters by federation OR federate substring. Esc clears. |
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
 Wire stats — window: 5s avg (last 5 ticks) — sort: FEDERATION
 FEDERATION       FEDERATE       SENDS     RECVS     DROPS    Q-DEPTH   Q-MAX     SENDS/s   RECVS/s   DROPS/s
 demo             buffer         125       250       12       4         8192      25.0      50.0      0.40
 demo             generator      250       0         0        0         8192      50.0      0         0
 demo             processor      0         125       0        0         8192      0         25.0      0

 Total: 375 sends  375 recvs  12 drops  (75.0 sends/s  75.0 recvs/s  0.40 drops/s)
 Outbox utilization (max q across federates): ░░░░░░░░ 0%

 S sort  T window  C columns  / filter  R refresh-rate  Esc back  Q quit
```

`S` cycles the sort column (federation, federate, sends, recvs,
drops, q-depth). `T` cycles the rate window (1s instantaneous /
5s avg / 1m avg). `C` opens the column-toggle popup so the
operator can hide / show any of the table's ten columns
(federation, federate, sends, recvs, drops, q-depth, q-max,
sends/s, recvs/s, drops/s) — the picker refuses to disable every
column at once. `/` filters rows by federation OR federate name
substring.

About the rate windows: "5s avg" means *the average over the
last 5 refresh ticks*. At the default 1Hz refresh that's a
5-second window; at 100ms refresh it's a 500ms window. Federate
re-joins (a counter dropping vs the prior tick) reset the per-
federate ring so rates don't flash a negative spike. Rates are
computed entirely client-side from `(updates_sent +
interactions_sent) / refresh_interval`; the AdminService proto
remains delta-free.

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

## Phase-3 added

Phase 3 closes every Phase-2 client-side ergonomic deferral. None
of these required new RPCs; one proto FIELD landed
(`FederateSnapshot.join_unix_seconds`, field 19).

- **`age` column on the federate row.** Computed client-side as
  `now - federate.join_unix_seconds`, formatted with magnitude-
  appropriate units (`5s`, `12m3s`, `2h15m`, `3d4h`). The
  federation manager stamps the wall-clock join time at
  JoinFederation; the AdminService Snapshot path threads it
  through.
- **Rate windows in Wire view (1s | 5s | 1m).** Rendered as
  `SENDS/s`, `RECVS/s`, `DROPS/s` per row plus an aggregate
  `Total:` line. Computed client-side from a 60-tick ring of
  per-(federation, federate) cumulative totals; `T` cycles the
  averaging window. Federate re-join detection (counter going
  backwards) resets the ring so rates never go negative.
- **Wire view column toggle (`C`).** Popup over the table; up/down
  navigate, space flips, esc/enter/c closes. Refuses to disable
  every column at once. Selection persists across view switches
  (in-memory only — no config file in Phase 3).
- **Filter polish.** The `/` filter narrows rows in the
  Federations view (by federation name), Drilldown view (by
  federate name within the current federation), and Wire view
  (by federation OR federate substring). The `F` filter in the
  Events view narrows the live tail by line substring. All
  filters are case-insensitive; Esc cancels filter input AND
  clears the substring.

## Phase-3 deferrals

- **Per-attribute ownership history**, **stall-risk indicators**,
  **last-activity timestamps** — explicitly out of scope per §3.2
  PINNED. Revisit only on operator demand.

## Phase-4 added

Phase 4 lands server-side improvements to the `TailEvents` stream
that previously drowned the renderer on busy federations.

- **Server-side filtering.** `TailEventsRequest` gains
  `event_class_filter` (case-sensitive substring against the event
  body's class name) and `federate_handle_filter` (whitelist of
  attributable federate handles). The events view's `F` keybinding
  now flows the substring into the next subscribe call, so the
  wire only carries what the operator wants.
- **Batched responses.** `TailEventsResponse` now carries
  `repeated TailedEvent events` plus the new tuning knobs
  `max_batch_events` (default 32, ceiling 1024) and
  `max_batch_latency_ms` (default 10ms, ceiling 1s). The handler
  flushes whichever bound trips first, mirroring the perf-pass
  batched-channel pattern in `multiOutbox`.
- **Backpressure-aware send.** When the gRPC server-stream's send
  buffer is full, the handler folds dropped events into an
  `overflow_skipped` counter that piggybacks on the next
  successful batch. The events view surfaces the cumulative count
  as a "renderer lag" status line — operators see exactly how many
  events the daemon dropped instead of silently losing data.
- **Richer event lines.** `TailedEvent` now carries `event_class`
  (e.g. `FederateJoined`, `InteractionSent`) and the
  attributable `federate_handle`, so `formatEvent` can render
  `seq=N CLASS fed=H payload_bytes=B`.

## Phase-5 added (opt-in mutating ops)

Phase 5 introduces operator-initiated mutating ops as an EXPLICIT
opt-in. Read-only mode is still the default: the daemon registers
`MutatingService` only when `--admin-mutating=true`, and the TUI
hides the X / D keybindings until the probe at startup succeeds.

- **`MutatingService` proto** in
  `proto/rti/v1/admin_mutating.proto` — separate service from
  `AdminService` so the read-only contract is preserved by
  construction. Three RPCs: `Probe`, `ForceResign`,
  `DestroyFederation`.
- **rtid composition root gate.** Two new flags: `--admin-mutating`
  (default `false`) registers `MutatingService` on the admin
  port; `--admin-mutating-allow-non-loopback` (default `false`) is
  the explicit override for non-loopback admin binds. With
  mutating enabled and a non-loopback bind without the override,
  `rtid` refuses to start (exit 2). A prominent `WARN` is logged
  at startup whenever mutating is enabled.
- **Both RPCs reuse `federation.Manager` primitives.**
  `ForceResign` calls `ResignFederation`; `DestroyFederation`
  optionally walks the roster and force-resigns each federate
  before calling `DestroyFederation`. Eventlog entries + MOM hooks
  fire identically to the federate-initiated path — critical for
  replay determinism + observability symmetry.
- **TUI keybindings.** With `MutatingService` reachable, `X` on a
  federate row opens a confirmation dialog (`y` proceeds), and
  `D` on a federation opens a double-confirm dialog (`y` typed
  twice). Both surface the result as a status line under the
  header. Cancel via `n` / `Esc`.

## Phase 5 deferrals (still unaddressed)

- **Rich event-log payloads.** `TailedEvent.payload` carries the
  proto-encoded `EventEntry` bytes but the renderer doesn't
  decode the body — `formatEvent` shows the byte count, not the
  per-event detail (sender, parameter values, ...). A future cut
  decodes through the proto registry.
- **Mutating-ops audit trail.** The handler logs at slog level via
  the standard event-log + MOM hooks; a dedicated audit logger
  (operator identity, source IP, RPC summary) is a follow-up.
- **mTLS / RBAC for the admin endpoint.** The whole admin port is
  plaintext; production deployments add their own ACL via mTLS or
  a reverse proxy. cut-3 backlog item.

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
