package object_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/declaration"
	"github.com/cbchoi/gorti/rti/internal/hla/object"
)

type registrationRegions struct {
	failure            error
	prepared, released int
	committed          core.ObjectHandle
	forgotten          []core.ObjectHandle
}

func (r *registrationRegions) PrepareObjectRegistration(context.Context, core.FederationName, core.FederateHandle, core.ObjectClassHandle, map[core.AttributeHandle][]core.DDMRegionHandleCore) (core.ObjectRegionRegistration, error) {
	r.prepared++
	if r.failure != nil {
		return nil, r.failure
	}
	return r, nil
}
func (r *registrationRegions) Commit(obj core.ObjectHandle) { r.committed = obj }
func (r *registrationRegions) Release()                     { r.released++ }
func (r *registrationRegions) ForgetObject(_ core.FederationName, obj core.ObjectHandle) {
	r.forgotten = append(r.forgotten, obj)
}

type registrationVisibilityOutbox struct {
	check  func(core.FederateHandle, core.OutboundEvent)
	events int
}

func (o *registrationVisibilityOutbox) Send(_ context.Context, _ core.FederationName, recipient core.FederateHandle, e core.OutboundEvent) error {
	o.events++
	if o.check != nil {
		o.check(recipient, e)
	}
	return nil
}

func regionRegistry(t *testing.T, regions *registrationRegions, journal core.EventLog, out core.Outbox, onRegister func(core.FederationName, core.FederateHandle, core.ObjectHandle, core.ObjectClassHandle, []core.AttributeHandle)) (*object.Registry, *declaration.Manager) {
	t.Helper()
	d := declaration.New()
	ctx := context.Background()
	if err := d.PublishObjectClassAttributes(ctx, "fed", 1, 7, []core.AttributeHandle{9}); err != nil {
		t.Fatal(err)
	}
	if err := d.SubscribeObjectClassAttributes(ctx, "fed", 2, 7, []core.AttributeHandle{9}); err != nil {
		t.Fatal(err)
	}
	if err := d.SubscribeObjectClassAttributes(ctx, "fed", 3, 7, []core.AttributeHandle{2}); err != nil {
		t.Fatal(err)
	}
	r, err := object.New(object.Options{Declarations: d, Outbox: out, FOMs: restoreFOMs{}, Clock: core.NewRealClock(), RegistrationRegions: regions, EventLog: journal, OnRegister: onRegister})
	if err != nil {
		t.Fatal(err)
	}
	return r, d
}

func TestRegionRegistrationInitialVisibilityAndHighAttribute(t *testing.T) {
	regions := &registrationRegions{}
	out := &registrationVisibilityOutbox{}
	owned := false
	r, _ := regionRegistry(t, regions, nil, out, func(_ core.FederationName, _ core.FederateHandle, obj core.ObjectHandle, _ core.ObjectClassHandle, attrs []core.AttributeHandle) {
		if regions.committed != obj || !reflect.DeepEqual(attrs, []core.AttributeHandle{9}) {
			t.Fatalf("ownership initialization: object=%d attrs=%v regions=%d", obj, attrs, regions.committed)
		}
		owned = true
	})
	if err := r.ReserveObjectInstanceName(context.Background(), "fed", 1, "scoped"); err != nil {
		t.Fatal(err)
	}
	out.events = 0
	out.check = func(recipient core.FederateHandle, evt core.OutboundEvent) {
		callback := evt.(*object.CallbackEvent)
		if recipient != 2 || callback.Kind != object.CallbackDiscover || !owned || regions.committed != callback.Object || regions.released != 1 {
			t.Fatalf("incomplete discovery state: recipient=%d callback=%+v owned=%v regions=%+v", recipient, callback, owned, regions)
		}
		if r.ClassOf("fed", callback.Object) != 7 {
			t.Fatal("object inaccessible during discovery")
		}
	}
	obj, name, err := r.RegisterWithRegions(context.Background(), "fed", 1, 7, "scoped", map[core.AttributeHandle][]core.DDMRegionHandleCore{9: {1}})
	if err != nil || obj != 1 || name != "scoped" || out.events != 1 {
		t.Fatalf("registration result: %d %q %v events=%d", obj, name, err, out.events)
	}
}

