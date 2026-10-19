package m24spec

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/internal/application/lifecycle"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/declaration"
	"github.com/cbchoi/gorti/rti/internal/hla/federation"
	"github.com/cbchoi/gorti/rti/internal/hla/object"
	"github.com/cbchoi/gorti/rti/internal/hla/ownership"
)

const resignFed core.FederationName = "resign-contract"

type resignOutbox struct {
	mu     sync.Mutex
	events []core.OutboxDelivery
	fail   error
}

func (o *resignOutbox) Send(_ context.Context, _ core.FederationName, h core.FederateHandle, e core.OutboundEvent) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.events = append(o.events, core.OutboxDelivery{Recipient: h, Event: e})
	return nil
}
func (o *resignOutbox) Reserve(_ context.Context, _ core.FederationName, deliveries []core.OutboxDelivery) (core.OutboxReservation, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.fail != nil {
		return nil, o.fail
	}
	return &resignReservation{o: o, events: append([]core.OutboxDelivery(nil), deliveries...)}, nil
}
func (o *resignOutbox) drain() []core.OutboxDelivery {
	o.mu.Lock()
	defer o.mu.Unlock()
	result := o.events
	o.events = nil
	return result
}

type resignReservation struct {
	o      *resignOutbox
	events []core.OutboxDelivery
	once   sync.Once
}

func (r *resignReservation) Commit() error {
	r.once.Do(func() { r.o.mu.Lock(); defer r.o.mu.Unlock(); r.o.events = append(r.o.events, r.events...) })
	return nil
}
func (r *resignReservation) Release() { r.once.Do(func() {}) }

type resignLog struct {
	mu      sync.Mutex
	records []core.EventRecord
	fail    error
}

func (l *resignLog) Append(_ context.Context, _ core.FederationName, e core.EventRecord) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.fail != nil {
		return l.fail
	}
	l.records = append(l.records, e)
	return nil
}
func (*resignLog) Sync(context.Context, core.FederationName) error { return nil }
func (*resignLog) OpenReader(context.Context, string) (core.EventLogReader, error) {
	return nil, errors.New("unused")
}

type resignFOM struct{ nopFOMHandle }

func (*resignFOM) AttributeName(cls core.ObjectClassHandle, attr core.AttributeHandle) (string, bool) {
	if cls != 1 {
		return "", false
	}
	switch attr {
	case 1:
		return "HLAprivilegeToDeleteObject", true
	case 2:
		return "value", true
	default:
		return "", false
	}
}

func (*resignFOM) AttributeHandles(cls core.ObjectClassHandle) []core.AttributeHandle {
	if cls != 1 {
		return nil
	}
	return []core.AttributeHandle{1, 2}
}

func (*resignFOM) LookupAttribute(_ core.ObjectClassHandle, name string) (core.AttributeHandle, bool) {
	if name == "HLAprivilegeToDeleteObject" {
		return 1, true
	}
	return 2, true
}

type resignFOMRepo struct{}

func (*resignFOMRepo) Load(context.Context, []core.FOMModule) (core.FOMHandle, error) {
	return &resignFOM{}, nil
}
func (*resignFOMRepo) Get(context.Context, core.FederationName) (core.FOMHandle, error) {
	return &resignFOM{}, nil
}

type resignFixture struct {
	fm      *federation.Manager
	om      *object.Registry
	own     *ownership.Manager
	dm      *declaration.Manager
	out     *resignOutbox
	log     *resignLog
	a, b, c core.FederateHandle
}

