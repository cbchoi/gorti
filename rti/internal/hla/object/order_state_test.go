package object

import (
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"reflect"
	"testing"
)

func TestOrderStateRoundTripAndValidation(t *testing.T) {
	r, _, _ := newTestRegistry(t, nil)
	state := &RegistryState{LastObjectHandle: 1, Objects: []InstanceState{{Handle: 1, Name: "object", Class: 1, Registrant: 2}}, AttributeOrders: []AttributeOrderState{{Object: 1, Attribute: 3, Order: core.OrderReceive}}, InteractionOrders: []InteractionOrderState{{Publisher: 2, Class: 4, Order: core.OrderTimestamp}}}
	commit, err := r.PrepareImportState("fed", state)
	if err != nil {
		t.Fatal(err)
	}
	commit()
	got := r.ExportState("fed")
	if !reflect.DeepEqual(got.AttributeOrders, state.AttributeOrders) || !reflect.DeepEqual(got.InteractionOrders, state.InteractionOrders) {
		t.Fatalf("order snapshot=%+v", got)
	}
	state.AttributeOrders[0].Order = 99
	if _, err := r.PrepareImportState("fed", state); err == nil {
		t.Fatal("invalid order snapshot accepted")
	}
	if r.ExportState("fed").AttributeOrders[0].Order != core.OrderReceive {
		t.Fatal("failed import mutated order state")
	}
	r.OnFederationDestroyed("fed")
	if len(r.ExportState("fed").AttributeOrders) != 0 {
		t.Fatal("destroy leaked order overrides")
	}
}