func TestRegionRegistrationFailurePreservesObjectAndName(t *testing.T) {
	for _, mode := range []string{"region", "journal"} {
		t.Run(mode, func(t *testing.T) {
			failure := errors.New("injected " + mode + " failure")
			regions := &registrationRegions{}
			journal := &registrationJournal{}
			if mode == "region" {
				regions.failure = failure
			} else {
				journal.failure = failure
			}
			out := &registrationVisibilityOutbox{}
			hooks := 0
			r, _ := regionRegistry(t, regions, journal, out, func(core.FederationName, core.FederateHandle, core.ObjectHandle, core.ObjectClassHandle, []core.AttributeHandle) {
				hooks++
			})
			ctx := context.Background()
			if err := r.ReserveObjectInstanceName(ctx, "fed", 1, "held"); err != nil {
				t.Fatal(err)
			}
			before, events := r.ExportState("fed"), out.events
			obj, name, err := r.RegisterWithRegions(ctx, "fed", 1, 7, "held", map[core.AttributeHandle][]core.DDMRegionHandleCore{9: {1}})
			if !errors.Is(err, failure) || obj != 0 || name != "" || regions.committed != 0 || hooks != 0 || out.events != events || !reflect.DeepEqual(before, r.ExportState("fed")) {
				t.Fatalf("failed transaction leaked: obj=%d name=%q error=%v regions=%+v", obj, name, err, regions)
			}
			if mode == "journal" && regions.released != 1 {
				t.Fatal("journal failure retained region lease")
			}
			regions.failure, journal.failure = nil, nil
			obj, _, err = r.RegisterWithRegions(ctx, "fed", 1, 7, "held", map[core.AttributeHandle][]core.DDMRegionHandleCore{9: {1}})
			if err != nil || obj != 1 || regions.committed != 1 || hooks != 1 {
				t.Fatalf("retry: %d %v %+v", obj, err, regions)
			}
		})
	}
}

func TestRegionRegistrationRejectsUnpublishedAttribute(t *testing.T) {
	regions := &registrationRegions{}
	out := &registrationVisibilityOutbox{}
	r, _ := regionRegistry(t, regions, nil, out, nil)
	obj, _, err := r.RegisterWithRegions(context.Background(), "fed", 1, 7, "bad", map[core.AttributeHandle][]core.DDMRegionHandleCore{9: {1}, 2: {1}})
	if !errors.Is(err, core.ErrAttributeNotPublished) || obj != 0 || regions.prepared != 0 || out.events != 0 {
		t.Fatalf("unpublished binding accepted: %d %v %+v", obj, err, regions)
	}
}

func TestRegionRegistrationDeletionReleasesAssociations(t *testing.T) {
	for _, resign := range []bool{false, true} {
		regions := &registrationRegions{}
		r, _ := regionRegistry(t, regions, nil, &registrationVisibilityOutbox{}, nil)
		ctx := context.Background()
		obj, _, err := r.RegisterWithRegions(ctx, "fed", 1, 7, "", map[core.AttributeHandle][]core.DDMRegionHandleCore{9: {1}})
		if err != nil {
			t.Fatal(err)
		}
		if resign {
			r.PrepareResignDeletions(ctx, "fed", 1, true).Commit()
		} else if err := r.Delete(ctx, "fed", 1, obj, nil, nil); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(regions.forgotten, []core.ObjectHandle{obj}) {
			t.Fatalf("resign=%v: stale associations %v", resign, regions.forgotten)
		}
	}
}

