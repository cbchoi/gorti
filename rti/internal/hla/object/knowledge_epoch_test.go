package object

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func TestKnowledgeEpochRediscovery(t *testing.T) {
	for _, sameClass := range []bool{false, true} {
		name := "child"
		if sameClass {
			name = "same-class"
		}
		t.Run(name, func(t *testing.T) {
			r, decl, _, obj := knownOwnershipPreparationFixture(t)
			ctx := context.Background()
			before, err := r.CaptureKnownObjectAttributes(ctx, "fed", 2, obj, []core.AttributeHandle{1})
			transportationOK(t, err)
			owner, err := r.CaptureKnownObjectAttributes(ctx, "fed", 1, obj, []core.AttributeHandle{1})
			transportationOK(t, err)
			if sameClass {
				transportationOK(t, decl.UnsubscribeObjectClassAttributes(ctx, "fed", 2, 1, nil))
			}
			transportationOK(t, before.Validate())
			transportationOK(t, r.LocalDelete(ctx, "fed", 2, obj))
			if !errors.Is(before.Validate(), ErrStaleObjectKnowledge) {
				t.Fatal("rediscovery reused an old knowledge epoch")
			}
			transportationOK(t, owner.Validate())
			after, err := r.CaptureKnownObjectAttributes(ctx, "fed", 2, obj, []core.AttributeHandle{1})
			transportationOK(t, err)
			transportationOK(t, after.Validate())
			want := core.ObjectClassHandle(1)
			if sameClass {
				want = 9
			}
			if after.KnownClass() != want || after.Epoch() == before.Epoch() {
				t.Fatalf("new knowledge class=%d epoch reused=%t", after.KnownClass(), after.Epoch() == before.Epoch())
			}
		})
	}
}

func TestKnowledgeEpochSnapshotAndLockFreeValidation(t *testing.T) {
	r, _, _, obj := knownOwnershipPreparationFixture(t)
	input := []core.AttributeHandle{1}
	projection, err := r.CaptureKnownObjectAttributes(context.Background(), "fed", 2, obj, input)
	transportationOK(t, err)
	input[0] = 999
	projection.Attributes()[0] = 999
	projection.RegisteredAttributes()[0] = 999
	if projection.KnownClass() != 9 || !slices.Equal(projection.Attributes(), []core.AttributeHandle{101}) || !slices.Equal(projection.RegisteredAttributes(), []core.AttributeHandle{1}) {
		t.Fatal("projection aliases caller-owned coordinates")
	}
	st := r.stateFor("fed")
	if !st.mu.TryLock() {
		t.Fatal("projection retained the object lock")
	}
	// Queue guards must also be callable while another path holds object state.
	err = projection.Validate()
	st.mu.Unlock()
	transportationOK(t, err)
	state := r.ExportState("fed")
	commit, err := r.PrepareImportState("fed", state)
	transportationOK(t, err)
	transportationOK(t, projection.Validate())
	commit()
	if !errors.Is(projection.Validate(), ErrStaleObjectKnowledge) {
		t.Fatal("restore retained a pre-restore callback capability")
	}
	restored, err := r.CaptureKnownObjectAttributes(context.Background(), "fed", 2, obj, []core.AttributeHandle{1})
	transportationOK(t, err)
	transportationOK(t, restored.Validate())
	if restored.KnownClass() != projection.KnownClass() || !slices.Equal(restored.Attributes(), projection.Attributes()) {
		t.Fatal("restore changed persisted known-class coordinates")
	}
	r.OnFederationDestroyed("fed")
	if !errors.Is(restored.Validate(), ErrStaleObjectKnowledge) {
		t.Fatal("destroy retained a callback capability")
	}
}

func TestKnowledgeEpochRejectedOperationsPreserveKnowledge(t *testing.T) {
	r, _, owners, obj := knownOwnershipPreparationFixture(t)
	ctx := context.Background()
	projection, err := r.CaptureKnownObjectAttributes(ctx, "fed", 2, obj, []core.AttributeHandle{1})
	transportationOK(t, err)
	if _, err := r.CaptureKnownObjectAttributes(ctx, "fed", 2, obj, []core.AttributeHandle{1, 999}); !errors.Is(err, core.ErrAttributeNotFound) {
		t.Fatalf("invalid later attribute accepted: %v", err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := r.CaptureKnownObjectAttributes(cancelled, "fed", 2, obj, []core.AttributeHandle{1}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled projection accepted: %v", err)
	}
	transportationOK(t, owners.Acquire(ctx, "fed", 2, obj, []core.AttributeHandle{1}, nil))
	if err := r.LocalDelete(ctx, "fed", 2, obj); !errors.Is(err, core.ErrOwnershipAcquisitionPending) {
		t.Fatalf("local deletion bypassed pending acquisition: %v", err)
	}
	transportationOK(t, projection.Validate())
	if err := (KnownAttributeProjection{}).Validate(); !errors.Is(err, ErrStaleObjectKnowledge) {
		t.Fatalf("zero projection accepted: %v", err)
	}
}
