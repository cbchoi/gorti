package declaration

import (
	"context"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/core"
)

func TestP0LifecycleCleanupRemovesFederateAndFederationState(t *testing.T) {
	m := New()
	ctx := context.Background()
	if err := m.PublishObjectClassAttributes(ctx, "fed", 1, 2, []core.AttributeHandle{3}); err != nil {
		t.Fatal(err)
	}
	if err := m.SubscribeInteractionClass(ctx, "fed", 1, 4); err != nil {
		t.Fatal(err)
	}
	if err := m.PublishInteractionClass(ctx, "fed", 2, 4); err != nil {
		t.Fatal(err)
	}

	m.OnFederateResign("fed", 1)
	snapshot := m.Snapshot("fed")
	if _, ok := snapshot.PerFederate[1]; ok {
		t.Fatal("resigned federate remains in declaration snapshot")
	}
	if _, ok := snapshot.PerFederate[2]; !ok {
		t.Fatal("surviving federate was removed")
	}

	m.OnFederationDestroyed("fed")
	if got := len(m.Snapshot("fed").PerFederate); got != 0 {
		t.Fatalf("destroyed federation declaration cardinality = %d", got)
	}
}
