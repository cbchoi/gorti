// Package cluster — distributed RTI cluster manager (M15 cut-1).
//
// Cut-1 ships single-node-correct: every federation maps to self,
// no peer awareness, no consensus. Cut-2 (deferred — needs Raft
// integration) replaces the in-memory assignment table with a
// replicated log + handles failover via M16.
//
// The surface here is what cut-2 must conform to; the cut-1
// implementation is the simplest correct version.

package cluster

import (
	"sync"

	"github.com/cbchoi/gorti/rti/internal/core"
)

// Status mirrors LookupFederationHostResponse_Status at the proto
// surface. Kept as a separate type so non-grpc callers (admin tooling,
// tests) don't pull in the proto package.
type Status uint8

const (
	StatusUnspecified Status = iota
	StatusNotFound
	StatusCurrent
	StatusRedirect
)

// LookupResult bundles the answer to a federation host lookup.
type LookupResult struct {
	Status      Status
	HostAddress string // populated on Redirect
	HostNodeID  string // populated on Current + Redirect
}

// Node bundles the public-facing cluster membership entry.
type Node struct {
	NodeID  string
	Address string
	IsSelf  bool
}

// Manager owns the federation-to-node assignment table and the
// cluster membership view from this node's perspective.
type Manager struct {
	selfID   string
	selfAddr string

	mu          sync.RWMutex
	assignments map[core.FederationName]string // federation → node_id
	nodes       map[string]string               // node_id → address (always includes self)
}

// New constructs a single-node cluster manager. selfAddr is the
// host:port the federate uses to dial this rtid (typically the
// --listen flag value); selfID is a stable opaque ID. M15 cut-1
// uses selfAddr as selfID when selfID == "" (callers should pass a
// stable UUID once cut-2 lands).
func New(selfID, selfAddr string) *Manager {
	if selfID == "" {
		selfID = selfAddr
	}
	m := &Manager{
		selfID:      selfID,
		selfAddr:    selfAddr,
		assignments: map[core.FederationName]string{},
		nodes:       map[string]string{selfID: selfAddr},
	}
	return m
}

// AssignFederation records a federation→self assignment. M15 cut-1
// always assigns to self; cut-2 (deferred) consults the replicated
// assignment table.
func (m *Manager) AssignFederation(name core.FederationName) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.assignments[name] = m.selfID
}

// UnassignFederation removes a federation from the assignment table.
// Called from federation.Manager.OnDestroyFederation in a future cut;
// M15 cut-1 ships the surface only.
func (m *Manager) UnassignFederation(name core.FederationName) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.assignments, name)
}

// Lookup returns the host of the given federation. Returns
// StatusNotFound when the federation has no assignment.
func (m *Manager) Lookup(name core.FederationName) LookupResult {
	m.mu.RLock()
	defer m.mu.RUnlock()
	hostID, ok := m.assignments[name]
	if !ok {
		return LookupResult{Status: StatusNotFound}
	}
	if hostID == m.selfID {
		return LookupResult{
			Status:     StatusCurrent,
			HostNodeID: m.selfID,
		}
	}
	return LookupResult{
		Status:      StatusRedirect,
		HostAddress: m.nodes[hostID],
		HostNodeID:  hostID,
	}
}

// Nodes returns the current membership view. Includes self always.
func (m *Manager) Nodes() []Node {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Node, 0, len(m.nodes))
	for id, addr := range m.nodes {
		out = append(out, Node{
			NodeID:  id,
			Address: addr,
			IsSelf:  id == m.selfID,
		})
	}
	return out
}

// SelfID returns the node's stable identifier.
func (m *Manager) SelfID() string { return m.selfID }

// SelfAddress returns the node's federate-facing dial address.
// M15 cut-2 broadcasts use this so peers can record the host in
// their assignment table.
func (m *Manager) SelfAddress() string { return m.selfAddr }

// --- M15 cut-2 demo additions ---------------------------------------------
//
// The single-node manager already carries the full surface; the
// cut-2 demo just lifts the "self is always the host" invariant
// and adds RegisterPeer + RecordAssignment for cross-node gossip.

// RegisterPeer adds (or updates) a peer node's address in the
// membership table. Idempotent. Called during rtid bootstrap from
// the --cluster-peers flag and on incoming gossip RPCs.
func (m *Manager) RegisterPeer(nodeID, address string) {
	if nodeID == "" || nodeID == m.selfID {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nodes[nodeID] = address
}

// RecordAssignment writes a federation→node mapping into the table.
// Used by the NotifyAssignment RPC handler to record an assignment
// the local node didn't make (last-writer-wins gossip; M15 cut-3
// will replace with Raft-replicated state).
func (m *Manager) RecordAssignment(name core.FederationName, hostNodeID string) {
	if hostNodeID == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.assignments[name] = hostNodeID
}

// AssignmentsSnapshot returns a defensive copy of the current
// federation→node_id assignment table. M15 cut-2 demo uses this to
// gossip the full table on peer join (best-effort initial sync).
func (m *Manager) AssignmentsSnapshot() map[core.FederationName]string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make(map[core.FederationName]string, len(m.assignments))
	for k, v := range m.assignments {
		out[k] = v
	}
	return out
}

// HostsLocally reports whether the calling node currently hosts
// the named federation. Convenience wrapper over Lookup; used by
// rtid service handlers that need to reject ops on non-hosted
// federations.
func (m *Manager) HostsLocally(name core.FederationName) bool {
	r := m.Lookup(name)
	return r.Status == StatusCurrent
}
