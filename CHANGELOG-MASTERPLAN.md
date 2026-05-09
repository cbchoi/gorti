# Master Plan Changelog

Append-only record of master-plan revisions. The orchestrator updates this after reading milestone status reports from all agents (`docs/reports/M<x>/agent-{a,b,c}.md`).

Entries are most-recent first. Each entry: date, summary of decision, link to the status reports that drove it.

---

## Release notes — curated by cut

Skim-able summary of what each cut shipped. The append-only log below has the full milestone-by-milestone audit trail.

### Cut 1 — MVP / walking skeleton (M0..M5; tag `mvp`)

**Goal**: smallest end-to-end system that demonstrates a real HLA federation. Federation lifecycle, pub/sub, object/interaction exchange, time-managed advancement (NER), deterministic replay, one Python federate built on a DEVS coupled model.

| Milestone | Deliverable |
|---|---|
| M0 | Repo skeleton; spec-test discipline; first orchestrator-frozen contract |
| M1 | FOM parser + cross-language byte-identical encoding (94 conformance vectors) |
| M2 | rtid Go server with federation/declaration/object/stream/eventlog services + gRPC handlers |
| M3 | Time management (NER + LBTS + stall timeout); 3-federate go-timed example |
| M4 | Python SDK with idiomatic asyncio API + pyjevsim bridge for DEVS↔HLA |
| M5 | Cross-language Python+Go federation; verbose+best-effort modes; perf baseline at sizes 2/5/25/100 |

### Cut 2 — Production-grade RTI service surface (M6..M11)

**Goal**: complete the IEEE 1516.1-2010 service surface so gorti is a real alternative to commercial RTIs (Pitch, MAK, Portico) for non-DDS workloads.

| Milestone | Deliverable |
|---|---|
| M6 | Hardening — cross-language handle alignment; gRPC TLS; EventLog Writer concurrency; real-pyjevsim structural adapter; in-process driver extraction |
| M7 | Time-advance primitives — TAR, TARA, FQR, NMRA |
| M8 | Synchronization points + 6-method ownership protocol (negotiated divest+acquire two-phase) |
| M9 | Federation save/restore — manifest format + filesystem storage backend |
| M10 | DDM — routing-space FOM parser, region lifecycle, overlap-driven SubscribersForUpdate, FR-DDM-6 zero-cost-when-empty |
| M11 | MOM runtime — HLAfederation + HLAfederate registration; lifecycle hooks across managers |

### Cut 3 — gRPC exposure, research platform, TUI, deeper polish (M12 + extensions)

**Goal**: make every cut-2 service group reachable over the wire; turn gorti into a research platform where alternative algorithms can be plugged in via TOML config; ship a top-style TUI for live federation observability; extend with HLAfederateType, structured save manifests, and event callbacks for sync/ownership/save.

#### M12 — Cut-2 service groups exposed via gRPC + Python SDK
- gRPC handlers for SyncService / OwnershipService / DDMService / SavepointService
- Python SDK Federate gains lazy `.sync` / `.ownership` / `.ddm` / `.savepoint` accessors
- Cut-3 deferrals: callback delivery for these services (closed later by proto Event variants), MOM gRPC exposure (closed later)

#### M12-close perf optimization pass
- 1.6×–3.8× throughput improvement across federation sizes 5/25/100
- Hoist inner proto + batched seq-alloc out of fanout loop
- Atomic-snapshot subscriber map (replaces RWMutex+map) in both perfOutbox and production multiOutbox
- Batched delivery (`chan []OutboundEvent` + per-recipient scratch + deferred-flush timer)

#### Research platform refactor (Phases 0–4)
- Phase 0 design doc; pinned: in-tree alternatives selected by TOML config; per-impl-opt-in determinism contract; time + ownership for Phase 2 focus
- Phase 1 — `core.<Service>` interfaces extracted for the 6 concrete-only Managers (sync, ownership, mom, ddm, savepoint, declaration)
- Phase 2 — algorithm-level strategies: `time.LBTSStrategy`, `time.GrantStrategy`, `ownership.NegotiationStrategy`
- Phase 3 — `internal/research` strategy registry + TOML config + `--research-config` flag in cmd/rtid + determinism gate
- Phase 4 — three reference alternative impls: `time.alt_maxprojected` (LBTS), `time.alt_eagergrant` (Grant), `ownership.alt_randomacquirer` (Negotiation, non-preserving — exercises the strict-mode rejection path)
- Worked-example how-to at `docs/research-platform-howto.md`

#### rtid TUI — top-style live federation observability (Phases 1–5)
- Phase 1 — `AdminService` gRPC: `Snapshot` / `TailEvents` / `Status` + per-Manager `Snapshot()` methods + `--admin-listen localhost:8443` flag (separate from federate `:8442` and Prometheus `:9090`)
- Phase 2 — `rti-top` binary using bubbletea + lipgloss + bubbles; five views (Federations / Drilldown / Federate-detail / Time / Wire / Events); 1Hz default refresh
- Phase 3 — filter / sort / column toggle + Wire-view client-side rate windows + age column populated
- Phase 4 — TailEvents server-side filter + batched response + backpressure overflow counter
- Phase 5 (opt-in) — separate `MutatingService` for `ForceResign` / `DestroyFederation` gated by `--admin-mutating=true` (refuses non-loopback bind without explicit override; prominent WARN log when enabled)

#### Federation-developer examples (in addition to the original go-pingpong / go-timed / pyjevsim producer-consumer)
- `examples/pyjevsim-relay/` — 3-federate Generator → Buffer → Processor with drop-on-overflow accounting
- `examples/pyjevsim-dashboard/` — Sensor + Dashboard (object instances + reflect callbacks; bypasses the bridge — Path A reference)
- `examples/pyjevsim-dashboard-bridged/` — same example using the bridge (Path B); requires bridge object-class extension landed in the same arc
- `examples/pyjevsim-time-advance/` — three regulators with different lookaheads; LBTS sparkline
- `examples/pyjevsim-sync-points/` — canonical HLA bootstrap rendezvous
- `examples/pyjevsim-relay-cross-process/` — production-shape: rtid subprocess + 3 separate Python federate processes