func newResignFixture(t *testing.T) *resignFixture {
	t.Helper()
	f := &resignFixture{out: &resignOutbox{}, log: &resignLog{}, dm: declaration.New()}
	var err error
	f.own, err = ownership.New(ownership.Options{Outbox: f.out})
	if err != nil {
		t.Fatal(err)
	}
	clock := core.NewManualClock(time.Unix(0, 0))
	foms := &resignFOMRepo{}
	f.dm.SetFOMRepository(foms)
	f.om, err = object.New(object.Options{Clock: clock, FOMs: foms, Declarations: f.dm, Outbox: f.out, Ownership: f.own,
		OnRegister: func(fed core.FederationName, h core.FederateHandle, obj core.ObjectHandle, _ core.ObjectClassHandle, attrs []core.AttributeHandle) {
			f.own.RegisterInitialOwnership(fed, h, obj, attrs)
		}, OnDelete: f.own.OnObjectDeleted})
	if err != nil {
		t.Fatal(err)
	}
	f.fm, err = federation.New(federation.Options{Clock: clock, FOMs: foms, EventLog: f.log,
		OnFederateResigned: func(_ context.Context, fed core.FederationName, h core.FederateHandle) {
			f.om.OnFederateResign(fed, h)
		},
		PrepareResign: func(ctx context.Context, fed core.FederationName, h core.FederateHandle, a core.ResignAction) (federation.ResignPreparation, error) {
			return lifecycle.PrepareResign(ctx, fed, h, a, f.om, f.own, f.out)
		}})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err = f.fm.CreateFederation(ctx, core.CreateFederationRequest{Name: resignFed}); err != nil {
		t.Fatal(err)
	}
	for i, p := range []*core.FederateHandle{&f.a, &f.b, &f.c} {
		*p, err = f.fm.JoinFederation(ctx, core.JoinFederationRequest{Federation: resignFed, FederateName: fmt.Sprintf("f%d", i)})
		if err != nil {
			t.Fatal(err)
		}
		if err = f.dm.PublishObjectClassAttributes(ctx, resignFed, *p, 1, []core.AttributeHandle{2}); err != nil {
			t.Fatal(err)
		}
		if err = f.dm.SubscribeObjectClassAttributes(ctx, resignFed, *p, 1, []core.AttributeHandle{2}); err != nil {
			t.Fatal(err)
		}
	}
	return f
}
func (f *resignFixture) register(t *testing.T, h core.FederateHandle) core.ObjectHandle {
	t.Helper()
	obj, _, err := f.om.Register(context.Background(), resignFed, h, 1, "")
	if err != nil {
		t.Fatal(err)
	}
	return obj
}
func (f *resignFixture) resign(h core.FederateHandle, a core.ResignAction) error {
	return f.fm.ResignFederation(context.Background(), resignFed, h, a)
}
func (f *resignFixture) transfer(t *testing.T, obj core.ObjectHandle, attr core.AttributeHandle, from, to core.FederateHandle) {
	t.Helper()
	ctx := context.Background()
	if err := f.own.UnconditionalDivest(ctx, resignFed, from, obj, []core.AttributeHandle{attr}); err != nil {
		t.Fatal(err)
	}
	if err := f.own.Acquire(ctx, resignFed, to, obj, []core.AttributeHandle{attr}, nil); err != nil {
		t.Fatal(err)
	}
}

func TestResignContractOwnedAttributePreconditions(t *testing.T) {
	for _, action := range []core.ResignAction{core.ResignActionNoAction, core.ResignActionCancelPendingOwnership, core.ResignActionDeleteObjects} {
		t.Run(fmt.Sprint(action), func(t *testing.T) {
			f := newResignFixture(t)
			obj := f.register(t, f.b)
			f.transfer(t, obj, 2, f.b, f.a)
			f.out.drain()
			before := len(f.log.records)
			if err := f.resign(f.a, action); !errors.Is(err, core.ErrFederateOwnsAttributes) {
				t.Fatalf("resign = %v", err)
			}
			if err := f.fm.ValidateMember(resignFed, f.a); err != nil {
				t.Fatal("rejected resign removed member", err)
			}
			if !f.own.IsOwnedBy(resignFed, f.a, obj, 2) || f.om.ClassOf(resignFed, obj) == 0 {
				t.Fatal("rejected resign changed service state")
			}
			if len(f.out.drain()) != 0 || len(f.log.records) != before {
				t.Fatal("rejected resign emitted callback or journal")
			}
		})
	}
}

