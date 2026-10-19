package object_test

import (
	"errors"
	"reflect"
	"slices"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/object"
)

func TestApplicableR1ImplicitDeletePublicationLifetime(t *testing.T) {
	f := newKnownFixture(t)
	privilege, ok := f.fom.LookupAttribute(f.child, "HLAprivilegeToDeleteObject")
	if !ok {
		t.Fatal("standard MIM delete privilege missing from fixture")
	}
	pub := core.FederateHandle(4)
	check := func(stage string, want bool) {
		t.Helper()
		got := slices.Contains(f.decl.PublishedObjectAttributes(knownFed, pub, f.child), privilege)
		if got != want {
			t.Errorf("%s: published delete privilege=%t, want %t", stage, got, want)
		}
	}
	// S1516.1 5.1.2(f): explicit unpublication inhibits implicit publication
	// only for the current publication lifetime.
	knownOK(t, f.decl.PublishObjectClassAttributes(f.ctx, knownFed, pub, f.child, []core.AttributeHandle{f.inherited}))
	check("initial implicit publication", true)
	knownOK(t, f.decl.UnpublishObjectClassAttributeSet(f.ctx, knownFed, pub, f.child, []core.AttributeHandle{privilege}))
	check("explicit privilege unpublication", false)
	knownOK(t, f.decl.PublishObjectClassAttributes(f.ctx, knownFed, pub, f.child, []core.AttributeHandle{f.own}))
	check("adding another attribute in same lifetime", false)
	knownOK(t, f.decl.PublishObjectClassAttributes(f.ctx, knownFed, pub, f.child, []core.AttributeHandle{privilege}))
	check("explicit privilege publication", true)
	knownOK(t, f.decl.UnpublishObjectClassAttributeSet(f.ctx, knownFed, pub, f.child, []core.AttributeHandle{privilege}))
	check("explicit publication then removal", false)
	knownOK(t, f.decl.UnpublishObjectClass(f.ctx, knownFed, pub, f.child))
	check("whole unpublication", false)
	knownOK(t, f.decl.PublishObjectClassAttributes(f.ctx, knownFed, pub, f.child, []core.AttributeHandle{f.own}))
	check("new publication lifetime", true)
}

func TestApplicableR1AdditiveSubscriptionPreservesOptions(t *testing.T) {
	f := newKnownFixture(t)
	passive := core.SubscriptionOptions{Active: false}
	knownOK(t, f.decl.SubscribeObjectClassAttributesWithOptions(f.ctx, knownFed, 2, f.child, []core.AttributeHandle{f.inherited}, passive))
	knownOK(t, f.decl.SubscribeObjectClassAttributesWithOptions(f.ctx, knownFed, 2, f.child, []core.AttributeHandle{f.own}, core.SubscriptionOptions{Active: true}))
	for attr, active := range map[core.AttributeHandle]bool{f.inherited: false, f.own: true} {
		got, found := f.decl.ObjectSubscriptionOptions(knownFed, 2, f.child, attr)
		if !found || got.Active != active {
			t.Fatalf("attribute %d options=%+v found=%t, want active=%t", attr, got, found, active)
		}
	}
	before := f.decl.ExportState(knownFed)
	knownOK(t, f.decl.SubscribeObjectClassAttributesWithOptions(f.ctx, knownFed, 2, f.child, nil, core.SubscriptionOptions{Active: true}))
	if !reflect.DeepEqual(before, f.decl.ExportState(knownFed)) {
		t.Fatal("empty subscription changed attributes or passive mode")
	}
	knownOK(t, f.decl.SubscribeObjectClassAttributesWithOptions(f.ctx, knownFed, 2, f.child, []core.AttributeHandle{f.inherited}, core.SubscriptionOptions{Active: true}))
	got, _ := f.decl.ObjectSubscriptionOptions(knownFed, 2, f.child, f.inherited)
	if !got.Active || len(f.decl.SubscribersFor(f.ctx, knownFed, f.child, []core.AttributeHandle{f.own})) != 1 {
		t.Fatal("mode overwrite removed unrelated subscribed attribute")
	}
}

func TestApplicableR1IllegalSingleReservationHasNoEffects(t *testing.T) {
	f := newKnownFixture(t)
	before := f.reg.ExportState(knownFed)
	if err := f.reg.ReserveObjectInstanceName(f.ctx, knownFed, 1, "HLAreserved"); !errors.Is(err, core.ErrObjectInstanceIllegalName) {
		t.Errorf("HLA-prefixed reservation error=%v, want IllegalName", err)
	}
	if !reflect.DeepEqual(before, f.reg.ExportState(knownFed)) || len(f.drain(1)) != 0 {
		t.Error("illegal reservation altered state or emitted a result callback")
	}
}

