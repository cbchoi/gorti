package ownership

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

type releaseDenier interface {
	ReleaseDenied(context.Context, core.FederationName, core.FederateHandle, core.ObjectHandle, []core.AttributeHandle) error
}

type denialOutbox struct {
	fakeOutbox
	fail          error
	beforeCommit  func()
	beforeReserve func() error
}

type denialReservation struct {
	once   sync.Once
	commit func()
}

func (r *denialReservation) Commit() error { r.once.Do(r.commit); return nil }
func (r *denialReservation) Release()      { r.once.Do(func() {}) }

func (o *denialOutbox) Reserve(ctx context.Context, fed core.FederationName, deliveries []core.OutboxDelivery) (core.OutboxReservation, error) {
	if o.beforeReserve != nil {
		if err := o.beforeReserve(); err != nil {
			return nil, err
		}
	}
	if o.fail != nil {
		return nil, o.fail
	}
	return &denialReservation{commit: func() {
		if o.beforeCommit != nil {
			o.beforeCommit()
		}
		for _, d := range deliveries {
			_ = o.Send(ctx, fed, d.Recipient, d.Event)
		}
	}}, nil
}

func denialManager(t *testing.T) (*Manager, *denialOutbox, releaseDenier) {
	t.Helper()
	o := &denialOutbox{}
	m, err := New(Options{Outbox: o})
	if err != nil {
		t.Fatal(err)
	}
	d, ok := any(m).(releaseDenier)
	if !ok {
		t.Fatal("ownership manager is missing ReleaseDenied")
	}
	m.RegisterInitialOwnership("fed", 1, 100, []core.AttributeHandle{10, 20, 30})
	m.RegisterInitialOwnership("fed", 1, 200, []core.AttributeHandle{10})
	for _, request := range []struct {
		who   core.FederateHandle
		obj   core.ObjectHandle
		attrs []core.AttributeHandle
	}{
		{2, 100, []core.AttributeHandle{10, 20, 30}},
		{3, 100, []core.AttributeHandle{10, 20}},
		{2, 200, []core.AttributeHandle{10}},
	} {
		if err := m.Acquire(context.Background(), "fed", request.who, request.obj, request.attrs, []byte("request")); err != nil {
			t.Fatal(err)
		}
	}
	o.sent = nil
	return m, o, d
}

func TestReleaseDeniedCancelsGroupedRequestsWithoutDivesting(t *testing.T) {
	m, out, d := denialManager(t)
	out.beforeCommit = func() {
		if got := len(m.ExportState("fed").PendingAcquires); got != 2 {
			t.Errorf("callback observes %d pending requests; want 2", got)
		}
	}
	if err := d.ReleaseDenied(context.Background(), "fed", 1, 100, []core.AttributeHandle{20, 10, 20}); err != nil {
		t.Fatal(err)
	}
	for _, attr := range []core.AttributeHandle{10, 20, 30} {
		if !m.IsOwnedBy("fed", 1, 100, attr) {
			t.Errorf("owner changed for %d", attr)
		}
	}
	wantPending := []PendingAcquireState{{Object: 100, Attribute: 30, Acquirer: 2, Tag: []byte("request")}, {Object: 200, Attribute: 10, Acquirer: 2, Tag: []byte("request")}}
	if got := m.ExportState("fed").PendingAcquires; !reflect.DeepEqual(got, wantPending) {
		t.Fatalf("pending = %#v", got)
	}
	records := out.Sent()
	if len(records) != 2 {
		t.Fatalf("callbacks = %d; want 2", len(records))
	}
	for i, record := range records {
		e, ok := record.Event.(*CallbackEvent)
		if !ok || record.Federate != core.FederateHandle(i+2) || e.Kind != CallbackUnavailable || e.Object != 100 || !reflect.DeepEqual(e.Attributes, []core.AttributeHandle{10, 20}) {
			t.Errorf("callback = %#v / %#v", record, e)
		}
	}
}

func TestReleaseDeniedValidatesWholeOwnerSetBeforeMutation(t *testing.T) {
	m, out, d := denialManager(t)
	m.RegisterInitialOwnership("fed", 4, 100, []core.AttributeHandle{30})
	before := m.ExportState("fed")
	if err := d.ReleaseDenied(context.Background(), "fed", 1, 100, []core.AttributeHandle{10, 30}); !errors.Is(err, core.ErrAttributeNotOwned) {
		t.Fatalf("error = %v", err)
	}
	if !reflect.DeepEqual(before, m.ExportState("fed")) || len(out.Sent()) != 0 {
		t.Fatal("invalid request changed state or delivered callbacks")
	}
}

func TestReleaseDeniedAdmissionFailurePreservesAllRequests(t *testing.T) {
	m, out, d := denialManager(t)
	out.fail = core.ErrFederateOverflow
	before := m.ExportState("fed")
	if err := d.ReleaseDenied(context.Background(), "fed", 1, 100, []core.AttributeHandle{10, 20}); !errors.Is(err, core.ErrFederateOverflow) {
		t.Fatalf("error = %v", err)
	}
	if !reflect.DeepEqual(before, m.ExportState("fed")) || len(out.Sent()) != 0 {
		t.Fatal("failed admission changed state or delivered callbacks")
	}
}

