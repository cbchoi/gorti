# `rtid` TUI — top-style live federation observability

Status: Phase 0 — design only, no code changes yet. Pending decisions
are marked **OPEN** and should be pinned before Phase 1 starts.

This document proposes an interactive terminal UI for `rtid` — a
top-like dashboard showing live state of every federation hosted by
the running daemon: federates, time-advance state, pub/sub topology,
wire throughput, queue depths, sync points, ownership, save/restore.

Audience: federation developers running rtid locally during
development, operators of a deployed rtid instance, and researchers
observing how alternative strategies (Phase-2 of the research
platform) behave under live load.

---

## 1. Goal

A `top`-shaped TUI that lets you, in one terminal:

1. See every federation hosted by `rtid` and the federates joined to each
2. Drill into a federation: per-federate handle, current logical time,
   role (regulating / constrained / observer), pub/sub classes, queue
   depth, wire throughput, drop count
3. Watch time-advance progression: LBTS evolution, pending grants,
   regulator contributions
4. See sync-point state, ownership transfers in flight, save/restore
   protocol state
5. Tail the recent event log (like `tail -f`) optionally filtered
6. Identify hot spots — which federates are dropping, which queues
   are saturating, which class has the highest fan-out

**Non-goals**: a federation **controller**. The TUI is read-only by
default. Killing federates, draining queues, force-resigning,
hot-restart — all explicitly out of scope. (Could be a Phase 4
extension; explicitly NOT Phase 1.)

---

## 2. Approach choices

### 2.1 Where the TUI runs — **PINNED: (B) separate `rti-top` binary**

Pinned 2026-05-05. `rtid` stays headless (logs to stdout/stderr as
today, runs cleanly under systemd / containers / CI). A new
`rti-top` binary in `rti/cmd/rti-top/` talks to `rtid` over a new
`AdminService` gRPC contract — works locally OR remotely, supports
multiple simultaneous observers, gives operators the wire surface
they'd need for any future observability consumer (a CLI snapshot
tool, a web dashboard, etc.).

The new `AdminService` is **read-only** in Phase 1. Mutating RPCs
(force-resign, etc.) are out of scope.

Considered and rejected:

- **(A) `rtid tui` subcommand** — local-only; doesn't work for
  remote `rtid` or containerized deployments.
- **(D) integrated TUI in `rtid`** — simpler for local dev but loses
  remote observation, multi-observer support, and the heterogeneous-
  consumer story (CLI / web / Prometheus). Door isn't closed: a
  thin `rtid --tui` wrapper that spawns `rti-top` against the local
  socket can land as a Phase-2 follow-up if local-dev ergonomics
  become a sticking point.

### 2.2 Data model — pull vs push

| Option | Sketch | Latency | Bandwidth |
|---|---|---|---|
| **Pull** | TUI calls `AdminService.Snapshot()` every Nms (default 1s) | Up to 1s stale | Snapshot overhead × N pulls/sec |
| **Push** | TUI subscribes to a `WatchStream`; rtid pushes deltas | Real-time | Constant; only deltas flow |
| **Hybrid** | Initial snapshot on connect, then subscribe to deltas | Real-time after snapshot | Optimal |

**Recommendation**: **Pull** in Phase 1 — simpler RPC, fits the
top-update-per-second cadence, no streaming complexity to debug. Move
to push (or hybrid) only if Phase 2 perf measurement shows pull
overhead matters.

### 2.3 TUI library

| Option | Pros | Cons |
|---|---|---|
| **bubbletea** (Charm) | MVU pattern; clean component model; popular for new Go TUIs (k9s-newer-versions, gh, glow); active community | Adds 4-5 deps |
| **tview** (rivo) | Battle-tested; what k9s built on; rich widget set | Dated style; less idiomatic |
| **tcell** (raw) | Minimal deps; full control | We'd build everything from scratch |

**Recommendation**: **bubbletea** + **lipgloss** (styling) +
**bubbles** (input/list/viewport widgets). Modern, well-maintained,
fits the model.

### 2.5 Admin listener — **PINNED: separate port**

Pinned 2026-05-05. The `AdminService` binds to a dedicated port via
a new `--admin-listen` flag, separate from the federate-facing
`--listen :8442` and the existing Prometheus `--metrics-listen :9090`.

