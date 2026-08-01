// TASK-260 (M23 W3) — change_attribute_transportation_type +
// change_interaction_transportation_type record-only semantics.

package m23spec

import (
	"context"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/core"
)

// TestSpec_M23_ChangeAttributeTransportRecorded — owner records;
// AttributeTransportType reads it back.
func TestSpec_M23_ChangeAttributeTransportRecorded(t *testing.T) {
	reg, declMgr, _ := newM23Registry(t)
	ctx := context.Background()
	const fed core.FederationName = "fed"
	const owner = core.FederateHandle(1)
	const cls = core.ObjectClassHandle(7)
	if err := declMgr.PublishObjectClassAttributes(ctx, fed, owner, cls, []core.AttributeHandle{1, 2}); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	obj, _, err := reg.Register(ctx, fed, owner, cls, "")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	// Default — no override recorded.
	if got := reg.AttributeTransportType(fed, obj, 1); got != core.TransportTypeUnspecified {
		t.Errorf("default AttributeTransportType = %v, want Unspecified", got)
	}

	if err := reg.ChangeAttributeTransportType(ctx, fed, owner, obj, []core.AttributeHandle{1, 2}, core.TransportTypeBestEffort); err != nil {
		t.Fatalf("Change: %v", err)
	}
	if got := reg.AttributeTransportType(fed, obj, 1); got != core.TransportTypeBestEffort {
		t.Errorf("after Change AttributeTransportType = %v, want BestEffort", got)
	}
	if got := reg.AttributeTransportType(fed, obj, 2); got != core.TransportTypeBestEffort {
		t.Errorf("after Change AttributeTransportType[2] = %v, want BestEffort", got)
	}
}

// TestSpec_M23_ChangeAttributeTransportNonOwnerRejected.
func TestSpec_M23_ChangeAttributeTransportNonOwnerRejected(t *testing.T) {
	reg, declMgr, _ := newM23Registry(t)
	ctx := context.Background()
	const fed core.FederationName = "fed"
	const owner = core.FederateHandle(1)
	const intruder = core.FederateHandle(2)
	const cls = core.ObjectClassHandle(7)
	if err := declMgr.PublishObjectClassAttributes(ctx, fed, owner, cls, []core.AttributeHandle{1}); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	obj, _, err := reg.Register(ctx, fed, owner, cls, "")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	err = reg.ChangeAttributeTransportType(ctx, fed, intruder, obj, []core.AttributeHandle{1}, core.TransportTypeReliable)
	if err != core.ErrObjectNotOwned {
		t.Errorf("non-owner err = %v, want ErrObjectNotOwned", err)
	}
}

// TestSpec_M23_ChangeAttributeTransportUnspecifiedRejected.
func TestSpec_M23_ChangeAttributeTransportUnspecifiedRejected(t *testing.T) {
	reg, declMgr, _ := newM23Registry(t)
	ctx := context.Background()
	const fed core.FederationName = "fed"
	const owner = core.FederateHandle(1)
	const cls = core.ObjectClassHandle(7)
	if err := declMgr.PublishObjectClassAttributes(ctx, fed, owner, cls, []core.AttributeHandle{1}); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	obj, _, err := reg.Register(ctx, fed, owner, cls, "")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	err = reg.ChangeAttributeTransportType(ctx, fed, owner, obj, []core.AttributeHandle{1}, core.TransportTypeUnspecified)
	if err != core.ErrTransportTypeUnspecified {
		t.Errorf("unspecified err = %v, want ErrTransportTypeUnspecified", err)
	}
}

// TestSpec_M23_ChangeInteractionTransportRecorded — record + readback.
func TestSpec_M23_ChangeInteractionTransportRecorded(t *testing.T) {
	reg, _, _ := newM23Registry(t)
	ctx := context.Background()
	const fed core.FederationName = "fed"
	const publisher = core.FederateHandle(1)
	const cls = core.InteractionClassHandle(5)

	if got := reg.InteractionTransportType(fed, publisher, cls); got != core.TransportTypeUnspecified {
		t.Errorf("default InteractionTransportType = %v, want Unspecified", got)
	}
	if err := reg.ChangeInteractionTransportType(ctx, fed, publisher, cls, core.TransportTypeBestEffort); err != nil {
		t.Fatalf("Change: %v", err)
	}
	if got := reg.InteractionTransportType(fed, publisher, cls); got != core.TransportTypeBestEffort {
		t.Errorf("after Change InteractionTransportType = %v, want BestEffort", got)
	}
}
