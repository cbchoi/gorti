package object_test

import (
	"context"
	"reflect"
	"slices"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/declaration"
	"github.com/cbchoi/gorti/rti/internal/hla/object"
)

type regionDeliveryFilter struct {
	bindings map[core.AttributeHandle][]object.DDMRegionHandle
	regional map[core.AttributeHandle][]core.FederateHandle
	overlaps map[core.AttributeHandle][]core.FederateHandle
}

func (d *regionDeliveryFilter) HasObjectAssociations(core.FederationName, core.ObjectHandle) bool {
	return len(d.bindings) != 0
}

func (d *regionDeliveryFilter) PublisherRegionsFor(_ core.FederationName, _ core.ObjectHandle, attr core.AttributeHandle) []object.DDMRegionHandle {
	return d.bindings[attr]
}

func (d *regionDeliveryFilter) SubscribersForUpdate(_ core.FederationName, _ core.ObjectClassHandle, attr core.AttributeHandle, _ []object.DDMRegionHandle) []core.FederateHandle {
	return d.overlaps[attr]
}

func (d *regionDeliveryFilter) RegionSubscribersFor(_ core.FederationName, _ core.ObjectClassHandle, attr core.AttributeHandle) []core.FederateHandle {
	return d.regional[attr]
}

type regionDeliveryOutbox struct{ deliveries []core.OutboxDelivery }

func (o *regionDeliveryOutbox) Send(_ context.Context, _ core.FederationName, recipient core.FederateHandle, event core.OutboundEvent) error {
	o.deliveries = append(o.deliveries, core.OutboxDelivery{Recipient: recipient, Event: event})
	return nil
}

func (o *regionDeliveryOutbox) Reserve(_ context.Context, _ core.FederationName, deliveries []core.OutboxDelivery) (core.OutboxReservation, error) {
	return &regionDeliveryReservation{outbox: o, deliveries: slices.Clone(deliveries)}, nil
}

type regionDeliveryReservation struct {
	outbox     *regionDeliveryOutbox
	deliveries []core.OutboxDelivery
}

func (p *regionDeliveryReservation) Commit() error {
	p.outbox.deliveries = append(p.outbox.deliveries, p.deliveries...)
	p.deliveries = nil
	return nil
}

func (p *regionDeliveryReservation) Release() { p.deliveries = nil }

func newRegionDeliveryRegistry(t *testing.T, filter object.DDMFilter, ordinary map[core.AttributeHandle][]core.FederateHandle) (*object.Registry, *regionDeliveryOutbox) {
	t.Helper()
	ctx := context.Background()
	decl := declaration.New()
	out := &regionDeliveryOutbox{}
	reg, err := object.New(object.Options{Declarations: decl, Outbox: out, FOMs: restoreFOMs{}, Clock: core.NewRealClock(), DDM: filter})
	if err != nil {
		t.Fatal(err)
	}
	if err := decl.PublishObjectClassAttributes(ctx, "regions", 1, 7, []core.AttributeHandle{1, 2, 3}); err != nil {
		t.Fatal(err)
	}
	for attr, subscribers := range ordinary {
		for _, sub := range subscribers {
			if err := decl.SubscribeObjectClassAttributes(ctx, "regions", sub, 7, []core.AttributeHandle{attr}); err != nil {
				t.Fatal(err)
			}
		}
	}
	// Seed an existing instance so these tests are independent of registration
	// and discovery behavior, which have separate transaction coverage.
	commit, err := reg.PrepareImportState("regions", &object.RegistryState{
		LastObjectHandle: 1,
		Objects:          []object.InstanceState{{Handle: 1, Name: "existing", Class: 7, Registrant: 1}},
	})
	if err != nil {
		t.Fatal(err)
	}
	commit()
	return reg, out
}

