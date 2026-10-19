package ownership

import (
	"context"
	"reflect"
	"sync"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func TestOwnershipPublicationRechecksEachAttributeAndPreservesRequests(t *testing.T) {
	ctx := context.Background()
	eligible := map[core.AttributeHandle]bool{}
	pending := 0
	m, out := newMgr(t, Options{
		Subscribers: func(context.Context, core.FederationName, core.ObjectHandle, []core.AttributeHandle) []core.FederateHandle {
			return []core.FederateHandle{1, 2, 3, 2}
		},
		PrepareAcquisition: func(_ context.Context, _ core.FederationName, h core.FederateHandle, _ core.ObjectHandle, attrs []core.AttributeHandle) (func(), error) {
			if h != 2 || !eligible[attrs[0]] {
				return nil, core.ErrAttributeNotPublished
			}
			pending++
			return sync.OnceFunc(func() { pending-- }), nil
		},
	})
	m.RegisterInitialOwnership("f", 1, 10, []core.AttributeHandle{1, 2, 3})
	for _, attr := range []core.AttributeHandle{1, 2} {
		if err := m.NegotiatedDivestTwoPhase(ctx, "f", 1, 10, []core.AttributeHandle{attr}, []byte{byte(attr), 0}); err != nil {
			t.Fatal(err)
		}
	}
	if err := m.UnconditionalDivest(ctx, "f", 1, 10, []core.AttributeHandle{3}); err != nil {
		t.Fatal(err)
	}
	eligible[1], eligible[2], eligible[3] = true, true, true
	m.ObjectClassPublished(ctx, "f", 2, 7, []core.AttributeHandle{3, 2, 1, 2})
	if pending != 0 || len(out.Sent()) != 3 {
		t.Fatal("lost object pin or separated offer", pending, out.Sent())
	}
	for i, sent := range out.Sent() {
		event := sent.Event.(*CallbackEvent)
		attr := core.AttributeHandle(i + 1)
		var tag []byte
		if attr < 3 {
			tag = []byte{byte(attr), 0}
		}
		if sent.Federate != 2 || event.Kind != CallbackAssumption || !reflect.DeepEqual(event.Attributes, []core.AttributeHandle{attr}) || !reflect.DeepEqual(event.Tag, tag) {
			t.Fatalf("request merged, misrouted or retagged: %+v %+v", sent, event)
		}
	}
	before := len(out.Sent())
	delete(eligible, 2)
	m.ObjectClassPublished(ctx, "f", 2, 7, []core.AttributeHandle{2})
	m.ObjectClassPublished(ctx, "f", 1, 7, []core.AttributeHandle{3})
	m.ObjectClassPublished(ctx, "f", 3, 7, []core.AttributeHandle{1, 2, 3})
	m.ObjectClassPublished(ctx, "other", 2, 7, []core.AttributeHandle{1, 2, 3})
	if len(out.Sent()) != before {
		t.Fatal("nonpublisher, unknown recipient, former divester, or wrong federation solicited")
	}
	if err := m.Acquire(ctx, "f", 2, 10, []core.AttributeHandle{1}, nil); err != nil {
		t.Fatal(err)
	}
	if err := m.CancelDivest(ctx, "f", 1, 10, []core.AttributeHandle{2}); err != nil {
		t.Fatal(err)
	}
	if err := m.AcquireIfAvailable(ctx, "f", 2, 10, []core.AttributeHandle{3}, nil); err != nil {
		t.Fatal(err)
	}
	before = len(out.Sent())
	eligible[2] = true
	m.ObjectClassPublished(ctx, "f", 2, 7, []core.AttributeHandle{1, 2, 3})
	if len(out.Sent()) != before || len(m.ExportState("f").UnownedOffers) != 0 {
		t.Fatal("acquiring, cancelled, or completed offer was solicited")
	}
}

func TestOwnershipUnownedOfferCleanupIsCommitOnly(t *testing.T) {
	ctx := context.Background()
	m, _ := newMgr(t, Options{})
	m.RegisterInitialOwnership("f", 1, 10, []core.AttributeHandle{1})
	if err := m.UnconditionalDivest(ctx, "f", 1, 10, []core.AttributeHandle{1}); err != nil {
		t.Fatal(err)
	}
	before := m.ExportState("f")
	plan, err := m.PrepareResign("f", 1, core.ResignActionNoAction, []core.ObjectHandle{10})
	if err != nil {
		t.Fatal(err)
	}
	plan.Release()
	if !reflect.DeepEqual(before, m.ExportState("f")) {
		t.Fatal("aborted resign changed pending offers")
	}
	plan, err = m.PrepareResign("f", 1, core.ResignActionNoAction, nil)
	if err != nil {
		t.Fatal(err)
	}
	plan.Commit()
	state := m.ExportState("f")
	if len(state.UnownedOffers) != 1 || state.UnownedOffers[0].Divester != 0 {
		t.Fatal("resigned former owner retained a non-restorable identity", state)
	}
	m.ImportState("restored", state)
	if !reflect.DeepEqual(state, m.ExportState("restored")) {
		t.Fatal("domain snapshot lost continuation")
	}
	m.OnObjectDeleted("f", 10)
	if len(m.ExportState("f").UnownedOffers) != 0 {
		t.Fatal("deleted object retained an offer")
	}
	if err := m.PrepareObjectReconstitution("restored", 10, map[core.AttributeHandle]core.FederateHandle{1: 2}).Commit(); err != nil {
		t.Fatal(err)
	}
	if len(m.ExportState("restored").UnownedOffers) != 0 {
		t.Fatal("reconstituted ownership retained an obsolete offer")
	}
}

func TestOwnershipPublicationOfferStopsAfterReentrantAcquisition(t *testing.T) {
	ctx := context.Background()
	var m *Manager
	var callbacks int
	out := &assumptionActionOutbox{out: &fakeOutbox{}, action: func(h core.FederateHandle, e *CallbackEvent) {
		if e.Kind == CallbackAssumption {
			callbacks++
			if err := m.AcquireIfAvailable(ctx, "f", h, 10, []core.AttributeHandle{1, 2}, nil); err != nil {
				t.Error(err)
			}
		}
	}}
	var err error
	m, err = New(Options{Outbox: out, Subscribers: func(context.Context, core.FederationName, core.ObjectHandle, []core.AttributeHandle) []core.FederateHandle {
		return []core.FederateHandle{2}
	}, PrepareAcquisition: func(context.Context, core.FederationName, core.FederateHandle, core.ObjectHandle, []core.AttributeHandle) (func(), error) {
		return nil, core.ErrAttributeNotPublished
	}})
	if err != nil {
		t.Fatal(err)
	}
	m.RegisterInitialOwnership("f", 1, 10, []core.AttributeHandle{1, 2})
	if err := m.UnconditionalDivest(ctx, "f", 1, 10, []core.AttributeHandle{1, 2}); err != nil {
		t.Fatal(err)
	}
	m.opts.PrepareAcquisition = nil
	m.ObjectClassPublished(ctx, "f", 2, 7, []core.AttributeHandle{1, 2})
	if callbacks != 1 || !m.IsOwnedBy("f", 2, 10, 2) {
		t.Fatal("publication continued after ownership was acquired", callbacks)
	}
}
