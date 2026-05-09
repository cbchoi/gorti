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
