package object

import (
	"context"
	"errors"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"testing"
)

func TestDeleteAdmissionFailurePreservesObject(t *testing.T) {
	r, decl, _ := newTestRegistry(t, nil)
	ctx := context.Background()
	if err := decl.PublishObjectClassAttributes(ctx, "fed", 1, 1, []core.AttributeHandle{1}); err != nil {
		t.Fatal(err)
	}
	if err := decl.SubscribeObjectClassAttributes(ctx, "fed", 2, 1, []core.AttributeHandle{1}); err != nil {
		t.Fatal(err)
	}
	reserveFixtureName(t, r, "fed", 1, "survives-delete")
	obj, _, err := r.Register(ctx, "fed", 1, 1, "survives-delete")
	if err != nil {
		t.Fatal(err)
	}
	failure := errors.New("full removal outbox")
	r.opts.Outbox = &atomicUpdateOutbox{reserveErr: failure, sendErr: failure}
	if err := r.Delete(ctx, "fed", 1, obj, nil, []byte("tag")); !errors.Is(err, failure) {
		t.Errorf("delete hid callback admission error: %v", err)
	}
	st := r.stateFor("fed")
	st.mu.Lock()
	_, exists := st.instances[obj]
	st.mu.Unlock()
	if !exists {
		t.Fatal("failed delete erased object")
	}
}

func TestRetractableDeleteWithoutTimestampRejectedBeforeEffects(t *testing.T) {
	r, decl, _ := newTestRegistry(t, nil)
	ctx := context.Background()
	if err := decl.PublishObjectClassAttributes(ctx, "fed", 1, 1, []core.AttributeHandle{1}); err != nil {
		t.Fatal(err)
	}
	reserveFixtureName(t, r, "fed", 1, "retained")
	obj, _, err := r.Register(ctx, "fed", 1, 1, "retained")
	if err != nil {
		t.Fatal(err)
	}
	if err := r.DeleteWithMetadata(ctx, "fed", 1, obj, nil, core.ObjectMessageOptions{RetractionHandle: 1}); !errors.Is(err, core.ErrTimeInvalidLogicalTime) {
		t.Fatalf("delete error=%v", err)
	}
	if r.stateFor("fed").instances[obj] == nil {
		t.Fatal("unsupported deletion changed state")
	}
}