func TestResignContractPendingAcquisitionPreconditions(t *testing.T) {
	for _, action := range []core.ResignAction{core.ResignActionNoAction, core.ResignActionUnconditionallyDivestAttributes, core.ResignActionDeleteObjects, core.ResignActionDeleteThenDivest} {
		t.Run(fmt.Sprint(action), func(t *testing.T) {
			f := newResignFixture(t)
			obj := f.register(t, f.b)
			if err := f.own.Acquire(context.Background(), resignFed, f.a, obj, []core.AttributeHandle{2}, nil); err != nil {
				t.Fatal(err)
			}
			f.out.drain()
			before := f.own.Snapshot(resignFed)
			if err := f.resign(f.a, action); !errors.Is(err, core.ErrOwnershipAcquisitionPending) {
				t.Fatalf("resign = %v", err)
			}
			if f.own.Snapshot(resignFed) != before || len(f.out.drain()) != 0 {
				t.Fatal("failed resign altered ownership")
			}
			if err := f.resign(f.a, core.ResignActionCancelPendingOwnership); err != nil {
				t.Fatal(err)
			}
			if err := f.own.CancelAcquire(context.Background(), resignFed, f.a, obj, []core.AttributeHandle{2}); !errors.Is(err, core.ErrOwnershipNotInTransfer) {
				t.Fatalf("acquisition remains: %v", err)
			}
		})
	}
}

func TestResignContractAllActions(t *testing.T) {
	for _, action := range []core.ResignAction{core.ResignActionNoAction, core.ResignActionCancelPendingOwnership, core.ResignActionUnconditionallyDivestAttributes, core.ResignActionDeleteObjects, core.ResignActionDeleteThenDivest, core.ResignActionCancelThenDelete} {
		t.Run(fmt.Sprint(action), func(t *testing.T) {
			f := newResignFixture(t)
			var obj core.ObjectHandle
			if action != core.ResignActionNoAction && action != core.ResignActionCancelPendingOwnership {
				obj = f.register(t, f.a)
			}
			if err := f.resign(f.a, action); err != nil {
				t.Fatal(err)
			}
			if err := f.fm.ValidateMember(resignFed, f.a); !errors.Is(err, core.ErrFederateNotJoined) {
				t.Fatalf("membership = %v", err)
			}
			if obj != 0 {
				wantPresent := action == core.ResignActionUnconditionallyDivestAttributes
				if (f.om.ClassOf(resignFed, obj) != 0) != wantPresent {
					t.Fatal("wrong deletion result")
				}
				if f.own.IsOwnedBy(resignFed, f.a, obj, 1) || f.own.IsOwnedBy(resignFed, f.a, obj, 2) {
					t.Fatal("resigned ownership remains")
				}
			}
			r, ok := f.log.records[len(f.log.records)-1].(*core.FederationResignedJournalRecord)
			if !ok || r.ResignAction != action || r.FederateHandle != f.a {
				t.Fatal("journal did not preserve action")
			}
		})
	}
}