#### Cut-3 backlog items closed
- Proto `FederateEvent` variants for sync/ownership/save (M12 W2 deferral #1)
- MOM gRPC exposure (`MomService.QueryFederationAttributes` / `QueryFederateAttributes` / `EnumerateMomInstances`)
- M13 — three threads: `federation.Manager.MembersOf` accessor (closes save callback delivery in production), `HLAfederateType` plumbing through `JoinFederationRequest`, structured per-manager state snapshots in the save manifest (sync/ownership/mom/ddm Marshal+Unmarshal)
- pyjevsim 1.3.1 → 2.0.1 dependency upgrade (API-compatible at every surface the bridge consumes)
- Bridge object-class extension — `ObjectClassFederateProtocol` sibling to `CoupledModelProtocol`
- Documentation site infrastructure — MkDocs Material + GitHub Pages (`https://cbchoi.github.io/gorti/`)

#### M23 — ObjectManagement (§6) + DDM (§9) completion (closed 2026-05-09)
- Two distinct gap closures in one milestone, identified by post-M22 audits.

§6 Object Management (W1-W3):
- `delete_object_instance` + `RemoveObjectInstance` callback wired end-to-end. The proto slot at `stream.proto:33` (FederateEvent.remove tag 12) had been declared since M0 but had ZERO consumers — no manager method emitted it, no wire conversion built it, no SDK delivered it. Object instances could never be deleted. M23 W1 closes this with `Registry.Delete` (rti/internal/object/delete.go, NEW), wire handler, both SDKs, and event delivery.
- `local_delete_object_instance` (§6.18) — federate-local cleanup; cut-1 simplification keeps global state intact.
- `request_attribute_value_update` (§6.24) + `request_class_attribute_value_update` (§6.25) + `ProvideAttributeValueUpdate` callback (FederateEvent oneof tag 15, NEW). The standard HLA "late joiner pulls initial state" pattern.
- `change_attribute_transportation_type` (§6.20) + `change_interaction_transportation_type` (§6.22) — per-instance / per-publisher transport overrides. Recorded in `transportStore` (rti/internal/object/transport.go, NEW); wire-level transport switching deferred to a later cut (the multi-Outbox path doesn't yet route per-message transport).

§9 DDM (W4-W5):
- Go SDK DDM coverage — `rti/pkg/federate/ddm.go` (NEW). Pre-M23 the Go SDK had ZERO DDM methods while pysdk had 10; W4 mirrors all 10 onto the Go SDK so cross-language feature parity is restored.
- 5 missing §9 services + the wire for the existing manager method: `AssociateRegionsForUpdates`, `UnassociateRegionsForUpdates`, `UnsubscribeObjectClassAttributesWithRegions`, `UnsubscribeInteractionClassWithRegions`, `SendInteractionWithRegions`, `RequestAttributeValueUpdateWithRegions`. Manager additions in `rti/internal/ddm/missing_services.go` (NEW).

Errors:
- 4 new `core.Err*` sentinels: `ErrObjectNotOwned`, `ErrAttributeNotPublishedByFederation`, `ErrObjectAlreadyDeleted`, `ErrTransportTypeUnspecified`. Pysdk typed exceptions at codes 710-713 continuing M22's 700-709 range.

Out of scope (deferred to M24+):
- §6 name reservation (`reserveObjectInstanceName` family — §6.2-§6.8).
- §6 order-type changes (`change_*_order_type` — §6.27, §6.28). Distinct from transport-type.
- §6 `attributes_in_scope` / `attributes_out_of_scope` callbacks.
- §7 ownership-resign correctness (`releaseAllOwnedBy`); enables proper resign actions.

Spec tests: 14 in `rti/spec/M23/` + 19 in `pysdk/tests/spec/m23/`. All green.

Frozen plan + 28 tasks (TASK-246..273) at `docs/M23_DISPATCH_PLAN.md`.

#### M22 — TimeService completion (closed 2026-05-09)
- Closes the four documented M21 carryovers in one milestone:
  1. Pysdk surface parity — Federate gains 12 time methods (TAR, TARA, NMRA, FQR, ModifyLookahead, 3 queries, async-delivery pair, plus the disable variants); Rti1516eAmbassador exposes 15 corresponding camelCase methods. M21 W3B had only flipped NER from no-op to real; the rest were wire-reachable but absent from the Python surface.
  2. `enable/disableAsynchronousDelivery` per IEEE 1516.1 §8.16-8.17 — proto extension (W2: 2 RPCs, append-only), manager state (`asyncDelivery bool` + per-federate `tsoBuffer` on nerState), wire handlers, both SDKs, typed pysdk exceptions (codes 708/709). Default = false per spec; existing examples that produce TSO without time-advance call `enable_asynchronous_delivery` on join (migration audit at plan §8 — the dashboard examples are the only ones affected).
  3. NER+forced-grant race — diagnosed as SDK-side semantics, not a server bug. Forced grants (`clearPending=false` in `advance.go::decideGrant`) leave the federate in time-advancing state per spec; reissuing an advance primitive correctly returns `ErrTimeAdvancingState`. M21 examples treated forced grants as cycle completion, then worked around the symptom (Go: TAR + 5 ms settle delay; Python: retry-on-TimeAdvancingState backoff). M22 W3 lands `waitForFullGrant`/`wait_for_full_grant` that accumulates forced grants and returns only on the full grant; both M21 workarounds are gone.
  4. Spec-test parity — `rti/spec/M22/` (8 tests across async_delivery + ner_forced_grant_race + ner_full_grant + time_service_completion) and `pysdk/tests/spec/m22/` (72 tests).
- New `core.TSODeliveryGate` interface; `*time.Manager` satisfies it. `object.Registry.Options.TSOGate` consults the gate before TSO `Outbox.Send` for both `fanoutReflect` (object updates) and `fanoutReceive` (interactions); RO events bypass. `cmd/rtid/main.go` wires `timeMgr` as the gate. When TSOGate is nil (test fixtures), behavior is pre-M22 always-async — backwards-compatible.
- Buffer is per-federate, FIFO, unbounded. Buffer cap + persistence are M23 follow-ups.
- Frozen plan + 25 tasks (TASK-221..245) at `docs/M22_DISPATCH_PLAN.md`.
- Runner config trade-off: examples/go-timed/ kept all-TAR for the multi-federate runner (NER with mismatched lookaheads in lockstep hits a scheduling-edge stall not in scope for M22). The SDK fix (`waitForFullGrant`) is the correct generalization for any NER/NMRA federate; TAR remains the right default for synchronized cycles.

#### M21 — Complete TimeService gRPC wiring (closed 2026-05-07)
- Closes the cut-1 / cut-2 time-service gap: NER was the only advance primitive with a wire path; TAR / TARA / NMRA / FQR / queries returned `Unimplemented`. Pysdk's time RPCs short-circuited as no-ops because of this.
- Proto extension (W1, append-only): `proto/rti/v1/time.proto` gains TAR / TARA / NMRA / FQR / ModifyLookahead + 3 query RPCs (QueryLogicalTime / QueryLookahead / QueryLBTS), plus 10 new message types
- Wire adapter (W2A): `rti/internal/transport/grpc/time.go` registered in rtid composition; 13 RPC handlers wrap `*time.Manager`. `Manager.ModifyLookahead` added (the one new manager mutator — append, no semantic change)
- Grant/halt event delivery (W2B): `toFederateEvent` extended to type-switch on `*timepkg.TimeAdvanceGrant` and `*timepkg.FederationHalted`; `Manager.OnFederateResign` chained via federation manager so resign-during-pending cleans up cleanly
- Go SDK time surface (W2½ + W3A): `rti/pkg/federate/` (Connection / Federate / events) + `time.go` exposes 13 time-management methods; bufconn-driven test fixture for SDK tests with no rtid subprocess
- Pysdk flip (W3B): `pysdk/rti1516e/_transport.py`'s 3 NER methods flip from no-op to real dispatch; `TimeServiceStub` wired; 8 typed exceptions added at codes 700-707 (`TimeRegulationAlreadyEnabled`, ..., `TimeAdvancingState`, `FederationHaltedError`)
- Showcase examples restored (W4A + W4B): `examples/go-timed/` (3 Go federates, lookaheads {0.5, 1.0, 2.0}, all-TAR after the NER+forced-grant race surfaced) and `examples/pyjevsim-time-advance/` (3 Python regulators with NER + retry-on-`TimeAdvancingState` backoff)
- Acceptance gate (W5): `rti/spec/M21/time_service_test.go` (8 spec tests binding AC §3 invariants) + `pysdk/tests/spec/m21/test_time_service_cross_language.py`; cut-3 README "no time-managed variant" caveats struck in `pyjevsim-relay-cross-process` / `pyjevsim` / `pyjevsim-sync-points` / `pyjevsim-dashboard-bridged` README files (they now point to `examples/pyjevsim-time-advance/` for the time-managed reference)
- Two known-narrow workarounds carried forward as M21 follow-ups: the NER `clearPending=False` race on sole-pending forced grants is documented in `regulator_main.py` and the Go example sidesteps it by switching to TAR; pysdk currently exposes NER only (TAR / TARA / NMRA / FQR Python surface is post-M21 ergonomics, not a semantic gap — the wire path works)
- `enableAsynchronousDelivery` / `disableAsynchronousDelivery` and lookahead-zero / optimistic time variants explicitly NOT in M21 (deferred to M20)
- Frozen plan + 22 tasks (TASK-201..220) at `docs/M21_DISPATCH_PLAN.md`

#### M19 — DDS / RTPS data plane adapter (Phase 0 + Phase 1a; halted clean)
- Phase 0 design doc at `docs/m19-dds-adapter.md` — architecture, library + binding choices, QoS mapping, distribution model, phasing
- §6.1 / §6.2 / §6.3 / §6.5 PINNED: Cyclone DDS / hand-rolled minimal CGo / `cyclonedds-python` / build-tag-gated split (default `rtid` stays CGo-free + DDS-free)
- Phase 1a foundation:
  - Proto extensions (append-only): `TransportMode` enum + `transport_mode` field on `CreateFederation`/`JoinFederation` + `dds_domain_id`
  - Federation manager records mode + domain id; surfaces via Snapshot
  - `cmd/rtid` `--enable-dds` + `--dds-domain-id` flags; default rtid rejects DDS-mode federations cleanly
  - `rti/internal/transport/dds/` build-tag-gated package: stub Participant/Topic/Writer/Reader returning `errors.ErrUnsupported` + pure-Go QoS mapping for the four core HLA combos
  - Makefile `make build-dds` / `make test-dds`
  - rti-top federation drilldown shows transport mode
  - 5 spec-test files under `rti/spec/M19/` documenting the contract; `dds_smoke_test.go` skips with Phase 1b reason
- **Default `bin/rtid` byte-identical to pre-M19** (no CGo, no DDS imports in the default code path)
- Phase 1b–5 NOT in flight — halted at Phase 1a per the design doc §12 (mission halt state, 2026-05-07). Next session needs Cyclone DDS available in the build environment to unblock Phase 1b's CGo implementation; from there Phases 2–5 unlock in turn

### Cut-3 backlog still open

Future cuts per `docs/srs.md` §10.4:

- **M14** — mTLS + OIDC client authentication
- **M15** — Distributed RTI: multi-process federation hosting
- **M16** — Hot standby + replay-driven RTI failover
- **M17** — C++ federate SDK
- **M18** — Java federate SDK
- **M19** — DDS/RTPS data plane adapter — **Phase 1b onwards** (Phase 0 + Phase 1a foundation already on main; resumes when build env has Cyclone DDS — see `docs/m19-dds-adapter.md` §11.2 for the concrete pickup steps)
- **M20** — MOM-driven control services + optimistic time variants

Plus smaller carryovers: restore callback variants on `FederateEvent` (mechanically symmetric to save variants), MOM-class subscription path through the standard `ObjectService`, suite-load timing flake on the synchronized-callback integration test.

---

## 2026-05-03 (deps: pyjevsim 1.3.1 → 2.0.1 — pin bump, no bridge changes)

**Upgraded the bridge's pyjevsim dependency from `==1.3.1` to `==2.0.1`.**
2.0.1 was published 2026-05-05 (alongside 2.0.0) and is the new latest on
PyPI. The upgrade ships as a pin bump only — no bridge code or adapter
code needed to be adapted. The relevant API surface is unchanged.

**API delta investigated by diffing the wheels and reading the source.**
Same module list (23 modules in both releases). 10 modules changed
content; the changes are internal-only optimisations and additive
features:

| Module | Change shape | Bridge impact |
|---|---|---|
| `__init__.py` | identical | none |
| `behavior_executor.py` | hot-path attribute snapshots; new `con_trans(port_msgs)` method | none — bridge never invokes con_trans |
| `behavior_model.py` | new `con_trans` default impl (δ_int ; δ_ext per std DEVS) | none |
| `executor.py` | removes `__lt__` (heap ordering moved to ScheduleQueue) | none — bridge doesn't compare executors |
| `message_deliverer.py` | docstring only (clarifies by-reference semantics) | none |
| `schedule_queue.py` | full rewrite to heapset (heap of unique timestamps + dict[time→set(executor)]); `pop_all_at(time)` added | none — same `push/pop/peek_time/remove` public API |
| `structural_executor.py` | uses ScheduleQueue + cached `_obj_id` | none |
| `structural_model.py` | docstrings only | none |
| `system_executor.py` | new `track_uncaught=False` kwarg (additive); `step()` rewritten as full Parallel-DEVS two-phase tick that advances `global_time` round-by-round and ends at `granted_time` per IEEE 1516-2010 (was: set `global_time=granted_time` upfront then drain at fixed time); explicit `HLA_TIME` execution-mode docs | none — bridge's `RealPyjevsimStructuralAdapter` was already using `step(0)` for entity bootstrap and `step(global_time + dt)` per cycle, both of which behave identically under the new round semantics |
| `system_message.py` | docstring only | none |
| `system_object.py` | drops `datetime.now()` field on `SystemObject` (perf) | none — never read by the bridge |

**Notably preserved (the bridge depends on these and they did not change):**

- Top-level `pyjevsim` exports: `StructuralModel`, `BehaviorModel`,
  `SysExecutor`, `SysMessage`, `ExecutionType`, `Infinite`.
- Method names the adapter calls: `output`, `int_trans`, `ext_trans`,
  `register_entity`, `coupling_relation`, `insert_external_event`,
  `insert_input_port`, `step`, `get_next_event_time`,
  `get_global_time`, `set_output_event_callback`,
  `output_event_queue`, `insert_state`, `insert_input_port`,
  `insert_output_port`, `init_state`, `get_models`, `get_couplings`.
- The `SysExecutor.single_output_handling` bug
  (`msg[1].retrieve()` on a non-subscriptable `SysMessage` when the
  destination is the executor itself) is unchanged in 2.0.1. The
  structural adapter's sink-leaf workaround remains required.
- `select_preserve.py` was already a deterministic stand-in (sort by
  port name) — pyjevsim's per-coupled-model `select()` is still
  outside the W6 `CoupledModelProtocol` contract, so the upgrade
  changed nothing here.

**Files touched** (pin bump + comment freshening only):

- `pysdk/pyproject.toml` — `pyjevsim==1.3.1` → `pyjevsim==2.0.1`,
  comment block rewritten to summarise the 2.0.1 surface.
- `README.md`, `docs/quickstart.md` — install line bumped.
- `pysdk/pyjevsim_bridge/_protocol.py`,
  `pysdk/pyjevsim_bridge/select_preserve.py`,
  `examples/pyjevsim/_real_pyjevsim_adapter.py` — docstring/comment
  refreshes to note 1.3.x and 2.0.x parity. No code changes.

**Test status (final)**:

- `python3 -m pytest pysdk/tests/` — 498 passed.
- Per-example: `examples/pyjevsim/` 3 passed, `examples/pyjevsim-relay/`
  6 passed, `examples/pyjevsim-dashboard/` 7 passed,
  `examples/pyjevsim-time-advance/` 8 passed,
  `examples/pyjevsim-sync-points/` 7 passed (31 example tests total).
- `go test -race -count=1 ./...` — clean across 30+ packages.
- M5 + M12 cross-language tests — green (covered by the pysdk suite).
- Smoke run of `python3 examples/pyjevsim/runner.py` — 5-tick
  producer/consumer round trip succeeds, payload sequence
  `[1, 2, 3, 4, 5]` delivered.

**Deferrals / not integrated:**

- 2.0.1's new `BehaviorModel.con_trans` (Parallel-DEVS confluent
  transition) and `ExecutionType.HLA_TIME` mode are NOT plumbed
  through the bridge. The current `HLAFederate` cycle is sequential
  (output → send → int_trans, or external_transition) which matches
  how IEEE 1516-2010 NER grants are surfaced — confluence at the
  same simulated instant collapses to either internal-only or
  external-only per the `t < t_request` check in `step_once`. Wiring
  `con_trans` would let a federation observe simultaneous internal +
  external events as a single δ_con instead of two ordered
  transitions, but it requires a CoupledModelProtocol shape change
  that is out of scope for a pin bump. Filed-in-comment for the next
  bridge revision.
- `track_uncaught` on `SysExecutor` is left at its default (False);
  the structural adapter does not register any models with dangling
  outputs, so the diagnostic adds no value at present.

---

## 2026-05-05 (post-M12 perf pass — outbox optimization, 1.6–3.8× throughput)

**Single-session optimization pass on the in-process perf harness, profile-driven.** Pre-pass revert tag: `perf-baseline-m12` at commit `3078f06` (M12 close). Full report in `docs/reports/perf/M12-optimization-pass.md`.

**Method**: profile baseline → identify hotspots → fix one at a time → benchmark each step → commit. `runtime.procyield` started at 48% flat (lock contention); `(*Registry).buildReceiveEvent` 65% of allocs (per-subscriber proto + map copy + lock acquire); `(*perfOutbox).Send` 28% cum (RWMutex + channel send).

**Optimizations** (each independently revertable):

| Commit | Change | Effect |
|---|---|---|
| `e01a6d3` | Hoist inner proto + map copy + batched seq-alloc out of `fanoutReceive` / `fanoutReflect` per-subscriber loop | 65% of allocs eliminated; lock acquires drop from N/fanout to 1/fanout |
| `890e18a` | `perfOutbox`: replace RWMutex+map subscriber lookup with `atomic.Pointer[map]` + copy-on-write | Send becomes lock-free atomic load |
| `a191bfd` | `cmd/rtid/multiOutbox`: same atomic-snapshot pattern (production gRPC outbox) | Production wire path matches perf harness lock-free Send |
| `b8489f3` | `perfOutbox`: switch channel element from `OutboundEvent` to `[]OutboundEvent` + per-recipient scratch + flush-on-batchSize=32 | Channel ops drop ~32× |
| `7ca819d` | Promote batched delivery to production: `SubscribableOutbox.Subscribe` returns `<-chan []core.OutboundEvent`; updated `multiOutbox`, `streamService.Events`, `syncOutbox`, all test fakes; added deferred-flush timer (`time.AfterFunc`, 1 ms) so low-rate workloads don't stall in scratch | Production gRPC wire path now amortizes channel ops across batches |

**Throughput** (in-process perf harness, isolated runs, 5s, 12-core i7):

| Size | Baseline | Final | Speedup |
|---:|---:|---:|---:|
|   5 | 1,238,820/s | 1,956,160/s | **1.58×** |
|  25 |   252,575/s |   966,753/s | **3.83×** |
| 100 |    71,347/s |   259,624/s | **3.64×** |

**Trade-offs documented**:
- Size-5 throughput drops 19% from peak (no-flush opt 1+2+4 was 2.85M/s) once the deferred-flush timer is added. The flush timer is a correctness-required mechanism — without it, low-rate production federations stall `batchSize/sender_rate` seconds. Size-25/100 shed ~5-7% similarly.
- Latency at size 25 improves substantially: p50 9.60 ms → 0.04 ms, p99 61 ms → 1.76 ms (shorter apparent queue depth as fewer items are in flight).
- Residual hotspot is now `runtime.selectgo` / `chansend` machinery itself — the unavoidable inter-goroutine communication cost. Further gains require architectural changes (lock-free MPSC queues, sharded outbox); marked out of scope.

**Verified at every step**: `go test -race ./...` clean across all 30 packages; cross-language M5 + M12 cross-process tests green (Python federate ↔ rtid subprocess over real gRPC); pysdk 498-test suite green; M0..M12 spec tests stable.

**Untouched**: production cmd/rtid main.go's `newMultiOutbox(1024)` call — still uses default `batchSize=32`, `flushInterval=1ms`. `newMultiOutboxWithBatch(...)` is the explicit-knobs constructor for tests and future tuning.

---

## 2026-05-05 (M12 — DONE; cut-3 service groups reachable over gRPC)

**`scripts/check-milestones.sh` reports M12: DONE.** First cut-3 milestone closed. M12 wires gRPC handlers + Python SDK exposure for the four cut-2 service groups (sync, ownership, DDM, savepoint) that had been internal-only at the close of cut-2 — closing the biggest user-visible gap from cut-2.

**Wave summary** (merged to `main` in dispatch order):

| Wave | Sub-agent | Bundled scope | Outcome |
|---|---|---|---|
| **W1** (Go side) | A | gRPC handlers for SyncService (2 RPCs), OwnershipService (8 RPCs), DDMService (10 RPCs), SavepointService (7 RPCs); Options extension on `transport/grpc.Server`; `errs.go` extended with 16 cut-2 sentinel mappings; composition root in `cmd/rtid` wires the 4 managers | 5/5 spec tests in `rti/spec/M12/`; merged via `d2fbe1b` |
| **W2** (Python SDK) | C | `Federate.{sync,ownership,ddm,savepoint}` lazy property accessors; 4 client modules + `_grpc_errors.py` typed exception layer; `_transport.py` upgraded from record-only to real RPC for pub/sub/register/update via FOM-cached attribute name→handle resolution; cross-process test harness (`_helpers.py` with `RtidProcess` async cm + 2-federate scenarios) | 4/4 spec tests in `pysdk/tests/spec/m12/` over real subprocess-spawned rtid; 498-test pysdk suite stays green |
| **Cleanup** | orchestrator | Fixed `rti/pkg/fom/mim/merge.go:166` `NewFOM` → `NewFOMWithDimensions` (Agent C deferral #2 — was silently dropping user `<dimensions>` post-merge); added `TestSpec_Merge_UserDimensionsPreserved` regression; reverted unintended smart-quote rewrite in `rti/cmd/rtid/main.go` doc comment from W1 | 1 critical bug fix; 1 cosmetic revert |

**M12 stats**:
- 2 sub-agents (A + C)
- ~3.5k LoC added (Go: 4 handler files + Options + sentinels + 5 round-trip tests; Python: 4 client modules + transport upgrade + helpers + 4 integration tests; 2 status reports)
- 5/5 Go spec tests + 4/4 Python spec tests = 9 net test additions
- 0 RED across M0..M12; 0 new skips
- Critical bug found mid-flight (merge.go dimensions drop) and closed in cleanup

**Notable architectural choices made during M12**:
- **Optional Options pattern** — cut-3 services use the same nil-permissive contract as the M3 `timeService`: nil → service simply not registered, mirroring the long-standing precedent so older test harnesses keep compiling unchanged.
- **Two-step DDM register-with-regions** — the §6.7 fused call straddles `object.Registry` and `ddm.Manager`. Go handler returns `object_handle=0` by design; Python SDK's `DDMClient.register_object_instance_with_regions` performs the two-step (ObjectService.RegisterObject → DDMService.RegisterObjectInstanceWithRegions). Cross-package straddle deferred.
- **Cross-process test harness as a reusable component** — Agent C extracted `RtidProcess` async cm + 2-federate fixtures into `pysdk/tests/spec/m12/_helpers.py` with robust subprocess teardown. Pattern for future cross-language tests.
- **Wire-callback gap accepted as cut-4 scope** — proto Event variants don't include sync/ownership/save event types, so federate-side callbacks for those services can't ride EventLog yet. Tests work around via Query RPCs (`query_attribute_ownership`, `query_save_state`, `query_restore_state`) and round-trip success assertions.

**Cut-4 backlog** (deferred from M12):
- Proto Event variants for sync/ownership/save (enables callback delivery + replay byte-determinism)
- MOM gRPC exposure (M12 explicitly scoped MOM out — Go runtime exists but no wire surface)
- Multi-federate save aggregation requires `MembersResolver` wiring (single-federate round trip works today)
- Restore-side federate-handle parity (rejoin under new handle fails membership check; same root cause as MembersResolver gap)
- Per-manager state snapshots in save bundle manifest (currently event-log slice is the FR-SR-5 vehicle; carried over from cut-2 backlog)
- Optimistic time advance variants beyond TARA, mTLS+OIDC, distributed RTI, non-Python SDKs, DDS adapter (carried over from cut-2 backlog)

**What gorti is now**: a complete IEEE 1516-2010 RTI with all cut-2 service groups reachable from federates of any supported language (Python today; C++/Java/C# = cut-4). The biggest cut-2 caveat — "internal-only sync/ownership/DDM/savepoint" — is closed. Production-deployable for HLA workloads of up to ~100 federates without DDM, ~25 with active DDM regions. Cross-language Python+Go federations supported via the SDK's full cut-3 service surface.

---

## 2026-05-03 (Cut 2 — DONE; M6..M11; production-grade RTI achieved)

**`scripts/check-milestones.sh` reports M0..M11: DONE.** Cut-2 closes the IEEE 1516.1-2010 service surface (modulo cut-3 deferrals: optimistic time variants beyond TARA, MOM-driven control services, mTLS+OIDC, distributed RTI, non-Python SDKs, DDS adapter).

**Cut-2 milestone summary** (all merged to `main` in dispatch order):

| Milestone | Sub-agents | Bundled scope | Outcome |
|---|---|---|---|
| **M6** (hardening) | W1A handle align + W1B concurrency+TLS + W1C RememberFor + W2 (TLS+pyjevsim+replay+driver) | Cross-language handle alignment, EventLog Writer concurrency, gRPC TLS server+client, real-pyjevsim structural adapter, M4 replay path, in-process driver extraction | Last skipped pysdk spec test → PASS; 0 RED; 0 scaffold-skips |
| **M7** (time primitives) | W1 single-agent | TAR + TARA + FQR + NMRA, all sharing M3 LBTS machinery | 9/9 spec tests; 17 unit tests; 20-scenario determinism harness |
| **M8** (sync + ownership) | W1 single-agent | sync points (register/announce/achieve/synchronized), 6-method ownership protocol incl. negotiated divest+acquire two-phase | 9/9 spec tests; 30 unit tests |
| **M11** (MOM runtime) | W1 single-agent | HLAfederation + HLAfederate runtime registration; lifecycle hooks across federation/time/object managers | 5/5 spec tests; 16 unit tests |
| **M10** (DDM) | W1 single-agent (cross-territory: Agent A + B) | Routing-space FOM parser, region lifecycle, overlap-driven SubscribersForUpdate, object.Registry integration with FR-DDM-6 zero-cost-when-empty contract | 5/5 spec tests; 11 unit tests; perf 1.45 ns/op zero-cost path; ~116 µs/op with 100×25 region matrix |
| **M9** (save/restore) | W1 single-agent | requestFederationSave + initiateFederateSave aggregation + federationSaved emission; Storage interface (in-mem + filesystem); manifest format documented in sdd.md §9 | 6/6 spec tests; 19 unit tests |

**Cut-2 stats since MVP**:
- 6 milestones (M6..M11) closed
- ~6 wall-clock hours sub-agent compute
- ~14k LoC added (Go packages: `internal/sync`, `ownership`, `mom`, `ddm`, `savepoint`; FOM parser dimension extension; spec tests for 6 milestones; 6 status reports)
- 0 RED spec tests across ALL milestones (M0..M11)
- ~36k → ~50k total project LoC
- Cut-2 dispatch model proven: orchestrator pre-work + single Agent A sub-agent per milestone (most cases) is significantly leaner than M2/M3/M4's 4-7-wave split, while staying clean

**Notable architectural choices made during cut-2**:
- **Optional Manager hooks pattern**: every cut-2 service group adds an `OnXSuccess` hook to the relevant cut-1 manager's Options (additive, nil-default = preserves cut-1 behavior). Composition root in `cmd/rtid/main.go` wires the hooks. Avoids reshaping M0-frozen interfaces.
- **MOM/DDM/Sync/Ownership all decline EventLog persistence** (cut-1 of cut-2): proto Event variants don't include these new event types. Replay byte-determinism for these transitions deferred to cut-3 (or when proto unfreeze happens).
- **Cross-agent FOM-parser extension** (M10): the orthogonality table reserves `rti/pkg/fom/` for Agent B, but cut-2's milestone-bundled scope permits cross-territory work when documented. Agent A added `<dimensions>` parsing for M10's routing-space declarations.
- **Save bundle format documented as sdd.md §9** (M9): manifest header (JSON) + length-prefixed event-log slice. Filesystem-backed storage one file per (fed, label) bundle; in-memory variant for tests.
- **Dynamic-mode aggregation pattern** (sync + savepoint): when no MembersResolver is provided, "any federate that responds counts" (cut-1 simplification). Production rtid leaves Members nil pending federation.Manager.MembersOf accessor (cut-3 work).

**Cut-3 backlog** (deferred from cut-2):
- gRPC handlers for sync/ownership/MOM/DDM/savepoint (proto extension required)
- Per-manager state snapshots in save bundle manifest (currently event-log slice is the FR-SR-5 vehicle)
- Federation membership accessor (federation.Manager.MembersOf)
- HLAfederateType plumbing (proto JoinFederationRequest extension)
- MOM-driven control services (HLAsetSwitches etc. as interactions)
- Optimistic time advance variants beyond TARA
- mTLS / OIDC client auth
- Distributed RTI / hot standby
- C++ / Java / C# federate SDKs
- DDS/RTPS data plane adapter

**What gorti is now**: a complete IEEE 1516-2010 RTI implementation covering all of §4-7 + §10 (everything except DDS data plane and the cut-3 backlog above). Production-deployable for HLA workloads of up to ~100 federates without DDM, ~25 federates with active DDM regions. Cross-language Python+Go federations supported.

---

## 2026-05-03 (M5 — DONE; MVP achieved; 3 waves, 6 sub-agents, first multi-agent milestone)

**`scripts/check-milestones.sh` reports `M0..M5: DONE`. Project MVP gate passed.**

M5 closed in one session. First multi-agent milestone — Wave 1 dispatched 3 sub-agents concurrently across Agents A, B, C (path ownership fully disjoint per `docs/ORTHOGONALITY.md` §2). Final spec test count: 0 RED across all milestones (1 documented skip awaiting cross-language handle alignment, deferred to M6).

**Wave summary** (all merged to `main` in dispatch order):

| Wave | Sub-agent | Agent | Tasks | Outcome |
|---|---|---|---|---|
| W1A | mode + best-effort | A | TASK-076 + TASK-077 | `--federation-mode` CLI flag; best-effort RO delivery via `object.AttributeOrderLookup`; no `core.FOMHandle` contract change (used optional interface assertion in `transport/grpc.FOMOrderResolver`) |
| W1B | determinism audit | B | TASK-083 | Audit clean; 0 critical, 2 minor non-blocking findings filed as issues #2 + #3 |
| W1C | cross-language smoke | C | TASK-081 | Real-gRPC transport in Python SDK + RealPyjevsimAdapter + Python+Python cross-process smoke. Bidirectional Python+Go deferred to M6 |
| W2A | hardening + perf | A | TASK-078 + TASK-079 + TASK-080 | Soak smoke (245k calls in 5s, 0 panics, 0 leaks); perf baseline at all 4 sizes (size 2 → 3M i/s p99 0.13ms; size 100 → 49k i/s p99 34ms); encoding share <0.3% CPU → TASK-084 confirmed CANCELLED |
| W2B | modes verification | C | TASK-082 | Verbose TSO PASS; best-effort RO documented skip pending cross-language handle alignment |
| W2C | FOMOrderResolver follow-up | A | (cleanup, ~60 LoC) | Production `*fomHandle` now implements `FOMOrderResolver`; Go-side best-effort RO verified by `rti/spec/M5/best_effort_test.go` |

**Critical-path wall time**: ~80 min sub-agent compute. W1 in parallel ~21 min (W1A 14, W1B 8, W1C 19); W2 in parallel ~21 min (W2A 21, W2B 12); W2C cleanup 5 min; orchestrator close 10 min.

**M5 exit criteria** (per `docs/srs.md` §10.2):

| Criterion | Status | Evidence |
|---|---|---|
| Cross-language federation works | ✓ | `pysdk/tests/spec/m5/test_spec_m5_cross_language.py` (Python+Python over real-gRPC against rtid binary) |
| Verbose + best-effort modes functional | ✓ | `rti/spec/M5/{mode_flag,best_effort}_test.go` + `pysdk/tests/spec/m5/test_spec_m5_modes.py` (Python+Go best-effort deferred to M6 alongside cross-language handle alignment) |
| Perf baseline at sizes 2/5/25/100 | ✓ | `docs/reports/M5/agent-a.md` + `perf-baseline.json`; runs in `examples/go-pingpong/perf_main.go` (build tag `perf`) |
| Determinism preserved | ✓ | `docs/reports/M5/agent-b.md` audit clean; 2 minor findings filed (#2 #3); no critical/major |

**Stats since M4 close**:
- 9 TASK-NNN sentinels added (TASK-076..083; TASK-085 = this commit)
- TASK-084 CANCELLED per decision rule (encoding 0.21% CPU at size 25, 0.11% at size 100; thresholds 5%/10%)
- ~3,200 lines added (Go: mode wiring + best-effort RO + perf harness + soak; Python: real-gRPC transport + adapter + modes test; Docs: 3 status reports)
- All M0..M5 spec tests GREEN; 1 documented skip (cross-lang Python+Go best-effort RO)
- Project total: ~36k lines across Go + Python + proto + tests + docs

**Notable architectural findings recorded for M6**:

- **Cross-language handle alignment** is the single most consequential M6 follow-up. Python's FOM parser merges the MIM differently from `rti/pkg/fom/mim/standard-mim.xml`, so the same class name lands at different numeric handles on each side. Affects: bidirectional Python+Go cross-language smoke, Python-publishes-to-Go best-effort RO. Fix: align Python's MIM merge against the canonical XML.
- **EventLog Writer concurrency bug** (W2A finding): `Writer.Append` has no mutex on `nextSeq`. Tripped by tight-loop perf workload; production hits it if multiple goroutines serve one federation's gRPC handlers concurrently. Recommended M6 follow-up; perf harness sidesteps via `EventLog: nil`.
- **Real-pyjevsim adapter is single-atomic only** (W1C cut-1). Structural hierarchies (`SysExecutor` driving multi-model federations) require additional adapter work. M6 follow-up.
- **Python SDK has no production transport hardening yet** (W1C cut-1 used `grpc.aio.insecure_channel`). TLS + retry + deadline propagation = M6.
- **`examples/pyjevsim/runner.py` imports from `pysdk/tests/spec/m4/_fakes/`** (W7 contract violation, accepted for M4). Extraction of a production in-process driver = M6.

**Multi-agent dispatch model — empirical findings**:

The first cross-agent parallel wave (W1A + W1B + W1C) ran cleanly. Zero file collisions; orchestration overhead was the merge sequencing (smallest first). Verifies the `docs/ORTHOGONALITY.md` §2 path-ownership table as the basis for safe parallelism. Recommended pattern for future milestones with >1 agent owning meaningful work.

**MVP gate** ✓

The walking-skeleton MVP described in `docs/srs.md` §1 is achieved:
- IEEE 1516-2010 RTI in Go (rtid binary; M2 + M3 + M5 hardening)
- HLA Evolved encoding rules with byte-identical Go/Python implementations (M1 + M4)
- FOM parser with shared FOM-NNN diagnostics across Go and Python (M1 + M4)
- Time management with NER + LBTS + stall timeout (M3)
- Python SDK with both Layer 1 (idiomatic asyncio) and Layer 2 (1516-shaped ambassador) APIs (M4)
- pyjevsim DEVS↔HLA bridge (M4 cut-1; structural-hierarchy adapter M6)
- Cross-language federation works end-to-end (M5)
- Both verbose and best-effort modes functional (M5)
- Perf baseline at sizes 2/5/25/100 recorded (M5)
- Reproducible determinism: byte-identical event logs across runs (M2 + M3 + M4)

**TASK-085 closes M5; project MVP achieved.**

---

## 2026-05-03 (M5 pre-work — orchestrator-frozen spec tests + perf stub + 3-wave multi-agent dispatch plan)

M5 (Hardening + modes + perf + cross-language end-to-end — Agents A/B/C concurrent) infrastructure landed. **First multi-agent milestone**: Wave 1 dispatches 3 sub-agents across all three coding agents in parallel.

**Delivered**:
- **`rti/spec/M5/`** Go-side spec tests (orchestrator-frozen): `doc.go`, `fixtures.go` (permissive FOM repo + event log + recording outbox + minimal FOM XML helper), `mode_flag_test.go` (TASK-076 contract: default=Verbose, BestEffort persists), `best_effort_test.go` (TASK-077 contract; skip-scaffold), `perf_test.go` (TASK-079 contract: asserts perf.Manager.RunBaseline schema + JSON serialization), `soak_test.go` (TASK-078 contract; build tag `soak`), `cross_lang_test.go` (TASK-081 Go-side orchestration scaffold).
- **`pysdk/tests/spec/m5/`** Python-side spec tests: `__init__.py`, `test_spec_m5_modes.py` (TASK-082 contract; skip-scaffold), `test_spec_m5_cross_language.py` (TASK-081 contract; skip-scaffold).
- **`rti/internal/perf/`** stubs: `doc.go` (JSON schema documented), `baseline.go` (Manager + Options + Result struct frozen at SchemaVersion=1; constructor returns ErrNotImplemented). FROZEN-shape: schema is the M5 contract, downstream agents (TASK-084) read the JSON output.
- **`docs/reports/M5/.gitkeep`** — per-agent status report directory landed (orchestrator-owned per `docs/ORTHOGONALITY.md` §2 last row).
- **`docs/M5_DISPATCH_PLAN.md`** — 3-wave model: W1 (3 parallel: Agent A mode + best-effort, Agent B determinism audit, Agent C cross-language smoke) → W2 (2 parallel: Agent A hardening + perf, Agent C modes verification) → W3 (orchestrator close + MVP gate). Critical-path estimate 45–60 min sub-agent compute. **First milestone with cross-agent parallelism.**
- **`scripts/check-milestones.sh`** M5 probe re-pointed at `rti/spec/M5/` + `pysdk/tests/spec/m5/`.

**Verification** (next commit will run): `go test ./rti/spec/M5/...` shows mode_flag tests skip-or-passing (federation.New is real), perf_test skips on stub, scaffolds skip explicitly. `pytest pysdk/tests/spec/m5/` shows skip-scaffolds skipping. M0/M1/M2/M3/M4 stay green.

**Notable design decisions**:
- **Cross-agent parallel dispatch** is the structural innovation. Prior milestones (M2/M3 single-agent waves; M4 single-agent multi-wave) had one agent owning each wave. M5 fans across A/B/C in W1 because path ownership is fully disjoint per `docs/ORTHOGONALITY.md` §2 — Agent A writes only `rti/internal/`, Agent B writes only `docs/reports/M5/agent-b.md`, Agent C writes only `pysdk/` + `examples/pyjevsim/`. Zero collision risk.
- **TASK-081 bundles M4 follow-ups**: real-gRPC transport in Python SDK + real-pyjevsim adapter both deferred from M4 land here, because cross-language smoke fundamentally requires both. Brief documents this expansion explicitly so the W1C sub-agent isn't surprised.
- **`mode` plumbing already partial from M2**: proto + core.Mode + gRPC handler all wired. TASK-076 just needs CLI flag at `rtid`. TASK-077 is the substantive RO-vs-TSO delivery work.
- **Perf JSON schema is FROZEN before any work**: `SchemaVersion=1` in `rti/internal/perf/baseline.go` pins the contract before TASK-079 implements anything. TASK-084's conditional decision rule reads this exact schema; locking it now prevents drift.
- **Spec tests in `rti/spec/M5/` (Go) + `pysdk/tests/spec/m5/` (Python)** — same convention as M3 + M4. `tests/spec/M5/` deliberately not used because Go's internal-package rule blocks `tests/...` from importing `rti/internal/...` (M2/M3 pattern).

**Next**: dispatch Wave 1 (W1A Agent A mode+best-effort, W1B Agent B audit, W1C Agent C cross-lang — 3 parallel sub-agents). Then W2 (Agent A hardening+perf parallel with Agent C modes verification). Then W3 (orchestrator close + MVP gate).

---

## 2026-05-03 (M4 — DONE; 7 waves, 13 sub-agents)

M4 closed in one session. **`scripts/check-milestones.sh` reports `M4: DONE (5/5)`** — pysdk package bootstrapped, examples/pyjevsim runs end-to-end, Python encoder passes 100% of conformance vectors, mypy --strict clean, ruff clean. Total spec/m4 test count: 131 passed, 1 skipped (replay path deferred to M5 alongside cross-language smoke; covered by determinism witness).

**Wave summary** (all merged to `main` in dispatch order):

| Wave | Sub-agents | Tasks | Outcome |
|---|---|---|---|
| W1 | 5 parallel (W1A int, W1B float, W1C byte, W1D opaque, W1E FOM model + codegen) | TASK-050, 051, 052, 058, 060, 062 | All primitive codecs implemented + FOM dataclass model + Python codegen wrapper |
| W2 | 4 parallel (W2A strings, W2B array composites, W2C record composites, W2D FOM parser) | TASK-053, 054, 055, 056, 057, 061 | All composite codecs + FOM parser with same 10 FOM-NNN diagnostics as Go side |
| W3 | 1 (W3 dispatcher) | TASK-059 | **Encoding gate closed** — codec_for(spec) wired; 95/95 conformance vectors GREEN cross-language |
| W4 | 1 (W4 SDK bundled) | TASK-063, 064, 065, 066, 067, 068 | Full SDK Layer 1 (RtiConnection + Federate + 4 sub-services + events stream + typed exceptions) + Layer 2 ambassador (sync 1516-2010 callback API). Transport injection via `memory://fake-rti` scheme |
| W5 | 2 parallel (W5A port mapping, W5B pyjevsim pin) | TASK-069, 072 | PortMapping with prefix-based direction inference; pyjevsim==1.3.1 pinned. **Orchestrator-side fix**: spec smoke test originally checked for `CoupledModel`/`AtomicModel` (DEVS-canonical names) but real pyjevsim 1.3.x exports `StructuralModel`/`BehaviorModel` — updated spec test + Protocol docs to clarify conceptual-vs-real-API distinction |
| W6 | 1 (W6 bridge core) | TASK-070, 071 | HLAFederate.run/step_once/deliver_external + select_preserve. Auto-grant on `next_message_request` added to FakeRtiServer (test-only) |
| W7 | 1 (W7 examples + gate) | TASK-073, 074, 075 | examples/pyjevsim/ (Producer + Consumer + Runner; in-process FakeRtiServer); 10× determinism harness; mypy/ruff CI gate; coverage 93% (rti1516e + pyjevsim_bridge). M4 milestone gate closed |

**Critical-path wall time**: ~1h sub-agent compute. Each wave: W1 ~4 min (5 parallel), W2 ~6 min (4 parallel), W3 ~7 min, W4 ~13 min (bundled), W5 ~3 min (2 parallel), W6 ~8 min, W7 ~11 min. Plus orchestrator merge + verify cycles.

**Notable mechanical findings**:
- **Real pyjevsim API gap**: M4 brief (`docs/agent-c-pysdk.md`) used DEVS-canonical method names (`CoupledModel`, `time_advance`, `output_handler`) which don't match real pyjevsim 1.3.x's actual exports (`StructuralModel`, `output()`, `int_trans()`, `ext_trans()`). Resolved by keeping the bridge's `CoupledModelProtocol` shim with canonical names (Protocol contract is the bridge's needs, not pyjevsim's API surface) and deferring the real-pyjevsim adapter to M5. Cut-1 example uses duck-typed pure-Python coupled models.
- **W7 example uses test fixture**: `examples/pyjevsim/runner.py` imports `FakeRtiServer` from `pysdk/tests/spec/m4/_fakes/` — documented contract violation. M5 follow-up: extract a production in-process driver. Spec tests don't depend on this.
- **Replay test deferred**: `test_spec_m4_python_example_replays_byte_identical` requires real `rtid` binary integration. M4 determinism is fully covered by the in-memory call log sha256 witness; byte-identical replay through rtid is M5 (alongside TASK-081 cross-language smoke).
- **W4 design pattern — transport registry**: `pysdk/rti1516e/_transport.py` module-level dict maps `memory://...` URLs to FakeRtiServer instances. SDK's `RtiConnection.connect(url)` checks the registry; non-memory URLs raise NotImplementedError until real gRPC wiring lands (M5 follow-up).
- **W6 design pattern — auto-grant in fake**: FakeRtiServer.record() auto-pushes `TimeAdvanceGrant(time)` on `next_message_request` calls. Production goes through real RTI; this is a test-only convenience. Documented in code.

**Stats since M3 close**:
- 26 TASK-NNN sentinels added (TASK-050..075)
- ~6,800 lines added (Python source + tests + docs)
- All M0/M1/M2/M3/M4 gates GREEN; coverage 92-93% on pysdk owned packages
- mypy --strict + ruff clean across 68 source files

**Next**: M5 (Hardening + modes + perf + cross-language — Agents A/B/C). 11 tasks (TASK-076..085 + conditional TASK-084 perf benchmark) plus 1 deferred from M4 (TASK-081 cross-language smoke includes the Python replay path).

---

## 2026-05-03 (M4 pre-work — orchestrator-frozen pysdk skeleton + spec tests + 7-wave dispatch plan)

M4 (Python SDK + pyjevsim bridge — Agent C territory) infrastructure landed. Sub-agents can now be dispatched against frozen-shape stubs and RED spec tests. Largest pre-work delivery to date by file count (~50 files); largest milestone by task count (26 tasks).

**Delivered**:
- **`pysdk/`** package skeleton (orchestrator-frozen): `pyproject.toml` (deps + ruff/mypy/pytest config; `mypy --strict` enabled, `asyncio_mode = "auto"`), `README.md`, `.gitignore`.
- **`pysdk/rti1516e/`** frozen-shape stubs: `__init__.py` (public API exports), `errors.py` (one typed exception per ErrorCode in proto/rti/v1/errors.proto, with lookup table), `connection.py` (RtiConnection + Federate signatures with full Layer 1 surface), `events.py` (5 typed event dataclasses), `standard.py` (Layer 2 Rti1516eAmbassador with all 1516-2010 method names), `declaration.py` + `object.py` + `interaction.py` (extension points). All public bodies raise `NotImplementedError("TASK-NNN")`.
- **`pysdk/rti1516e/encoding/`** stubs (10 codec modules): `_base.py` (`Codec` ABC + `pad_to_boundary` / `aligned_offset` helpers), `integer.py` + `float_codec.py` + `byte_codec.py` + `string_codec.py` (16 primitive codec classes), `fixed_array.py` + `variable_array.py` + `fixed_record.py` + `variant_record.py` + `opaque.py` (5 composite codec classes), `dispatch.py` (`codec_for(spec)` entry point).
- **`pysdk/rti1516e/fom/`** stubs: `model.py` (FOM dataclass mirror of Go: ObjectClass, Attribute, InteractionClass, Parameter, DataType sum), `parser.py` (`parse(modules)` returning `ParseResult` with FOM-NNN diagnostics).
- **`pysdk/pyjevsim_bridge/`** stubs: `_protocol.py` (`CoupledModelProtocol` typing.Protocol shim — avoids pyjevsim hard dep in spec tests), `port_mapping.py` (PortMapping dataclass + lookup helpers), `time_advance.py` (HLAFederate.run / step_once / deliver_external), `select_preserve.py` (order_simultaneous_events helper).
- **`pysdk/tests/spec/m4/`** orchestrator-frozen pytest spec tests (13 files): encoding conformance (parametrized over all 94 vectors in encoding_vectors.json), FOM diagnostics (10 bad fixtures + good fixtures), connection lifecycle, declaration, object, interaction, events stream + typed-exception mapping check, Layer 2 ambassador surface check, port mapping, time advance, select preserve, pyjevsim API drift smoke (skips if pyjevsim absent), determinism gate (skip-scaffold), replay gate (skip-scaffold).
- **`pysdk/tests/spec/m4/_fakes/`** test doubles: `FakeRtiServer` (pure-Python in-process double of RTI gRPC surface; records calls, accepts canned events, mints handles), `StubCoupledModel` (pyjevsim coupled-model substitute with controllable ta/output schedules, recorded transitions), `vector_loader` (encoding_vectors.json normalizer with primitive/composite/all filters).
- **`pysdk/tests/spec/m4/conftest.py`** — pytest fixtures + repo-root constants for off-tree fixture access (encoding vectors, FOM fixtures).
- **`docs/M4_DISPATCH_PLAN.md`** — 7-wave dispatch model: W1 (5 parallel: int/float/byte/opaque codecs + FOM model+codegen) → W2 (4 parallel: strings + array composites + record composites + FOM parser) → W3 (1: codec_for dispatcher = encoding gate) → W4 (1 bundled: full SDK Layer 1+2) → W5 (2 parallel: bridge port mapping + pyjevsim smoke/version pin) → W6 (1: bridge time-advance + select-preserve) → W7 (1: examples + determinism + lint/coverage gate). Critical-path estimate 50–80 min wall-time.
- **Infrastructure**: `Makefile` extended with `py-codegen`, `py-test`, `py-lint`, `py-typecheck` targets. `scripts/check-frozen-paths.sh` extended to block agent writes to `pysdk/tests/spec/` AND retroactively `rti/spec/` (M2/M3 oversight fix). `scripts/check-milestones.sh` M4 probe re-pointed at `pysdk/tests/spec/m4/test_spec_m4_encoding_conformance.py`.

**Verification** (next commit will run): pytest collects all spec tests, fails RED with `NotImplementedError`/`AttributeError` for the right reason; mypy --strict on stubs is clean; Go-side M0/M1/M2/M3 tests stay green; M4 milestone probe shows partial credit (spec dir + pysdk skeleton present; conformance + mypy not yet GREEN until Agent C's waves land).

**Notable design decisions**:
- Spec tests live in `pysdk/tests/spec/m4/` (lowercase, Python convention) — diverges from M2/M3's `rti/spec/M<x>/` only because Python doesn't have Go's internal-package import rule. Frozen-paths protection equivalent.
- Layer 2 ambassador (`Rti1516eAmbassador`) preserves IEEE 1516-2010 camelCase method names (`createFederationExecution`, `joinFederationExecution`, etc.) instead of converting to snake_case. The N802 lint warnings are intentional — these names exist for users porting from Java/C++ RTIs.
- `FakeRtiServer` is pure Python (no real gRPC server) so spec tests stay fast and dependency-light. The SDK's transport layer must be injectable; spec tests fail with AttributeError when it's not, signaling a design issue early.
- `StubCoupledModel` keeps spec bridge tests pyjevsim-free. ONE spec test (`test_spec_m4_pyjevsim_smoke.py`) imports real pyjevsim and skips if not installed — making API drift detection explicit and isolated.
- `errors.py` provides a complete `ERROR_CODE_TO_EXCEPTION` lookup table (27 typed exceptions, one per proto ErrorCode) so Agent C's TASK-067 wires gRPC trailers via a single `dict.get(code, RtiError)` lookup. Test asserts the table is complete against the proto file.

**Next**: dispatch Wave 1 (W1A integer + W1B float + W1C byte + W1D opaque + W1E FOM model + codegen — 5 sub-agents in parallel). Then W2..W7 per the plan.

---

## 2026-05-03 (M3 — DONE; 4 waves, 5 sub-agents)

M3 closed in one session. **`scripts/check-milestones.sh` reports `M3: DONE (4/4)`** — examples/go-timed runs deterministically across 20 randomized scenarios + 10 same-seed iterations and replays byte-identical (NFR-DET-1, NFR-DET-2). Stall detection fires within configured window and halts the federation cleanly with FederationHalted recorded in the event log.

**Wave summary** (all merged to `main` in dispatch order):

| Wave | Sub-agent | Tasks | Branch | Outcome |
|---|---|---|---|---|
| W1A | regulation state machine | TASK-041 | `agent/a/m3-w1a-regulation` | 9 spec tests green; per-federation isolation verified |
| W1B | LBTS pure function | TASK-042 | `agent/a/m3-w1b-lbts` | 6 property tests green; order-independent confirmed |
| W2 | NER + lookahead | TASK-043, TASK-044 | `agent/a/m3-w2-ner` | 6 NER spec tests green; deterministic grant order verified; key insight: side-table approach via `extOf(*Manager)` to honor "do-not-reshape Manager struct" rule |
| W3 | stall detection | TASK-045 | `agent/a/m3-w3-stall` | 5 stall spec tests green; halted-state enforcement at top of every method |
| W4 | examples/go-timed + harnesses + gate | TASK-046, 047, 048, 049 | `agent/a/m3-w4-go-timed` | M3 gate: replay byte-identical, 20 randomized determinism scenarios green |

**Critical-path wall time**: ~40 min sub-agent compute. W1A+W1B in parallel (~6 min); W2 (~16 min); W3 (~7 min); W4 (~18 min). Plus orchestrator merge/verify cycles.

**Notable mechanical findings**:
- Sub-agents in fresh worktrees can't see `rti/internal/genproto/rti/v1/` (gitignored). Each sub-agent flagged this as "pre-existing infra hiccup" and scoped tests to packages outside the genproto-dependent path. Their work was unaffected. Future M4/M5 dispatch: keep this constraint in mind when bundling tasks.
- The W2 sub-agent independently chose a `sync.Map`-based extension table to add per-Manager state without modifying the frozen `Manager` struct. This honored the "don't reshape" rule cleanly — orchestrator should adopt this as the pattern for future stub-extension work.
- W4 unskipped the orchestrator-frozen replay/determinism scaffolds in `rti/spec/M3/` and added `buildTimedExampleLog` + `replayLog` helpers in the same file. The shape is reusable for M4/M5 cross-language replay tests.
- Replayer extension was NOT needed — the existing `eventlog.Replayer.dispatchProtoEvent` already handles `TimeAdvanceGranted`/`FederationHalted` via the empty-body `*rtiv1.Event{Seq: N}` passthrough path. The time package's records serialize as synthetic empty bodies on the wire and round-trip identically.

**Stats since M2 close**:
- 9 TASK-NNN sentinels added (TASK-041..049)
- ~3,300 lines added (mostly under `rti/internal/time/`, `rti/cmd/rtid/`, `examples/go-timed/`)
- All M0/M1/M2/M3 tests green; race-clean under `-race`; lint debt unchanged

**Next**: M4 (Python SDK + pyjevsim bridge — Agent C territory). Pre-work: orchestrator must write `tests/spec/M4/` + bootstrap `pysdk/` skeleton expectations + add `pysdk/` build target to Makefile. M4 has 26 tasks (TASK-050..075) plus 2 conditional perf tasks; orchestrator drafts the M4 dispatch plan mirroring M2/M3 wave pattern, scoped by SDK layer (encoding → FOM → connection → declaration/object/interaction → bridge → integration).

---

## 2026-05-03 (M3 pre-work — orchestrator-frozen stubs + spec tests + wave-based dispatch plan)

M3 (Time Management — NER + LBTS + stall timeout) infrastructure landed. Sub-agents can now be dispatched against frozen-shape stubs and RED spec tests.

**Delivered**:
- **`rti/internal/time/`** stubs (frozen, orchestrator-only): `doc.go`, `manager.go` (Manager + Options + 5 `core.TimeManager` methods + `CheckStalls`), `lbts.go` (RegulatingFederate + pure `LBTS(set)` function). Constructor `New(opts)` returns `ErrNotImplemented`; all method bodies stubbed; `var _ core.TimeManager = (*Manager)(nil)` asserts the contract.
- **`rti/spec/M3/`** spec tests (frozen, orchestrator-only): `doc.go`, `fixtures.go` (fakeOutbox + permissiveEventLog mirroring M2), `regulation_test.go` (10 tests — Enable/Disable Regulation/Constrained, twice errors, invalid lookahead negative+NaN, per-federation isolation), `lbts_test.go` (6 property tests — empty set→+Inf, single, min-across, order-independent, zero lookahead, +Inf lookahead), `ner_test.go` (6 tests — not-regulating reject, request-in-past, sole-regulator immediate grant, two-regulator wait, duplicate request, simultaneous-ready deterministic order), `stall_test.go` (6 tests — empty no-halt, before-timeout no-halt, past-timeout halts, halted-rejects-further, per-federation isolation, default-60s), `replay_test.go` (2 scaffold-skips for M3 gate), `determinism_test.go` (2 scaffold-skips). All RED with `ErrNotImplemented` for the right reason.
- **`docs/M3_DISPATCH_PLAN.md`** — 4-wave dispatch model mirroring M2's proven shape: Wave 1 (W1A regulation + W1B LBTS, parallel), Wave 2 (NER + lookahead, single sub-agent), Wave 3 (stall detection, single sub-agent), Wave 4 (examples/go-timed integration + harnesses, single sub-agent). 9 tasks across 4 waves; critical path estimate 25–35 min wall-time.

**Verification**: `go build ./... && go test ./...` — M0/M1/M2 all green; M3 spec tests RED with `ErrNotImplemented` from time-package stubs (the expected pre-dispatch state per `docs/TDD.md` §5).

**Next action**: orchestrator updates `scripts/check-milestones.sh` M3 probe to look at `rti/spec/M3/`, then dispatches Wave 1 (W1A + W1B in one parallel call).

---

## 2026-05-02 (M2 — DONE; 4 waves, 9 sub-agents)

M2 closed in one session. **`scripts/check-milestones.sh` reports `M2: DONE (4/4)`** — examples/go-pingpong runs deterministically across 10 runs and replays byte-identical (NFR-DET-1, NFR-DET-2). Pingpong runtime: 268ms for 1000 round-trips (M2 budget was 5s).

### Wave summary

| Wave | Sub-agents | Tasks closed | Key deliverable |
|---|---|---|---|
| W1 | 3 parallel (W1A federation, W1B eventlog, W1C declaration) | TASK-020..025 + 027..029 | Federation lifecycle + EventLog Writer/Reader + DeclarationManager |
| W2 | 2 parallel (W2A object, W2B replayer) | TASK-026 + 030..033 | Object Registry + EventLog Replayer (passthrough + proto-dispatch) |
| W3 | 3 parallel (W3A federation+server, W3B declaration, W3C object+stream) | TASK-034..036 | gRPC handlers (16 RPCs total) + Server compose + SubscribableOutbox forward-decl |
| W4 | 1 sequential (M2 gate) | TASK-037..040 | cmd/rtid wiring + Prometheus + go-pingpong + determinism + replay harness |

Total: 9 sub-agents, 21 tasks closed (TASK-020..040), 33 of 33 M2 spec subtests green, 100+ unit tests across 5 new packages, all `-race` clean, `golangci-lint` clean.

### Notable findings + corrections during M2

Two spec-test bugs that sub-agents flagged as orchestrator-side errors and the orchestrator corrected:

- **W1A**: original `JoinFederation_DeterministicHandles` asserted handles match the federate's FINAL sort-position in the COMPLETE roster — requires future knowledge no online algorithm can satisfy. Standard HLA assigns monotonic handles in arrival order; replay determinism comes from the FederateJoined event log, not from a sort-order property. Spec test rewritten to match this. Algorithm switched from "re-key on each join" to "monotonic counter."

- **W2A**: `Object_Register_AssignsMonotonicHandle` and `Register_RejectsUnpublished` were mutually unsatisfiable (same setup, contradictory assertions). Fixed by adding the missing publish call to the monotonic-handle test.

Both surfaced via the spec-clarification protocol (`docs/AGENTS.md` §7) — sub-agents implemented per-spec, then explicitly flagged the inconsistency in their reports. The right discipline.

### Architectural forward-declarations resolved in W4

- **Multi-federation EventLog router**: W1B's Writer is single-federation by design (correct for production where each federation has its own log file). W2A flagged the gap; W4 implemented `eventlog.MultiplexWriter` that routes Append/Sync per federation, lazily opening per-federation Writers via a pluggable factory (file-backed in production, bytes.Buffer in tests).

- **Production `SubscribableOutbox`**: W3C declared the interface in `transport/grpc/stream.go` (additive, transport-grpc-local, no `core` change). W4 implemented `MultiOutbox` in `cmd/rtid/outbox.go` — per-federate channels with bounded capacity, returns `core.ErrFederateOverflow` on overflow per the `core.Outbox` contract.

### W4 architectural concessions worth flagging

- **examples/go-pingpong subprocess shim**: Go's `internal` package rule blocks `examples/...` from importing `rti/internal/...` or generated proto. The pingpong demo logic lives in `rti/cmd/rtid/pingpong.go` (allowed via Agent A's owned cmd path); `examples/go-pingpong/main.go` is a thin wrapper that subprocess-execs `rtid -mode=pingpong-demo`. Functionally identical to a literal in-process import; `examples/` retains its symbolic role as the documentation-facing demo location.

- **Hand-rolled Prometheus exposition**: rather than add `github.com/prometheus/client_golang` (a runtime dep that requires a separate `deps:` PR per `docs/AGENTS.md` §8/9), W4 hand-wrote ~60 lines of Prom text format covering the four NFR-OPS-1 gauges (federations, federates per fed, eventlog seq per fed, object handles per fed). Future PR can swap in client_golang if/when richer metrics are needed.

- **`-pingpong-deterministic` flag**: the demo's `Event.wall_ns` field is informational per the proto comment, but it varied across runs because production wires `RealClock`. The new flag wires `FakeClock` fixed at epoch into the demo so the captured body is byte-identical across runs (the field is documented as informational; the determinism contract excludes it). Production runs leave the flag false; it's solely a testing affordance.

- **Eventlog coverage 83.9%** (W4 brief asked for 90%). The `rti/internal/eventlog` package coverage ceiling is bounded by the W2B Replayer's error branches, which W4 cannot exercise without modifying the frozen-by-merge replayer.go. Meets the project minimum (`docs/CODING_CONVENTIONS.md` §2.5 = 80%).

### Dispatch protocol footnote

Two W3 sub-agents (W3B and W3C) both wrote `errs.go` per the brief's "shared helper" guidance — orchestrator picked W3B's canonical version at merge time and merged W3C's version-specific test cases by adding the missing `ErrAttributeNotOwned` → `PermissionDenied` branch (W3C's semantically correct mapping; W3B had it under FailedPrecondition). The merge-time conflict resolution was straightforward thanks to both agents producing consistent core mappings.

W3A's stub `declarationService`/`objectService`/`streamService` types in `server.go` were removed pre-merge to avoid type redeclaration conflicts with W3B/W3C's real types. This is a recurring pattern when sibling sub-agents own distinct files but one needs to forward-reference the others' constructors — the lead sub-agent (W3A) defines stubs to ship standalone; the orchestrator removes them at merge time.

### State after M2

```
M0: DONE
M1: DONE
M2: DONE (4/4)   ← shipped this session
M3: NOT_STARTED
M4: NOT_STARTED
M5: NOT_STARTED
No regressions.
```

Outstanding: CI workflow file (`/tmp/ci-fix.patch`, commit `c42f380`) cannot be pushed without `workflow` PAT scope; user disabled the workflow via web UI as a workaround.

### Next concrete actions (orchestrator)

- Pre-write `tests/spec/M3/` (or `rti/spec/M3/`) for time management — the M3 milestone gate.
- Decompose M3 into a wave model and update `docs/M3_DISPATCH_PLAN.md` (or extend M2's plan).
- M3 is owned by Agent A; smaller surface area than M2 (TASK-041..049 = 9 tasks vs M2's 21).

---

## 2026-05-02 (M2 pre-work — orchestrator-frozen stubs + spec tests + wave-based dispatch plan)

Closed M1, opened M2. Pre-work delivered so Agent A can start working through the M2 wave model.

### What landed

- **Frozen-shape stubs** in five new Agent A packages, each with package doc + `Manager`/`Writer`/`Registry`/`Server` type + `Options` struct + constructor + interface-method stubs returning `ErrNotImplemented`. Compile-time `var _ core.Foo = (*X)(nil)` assertions guard against signature drift:
  - `rti/internal/federation/{doc,manager}.go` — implements `core.FederationStore`
  - `rti/internal/eventlog/{doc,format,writer,reader,replayer}.go` — implements `core.EventLog` + `core.EventLogReader`
  - `rti/internal/declaration/{doc,manager}.go` — pure data, no core interface
  - `rti/internal/object/{doc,registry}.go` — implements `core.ObjectRegistry`
  - `rti/internal/transport/grpc/{doc,server}.go` — composes the four core services
- **Orchestrator-frozen spec tests** at `rti/spec/M2/*.go` (NOT `tests/spec/M2/`). 7 files: `doc.go`, `fixtures.go`, `federation_test.go`, `eventlog_test.go`, `declaration_test.go`, `object_test.go`, `replay_test.go`, `grpc_test.go`. Spec tests RED-by-design — every test's first call into a stub fails with the package's `ErrNotImplemented` sentinel. Agent A turns them green per task.
- **`docs/M2_DISPATCH_PLAN.md`** documents the wave model (4 waves, up to 3 sub-agents per wave, total 8 sub-agents). Critical path is Wave 1 → Wave 2 → Wave 3 → Wave 4. Includes per-wave file-ownership table, dependency graph, sentinel-bundling pattern, and dispatch checklist.
- **All 21 M2 task briefs** (`docs/tasks/TASK-020.md` through `TASK-040.md`) gain a Notes-section reference to `docs/M2_DISPATCH_PLAN.md`.
- **Path convention amended**: M2+ spec tests live at `rti/spec/M<x>/`, not `tests/spec/M<x>/`. Reason: Go's `internal` package rule blocks `tests/...` from importing `rti/internal/*`. M1 stays at `tests/spec/M1/` because it imports only public packages (`rti/pkg/fom`, `rti/pkg/encoding`); future milestones whose work is in `rti/internal/` follow the M2 convention.
- **`scripts/check-milestones.sh`** updated: M2 + M3 spec-test directory probes now look at `rti/spec/M<x>/` instead of `tests/spec/M<x>/`. M1 probe unchanged.

### Design-for-testability decisions baked into the stubs

- **Options pattern**: every constructor takes a value-type `Options` struct. Tests substitute `FakeClock`, in-memory `EventLog`, fake `Outbox`, fake `FOMRepository` without touching production wiring.
- **Inline-fake test pattern** (per `docs/TDD.md` §7.5): the spec test fixtures (`rti/spec/M2/fixtures.go` + `grpc_test.go`'s `stubFedStore`/`stubObjectRegistry`) use small struct fakes, not mocking frameworks. Each fake records calls; tests inspect via simple slice comparisons.
- **Compile-time interface assertions**: `var _ core.FederationStore = (*Manager)(nil)` lines at the bottom of each stub file. Removing a required method fails the build at that line, not deep inside Agent A's implementation.
- **Stubs populate their fields**: `New` returns `&Manager{opts: opts}, ErrNotImplemented` rather than `nil, ErrNotImplemented`. Spec tests proceed past construction and fail loudly on the FIRST genuine method call, giving clearer signal about which method needs implementation.

### Wave model (full doc in `docs/M2_DISPATCH_PLAN.md`)

```
Wave 1 (3 parallel) — federation + eventlog + declaration
   ↓
Wave 2 (1–2 parallel) — object registry, then eventlog replayer
   ↓
Wave 3 (3 parallel) — gRPC FederationService / DeclarationService / ObjectService+StreamService
   ↓
Wave 4 (1 sub-agent) — cmd/rtid wiring + go-pingpong example + harnesses (M2 gate)
```

Total: 4 waves, 8 sub-agents. Same proven structure as M1 (which closed in 3 waves + ~9 sub-agents in one session).

### State after this commit

- `M0: DONE`, `M1: DONE`, `M2: IN_PROGRESS (1/4)` — only spec-test directory probe is now green; the other three M2 probes (`go-pingpong/main.go`, determinism harness, replay harness) remain pending Agent A's Wave 4 work.
- No regressions on M1.
- `make verify` (build + lint + tests) passes for everything except the deliberately-RED M2 spec tests.

### Next concrete actions (orchestrator)

1. Dispatch Wave 1: spawn three sub-agents (W1A federation, W1B eventlog, W1C declaration) in one parallel `Agent` call.
2. Review + merge each branch on completion.
3. Re-run milestone-check, expect M2 IN_PROGRESS (still — Wave 4 hasn't run yet).
4. Continue through Waves 2, 3, 4.

---

## 2026-05-02 (M1 follow-ups — canonical MIM landed, issue #1 resolved, octet-pair vectors added, JSON coercion fixes)

Two follow-ups carried over from the M1 closure round, both resolved this same day.

### Canonical MIM (issue #1 → resolved)

Replaced the interim hand-derived MIM with the canonical IEEE 1516.1-2010 standard MIM, sourced from openlvc/portico (CDDL-1.0). The file itself carries an explicit IEEE royalty-free attribution license at its head; that header comment is preserved verbatim. Provenance, blob sha (`713d000…`), content sha256 (`649f008a…`), and retrieval date recorded in `rti/pkg/fom/mim/embed.go`'s package doc.

Two follow-on fixes were needed to integrate the canonical content:

- **`<note>` added to the DIF Annex A whitelist** in `rti/pkg/fom/parser/strict.go`. The canonical MIM uses the singular annotation element which our interim file hadn't needed; all other 64 distinct elements were already covered.
- **`isMIMTypeModule` heuristic widened** in `rti/pkg/fom/parser/mim_merge.go`. The canonical MIM declares `<type>FOM</type>` (not `<type>MIM</type>`) for historical-compat reasons; without widening, `parser.Parse` on the embedded MIM self-collides via FOM-101 on every shared name. The new heuristic also matches modelIdentification names containing "Standard MOM and Initialization Module" or "HLAstandardMIM" (case-insensitive).

`hla-standard-mim.xml` re-classified from "interim approximation" to "empty wrapper" since the canonical MIM is fully self-sufficient for cut-1.

Issue #1 is closed by the orchestrator's commit `0e37c62`. The PAT in this conversation could not close the GitHub issue programmatically — user closes manually.

### HLAoctetPair vectors + JSON coercion

Two coupled changes closed the second M1 follow-up:

- **`tests/conformance/encoding_vectors.json`** gained 6 new vectors covering `HLAoctetPairBE` (zero, mixed `[0xAB, 0xCD]`, max) and `HLAoctetPairLE` (same logical values + asymmetric to exercise byte-swap).
- **`rti/pkg/encoding/byte.go`**: `octetPairBytes` accepts `[]any` (the JSON-array form) in addition to `[2]byte` and `[]byte`. New `coerceOctet` helper narrows each element from float64/int/byte with range checking.
- **`tests/spec/M1/encoding_vectors_test.go`**: `valuesEqual` gains a `[]any` case comparing against `[2]byte` (and `[]byte` for symmetry); both element types reuse the existing float64-coercion path. Frozen-file edit by orchestrator: purely additive, no existing case altered.

Earlier the same day, two additional JSON-coercion fixes had landed for the composite spec test (variant-record discriminator round-trip canonicalization in `variant_record.go`; opaque-data hex-string acceptance in `opaque.go`). All composite vector subtests now pass byte-identical.

### Net M1 state at close

```
M1: DONE
  ✓ 10 bad-FOM fixtures committed
  ✓ TestSpec_M1_BadFOMDiagnostics (all 10 codes including FOM-101)
  ✓ TestSpec_M1_PrimitiveVectorsRoundTrip (53 + 6 octet-pair = 59 subtests)
  ✓ TestSpec_M1_CompositeVectorsRoundTrip (17 subtests)
  ✓ rti/pkg/encoding coverage = 95.9%
```

No regressions. Issue #1 closed. Both M1 follow-ups absorbed. Ready to dispatch M2 once the orchestrator pre-writes `tests/spec/M2/`.

---

## 2026-05-02 (Wave 1 + Wave 2 + Wave 3 dispatch) — M1 driven from 0 to 9/10 BadFOM + full primitive + composite codecs; issue #1 interim resolution

Spawned three waves of orchestrator-driven sub-agents (worktree-isolated `general-purpose` agents role-playing Agent B) to drive M1 toward DONE.

**Wave 1 (4 parallel sub-agents)**: TASK-001 (parser+model skeleton), TASK-010 (integer codecs), TASK-011 (float codecs), TASK-012 (octet/boolean/char codecs). All four merged on `main` at `f2d8ae0`. Outcomes:
- Parser+model package green; spec test `TestSpec_M1_ParseMinimalGoodFOM_NoDiagnostics` passes; coverage parser=69.6% / model=73.7%.
- 16 primitive codecs (6 integer + 4 float + 6 byte/bool/char families) byte-identical to golden vectors; coverage on each ≥90%.
- 38 new vectors in `tests/conformance/encoding_vectors.json` (additive-only).
- `PrimitiveByName` refactored from a giant switch to a `primitiveCodecs` map dispatch (gocyclo limit was being exceeded).
- HLAoctetPair vectors NOT added — the orchestrator-frozen spec test's `valuesEqual` helper doesn't handle `[2]byte` vs `[]any{f64,f64}`. Sub-agent flagged for future orchestrator-side helper extension.

**Wave 2 (3 parallel sub-agents)**: parser diagnostics bundle (TASK-003..007 + 086..089 — 9 codes in one branch via the new `diagnoser` registry pattern), strings + arrays + opaque (TASK-013/014/015/018), records (TASK-016/017 with `_disabled` flag dropped from `fixed-record-octet-float64` and the embedded literal-space typo in its `bytes` field corrected by orchestrator). All three merged at `08bf89a`. Outcomes:
- Spec test `TestSpec_M1_BadFOMDiagnostics`: 9/10 subtests green (all except FOM-101 which depends on TASK-009).
- All composite codecs implemented as constructor functions (`NewFixedArray`, `NewVariableArray`, `NewFixedRecord`, `NewVariantRecord`, `NewOpaqueData`).
- 24 more vectors added (string + composite). Total now 88.
- Coverage on `rti/pkg/encoding` package: 96.0%; on `rti/pkg/fom/parser`: 83.3%.
- `diagnoser` registry pattern: each FOM-NNN detector lives in its own file, registers via `init()`, runs from `Parse` after the structural walk. Trades a tiny abstraction for ~9 future-merge-conflict-free additions.

**Issue #1 — interim resolution (orchestrator)**: hand-derived faithful MIM committed at `rti/pkg/fom/mim/standard-mim.xml` and `hla-standard-mim.xml` with strong "INTERIM" provenance comments pointing at issue #1 for canonical sourcing post-M1. `docs/ORTHOGONALITY.md` §2 amended to mark these two specific XML files as orchestrator-vendored; Agent B reads them via `//go:embed` but does not edit. TASK-008 and TASK-009 unblocked (`Status: BLOCKED` → `Status: DISPATCHED`); their Notes record the interim resolution.

**Wave 3 (planned, dispatching next)**: TASK-008+009 bundle (MIM embed + Merge + FOM-101 detection — closes the last red M1 spec subtest) and TASK-019 (CodecFor wiring + composite vector test goes from `t.Skip` to green). Two parallel sub-agents.

After Wave 3 lands, the orchestrator's `scripts/check-milestones.sh` will report **M1: DONE (4/4)** assuming no regressions.

**Process notes**:
- Sub-agents pushed to `origin` directly to enable orchestrator review-and-merge from the main worktree. No agent had write access to `main`.
- Three task-bundle commits ship with their bundle's TASK-NNN sentinels touched together (per the bundled-dispatch decision documented in each commit body); strict one-PR-per-task is relaxed for sub-agent dispatch efficiency, with documentation in the sentinel commit.
- W2A introduced an architectural-pattern improvement (the `diagnoser` registry) that the orchestrator should formalize in `docs/sdd.md` as the standard pattern for "many-validator" components. Tracked as future-doc-update work; not a blocker.
- Pre-existing `fixed-record-octet-float64` vector had a literal space in its `bytes` field. Orchestrator removed the space at merge time as a "fix-broken-placeholder" (the entry was `_disabled` until W2C enabled it; no test had ever exercised the broken bytes), with the rationale that this is not "modifying a working vector" forbidden by additive-only policy but rather "fixing a placeholder typo before activation."

---

## 2026-05-02 — Backlog committed; lint unblocked; M1 spec extended; discipline drift recorded

Material reconciliation between planned and actual state. No agent status reports yet (M1 still in flight); this revision is orchestrator-driven from observed working-tree drift.

### What landed on `main`

- **89 TASK files committed** to `docs/tasks/TASK-001.md` … `TASK-089.md`. The full M1..M5 backlog is now reachable via `git log` on `main`. Until this commit, agents had been working off untracked TASK files — the protocol requirement that "orchestrator commits TASK file to `main`" (see `docs/DISPATCH.md` §2 step 3) was not being honored.
- **TASK-084 cancelled** (per its own decision rule — TASK-080 perf baseline absent; do not optimize speculatively per `docs/agent-b-fom-encoding.md` §4 anti-goal). File retained for traceability per `docs/DISPATCH.md` §7.1; ID-084 will not be reused.
- **TASK-008 and TASK-009 marked `BLOCKED`** by [issue #1](https://github.com/cbchoi/gorti/issues/1) (canonical MIM XML sourcing). Agent B should not progress these until orchestrator resolves the contract-change-request and lands canonical MIM content.
- **`.golangci.yml` amended** to exclude `rti/internal/core/clock.go` from `forbidigo`'s `time.Now` ban. That file is the deliberate single sanctioned wrapper around `time.Now` (the whole reason `core.Clock` exists); without this exclude every PR fails `make verify`.
- **`.gitignore` extended** with `.tools/` and `.tmp/` — ad-hoc local toolchain caches (one local cache was 333 MB) that must never enter the repo.
- **`tests/spec/M1/parser_diagnostics_test.go`** extended for FOM-003, FOM-005, FOM-012, FOM-013 (the 4 codes the M1 exit criterion of "10 malformed FOMs" requires beyond the original 6). Pairs with 4 new bad-FOM fixtures under `tests/conformance/foms/bad/`. Unblocks TASK-086..089 dispatch.
- **`tests/spec/M1/encoding_vectors_test.go` composite extension deferred** — the upgrade (lifting composite vector `{kind, ...}` Type descriptors into `model.DataType` values to drive `encoding.CodecFor`) imports `rti/pkg/fom/model`, a package that does not yet exist on `main`. Landing the test now would break `go test ./...`. The extension stays in the stash and lands together with TASK-019 (Agent B's M1 exit task) so the test moves from `t.Skip` to passing in a single coherent step.
- **`docs/DISPATCH.md` §3 + new §7.2**: `BLOCKED` added to the canonical Status enumeration. New §7.2 distinguishes task-graph dependencies (`Depends-on:`) from external-artifact blockers (BLOCKED).

### Discipline drift recorded (not penalised, but called out)

- **Sentinel-without-merged-TASK** on `agent/c/codegen-setup`: 14 commits including TASK-050..062 sentinels were created on a topic branch while the corresponding `docs/tasks/TASK-NNN.md` briefs were not yet on `main`. Per `docs/DISPATCH.md` §10, sentinels reference the TASK file as their durable signal; without the brief on `main` the sentinel is dangling. Recommended remediation: rebase that branch onto the new `main` (this commit), so the sentinels land alongside their briefs.
- **Multiple IN_PROGRESS per agent** (Agent C did TASK-050..062 in 14 sequential commits without orchestrator review/merge between each). `docs/DISPATCH.md` §4.4 caps at one IN_PROGRESS per agent. The branch will need staged review (sentinel-by-sentinel) before merging.
- **Substantial uncommitted Agent B work** for TASK-001..009 + TASK-086..089: ~30 untracked Go source files. Not lost — preserved in stash + working-tree fragments — but never committed via TDD-discipline. Agent B should redo the work properly with red-green commit pattern per `docs/TDD.md` §3, since the existing fragments lack the test-first commit history reviewers walk.
- **Frozen-path violation (cosmetic only)**: `rti/internal/core/errors.go` and `rti/internal/core/federation.go` had local gofmt alignment changes from someone running `make fmt` over the whole tree. No semantic change. The pre-commit hook should have rejected if anyone tried to commit on an agent branch; this commit absorbs the cosmetic fix on `main`.

### What is NOT in this commit

- Agent C's pysdk encoding/codegen work on `agent/c/codegen-setup` — left for review-and-merge cycle per `docs/DISPATCH.md` §10.
- Agent B's parser/model/MIM/encoding work in the working tree — left for proper test-first redo on a clean topic branch.
- Resolution of issue #1 (canonical MIM XML) — pending orchestrator decision on sourcing path.

### Next concrete actions (orchestrator)

1. Resolve [issue #1](https://github.com/cbchoi/gorti/issues/1): pick a sourcing path (Portico CDDL is the recommendation) and commit canonical MIM XML to `rti/pkg/fom/mim/`. Flip TASK-008 and TASK-009 back to `DISPATCHED`.
2. Triage `agent/c/codegen-setup`: rebase onto this `main`, then merge sentinels in order with review per `docs/DISPATCH.md` §10.
3. Re-dispatch TASK-001 to Agent B on a clean topic branch off this `main`.

---

## 2026-04-28 (later) — M0 deliverables produced; orthogonality + dispatch + sentinel locked

Built out M0 contracts and scaffolding under `/workspace/gorti/`:

- **Proto contracts**: 8 `.proto` files in `proto/rti/v1/` (common, errors, federation, declaration, object, time, stream, eventlog) covering all five gRPC services + the event log binary format.
- **Go core interfaces**: 12 files in `rti/internal/core/` — frozen, orchestrator-only (Transport, FederationStore, ObjectRegistry, TimeManager, EventLog, FOMRepository, Codec, Outbox, Clock + typed handles + sentinel errors).
- **Stub agent packages**: `rti/pkg/fom/parser` and `rti/pkg/encoding` contain minimum API surfaces (Parse / Result / Diagnostic / Codec / CodecFor / PrimitiveByName) returning `ErrNotImplemented`. Signatures are part of the M0 contract; bodies are Agent B's M1 work.
- **M1 specification tests**: `tests/spec/M1/` (orchestrator-written, frozen) — `parser_diagnostics_test.go` covering FOM-001/002/004/009/011/101 and 2 good-FOM accepts; `encoding_vectors_test.go` covering 16 primitive vectors.
- **Conformance fixtures**: `tests/conformance/encoding_vectors.json` (16 vectors + 1 disabled composite example), 2 good FOMs, 6 bad FOMs.
- **CI + tooling**: Makefile, `.golangci.yml` (depguard isolating `pkg/` from `internal/`, forbidigo blocking `time.Now`/`fmt.Println`), `ruff.toml`, `buf.yaml`/`buf.gen.yaml`, `.pre-commit-config.yaml`, `scripts/check-{frozen-paths,no-emojis,no-debug-prints}.sh`, `.github/workflows/ci.yml`.
- **Skeleton main**: `rti/cmd/rtid/main.go` with flags wired and `TODO(#1)` for services.

Three governance documents added on top of the original plan:

- **`docs/ORTHOGONALITY.md`** — exhaustive path-to-owner table; zero co-ownership policy; producer/consumer rules; working-directory isolation via git worktrees (`/workspace/gorti-agent-{a,b,c}/`).
- **`docs/DISPATCH.md`** — orchestrator-driven task assignment; agents do not self-select; one IN_PROGRESS task per agent; idle protocol; cancellation; orchestrator commitments.
- **`docs/tasks/signals/README.md`** — completion sentinel: agents create `docs/tasks/signals/TASK-NNN.done` as the FINAL commit on the topic branch; without it the PR is treated as draft. Pre-commit hook allow-lists this specific path while keeping all other writes under `docs/tasks/**` frozen.

Plus `scripts/setup-agent-worktrees.sh` to initialize the three sibling worktrees from `main`.

**State**: `/workspace/gorti/` is NOT yet git-init'd. Next action: user runs `git init -b main` + initial commit, then `./scripts/setup-agent-worktrees.sh`, then orchestrator dispatches TASK-001 to agent-b (minimal parser skeleton accepting `good/minimal.xml`). No agent status reports yet — M1 has not started.

---

## 2026-04-28 — Initial plan locked

Initial plan and doc set established by orchestrator-driven conversation. Walking-skeleton MVP, milestones M0..M5, three sandboxed coding agents (claude-sandbox / codex-sandbox / gemini-sandbox), TDD methodology with orchestrator-written spec tests as milestone contracts.

See:
- `docs/srs.md` — SRS
- `docs/sdd.md`, `docs/idd.md` — design + interfaces
- `docs/AGENTS.md`, `docs/CODING_CONVENTIONS.md`, `docs/TDD.md`, `docs/WORKFLOW.md` — operating rules
- `docs/agent-{a,b,c}-*.md` — per-agent briefs

No prior status reports (this is the starting point).
