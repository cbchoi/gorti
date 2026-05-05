package object

import (
	"context"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/core"
)

func TestRegistry_Snapshot_RecordsInstanceCount(t *testing.T) {
	t.Parallel()
	reg, declMgr, _ := newTestRegistry(t, nil)
	ctx := context.Background()
	// Federate 1 must publish before it can register an object instance.
	if err := declMgr.PublishObjectClassAttributes(ctx, "demo", 1, 7, []core.AttributeHandle{1}); err != nil {
		t.Fatalf("PublishObjectClassAttributes: %v", err)
	}
	if _, _, err := reg.Register(ctx, "demo", 1, 7, ""); err != nil {
		t.Fatalf("Register 1: %v", err)
	}
	if _, _, err := reg.Register(ctx, "demo", 1, 7, ""); err != nil {
		t.Fatalf("Register 2: %v", err)
	}
	snap := reg.Snapshot("demo")
	if snap.InstanceCount != 2 {
		t.Errorf("InstanceCount = %d, want 2", snap.InstanceCount)
	}
}

func TestRegistry_Snapshot_UnknownFederation_ReturnsZero(t *testing.T) {
	t.Parallel()
	reg, _, _ := newTestRegistry(t, nil)
	snap := reg.Snapshot("nope")
	if snap.InstanceCount != 0 {
		t.Errorf("InstanceCount unknown = %d, want 0", snap.InstanceCount)
	}
}