func TestResignContractDeletesSparseAndTransferredPrivilege(t *testing.T) {
	f := newResignFixture(t)
	ctx := context.Background()
	for i := 0; i < 40; i++ {
		obj := f.register(t, f.a)
		if err := f.om.Delete(ctx, resignFed, f.a, obj, nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	obj := f.register(t, f.a)
	if obj <= 40 {
		t.Fatal("fixture lacks sparse high handle")
	}
	f.transfer(t, obj, 1, f.a, f.b)
	if err := f.om.Delete(ctx, resignFed, f.a, obj, nil, nil); !errors.Is(err, core.ErrObjectNotOwned) {
		t.Fatalf("old privilege owner delete = %v", err)
	}
	f.out.drain()
	if err := f.resign(f.b, core.ResignActionDeleteObjects); err != nil {
		t.Fatal(err)
	}
	if f.om.ClassOf(resignFed, obj) != 0 {
		t.Fatal("sparse object survived")
	}
	if got := f.own.Snapshot(resignFed); got != (core.OwnershipSnapshot{}) {
		t.Fatalf("deleted ownership remains: %+v", got)
	}
	events := f.out.drain()
	if len(events) != 2 {
		t.Fatalf("removal callbacks = %d, want original registrant and other subscriber", len(events))
	}
	for _, d := range events {
		e, ok := d.Event.(*object.CallbackEvent)
		if !ok || e.Kind != object.CallbackRemove || e.Object != obj || d.Recipient == f.b {
			t.Fatalf("wrong callback: %+v", d)
		}
	}
}

func TestResignContractDivestFulfillsAcquirer(t *testing.T) {
	f := newResignFixture(t)
	obj := f.register(t, f.a)
	if err := f.own.Acquire(context.Background(), resignFed, f.b, obj, []core.AttributeHandle{2}, nil); err != nil {
		t.Fatal(err)
	}
	f.out.drain()
	if err := f.resign(f.a, core.ResignActionUnconditionallyDivestAttributes); err != nil {
		t.Fatal(err)
	}
	if !f.own.IsOwnedBy(resignFed, f.b, obj, 2) {
		t.Fatal("queued acquirer not selected")
	}
	events := f.out.drain()
	if len(events) != 1 {
		t.Fatalf("callbacks = %d", len(events))
	}
	e, ok := events[0].Event.(*ownership.CallbackEvent)
	if !ok || e.Kind != ownership.CallbackAcquisitionNotification || events[0].Recipient != f.b {
		t.Fatal("missing acquisition callback")
	}
}

func TestResignContractFailurePreservesAllState(t *testing.T) {
	for _, boundary := range []string{"reservation", "journal"} {
		t.Run(boundary, func(t *testing.T) {
			f := newResignFixture(t)
			obj := f.register(t, f.a)
			f.out.drain()
			before := f.own.Snapshot(resignFed)
			n := len(f.log.records)
			failure := errors.New("injected " + boundary)
			if boundary == "reservation" {
				f.out.fail = failure
			} else {
				f.log.fail = failure
			}
			if err := f.resign(f.a, core.ResignActionCancelThenDelete); !errors.Is(err, failure) {
				t.Fatalf("resign = %v", err)
			}
			if f.om.ClassOf(resignFed, obj) == 0 || f.own.Snapshot(resignFed) != before {
				t.Fatal("failed resign changed state")
			}
			if err := f.fm.ValidateMember(resignFed, f.a); err != nil {
				t.Fatal(err)
			}
			if len(f.out.drain()) != 0 || len(f.log.records) != n {
				t.Fatal("failed resign emitted effects")
			}
			f.out.fail = nil
			f.log.fail = nil
			if err := f.resign(f.a, core.ResignActionCancelThenDelete); err != nil {
				t.Fatal("retry", err)
			}
		})
	}
}

func TestResignContractCancelDeleteDivestsOtherObjects(t *testing.T) {
	f := newResignFixture(t)
	ctx := context.Background()
	deleted := f.register(t, f.a)
	retained := f.register(t, f.b)
	f.transfer(t, retained, 2, f.b, f.a)
	if err := f.own.Acquire(ctx, resignFed, f.a, retained, []core.AttributeHandle{1}, nil); err != nil {
		t.Fatal(err)
	}
	if err := f.own.Acquire(ctx, resignFed, f.c, retained, []core.AttributeHandle{2}, nil); err != nil {
		t.Fatal(err)
	}
	f.out.drain()
	if err := f.resign(f.a, core.ResignActionCancelThenDelete); err != nil {
		t.Fatal(err)
	}
	if f.om.ClassOf(resignFed, deleted) != 0 || f.om.ClassOf(resignFed, retained) == 0 {
		t.Fatal("wrong object deletion set")
	}
	if !f.own.IsOwnedBy(resignFed, f.c, retained, 2) || !f.own.IsOwnedBy(resignFed, f.b, retained, 1) {
		t.Fatal("remaining ownership not divested correctly")
	}
	if err := f.own.CancelAcquire(ctx, resignFed, f.a, retained, []core.AttributeHandle{1}); !errors.Is(err, core.ErrOwnershipNotInTransfer) {
		t.Fatalf("resigned acquisition remains: %v", err)
	}
	events := f.out.drain()
	if len(events) != 3 {
		t.Fatalf("callbacks = %d, want two removals and one acquisition", len(events))
	}
	for _, d := range events {
		if d.Recipient == f.a {
			t.Fatal("callback targets departed member")
		}
	}
}

func TestResignContractKnownObjectSurvivesUnsubscribe(t *testing.T) {
	f := newResignFixture(t)
	obj := f.register(t, f.a)
	if err := f.dm.UnsubscribeObjectClassAttributes(context.Background(), resignFed, f.b, 1, []core.AttributeHandle{2}); err != nil {
		t.Fatal(err)
	}
	f.out.drain()
	if err := f.resign(f.a, core.ResignActionDeleteObjects); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, d := range f.out.drain() {
		if e, ok := d.Event.(*object.CallbackEvent); ok && e.Object == obj && e.Kind == object.CallbackRemove && d.Recipient == f.b {
			found = true
		}
	}
	if !found {
		t.Fatal("known object removal lost after unsubscribe")
	}
}

func TestResignContractDeletedNameReuseAndFailure(t *testing.T) {
	f := newResignFixture(t)
	ctx := context.Background()
	if err := f.om.ReserveObjectInstanceName(ctx, resignFed, f.a, "reusable"); err != nil {
		t.Fatal(err)
	}
	obj, name, err := f.om.Register(ctx, resignFed, f.a, 1, "reusable")
	if err != nil {
		t.Fatal(err)
	}
	f.out.drain()
	checkReservation := func(caller core.FederateHandle, want object.CallbackKind) {
		t.Helper()
		if err := f.om.ReserveObjectInstanceName(ctx, resignFed, caller, name); err != nil {
			t.Fatal(err)
		}
		events := f.out.drain()
		if len(events) != 1 {
			t.Fatalf("reservation callbacks = %d", len(events))
		}
		e, ok := events[0].Event.(*object.CallbackEvent)
		if !ok || e.Kind != want || e.ObjectName != name || events[0].Recipient != caller {
			t.Fatalf("reservation result = %+v", events[0])
		}
	}
	f.log.fail = errors.New("journal unavailable")
	if err := f.resign(f.a, core.ResignActionDeleteObjects); err == nil {
		t.Fatal("journal failure ignored")
	}
	checkReservation(f.b, object.CallbackReservationFailed)
	if got, ok := f.om.LookupObjectInstanceByName(resignFed, name); !ok || got != obj {
		t.Fatal("failed resign lost name binding")
	}
	f.log.fail = nil
	if err := f.resign(f.a, core.ResignActionDeleteObjects); err != nil {
		t.Fatal(err)
	}
	f.out.drain()
	checkReservation(f.b, object.CallbackReservationSucceeded)
	replacement, _, err := f.om.Register(ctx, resignFed, f.b, 1, name)
	if err != nil {
		t.Fatal("re-register deleted name", err)
	}
	if err := f.om.Delete(ctx, resignFed, f.b, replacement, nil, nil); err != nil {
		t.Fatal(err)
	}
	f.out.drain()
	checkReservation(f.c, object.CallbackReservationFailed)
	if err := f.om.ReleaseObjectInstanceName(ctx, resignFed, f.b, name); err != nil {
		t.Fatal(err)
	}
	checkReservation(f.c, object.CallbackReservationSucceeded)
}