func TestApplicableR1IllegalMultipleReservationIsAtomic(t *testing.T) {
	for _, names := range [][]string{nil, {"validCandidate", "HLAreserved"}} {
		name := "mixed-illegal"
		if len(names) == 0 {
			name = "empty-set"
		}
		t.Run(name, func(t *testing.T) {
			f := newKnownFixture(t)
			before := f.reg.ExportState(knownFed)
			want := core.ErrObjectInstanceIllegalName
			if len(names) == 0 {
				want = core.ErrObjectInstanceNameSetEmpty
			}
			if err := f.reg.ReserveMultipleObjectInstanceNames(f.ctx, knownFed, 1, names); !errors.Is(err, want) {
				t.Errorf("invalid multiple reservation error=%v, want %v", err, want)
			}
			if !reflect.DeepEqual(before, f.reg.ExportState(knownFed)) || len(f.drain(1)) != 0 {
				t.Error("invalid batch changed state or emitted a result callback")
			}
		})
	}
}

func TestApplicableR1ReservationCallbacksCorrelateAndExcludeOtherHolders(t *testing.T) {
	for _, multiple := range []bool{false, true} {
		name := "single"
		if multiple {
			name = "multiple"
		}
		t.Run(name, func(t *testing.T) {
			f := newKnownFixture(t)
			names := []string{"reservedCandidate"}
			if multiple {
				names = append(names, "secondCandidate")
			}
			reserve := func(caller core.FederateHandle, success bool) {
				t.Helper()
				kind := object.CallbackReservationFailed
				if success {
					kind = object.CallbackReservationSucceeded
				}
				if multiple {
					knownOK(t, f.reg.ReserveMultipleObjectInstanceNames(f.ctx, knownFed, caller, names))
					kind = object.CallbackMultipleReservationFailed
					if success {
						kind = object.CallbackMultipleReservationSucceeded
					}
				} else {
					knownOK(t, f.reg.ReserveObjectInstanceName(f.ctx, knownFed, caller, names[0]))
				}
				events := f.drain(caller)
				if len(events) != 1 || events[0].Kind != kind {
					t.Fatalf("caller %d result=%+v want kind %v", caller, events, kind)
				}
				if multiple && !slices.Equal(events[0].ObjectNames, names) || !multiple && events[0].ObjectName != names[0] {
					t.Fatal("reservation result lost requested names")
				}
				other := core.FederateHandle(3 - caller)
				if len(f.drain(other)) != 0 {
					t.Fatal("reservation callback sent to another federate")
				}
			}
			reserve(1, true)
			reserve(2, false)
			for _, name := range names {
				knownOK(t, f.reg.ReleaseObjectInstanceName(f.ctx, knownFed, 1, name))
			}
			reserve(2, true)
		})
	}
}

func TestApplicableR1ExplicitRegistrationRequiresReservation(t *testing.T) {
	f := newKnownFixture(t)
	before := f.reg.ExportState(knownFed)
	_, _, err := f.reg.Register(f.ctx, knownFed, 1, f.child, "neverReserved")
	if !errors.Is(err, core.ErrObjectInstanceNameNotReserved) {
		t.Errorf("unreserved explicit registration error=%v, want ErrObjectInstanceNameNotReserved", err)
	}
	if !reflect.DeepEqual(before, f.reg.ExportState(knownFed)) {
		t.Error("unreserved named registration created visible object/name state")
	}
}

func TestApplicableR2ReservationSurvivesRegistrationDeletionAndRestore(t *testing.T) {
	f := newKnownFixture(t)
	knownOK(t, f.reg.ReserveObjectInstanceName(f.ctx, knownFed, 1, "retained"))
	f.drain(1)
	obj, _, err := f.reg.Register(f.ctx, knownFed, 1, f.child, "retained")
	knownOK(t, err)
	saved := f.reg.ExportState(knownFed)
	if len(saved.Reservations) != 1 || saved.Reservations[0].Name != "retained" || saved.Reservations[0].Holder != 1 {
		t.Fatalf("registration lost reservation: %+v", saved.Reservations)
	}
	knownOK(t, f.reg.ReleaseObjectInstanceName(f.ctx, knownFed, 1, "retained"))
	commit, err := f.reg.PrepareImportState(knownFed, saved)
	knownOK(t, err)
	commit()
	knownOK(t, f.reg.Delete(f.ctx, knownFed, 1, obj, nil, nil))
	knownOK(t, f.reg.ReserveObjectInstanceName(f.ctx, knownFed, 2, "retained"))
	events := f.drain(2)
	if len(events) != 1 || events[0].Kind != object.CallbackReservationFailed {
		t.Fatalf("other federate obtained retained reservation: %+v", events)
	}
	_, _, err = f.reg.Register(f.ctx, knownFed, 1, f.child, "retained")
	knownOK(t, err)
	knownOK(t, f.reg.ReleaseObjectInstanceName(f.ctx, knownFed, 1, "retained"))
	if len(f.reg.ExportState(knownFed).Reservations) != 0 {
		t.Fatal("explicit release did not remove reservation")
	}
}