type changingRegistrationDeclarations struct {
	*declaration.Manager
	snapshots int
}

func (d *changingRegistrationDeclarations) PublishedObjectAttributes(fed core.FederationName, owner core.FederateHandle, cls core.ObjectClassHandle) []core.AttributeHandle {
	attrs := d.Manager.PublishedObjectAttributes(fed, owner, cls)
	d.snapshots++
	_ = d.UnpublishObjectClassAttributes(context.Background(), fed, owner, cls, []core.AttributeHandle{9})
	_ = d.PublishObjectClassAttributes(context.Background(), fed, owner, cls, []core.AttributeHandle{2})
	return attrs
}

func TestRegionRegistrationUsesOnePublicationSnapshot(t *testing.T) {
	d := &changingRegistrationDeclarations{Manager: declaration.New()}
	ctx := context.Background()
	if err := d.PublishObjectClassAttributes(ctx, "fed", 1, 7, []core.AttributeHandle{9}); err != nil {
		t.Fatal(err)
	}
	if err := d.SubscribeObjectClassAttributes(ctx, "fed", 2, 7, []core.AttributeHandle{9}); err != nil {
		t.Fatal(err)
	}
	if err := d.SubscribeObjectClassAttributes(ctx, "fed", 3, 7, []core.AttributeHandle{2}); err != nil {
		t.Fatal(err)
	}
	out := &registrationVisibilityOutbox{check: func(recipient core.FederateHandle, _ core.OutboundEvent) {
		if recipient != 2 {
			t.Fatalf("discovery mixed publication snapshots: %d", recipient)
		}
	}}
	var owned []core.AttributeHandle
	r, err := object.New(object.Options{Declarations: d, Outbox: out, FOMs: restoreFOMs{}, Clock: core.NewRealClock(), RegistrationRegions: &registrationRegions{}, OnRegister: func(_ core.FederationName, _ core.FederateHandle, _ core.ObjectHandle, _ core.ObjectClassHandle, attrs []core.AttributeHandle) {
		owned = attrs
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.RegisterWithRegions(ctx, "fed", 1, 7, "", map[core.AttributeHandle][]core.DDMRegionHandleCore{9: {1}}); err != nil {
		t.Fatal(err)
	}
	if d.snapshots != 1 || !reflect.DeepEqual(owned, []core.AttributeHandle{9}) || out.events != 1 {
		t.Fatalf("incoherent registration: snapshots=%d owned=%v events=%d", d.snapshots, owned, out.events)
	}
}

func TestRegionRegistrationGeneratedNameSkipsOccupiedCandidates(t *testing.T) {
	for _, withRegions := range []bool{false, true} {
		regions := &registrationRegions{}
		r, _ := regionRegistry(t, regions, nil, &registrationVisibilityOutbox{}, nil)
		ctx := context.Background()
		// Older snapshots may contain names that new reservation calls reject.
		image := r.ExportState("fed")
		image.Reservations = append(image.Reservations, object.NameReservationState{Name: "HLAobj_7_1", Holder: 2})
		commit, err := r.PrepareImportState("fed", image)
		if err != nil {
			t.Fatal(err)
		}
		commit()
		var obj core.ObjectHandle
		var name string
		if withRegions {
			obj, name, err = r.RegisterWithRegions(ctx, "fed", 1, 7, "", map[core.AttributeHandle][]core.DDMRegionHandleCore{9: {1}})
		} else {
			obj, name, err = r.Register(ctx, "fed", 1, 7, "")
		}
		if err != nil || obj != 2 || name != "HLAobj_7_2" {
			t.Fatalf("withRegions=%v: occupied generated name prevented registration: %d %q %v", withRegions, obj, name, err)
		}
		if err := r.ReleaseObjectInstanceName(ctx, "fed", 2, "HLAobj_7_1"); err != nil {
			t.Fatalf("another federate's reservation changed: %v", err)
		}
	}
}
