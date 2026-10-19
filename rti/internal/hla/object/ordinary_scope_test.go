package object

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/declaration"
)

func enableScopeAdvisories(t *testing.T, decl *declaration.Manager, recipients ...core.FederateHandle) {
	t.Helper()
	for _, recipient := range recipients {
		if err := decl.SetAdvisorySwitch(context.Background(), "fed", recipient, core.AdvisoryAttributeScope, true); err != nil {
			t.Fatal(err)
		}
	}
}

func TestOrdinaryScopeEligibilityAndSwitchSuppression(t *testing.T) {
	for _, withDDM := range []bool{false, true} {
		for _, enabled := range []bool{false, true} {
			name := map[bool]string{false: "no-ddm", true: "no-regional-match"}[withDDM] + map[bool]string{false: "/off", true: "/on"}[enabled]
			t.Run(name, func(t *testing.T) {
				ctx := context.Background()
				out := &atomicUpdateOutbox{}
				reg, decl, _ := newUpdateDeliveryRegistry(t, out)
				object := registerUpdateProducer(t, reg, decl, 2, 3)
				if withDDM {
					reg.opts.DDM = &discoveryLockDDM{}
				}
				if err := decl.SubscribeObjectClassAttributes(ctx, "fed", 2, 7, []core.AttributeHandle{2}); err != nil {
					t.Fatal(err)
				}
				if err := decl.SubscribeObjectClassAttributes(ctx, "fed", 3, 7, []core.AttributeHandle{3}); err != nil {
					t.Fatal(err)
				}
				if enabled {
					enableScopeAdvisories(t, decl, 2)
				}
				if err := reg.UpdateAttributes(ctx, "fed", 1, object, map[core.AttributeHandle][]byte{2: {4}}, nil); err != nil {
					t.Fatal(err)
				}
				want := []CallbackKind{CallbackReflect}
				if enabled {
					want = []CallbackKind{CallbackAttributesInScope, CallbackReflect}
				}
				assertOrdinaryScopeDeliveries(t, out.records, object, want)
				out.records = nil
				if err := reg.UpdateAttributes(ctx, "fed", 1, object, map[core.AttributeHandle][]byte{2: {5}}, nil); err != nil {
					t.Fatal(err)
				}
				assertOrdinaryScopeDeliveries(t, out.records, object, []CallbackKind{CallbackReflect})
			})
		}
	}
}

func TestOrdinaryScopeFailedAdmissionRetainsInScopeNotificationForRetry(t *testing.T) {
	ctx := context.Background()
	out := &atomicUpdateOutbox{}
	reg, decl, log := newUpdateDeliveryRegistry(t, out)
	enableScopeAdvisories(t, decl, 2)
	object := registerUpdateProducer(t, reg, decl, 2)
	if err := decl.SubscribeObjectClassAttributes(ctx, "fed", 2, 7, []core.AttributeHandle{2}); err != nil {
		t.Fatal(err)
	}
	out.reserveErr = core.ErrFederateOverflow
	before := len(log.Records())
	if err := reg.UpdateAttributes(ctx, "fed", 1, object, map[core.AttributeHandle][]byte{2: {4}}, nil); !errors.Is(err, core.ErrFederateOverflow) {
		t.Fatalf("failed admission=%v", err)
	}
	if len(out.records) != 0 || len(log.Records()) != before {
		t.Fatal("failed scope admission changed delivery or journal state")
	}
	out.reserveErr = nil
	if err := reg.UpdateAttributes(ctx, "fed", 1, object, map[core.AttributeHandle][]byte{2: {4}}, nil); err != nil {
		t.Fatal(err)
	}
	assertOrdinaryScopeDeliveries(t, out.records, object, []CallbackKind{CallbackAttributesInScope, CallbackReflect})
}

func assertOrdinaryScopeDeliveries(t *testing.T, records []outboxRecord, object core.ObjectHandle, want []CallbackKind) {
	t.Helper()
	var got []CallbackKind
	for _, record := range records {
		event, ok := record.Event.(*CallbackEvent)
		if !ok || record.Federate != 2 || event.Object != object {
			t.Fatalf("wrong scope recipient or object: %+v", record)
		}
		if event.Kind == CallbackAttributesInScope && !reflect.DeepEqual(event.AttributeHandles, []core.AttributeHandle{2}) {
			t.Fatalf("scope attributes=%v", event.AttributeHandles)
		}
		if event.Kind == CallbackReflect && len(event.Attributes) != 1 {
			t.Fatalf("reflection attributes=%v", event.Attributes)
		}
		got = append(got, event.Kind)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("callbacks=%v want=%v", got, want)
	}
}
