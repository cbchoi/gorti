package ownership

import (
	"context"
	"reflect"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

type momStateOutbox struct{ sent int }

func (o *momStateOutbox) Send(context.Context, core.FederationName, core.FederateHandle, core.OutboundEvent) error {
	o.sent++
	return nil
}

func TestMOMOwnershipStateAtomicAndSilent(t *testing.T) {
	o := &momStateOutbox{}
	m, err := New(Options{Outbox: o})
	if err != nil {
		t.Fatal(err)
	}
	m.RegisterInitialOwnership("fed", 7, 1, []core.AttributeHandle{11, 12})
	if err := m.ModifyAttributeState(context.Background(), "fed", 8, 1, 11, true); err != nil {
		t.Fatal(err)
	}
	if h, ok := m.QueryOwnership("fed", 1, 11); !ok || h != 8 {
		t.Fatal("owner not replaced")
	}
	if h, _ := m.QueryOwnership("fed", 1, 12); h != 7 {
		t.Fatal("unrelated attribute changed")
	}
	if o.sent != 0 {
		t.Fatal("MOM adjustment sent ownership notification")
	}
	if err := m.ModifyAttributeState(context.Background(), "fed", 8, 1, 11, false); err != nil {
		t.Fatal(err)
	}
	if _, ok := m.QueryOwnership("fed", 1, 11); ok {
		t.Fatal("attribute remains owned")
	}
	m.RegisterInitialOwnership("fed", ^core.FederateHandle(0), 1, []core.AttributeHandle{11})
	before := m.ExportState("fed")
	if err := m.ModifyAttributeState(context.Background(), "fed", 8, 1, 11, true); err == nil {
		t.Fatal("RTI ownership overwritten")
	}
	if !reflect.DeepEqual(before, m.ExportState("fed")) {
		t.Fatal("rejection changed state")
	}
}

func TestMOMOwnershipPendingAdjustmentRejected(t *testing.T) {
	o := &momStateOutbox{}
	m, _ := New(Options{Outbox: o})
	m.RegisterInitialOwnership("fed", 7, 1, []core.AttributeHandle{11})
	m.fed["fed"].pendingDivests[ownershipKey{obj: 1, attr: 11}] = pendingDivest{owner: 7}
	before := m.ExportState("fed")
	if err := m.ModifyAttributeState(context.Background(), "fed", 8, 1, 11, true); err == nil {
		t.Fatal("pending negotiation silently discarded")
	}
	if !reflect.DeepEqual(before, m.ExportState("fed")) {
		t.Fatal("pending state changed")
	}
}