func TestRegionReflectionRecipientComposition(t *testing.T) {
	cases := []struct {
		name     string
		filter   *regionDeliveryFilter
		ordinary map[core.AttributeHandle][]core.FederateHandle
		attrs    []core.AttributeHandle
		want     []core.FederateHandle
		scopes   []object.ScopeState
	}{
		{
			name:     "bound_ordinary_without_overlap",
			filter:   &regionDeliveryFilter{bindings: map[core.AttributeHandle][]object.DDMRegionHandle{1: {10}}},
			ordinary: map[core.AttributeHandle][]core.FederateHandle{1: {2}}, attrs: []core.AttributeHandle{1},
			want: []core.FederateHandle{2}, scopes: []object.ScopeState{{Object: 1, Federate: 2, Attributes: []core.AttributeHandle{1}}},
		},
		{
			name: "bound_union_excludes_disjoint_and_producer",
			filter: &regionDeliveryFilter{
				bindings: map[core.AttributeHandle][]object.DDMRegionHandle{1: {10}},
				regional: map[core.AttributeHandle][]core.FederateHandle{1: {1, 2, 3, 4}},
				overlaps: map[core.AttributeHandle][]core.FederateHandle{1: {1, 3}},
			},
			ordinary: map[core.AttributeHandle][]core.FederateHandle{1: {1, 2, 3}}, attrs: []core.AttributeHandle{1},
			want: []core.FederateHandle{2, 3}, scopes: []object.ScopeState{
				{Object: 1, Federate: 2, Attributes: []core.AttributeHandle{1}},
				{Object: 1, Federate: 3, Attributes: []core.AttributeHandle{1}},
			},
		},
		{
			name:     "default_without_any_object_associations",
			filter:   &regionDeliveryFilter{regional: map[core.AttributeHandle][]core.FederateHandle{1: {1, 3, 4}}},
			ordinary: map[core.AttributeHandle][]core.FederateHandle{1: {1, 2, 3}}, attrs: []core.AttributeHandle{1},
			want: []core.FederateHandle{2, 3, 4}, scopes: []object.ScopeState{
				{Object: 1, Federate: 2, Attributes: []core.AttributeHandle{1}},
				{Object: 1, Federate: 3, Attributes: []core.AttributeHandle{1}},
				{Object: 1, Federate: 4, Attributes: []core.AttributeHandle{1}},
			},
		},
		{
			name: "unbound_attribute_on_partially_bound_object",
			filter: &regionDeliveryFilter{
				bindings: map[core.AttributeHandle][]object.DDMRegionHandle{2: {10}},
				regional: map[core.AttributeHandle][]core.FederateHandle{1: {3}},
			},
			attrs: []core.AttributeHandle{1}, want: []core.FederateHandle{3},
			scopes: []object.ScopeState{{Object: 1, Federate: 3, Attributes: []core.AttributeHandle{1}}},
		},
		{
			name: "mixed_attributes_union_once_with_per_attribute_scope",
			filter: &regionDeliveryFilter{
				bindings: map[core.AttributeHandle][]object.DDMRegionHandle{1: {10}},
				regional: map[core.AttributeHandle][]core.FederateHandle{1: {3, 5}, 2: {4}, 3: {6}},
				overlaps: map[core.AttributeHandle][]core.FederateHandle{1: {3}},
			},
			ordinary: map[core.AttributeHandle][]core.FederateHandle{1: {2, 4}, 2: {2}, 3: {7}},
			attrs:    []core.AttributeHandle{1, 2}, want: []core.FederateHandle{2, 3, 4}, scopes: []object.ScopeState{
				{Object: 1, Federate: 2, Attributes: []core.AttributeHandle{1, 2}},
				{Object: 1, Federate: 3, Attributes: []core.AttributeHandle{1}},
				{Object: 1, Federate: 4, Attributes: []core.AttributeHandle{1, 2}},
			},
		},
		{
			name: "ordinary_only_with_idle_ddm_has_scope", filter: &regionDeliveryFilter{},
			ordinary: map[core.AttributeHandle][]core.FederateHandle{1: {2}}, attrs: []core.AttributeHandle{1}, want: []core.FederateHandle{2},
			scopes: []object.ScopeState{{Object: 1, Federate: 2, Attributes: []core.AttributeHandle{1}}},
		},
		{
			name: "ordinary_only_without_ddm", ordinary: map[core.AttributeHandle][]core.FederateHandle{1: {2}},
			attrs: []core.AttributeHandle{1}, want: []core.FederateHandle{2},
			scopes: []object.ScopeState{{Object: 1, Federate: 2, Attributes: []core.AttributeHandle{1}}},
		},
	}
	for _, tc := range cases {
		for _, wire := range []bool{false, true} {
			path := "typed"
			if wire {
				path = "wire"
			}
			t.Run(tc.name+"/"+path, func(t *testing.T) {
				reg, out := newRegionDeliveryRegistry(t, tc.filter, tc.ordinary)
				payload := make(map[uint64][]byte, len(tc.attrs))
				typedPayload := make(map[core.AttributeHandle][]byte, len(tc.attrs))
				for _, attr := range tc.attrs {
					payload[uint64(attr)] = []byte{byte(attr)}
					typedPayload[attr] = payload[uint64(attr)]
				}
				var err error
				if wire {
					err = reg.UpdateAttributesWire(context.Background(), "regions", 1, 1, payload, nil)
				} else {
					err = reg.UpdateAttributes(context.Background(), "regions", 1, 1, typedPayload, nil)
				}
				if err != nil {
					t.Fatal(err)
				}
				var recipients []core.FederateHandle
				for _, delivery := range out.deliveries {
					event := delivery.Event.(*object.CallbackEvent)
					if event.Kind == object.CallbackReflect {
						recipients = append(recipients, delivery.Recipient)
						if event.Metadata == nil || event.Metadata.SentOrder != core.OrderReceive || event.Metadata.ReceivedOrder != core.OrderReceive {
							t.Fatalf("reflect RO metadata = %+v", event.Metadata)
						}
						wantPayload := payload
						for _, scope := range tc.scopes {
							if scope.Federate == delivery.Recipient {
								wantPayload = make(map[uint64][]byte, len(scope.Attributes))
								for _, attr := range scope.Attributes {
									wantPayload[uint64(attr)] = payload[uint64(attr)]
								}
							}
						}
						if event.Object != 1 || !reflect.DeepEqual(event.Attributes, wantPayload) {
							t.Errorf("reflect payload changed: %+v", event)
						}
					} else if len(recipients) != 0 {
						t.Errorf("scope advisory followed a reflection: %+v", delivery)
					}
				}
				if !slices.Equal(recipients, tc.want) {
					t.Errorf("reflection recipients = %v, want %v", recipients, tc.want)
				}
				if got := reg.ExportState("regions").Scopes; !reflect.DeepEqual(got, tc.scopes) {
					t.Errorf("per-attribute scopes = %+v, want %+v", got, tc.scopes)
				}
			})
		}
	}
}

