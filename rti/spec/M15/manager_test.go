// TASK-309 (M15 W1) — single-node cluster manager surface.

package m15spec

import (
	"testing"

	"github.com/cbchoi/gorti/rti/internal/cluster"
	"github.com/cbchoi/gorti/rti/internal/core"
)

// TestSpec_M15_NewSingleNodeAssignsSelf — fresh manager has self in
// the membership view.
func TestSpec_M15_NewSingleNodeAssignsSelf(t *testing.T) {
	mgr := cluster.New("node-a", "127.0.0.1:8442")
	nodes := mgr.Nodes()
	if len(nodes) != 1 {
		t.Fatalf("Nodes len = %d, want 1", len(nodes))
	}
	if nodes[0].NodeID != "node-a" || !nodes[0].IsSelf {
		t.Errorf("Node[0] = %+v, want {node-a, IsSelf=true}", nodes[0])
	}
}

// TestSpec_M15_LookupUnknownReturnsNotFound.
func TestSpec_M15_LookupUnknownReturnsNotFound(t *testing.T) {
	mgr := cluster.New("node-a", "127.0.0.1:8442")
	r := mgr.Lookup(core.FederationName("nonexistent"))
	if r.Status != cluster.StatusNotFound {
		t.Errorf("Lookup unknown = %v, want StatusNotFound", r.Status)
	}
}

// TestSpec_M15_AssignThenLookupReturnsCurrent.
func TestSpec_M15_AssignThenLookupReturnsCurrent(t *testing.T) {
	mgr := cluster.New("node-a", "127.0.0.1:8442")
	mgr.AssignFederation("fed-1")
	r := mgr.Lookup("fed-1")
	if r.Status != cluster.StatusCurrent {
		t.Errorf("Lookup after assign = %v, want StatusCurrent", r.Status)
	}
	if r.HostNodeID != "node-a" {
		t.Errorf("HostNodeID = %q, want node-a", r.HostNodeID)
	}
}

// TestSpec_M15_UnassignDropsAssignment.
func TestSpec_M15_UnassignDropsAssignment(t *testing.T) {
	mgr := cluster.New("node-a", "127.0.0.1:8442")
	mgr.AssignFederation("fed-1")
	mgr.UnassignFederation("fed-1")
	r := mgr.Lookup("fed-1")
	if r.Status != cluster.StatusNotFound {
		t.Errorf("Lookup after unassign = %v, want StatusNotFound", r.Status)
	}
}

// TestSpec_M15_SelfIDFallback — empty selfID falls back to selfAddr.
func TestSpec_M15_SelfIDFallback(t *testing.T) {
	mgr := cluster.New("", "127.0.0.1:8442")
	if mgr.SelfID() != "127.0.0.1:8442" {
		t.Errorf("SelfID = %q, want fallback to selfAddr", mgr.SelfID())
	}
}

// --- M15 cut-2 demo additions ---------------------------------------------

// TestSpec_M15_2_RegisterPeerAddsToMembership — RegisterPeer makes a
// peer node visible via Nodes().
func TestSpec_M15_2_RegisterPeerAddsToMembership(t *testing.T) {
	mgr := cluster.New("node-a", "127.0.0.1:8442")
	mgr.RegisterPeer("node-b", "127.0.0.1:8443")
	nodes := mgr.Nodes()
	if len(nodes) != 2 {
		t.Fatalf("Nodes len = %d, want 2", len(nodes))
	}
	foundB := false
	for _, n := range nodes {
		if n.NodeID == "node-b" {
			foundB = true
			if n.IsSelf {
				t.Errorf("node-b reports IsSelf=true on node-a")
			}
			if n.Address != "127.0.0.1:8443" {
				t.Errorf("node-b address = %q, want 127.0.0.1:8443", n.Address)
			}
		}
	}
	if !foundB {
		t.Errorf("node-b not in Nodes()")
	}
}

// TestSpec_M15_2_RegisterPeerSelfIsNoop — registering oneself is a no-op.
func TestSpec_M15_2_RegisterPeerSelfIsNoop(t *testing.T) {
	mgr := cluster.New("node-a", "127.0.0.1:8442")
	mgr.RegisterPeer("node-a", "different:9999")
	nodes := mgr.Nodes()
	if len(nodes) != 1 {
		t.Errorf("after self-RegisterPeer Nodes len = %d, want 1", len(nodes))
	}
	for _, n := range nodes {
		if n.Address == "different:9999" {
			t.Errorf("self-RegisterPeer overwrote self address")
		}
	}
}

// TestSpec_M15_2_RecordAssignmentRemoteHostsRedirect — recording a
// federation→peer assignment makes Lookup return REDIRECT with the
// peer's address.
func TestSpec_M15_2_RecordAssignmentRemoteHostsRedirect(t *testing.T) {
	mgr := cluster.New("node-a", "127.0.0.1:8442")
	mgr.RegisterPeer("node-b", "127.0.0.1:8443")
	mgr.RecordAssignment("fed-1", "node-b")
	r := mgr.Lookup("fed-1")
	if r.Status != cluster.StatusRedirect {
		t.Errorf("Lookup remote = %v, want StatusRedirect", r.Status)
	}
	if r.HostNodeID != "node-b" {
		t.Errorf("HostNodeID = %q, want node-b", r.HostNodeID)
	}
	if r.HostAddress != "127.0.0.1:8443" {
		t.Errorf("HostAddress = %q, want 127.0.0.1:8443", r.HostAddress)
	}
}

// TestSpec_M15_2_RecordAssignmentSelfHostsCurrent — recording a
// federation→self assignment returns CURRENT.
func TestSpec_M15_2_RecordAssignmentSelfHostsCurrent(t *testing.T) {
	mgr := cluster.New("node-a", "127.0.0.1:8442")
	mgr.RecordAssignment("fed-self", "node-a")
	r := mgr.Lookup("fed-self")
	if r.Status != cluster.StatusCurrent {
		t.Errorf("Lookup self-recorded = %v, want StatusCurrent", r.Status)
	}
}

// TestSpec_M15_2_AssignmentsSnapshotReflectsState.
func TestSpec_M15_2_AssignmentsSnapshotReflectsState(t *testing.T) {
	mgr := cluster.New("node-a", "127.0.0.1:8442")
	mgr.AssignFederation("fed-1")            // self
	mgr.RegisterPeer("node-b", "addr-b")
	mgr.RecordAssignment("fed-2", "node-b")  // remote
	snap := mgr.AssignmentsSnapshot()
	if len(snap) != 2 {
		t.Fatalf("snapshot len = %d, want 2", len(snap))
	}
	if snap["fed-1"] != "node-a" || snap["fed-2"] != "node-b" {
		t.Errorf("snapshot = %v", snap)
	}
}

// TestSpec_M15_2_HostsLocallyTrueForSelfAssigned.
func TestSpec_M15_2_HostsLocallyTrueForSelfAssigned(t *testing.T) {
	mgr := cluster.New("node-a", "127.0.0.1:8442")
	mgr.AssignFederation("fed-1")
	if !mgr.HostsLocally("fed-1") {
		t.Errorf("HostsLocally(fed-1) = false, want true")
	}
	mgr.RegisterPeer("node-b", "addr-b")
	mgr.RecordAssignment("fed-remote", "node-b")
	if mgr.HostsLocally("fed-remote") {
		t.Errorf("HostsLocally(fed-remote) = true, want false")
	}
}