**Default**: `--admin-listen localhost:8443`. Bound to `localhost`
by default so admin RPCs are not exposed on the network unless the
operator explicitly opts in (`--admin-listen 0.0.0.0:8443` for
remote rti-top access). This matches the existing convention of the
metrics endpoint and aligns with how etcd, envoy, kubernetes-apiserver,
and Prometheus separate "control" from "data" ports.

**Implementation**: `cmd/rtid/main.go` constructs a second
`grpc.NewServer()` instance for the admin service, listens on the
admin port, and registers the AdminService handler against it.
Shutdown wires both servers into the existing SIGINT/SIGTERM signal
handler.

**Trade made**: ~30 lines of additional listener wiring +
documentation surface for one more flag, in exchange for:
independent TLS / ACL when mTLS lands (cut-3 backlog item),
resource isolation (admin clients can't starve federate RPCs), and
safer defaults for the future mutating-ops phase.

### 2.4 Refresh cadence

| Option | Default | Trade |
|---|---|---|
| Fixed 1Hz | 1s | Familiar (top, htop) |
| Configurable via key (1, 2, 5, 10s) | 1s, key `r` to cycle | Lets users back off when remote |
| Adaptive (faster when activity, slower when idle) | varies | Complex; deferred |

**Recommendation**: configurable via key; default 1Hz; range
[100ms, 60s].

---

## 3. UI mockups

### 3.1 Federations view (default landing)

```
┌─ gorti rtid v0.1.0 ── uptime 3h22m ── 2 federations ── 8 federates ─────────┐
│                                                                              │
│ [F]ederations  [T]ime  [W]ire  [O]bjects  [S]ync  [I]nteractions  [Q]uit    │
├──────────────────────────────────────────────────────────────────────────────┤
│ FEDERATION         MODE         FEDERATES  PUB CLASSES  OBJECTS    TPS      │
│ ▶ demo             verbose          3        5             12     150.3    │
│   benchmark        best-effort      5        2              0    1947.2    │
│                                                                              │
└── ↑↓ select  Enter drill-down  R refresh-rate  / filter  Q quit ────────────┘
```

### 3.2 Federation drill-down (per-federate) — PINNED

**Default columns** (pinned 2026-05-05):
`name`, `handle`, `current_time`, `lookahead`, `role` (regulator /
constrained / both), `tps`, `queue_depth`, `drops_total`, `age`.

**Federate-expanded view (Enter on a row)**: identity + time +
pub/sub + wire stats. Save state, ownership pending, DDM regions,
sync labels — only shown when **non-empty** (collapsed sections that
auto-expand on activity).

No additional columns or fields beyond the above set in Phase 1.
Stall-risk indicators, last-activity timestamps, per-attribute
ownership history, and any field requiring data not currently on
the wire (notably `HLAfederateType`, which is a cut-3 backlog item)
are explicitly out of scope for Phase 1; revisit only when there's
operator demand.

```
┌─ Federation: demo ─ verbose ─ created 12m ago ──────────────────────────────┐
│                                                                              │
│ FEDERATE      HANDLE  TIME    LOOKAHEAD  ROLE          TPS    Q     DROP   │
│ ▶ generator      1     5.0      1.0      regulator      50    0     0      │
│   buffer         2     5.0      0.5      reg+const      50    4     6      │
│   processor      3     5.0      1.0      constrained    45    0     0      │
│                                                                              │
│ LBTS: 5.5    Pending grants: [generator @ 6.0]                              │
│ Sync points: [start_simulation: ✓ all achieved]                             │
│ Save state:  IDLE                                                           │
│ Region count: 0 (no DDM activity)                                           │
└── Esc back  ↑↓ select federate  Enter inspect  T time view  W wire view ───┘
```

### 3.3 Time-advance view

```
┌─ Time advance ─ Federation: demo ───────────────────────────────────────────┐
│                                                                              │
│ FEDERATE     CURRENT  PENDING  LOOKAHEAD  CONTRIBUTION  STATE                │
│ generator     5.0      6.0      1.0        6.0          awaiting LBTS≥6.0   │
│ buffer        5.0      —        0.5        5.5          idle (no request)   │
│ processor     5.0      —        1.0        —            constrained-only    │
│                                                                              │
│ LBTS: 5.5  (= min over regulators of current+lookahead)                     │
│                                                                              │
│ Time history (last 30 ticks, normalized):                                    │
│ generator  ▁▁▂▃▄▅▆▇█████████████████████████                                 │
│ buffer     ▁▁▂▂▃▃▄▄▅▅▆▆▇▇████████████████████                                │
│ processor  ▁▁▁▂▂▃▃▄▄▅▅▆▆▇▇████████████████████                               │
│                                                                              │
│ Strategies: time.lbts="default"  time.grant="default"                       │
│ Determinism mode: per-impl-opt-in  AllPreserving=true                       │
└──────────────────────────────────────────────────────────────────────────────┘
```

### 3.4 Wire stats view (the "top -like" core)

```
┌─ Wire stats ─ 1s window ─ all federations ──────────────────────────────────┐
│ FEDERATION  FEDERATE      SENDS   RECVS  REFLS  DROPS  Q-DEPTH  Q-MAX       │
│ demo        generator       50       0     0      0       0     8192        │
│ demo        buffer          25      50     0      0       4     8192        │
│ demo        processor        0      25     0      0       0     8192        │
│ benchmark   fed-A         1947    7788     0     12      31     8192        │
│ benchmark   fed-B         1944    7791     0      9      28     8192        │
│ benchmark   fed-C         ...                                              │
│                                                                              │
│ Total: 2022 sends/s  7813 recvs/s  12 drops/s                                │
│ Outbox utilization (max queue across all federates): ████░░░░ 39%           │
└── S sort  C column toggle  T 1s|5s|1m window  Esc back ─────────────────────┘
```

### 3.5 Event log tail

```
┌─ Event log ─ federation: demo ─ tail ────────────────────────────────────────┐
│ 5.000  InteractionSent     generator(1) → GenToBuffer  seq=1                │
│ 5.000  ReceiveInteraction  → buffer(2)  GenToBuffer  seq=1                  │
│ 5.500  InteractionSent     buffer(2) → BufferToProc  seq=1                  │
│ 5.500  ReceiveInteraction  → processor(3)  BufferToProc  seq=1              │
│ 6.000  InteractionSent     generator(1) → GenToBuffer  seq=2                │
│ ... (live tail; new lines appended; older scrolls off)                      │
│                                                                              │
└── F filter (class | federate | event-type)  P pause/resume  Esc back ──────┘
```

---

## 4. AdminService gRPC contract sketch

New proto file `proto/rti/v1/admin.proto`:

```proto
service AdminService {
  // Returns one consistent snapshot of every federation's state.
  // Pull-mode primary; cheap enough for 1Hz polling at modest scale
  // (~100 federates per federation).
  rpc Snapshot(SnapshotRequest) returns (SnapshotResponse);

  // Tail the event log of a single federation. Streams Event variants
  // as they're appended. Cancel via stream context.
  rpc TailEvents(TailEventsRequest) returns (stream Event);

  // Lightweight liveness probe. Returns rtid version + uptime.
  rpc Status(StatusRequest) returns (StatusResponse);
}

message SnapshotResponse {
  string rtid_version = 1;
  uint64 uptime_seconds = 2;
  repeated FederationSnapshot federations = 3;
}

message FederationSnapshot {
  string name = 1;
  Mode mode = 2;
  uint64 created_unix_seconds = 3;
  repeated FederateSnapshot federates = 4;
  TimeSnapshot time = 5;
  repeated string published_classes = 6;
  uint32 object_instance_count = 7;
  repeated SyncPointSnapshot sync_points = 8;
  SaveSnapshot save = 9;          // optional
  uint32 region_count = 10;
  WireStats wire = 11;
}

message FederateSnapshot {
  uint64 handle = 1;
  string name = 2;
  double current_time = 3;
  optional double pending_request_time = 4;
  double lookahead = 5;
  bool regulating = 6;
  bool constrained = 7;
  uint64 sends_total = 8;
  uint64 recvs_total = 9;
  uint64 drops_total = 10;
  uint32 outbox_queue_depth = 11;
  uint32 outbox_capacity = 12;
}

// ... similar for Time / SyncPoint / Save / Wire snapshots
```

The snapshot is built by walking every Manager (federation, time, sync,
ownership, ddm, savepoint, mom) and reading their public state. Adds a
`Snapshot()` method to each `core.<Service>` interface that Phase 1 of
the research-platform refactor extracted.

---

## 5. Implementation plan

| Phase | Deliverable | Risk | Effort |
|---|---|---|---|
| 0 | This doc + decisions pinned | none | DONE pending review |
| 1 | `proto/rti/v1/admin.proto` + AdminService handler in `transport/grpc` + per-Manager `Snapshot()` methods | low — additive | medium |
| 2 | `rti/cmd/rti-top/` binary: bubbletea TUI with the 5 views (federations, drill-down, time, wire, events) | medium — new TUI codebase | medium-large |
| 3 | Filter / search / sort / column toggle | low | small |
| 4 | (Optional) Event-log push streaming as a perf optimization for noisy federations | medium | medium |
| 5 | (Optional) Mutating ops — force-resign federate, kill federation. **Out of scope unless explicitly requested.** | high (correctness implications) | medium |

Each Phase-1 commit is independently revertable: one commit adds the
proto + handler stub returning empty snapshots, then one commit per
Manager fills in its share of the snapshot.

---

## 6. Determinism + perf considerations

- `Snapshot()` reads per-Manager state under the existing locks. At
  1Hz polling, the read-side cost is negligible (microseconds vs
  milliseconds of inter-poll idle). Quantify in Phase 1 with a
  microbenchmark to confirm.
- Pull is **deterministic-replay safe** — the AdminService doesn't
  emit events into the event log, so observation does not perturb
  replay.
- Event-log tail uses the existing `eventlog.MultiplexWriter`'s
  `OpenReader` (already deployed for the M4 replay path); no new
  persistence machinery.

---

## 7. Decisions

1. **Approach (§2.1)**: PINNED 2026-05-05 — **(B) separate `rti-top`
   binary** + AdminService gRPC. Optional Phase-2 follow-up: a thin
   `rtid --tui` wrapper that spawns `rti-top` against the local socket.
2. **Pull vs push (§2.2)**: PINNED 2026-05-05 — pull-only Phase 2.
   `Snapshot` polled at the configured refresh rate; `TailEvents` is
   streaming (server-stream), the one push channel that ships now.
   Revisit only if measurement shows 1Hz overhead matters.
3. **TUI library (§2.3)**: PINNED 2026-05-05 — bubbletea +
   lipgloss + bubbles (Charm). Adds 4–5 transitive deps; worth it
   for the MVU pattern + community + active maintenance.
4. **Default refresh rate (§2.4)**: PINNED 2026-05-05 — 1Hz default,
   configurable via key in [100ms, 60s] range. Familiar `top`-style
   ergonomics; tail values cover both fast-debug and remote-WAN.
5. **Out-of-scope reaffirmation**: REVISED 2026-05-06 — Phases 1-3
   shipped read-only. **Phase 5 unblocks mutating ops** under an
   explicit opt-in flag. AdminService stays read-only; a separate
   `MutatingService` ships ForceResign / DestroyFederation, gated
   by `--admin-mutating=true` (default false) and refusing to enable
   when the admin listener is bound to non-loopback addresses
   without explicit override. Phase 4 stays optional (high-rate
   event-log streaming improvements).
6. **Federate column set (§3.2)**: PINNED 2026-05-05 — default
   columns (`name`, `handle`, `current_time`, `lookahead`, `role`,
   `tps`, `queue_depth`, `drops_total`, `age`) and the expanded view
   (identity + time + pub/sub + wire stats; collapsed-when-empty
   sections for save / ownership / DDM / sync). No additions.
7. **Admin listener (§2.5)**: PINNED 2026-05-05 — separate port via
   `--admin-listen` flag, default `localhost:8443`. Independent
   `grpc.Server` instance from federate-facing `:8442`. Aligns with
   existing `--metrics-listen :9090` convention.

§7.1 + §7.6 + §7.7 are now pinned. Phase 1 (AdminService proto +
handler + per-Manager Snapshot methods + admin listener wiring) is
dispatchable. §7.2..§7.5 affect Phase 2 (the `rti-top` TUI itself)
and can be pinned later.

---

## 8. Non-goals (stated to prevent scope creep)

- A web dashboard (Prometheus + Grafana cover that with the existing
  metrics endpoint at `:9090`)
- Mutating control plane operations (kill / force-resign / drain)
- Authentication / RBAC for the admin endpoint (binds to localhost
  by default; production deployments add their own ACL via mTLS or a
  reverse proxy)
- Remote-attach against rtid running on a different host without a
  network path — bring your own VPN / SSH tunnel
- An IDE plugin or GUI app — TUI only
- Replacing the Prometheus metrics endpoint — that stays for
  alerting / Grafana dashboards. The TUI is for **live development**,
  Prometheus is for **production observability**.