func TestRegionReflectionDefaultSubscriptionRemovalClearsScope(t *testing.T) {
	filter := &regionDeliveryFilter{regional: map[core.AttributeHandle][]core.FederateHandle{1: {3}}}
	reg, out := newRegionDeliveryRegistry(t, filter, map[core.AttributeHandle][]core.FederateHandle{1: {2}})
	payload := map[core.AttributeHandle][]byte{1: {1}}
	if err := reg.UpdateAttributes(context.Background(), "regions", 1, 1, payload, nil); err != nil {
		t.Fatal(err)
	}
	filter.regional[1] = nil
	out.deliveries = nil
	if err := reg.UpdateAttributes(context.Background(), "regions", 1, 1, payload, nil); err != nil {
		t.Fatal(err)
	}
	if len(out.deliveries) != 2 || out.deliveries[0].Recipient != 3 ||
		out.deliveries[0].Event.(*object.CallbackEvent).Kind != object.CallbackAttributesOutOfScope ||
		out.deliveries[1].Recipient != 2 || out.deliveries[1].Event.(*object.CallbackEvent).Kind != object.CallbackReflect {
		t.Fatalf("after removing last regional subscription got %+v, want out-of-scope to 3 then reflect to 2", out.deliveries)
	}
	want := []object.ScopeState{{Object: 1, Federate: 2, Attributes: []core.AttributeHandle{1}}}
	if got := reg.ExportState("regions").Scopes; !reflect.DeepEqual(got, want) {
		t.Fatalf("scope after removal = %+v, want %+v", got, want)
	}
}