func TestReleaseDeniedWithoutPendingRequestHasNoCallback(t *testing.T) {
	m, out, d := denialManager(t)
	m.RegisterInitialOwnership("fed", 1, 100, []core.AttributeHandle{40})
	before := m.ExportState("fed")
	if err := d.ReleaseDenied(context.Background(), "fed", 1, 100, []core.AttributeHandle{40}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, m.ExportState("fed")) || len(out.Sent()) != 0 {
		t.Fatal("unrequested denial changed state or delivered callbacks")
	}
}

func TestReleaseDeniedPreservesIndependentDivestOffer(t *testing.T) {
	m, _, d := denialManager(t)
	if err := m.NegotiatedDivestTwoPhase(context.Background(), "fed", 1, 100, []core.AttributeHandle{30}, []byte("offer")); err != nil {
		t.Fatal(err)
	}
	before := m.ExportState("fed").PendingDivests
	if err := d.ReleaseDenied(context.Background(), "fed", 1, 100, []core.AttributeHandle{10, 20}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, m.ExportState("fed").PendingDivests) {
		t.Fatal("denial changed independent divest offer")
	}
}

func TestReleaseDeniedReservesBeforePinningObjectState(t *testing.T) {
	m, out, d := denialManager(t)
	var objectMu sync.Mutex
	m.opts.PrepareReleaseDenied = func(context.Context, core.FederationName, core.FederateHandle, core.ObjectHandle, []core.AttributeHandle) (func(), error) {
		objectMu.Lock()
		return objectMu.Unlock, nil
	}
	out.beforeReserve = func() error {
		if !objectMu.TryLock() {
			return errors.New("object state held while reserving outbox: lock inversion")
		}
		objectMu.Unlock()
		return nil
	}
	if err := d.ReleaseDenied(context.Background(), "fed", 1, 100, []core.AttributeHandle{10, 20}); err != nil {
		t.Fatal(err)
	}
}

func TestReleaseDeniedRevalidatesPendingSetAfterReservation(t *testing.T) {
	m, out, d := denialManager(t)
	reservations := 0
	out.beforeReserve = func() error {
		reservations++
		if reservations == 1 {
			return m.CancelAcquire(context.Background(), "fed", 2, 100, []core.AttributeHandle{10})
		}
		return nil
	}
	if err := d.ReleaseDenied(context.Background(), "fed", 1, 100, []core.AttributeHandle{10, 20}); err != nil {
		t.Fatal(err)
	}
	if reservations != 2 {
		t.Fatalf("reservations=%d; want retry", reservations)
	}
	for _, record := range out.Sent() {
		e := record.Event.(*CallbackEvent)
		if e.Kind == CallbackUnavailable && record.Federate == 2 && !reflect.DeepEqual(e.Attributes, []core.AttributeHandle{20}) {
			t.Fatalf("cancelled attribute incorrectly denied: %v", e.Attributes)
		}
	}
	if got := len(m.ExportState("fed").PendingAcquires); got != 2 {
		t.Fatalf("pending=%d", got)
	}
}

func TestReleaseDeniedRevalidatesOwnerAfterReservation(t *testing.T) {
	m, out, d := denialManager(t)
	before := m.ExportState("fed").PendingAcquires
	out.beforeReserve = func() error { m.RegisterInitialOwnership("fed", 4, 100, []core.AttributeHandle{10}); return nil }
	if err := d.ReleaseDenied(context.Background(), "fed", 1, 100, []core.AttributeHandle{10, 20}); !errors.Is(err, core.ErrAttributeNotOwned) {
		t.Fatalf("error=%v", err)
	}
	if !reflect.DeepEqual(before, m.ExportState("fed").PendingAcquires) || len(out.Sent()) != 0 {
		t.Fatal("owner changed during admission but denial still committed")
	}
}

type denialFailLog struct{ permissiveLog }

func (*denialFailLog) Append(context.Context, core.FederationName, core.EventRecord) error {
	return errors.New("journal unavailable")
}

func TestReleaseDeniedJournalFailureAndMissingReservationLeaveStateUntouched(t *testing.T) {
	for _, scenario := range []string{"journal", "reservation"} {
		t.Run(scenario, func(t *testing.T) {
			m, out, d := denialManager(t)
			if scenario == "journal" {
				m.opts.EventLog = &denialFailLog{}
			} else {
				m.opts.Outbox = &fakeOutbox{}
			}
			before := m.ExportState("fed")
			if err := d.ReleaseDenied(context.Background(), "fed", 1, 100, []core.AttributeHandle{10, 20}); err == nil {
				t.Fatal("expected infrastructure failure")
			}
			if !reflect.DeepEqual(before, m.ExportState("fed")) || len(out.Sent()) != 0 {
				t.Fatal("failed denial caused side effects")
			}
		})
	}
}
