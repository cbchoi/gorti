# M15 Dispatch Plan — Distributed RTI: multi-process federation hosting

How the orchestrator dispatches the M15 tasks. Frozen scope; agents follow.

---

## 0. Scope honesty (READ FIRST)

**M15 is genuinely distributed-systems-hard work.** Production-correct distributed RTI requires:

1. Cluster membership (failure detection, view consistency).
2. Federation-to-node routing with cluster-wide consistent assignments.
3. State replication for the hosting node (so federation state survives node crashes).
4. Membership-change handling (federation reassignment when nodes join/leave).
5. Cross-node event ordering for the federation that spans them.

Each item is a distributed-systems sub-project of its own. **The first milestone-cut of M15 ships only the surface contract + a single-node-cluster correct implementation**; multi-node distributed correctness is a follow-up cut that depends on choosing a consensus protocol (Raft is the most common path) and integrating it with the existing rtid runtime.

This document pins the M15 surface so the multi-node work can land without churning the API.

---

## 1. Goal & non-goals

### Goal (M15 single-node-cluster cut)

A federate connects to ANY rtid in the cluster, asks "which rtid hosts federation X?", and dials that rtid (or follows a transparent server-side forward). Returns a deterministic federation→node mapping; for N=1 the mapping is "this node hosts everything," matching pre-M15 behavior. For N>1 the mapping requires cluster membership to be live — implementation deferred.

### Goal (M15 multi-node cut, deferred)

Cluster of N rtid processes maintains a federation→node assignment via a consensus protocol. Federate routing transparently follows reassignment on node failure (shifts to M16's failover scope; M15 only tracks live assignments, not failover).

### Non-goals (always)

- **Federation state replication across nodes** — M16 territory.
- **Strong consistency over partitions** — gorti chooses CP (Raft) for the assignment table; partitioned minorities lose write capability.
- **Federate-side cluster awareness** — the SDK dials any node; the hosting node either serves directly or returns a `RoutingHint` directing the SDK to redial.
- **Hot-add of new federations** — federation creation routes through the assignment table the same way as JoinFederation.

---

## 2. Surface design

### 2.1 New ClusterService (proto)

```proto
service ClusterService {
  // ListClusterNodes — returns the live cluster membership view.
  // Single-node deployments return [{Self, Address}].
  rpc ListClusterNodes(ListClusterNodesRequest) returns (ListClusterNodesResponse);

  // LookupFederationHost — given a federation name, returns the
  // address of the rtid that hosts it. Returns NOT_FOUND if the
  // federation doesn't exist; CURRENT (this node) when this node
  // hosts it; or REDIRECT (other node) with the host's address.
  rpc LookupFederationHost(LookupFederationHostRequest) returns (LookupFederationHostResponse);

  // ReportNodeHealth — heartbeat from peer nodes (multi-node only).
  rpc ReportNodeHealth(ReportNodeHealthRequest) returns (Empty);
}

message ClusterNode {
  string node_id = 1;       // Stable opaque ID assigned at cluster join.
  string address = 2;       // host:port for federate dialing.
  bool is_self = 3;         // True for the node that served this RPC.
}

message LookupFederationHostResponse {
  enum Status {
    NOT_FOUND = 0;
    CURRENT = 1;             // This node hosts the federation.
    REDIRECT = 2;            // Another node hosts; host_address is set.
  }
  Status status = 1;
  string host_address = 2;
  string host_node_id = 3;
}
```

### 2.2 Cluster manager (rti/internal/cluster/)

- `Manager` — tracks cluster nodes + federation→node assignments. Single-node mode: `nodes = [self]`, every federation maps to self.
- `Manager.AssignFederation(name)` — picks a host node for a new federation. Single-node returns self; multi-node (deferred) consults the consensus log.
- `Manager.Lookup(name)` — returns the (node_id, address) that hosts the federation, or `(empty, false)` if unknown.

### 2.3 Federate routing

- `federate.Connect` unchanged. Internally, after dialing, the SDK calls `ClusterService.LookupFederationHost(federation_name)` BEFORE `JoinFederation`.
- If `Status == CURRENT`: proceed with `JoinFederation` on the current channel.
- If `Status == REDIRECT`: close the current channel, dial `host_address`, retry. Bounded redirect-follow count (default 3) to prevent loops.
- If `Status == NOT_FOUND` and the federate is creating: `CreateFederation` on the current node, which calls `Manager.AssignFederation` to make the assignment.

### 2.4 Single-node mode (M15 cut-1)

The simplest correct shape: `--cluster-size=1` (default). Behavior identical to pre-M15:
- `ListClusterNodes` → [{self, listen_addr, is_self=true}].
- `LookupFederationHost` → CURRENT for any known federation; NOT_FOUND otherwise.
- All federations created on this node; no redirect.

### 2.5 Multi-node mode (M15 cut-2, deferred)

Requires picking a consensus library — Raft (e.g. hashicorp/raft) is the natural choice. Federation→node assignments live in the Raft log; cluster membership is also Raft-managed. Heartbeat liveness updates the address table.

Out of M15 scope: byzantine fault tolerance, geo-distributed clusters, dynamic resharding.

---

## 3. Acceptance criteria (single-node cut)

1. **`ClusterService.ListClusterNodes` returns one entry** with `is_self=true`.
2. **`LookupFederationHost(known)` returns CURRENT.**
3. **`LookupFederationHost(unknown)` returns NOT_FOUND.**
4. **Existing examples unchanged** — single-node mode is a no-op for current users.
5. **SDK lookup-then-redirect path works** — set up a 2-node mock test where node A redirects to B; SDK transparently follows.

Multi-node correctness ACs (deferred):
- Federation reassignment on node failure (M16 territory).
- Consistent federation→node mapping under partition.
- Cross-node event ordering.

---

## 4. Wave structure

W1 — proto + cluster.Manager (single-node). Spec test for surface.
W2 — federate SDK lookup-then-redirect. Cross-node redirect test (using two bufconn rtids).
W3 — cmd/rtid `--cluster-listen-addr` + `--cluster-peers` flags (placeholder; multi-node disabled).
W4 — Multi-node cut (deferred): pick Raft library, integrate, federation assignment in raft log.

M15 cut-1 ships W1+W2+W3. W4 is a separate dispatch.

---

## 5. Tasks (cut-1)

- TASK-304: proto/rti/v1/cluster.proto + buf generate.
- TASK-305: rti/internal/cluster/manager.go (single-node). 
- TASK-306: rti/internal/transport/grpc/cluster.go handlers.
- TASK-307: rti/pkg/federate lookup-then-redirect logic.
- TASK-308: cmd/rtid wiring + flags (no-op in single-node).
- TASK-309: rti/spec/M15/* — surface tests + redirect smoke (mock).
- TASK-310: srs.md M15 row (mark "cut-1 done; multi-node cut-2 deferred").
- TASK-311: CHANGELOG entry + check-milestones M15 probe.

---

## 6. Out of scope explicitly

- Multi-node consensus integration (W4, separate dispatch).
- Federation state replication.
- Live federation reassignment.
- Cross-node event ordering.
- Cluster-aware admin RPC.

---

## 7. M15 row append target

```markdown
| **M15** | Agent A | Distributed RTI: multi-process federation hosting | Cut-1: ClusterService surface + single-node correct (mapping always self); SDK lookup-then-redirect supported. Cut-2 (deferred): multi-node consensus via Raft. **CUT-1 DONE 2026-MM-DD**; multi-node cut tracked separately. |
```
