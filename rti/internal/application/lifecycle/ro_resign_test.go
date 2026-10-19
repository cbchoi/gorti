package lifecycle

import (
	"context"
	"errors"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/declaration"
	"github.com/cbchoi/gorti/rti/internal/hla/object"
	"github.com/cbchoi/gorti/rti/internal/hla/ownership"
	hlatime "github.com/cbchoi/gorti/rti/internal/hla/time"
)

type roResignFOM struct{}

func (roResignFOM) AttributeName(_ core.ObjectClassHandle, attr core.AttributeHandle) (string, bool) {
	if attr == 1 {
		return "HLAprivilegeToDeleteObject", true
	}
	if attr == 2 {
		return "x", true
	}
	return "", false
}

func (roResignFOM) IsValid() bool                                                     { return true }
func (roResignFOM) LookupObjectClass(string) (core.ObjectClassHandle, bool)           { return 1, true }
func (roResignFOM) LookupInteractionClass(string) (core.InteractionClassHandle, bool) { return 1, true }
func (roResignFOM) LookupAttribute(_ core.ObjectClassHandle, name string) (core.AttributeHandle, bool) {
	if name == "HLAprivilegeToDeleteObject" {
		return 1, true
	}
	return 2, true
}
func (roResignFOM) LookupParameter(core.InteractionClassHandle, string) (core.ParameterHandle, bool) {
	return 1, true
}

type roResignFOMs struct{}

func (roResignFOMs) Get(context.Context, core.FederationName) (core.FOMHandle, error) {
	return roResignFOM{}, nil
}
func (roResignFOMs) Load(context.Context, []core.FOMModule) (core.FOMHandle, error) {
	return roResignFOM{}, nil
}

type roResignOutbox struct {
	events []core.OutboxDelivery
	fail   error
}

func (o *roResignOutbox) Send(_ context.Context, _ core.FederationName, h core.FederateHandle, event core.OutboundEvent) error {
	o.events = append(o.events, core.OutboxDelivery{Recipient: h, Event: event})
	return nil
}

func (o *roResignOutbox) Reserve(_ context.Context, _ core.FederationName, events []core.OutboxDelivery) (core.OutboxReservation, error) {
	if o.fail != nil {
		return nil, o.fail
	}
	return &roResignReservation{out: o, events: append([]core.OutboxDelivery(nil), events...)}, nil
}

type roResignReservation struct {
	out    *roResignOutbox
	events []core.OutboxDelivery
	done   bool
}

func (r *roResignReservation) Commit() error {
	if !r.done {
		r.out.events = append(r.out.events, r.events...)
		r.done = true
	}
	return nil
}
func (r *roResignReservation) Release() { r.done = true }

