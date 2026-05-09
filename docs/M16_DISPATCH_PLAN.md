# M16 Dispatch Plan — Hot standby + replay-driven RTI failover

DISPATCH PLAN ONLY. M16 implementation is **deferred** — it depends on M15 cut-2 (multi-node consensus) which is itself deferred.

This document pins the design contract so the implementation can land without churning the API once the cluster substrate is in place.

---

## 0. Why deferred

M16 promises: "standby rtid replicates event log + can take over on primary failure within configured window; spec test simulates primary kill mid-federation."

That requires:
1. **Multi-node cluster** (M15 cut-2) — without it, "standby" has no meaning.
2. **Replicated event log** — every committed event in the federation must reach the standby BEFORE the primary acks. That's a write-path latency multiplier.
3. **Federate redirection on primary failure** — federate's stream drops, federate redials any cluster node, gets `LookupFederationHost = REDIRECT new-primary`, retries.
4. **Idempotent recovery** — federate may have observed events the standby hasn't yet committed; the recovery protocol has to either re-deliver those events OR drop the in-flight ones consistently.

Each is genuine distributed-systems work. M16 sits on top of M15 cut-2 and inherits its choice of consensus protocol (likely Raft).

---

## 1. Goal & non-goals

### Goal

A federation hosted on `node-A` continues to operate after `node-A` crashes, by promoting `node-B` (the standby) to primary. Federates reconnect transparently; the federation's logical state (joined federates, registered objects, ownership, time-state, sync-points, savepoints) is preserved.

### Non-goals

- **Zero-data-loss across crashes** — gorti accepts bounded data loss. The replication protocol's commit window defines the bound.
- **Geo-distributed failover** — same-LAN cluster only.
- **Active-active for the same federation** — exactly-one primary per federation.
- **Federate-side reconnect logic in M16** — handled by M15's redirect machinery.

---

## 2. Surface design

### 2.1 EventLog replication

The existing `eventlog.MultiplexWriter` is the single point at which the rtid commits state changes. M16 inserts a replication step BEFORE the local Append call:

```go
// Pseudo-code:
func (m *MultiplexWriter) Append(ctx, fed, evt) error {
    // M16: replicate to N standbys in parallel; wait for quorum.
    if err := m.replicator.Replicate(ctx, fed, evt); err != nil {
        return err  // commit fails if replication can't reach quorum
    }
    return m.local.Append(ctx, fed, evt)
}
```

`replicator` is a new `rti/internal/replication/` package, satisfied by:
- `singleton{}` — no-op (current behavior; replication disabled).
- `raftReplicator{}` — Raft-backed, deferred to M16 cut-1.
- `bufferedReplicator{}` — sends async with bounded queue (lower latency, weaker consistency).

### 2.2 Failover RPC

When a federate's stream drops, the SDK redials any cluster node and issues `LookupFederationHost`. If the federation is in a "promoting" state (mid-failover), the cluster manager returns `REDIRECT` to the new primary as soon as the consensus log records the promotion.

Promotion is triggered by:
1. Health-check failure (peer ping timeouts).
2. Manual via admin RPC: `Promote(fed, target_node)`.
3. Lease expiry (the primary holds a federation-level lease; failure to renew triggers election).

### 2.3 New AdminService extension

```proto
service AdminService {
  // Existing M12 read-only RPCs ...

  // M16 — manual failover trigger.
  rpc PromoteFederation(PromoteFederationRequest) returns (Empty);
  // M16 — federation status incl. primary/standby roles.
  rpc QueryFederationRole(QueryFederationRoleRequest) returns (QueryFederationRoleResponse);
}
```

### 2.4 Federate-side recovery

`federate.Federate` gains a reconnect loop:

```go
opts.OnStreamDropped = func(ctx) {
    // 1. Redial via cluster manager.
    // 2. Issue LookupFederationHost; follow REDIRECT.
    // 3. Resume — server replays buffered events with seq > lastSeen.
}
```

The "replay events with seq > lastSeen" path requires the new primary to have access to the same event log ordering as the old primary — which is exactly what the replication step in §2.1 provides.

### 2.5 Health checking

Peer-to-peer ping every 1s; mark a node DEAD after 3 missed pings. Cluster-manager view updates trigger reassignment.

---

## 3. Acceptance criteria

1. **Federation state preserved across promotion.** Owned objects survive; ownership records intact; sync-point states intact.
2. **Federate transparently reconnects.** No SDK-API changes visible to callers (existing `Resign` / `JoinFederation` flow unchanged).
3. **Bounded data loss.** Replication window documented; events committed AFTER the last replicated event are lost on primary kill (same as any leader-follower system).
4. **No split-brain.** Quorum-based promotion; minority partitions can't independently elect.
5. **Replay determinism preserved.** Events replayed by the new primary produce the same outcomes as the failed primary.
6. **Spec test simulates primary kill mid-federation.** Federate observes a hiccup but completes its workflow.

---

## 4. Wave structure (deferred)

W1 — pick + integrate consensus library (likely hashicorp/raft).
W2 — eventlog.Replicator interface + raftReplicator.
W3 — Federation lease + automatic promotion on lease expiry.
W4 — Federate reconnect loop + LookupFederationHost redirect.
W5 — AdminService PromoteFederation + QueryFederationRole.
W6 — Spec test: simulated primary kill + federate workflow completion.

Each wave is genuine distributed-systems work; estimating 1-2 weeks per wave for production correctness.

---

## 5. Dependencies

- **M15 cut-2** (multi-node) — M16 is meaningless without it.
- **Consensus library** — orchestrator decision pinned in M15 cut-2 plan.

---

## 6. M16 row append target

```markdown
| **M16** | Agent A | Hot standby + replay-driven RTI failover | Standby rtid replicates event log + can take over on primary failure; spec test simulates primary kill mid-federation. **DEFERRED** — requires M15 cut-2 (multi-node consensus). Plan at `docs/M16_DISPATCH_PLAN.md`. |
```
