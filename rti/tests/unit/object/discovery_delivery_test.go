package object_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/declaration"
	"github.com/cbchoi/gorti/rti/internal/hla/object"
)

type knowledgeOutbox struct {
	registry      *object.Registry
	failDiscovery bool
	failCommit    bool
	unknown       bool
	kinds         []object.CallbackKind
}

func (o *knowledgeOutbox) Send(_ context.Context, fed core.FederationName, h core.FederateHandle, event core.OutboundEvent) error {
	e := event.(*object.CallbackEvent)
	if o.failDiscovery && e.Kind == object.CallbackDiscover {
		return errors.New("discovery queue full")
	}
	if _, _, known := o.registry.LookupKnownObjectInstance(fed, h, e.Object); !known {
		o.unknown = true
	}
	o.kinds = append(o.kinds, e.Kind)
	return nil
}

func (o *knowledgeOutbox) Reserve(_ context.Context, fed core.FederationName, deliveries []core.OutboxDelivery) (core.OutboxReservation, error) {
	for _, delivery := range deliveries {
		if o.failDiscovery && delivery.Event.(*object.CallbackEvent).Kind == object.CallbackDiscover {
			return nil, errors.New("discovery queue full")
		}
	}
	return &knowledgeReservation{o, fed, slices.Clone(deliveries)}, nil
}

type knowledgeReservation struct {
	out        *knowledgeOutbox
	fed        core.FederationName
	deliveries []core.OutboxDelivery
}

func (p *knowledgeReservation) Commit() error {
	if p.out.failCommit {
		return errors.New("federation generation changed")
	}
	for _, d := range p.deliveries {
		if err := p.out.Send(context.Background(), p.fed, d.Recipient, d.Event); err != nil {
			return err
		}
	}
	return nil
}

func (p *knowledgeReservation) Release() { p.deliveries = nil }

func knowledgeFixture(t *testing.T) (*object.Registry, *knowledgeOutbox, core.ObjectHandle) {
	t.Helper()
	d := declaration.New()
	out := &knowledgeOutbox{}
	r, err := object.New(object.Options{Declarations: d, Outbox: out, FOMs: restoreFOMs{}, Clock: core.NewRealClock()})
	if err != nil {
		t.Fatal(err)
	}
	out.registry = r
	ctx := context.Background()
	if err := d.PublishObjectClassAttributes(ctx, "known", 1, 7, []core.AttributeHandle{1}); err != nil {
		t.Fatal(err)
	}
	if err := d.SubscribeObjectClassAttributes(ctx, "known", 2, 7, []core.AttributeHandle{1}); err != nil {
		t.Fatal(err)
	}
	obj, _, err := r.Register(ctx, "known", 1, 7, "")
	if err != nil {
		t.Fatal(err)
	}
	return r, out, obj
}

func TestDiscoveryKnowledgePrecedesCallback(t *testing.T) {
	_, out, _ := knowledgeFixture(t)
	if out.unknown {
		t.Fatal("discovery callback observed an unknown object")
	}
}

func TestLocalDeleteRediscoveryAndReflectAdmission(t *testing.T) {
	r, out, obj := knowledgeFixture(t)
	ctx := context.Background()
	out.failDiscovery = true
	if err := r.LocalDelete(ctx, "known", 2, obj); err != nil {
		t.Fatal(err)
	}
	if _, _, known := r.LookupKnownObjectInstance("known", 2, obj); known {
		t.Fatal("failed rediscovery retained knowledge")
	}
	out.kinds = nil
	if err := r.UpdateAttributes(ctx, "known", 1, obj, map[core.AttributeHandle][]byte{1: {1}}, nil); err == nil {
		t.Fatal("update accepted while discovery cannot be admitted")
	}
	if len(out.kinds) != 0 {
		t.Fatalf("partial callbacks: %v", out.kinds)
	}
	out.failDiscovery = false
	out.failCommit = true
	if err := r.UpdateAttributes(ctx, "known", 1, obj, map[core.AttributeHandle][]byte{1: {1}}, nil); err == nil {
		t.Fatal("update accepted after failed commit")
	}
	if _, _, known := r.LookupKnownObjectInstance("known", 2, obj); known {
		t.Fatal("failed commit retained knowledge")
	}
	out.failCommit = false
	out.unknown = false
	if err := r.UpdateAttributes(ctx, "known", 1, obj, map[core.AttributeHandle][]byte{1: {1}}, nil); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(out.kinds, []object.CallbackKind{object.CallbackDiscover, object.CallbackAttributesInScope, object.CallbackReflect}) || out.unknown {
		t.Fatalf("callback order/knowledge: %v unknown=%v", out.kinds, out.unknown)
	}
	out.kinds = nil
	if err := r.UpdateAttributes(ctx, "known", 1, obj, map[core.AttributeHandle][]byte{1: {1}}, nil); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(out.kinds, []object.CallbackKind{object.CallbackReflect}) {
		t.Fatalf("duplicate discovery: %v", out.kinds)
	}
}