func roResignFixture(t *testing.T) (*object.Registry, *ownership.Manager, *hlatime.Manager, *roResignOutbox, core.ObjectHandle) {
	t.Helper()
	ctx := context.Background()
	out := &roResignOutbox{}
	clock := core.NewRealClock()
	owners, err := ownership.New(ownership.Options{Outbox: out})
	if err != nil {
		t.Fatal(err)
	}
	tm, err := hlatime.New(hlatime.Options{Clock: clock, Outbox: out})
	if err != nil {
		t.Fatal(err)
	}
	decl := declaration.New()
	objects, err := object.New(object.Options{
		Clock: clock, FOMs: roResignFOMs{}, Declarations: decl, Outbox: out, Ownership: owners, TSOGate: tm,
		OnRegister: func(f core.FederationName, h core.FederateHandle, o core.ObjectHandle, _ core.ObjectClassHandle, attrs []core.AttributeHandle) {
			owners.RegisterInitialOwnership(f, h, o, attrs)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	tm.SetDeletionLifecycle(objects)
	tm.SetQueuedMessageFilter(objects)
	if err := decl.PublishObjectClassAttributes(ctx, "fed", 1, 1, []core.AttributeHandle{1, 2}); err != nil {
		t.Fatal(err)
	}
	if err := decl.PublishObjectClassAttributes(ctx, "fed", 3, 1, []core.AttributeHandle{1, 2}); err != nil {
		t.Fatal(err)
	}
	for _, h := range []core.FederateHandle{2, 3} {
		if err := decl.SubscribeObjectClassAttributes(ctx, "fed", h, 1, []core.AttributeHandle{2}); err != nil {
			t.Fatal(err)
		}
	}
	obj, _, err := objects.Register(ctx, "fed", 1, 1, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := tm.EnableConstrained(ctx, "fed", 2); err != nil {
		t.Fatal(err)
	}
	out.events = nil
	return objects, owners, tm, out, obj
}

func TestROResignQueuesRemovalAndRetainsMetadata(t *testing.T) {
	for _, release := range []string{"async", advanceOperationName, "unconstrain"} {
		t.Run(release, func(t *testing.T) {
			ctx := context.Background()
			objects, owners, tm, out, obj := roResignFixture(t)
			plan, err := PrepareResign(ctx, "fed", 1, core.ResignActionDeleteObjects, objects, owners, out, tm)
			if err != nil {
				t.Fatal(err)
			}
			defer plan.Release()
			if len(out.events) != 0 {
				t.Fatal("preparation published callbacks")
			}
			if err := plan.Commit(); err != nil {
				t.Fatal(err)
			}
			if len(out.events) != 1 || out.events[0].Recipient != 3 || tm.Metrics("fed", 2).ROQueued != 1 {
				t.Fatalf("RO resign callbacks=%+v metrics=%+v", out.events, tm.Metrics("fed", 2))
			}
			if len(objects.ExportState("fed").Objects) != 0 || owners.IsOwnedBy("fed", 1, obj, 1) {
				t.Fatal("resign did not retire object and ownership")
			}
			tm.OnFederateResign(ctx, "fed", 1)
			switch release {
			case "async":
				err = tm.EnableAsynchronousDelivery(ctx, "fed", 2)
			case advanceOperationName:
				err = tm.TimeAdvanceRequest(ctx, "fed", 2, 1)
			case "unconstrain":
				err = tm.DisableConstrained(ctx, "fed", 2)
			}
			if err != nil {
				t.Fatal(err)
			}
			wantCallbacks := 2
			if release == advanceOperationName {
				wantCallbacks++
			}
			if len(out.events) != wantCallbacks || out.events[1].Recipient != 2 {
				t.Fatalf("deferred removal=%+v", out.events)
			}
			if release == advanceOperationName {
				if _, ok := out.events[2].Event.(*hlatime.TimeAdvanceGrant); !ok {
					t.Fatal("resign removal did not precede grant")
				}
			}
			for _, delivery := range out.events[:2] {
				event, ok := delivery.Event.(*object.CallbackEvent)
				if !ok || event.Kind != object.CallbackRemove || event.Object != obj || event.ObjectClass != 1 || event.LogicalTime != nil || event.Metadata == nil || event.Metadata.Producer != 1 || event.Metadata.SentOrder != core.OrderReceive || event.Metadata.ReceivedOrder != core.OrderReceive || event.Metadata.RetractionHandle != 0 {
					t.Fatalf("resign removal metadata=%+v", delivery.Event)
				}
			}
			if err := plan.Commit(); err != nil {
				t.Fatal(err)
			}
			if tm.Metrics("fed", 2).ROQueued != 0 {
				t.Fatal("idempotent commit requeued removal")
			}
		})
	}
}

func TestROResignOwnershipControlIsImmediate(t *testing.T) {
	ctx := context.Background()
	objects, owners, tm, out, _ := roResignFixture(t)
	survivor, _, err := objects.Register(ctx, "fed", 3, 1, "")
	if err != nil {
		t.Fatal(err)
	}
	owners.RegisterInitialOwnership("fed", 1, survivor, []core.AttributeHandle{2})
	if err := owners.Acquire(ctx, "fed", 2, survivor, []core.AttributeHandle{2}, nil); err != nil {
		t.Fatal(err)
	}
	out.events = nil
	plan, err := PrepareResign(ctx, "fed", 1, core.ResignActionDeleteThenDivest, objects, owners, out, tm)
	if err != nil {
		t.Fatal(err)
	}
	defer plan.Release()
	if err := plan.Commit(); err != nil {
		t.Fatal(err)
	}
	if len(out.events) != 2 || out.events[0].Recipient != 3 || out.events[1].Recipient != 2 {
		t.Fatalf("immediate removal/control callbacks=%+v", out.events)
	}
	if _, ok := out.events[1].Event.(*ownership.CallbackEvent); !ok {
		t.Fatalf("control callback=%T", out.events[1].Event)
	}
	if !owners.IsOwnedBy("fed", 2, survivor, 2) || tm.Metrics("fed", 2).ROQueued != 1 {
		t.Fatal("ownership control was delayed with RO removal")
	}
	if len(objects.ExportState("fed").Objects) != 1 {
		t.Fatal("unrelated object was deleted")
	}
}

func TestROResignDiscardsSupersededTSOWithoutDroppingRO(t *testing.T) {
	ctx := context.Background()
	objects, owners, tm, out, obj := roResignFixture(t)
	if err := tm.EnableConstrained(ctx, "fed", 3); err != nil {
		t.Fatal(err)
	}
	out.events = nil
	ts := core.LogicalTime(10)
	if err := objects.Delete(ctx, "fed", 1, obj, &ts, nil); err != nil {
		t.Fatal(err)
	}
	if tm.Metrics("fed", 2).TSOQueued != 1 || tm.Metrics("fed", 3).TSOQueued != 1 {
		t.Fatal("expected pending timestamped removals")
	}
	plan, err := PrepareResign(ctx, "fed", 1, core.ResignActionDeleteObjects, objects, owners, out, tm)
	if err != nil {
		t.Fatal(err)
	}
	defer plan.Release()
	if err := plan.Commit(); err != nil {
		t.Fatal(err)
	}
	for _, h := range []core.FederateHandle{2, 3} {
		if metrics := tm.Metrics("fed", h); metrics.TSOQueued != 0 || metrics.ROQueued != 1 {
			t.Fatalf("post-resign queues=%+v", metrics)
		}
		if err := tm.EnableAsynchronousDelivery(ctx, "fed", h); err != nil {
			t.Fatal(err)
		}
	}
	if len(out.events) != 2 {
		t.Fatalf("duplicate or missing removal callbacks=%+v", out.events)
	}
	for _, delivery := range out.events {
		event := delivery.Event.(*object.CallbackEvent)
		if event.Kind != object.CallbackRemove || event.LogicalTime != nil || event.Metadata.ReceivedOrder != core.OrderReceive {
			t.Fatalf("superseded timed removal leaked: %+v", event)
		}
	}
}

func TestROResignAdmissionFailureAndReleasePreserveState(t *testing.T) {
	for _, failure := range []string{"reserve", "ownership", "release"} {
		t.Run(failure, func(t *testing.T) {
			ctx := context.Background()
			objects, owners, tm, out, obj := roResignFixture(t)
			action := core.ResignActionDeleteObjects
			injected := errors.New("resign outbox unavailable")
			if failure == "reserve" {
				out.fail = injected
			}
			if failure == "ownership" {
				action = core.ResignActionNoAction
			}
			plan, err := PrepareResign(ctx, "fed", 1, action, objects, owners, out, tm)
			if failure == "release" {
				if err != nil {
					t.Fatal(err)
				}
				plan.Release()
			} else if err == nil {
				plan.Release()
				t.Fatal("expected preparation failure")
			}
			if failure == "reserve" && !errors.Is(err, injected) {
				t.Fatalf("reserve error=%v", err)
			}
			if failure == "ownership" && !errors.Is(err, core.ErrFederateOwnsAttributes) {
				t.Fatalf("ownership error=%v", err)
			}
			if len(objects.ExportState("fed").Objects) != 1 || !owners.IsOwnedBy("fed", 1, obj, 1) {
				t.Fatal("failed resign mutated service state")
			}
			if len(out.events) != 0 || tm.Metrics("fed", 2).ROQueued != 0 {
				t.Fatal("failed resign published callbacks")
			}
			out.fail = nil
			plan, err = PrepareResign(ctx, "fed", 1, core.ResignActionDeleteObjects, objects, owners, out, tm)
			if err != nil {
				t.Fatal(err)
			}
			if err := plan.Commit(); err != nil {
				t.Fatal(err)
			}
			plan.Release()
			if tm.Metrics("fed", 2).ROQueued != 1 {
				t.Fatal("retry did not queue constrained recipient")
			}
		})
	}
}

const advanceOperationName = "advance"
