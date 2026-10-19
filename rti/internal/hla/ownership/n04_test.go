package ownership

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

type n04Log struct {
	permissiveLog
	onDivest func()
}

func (l *n04Log) Append(ctx context.Context, fed core.FederationName, e core.EventRecord) error {
	if e.(*eventRecord).kind == evtDivestIfWanted && l.onDivest != nil {
		l.onDivest()
	}
	return l.permissiveLog.Append(ctx, fed, e)
}

type n04Result interface {
	DivestIfWantedWithResult(context.Context, core.FederationName, core.FederateHandle, core.ObjectHandle, []core.AttributeHandle) ([]core.AttributeHandle, error)
}

type n04Confirm interface {
	ConfirmDivestitureWithTag(context.Context, core.FederationName, core.FederateHandle, core.ObjectHandle, []core.AttributeHandle, []byte) error
}

func TestN04AcquisitionBinaryTag(t *testing.T) {
	m, out := newMgr(t, Options{})
	tag := []byte{0, 0x80, 0xff}
	if err := m.Acquire(context.Background(), "fed", 2, 100, []core.AttributeHandle{1}, tag); err != nil {
		t.Fatal(err)
	}
	tag[0] = 42
	e := out.Sent()[0].Event.(*CallbackEvent)
	if e.Kind != CallbackAcquisitionNotification || !bytes.Equal(e.Tag, []byte{0, 0x80, 0xff}) {
		t.Fatalf("callback = %+v", e)
	}
}

func TestN04ConfirmBinaryTag(t *testing.T) {
	m, out := newMgr(t, Options{})
	c, ok := any(m).(n04Confirm)
	if !ok {
		t.Fatal("missing tag-preserving confirmation")
	}
	ctx := context.Background()
	m.RegisterInitialOwnership("fed", 1, 100, []core.AttributeHandle{1})
	if err := m.NegotiatedDivestTwoPhase(ctx, "fed", 1, 100, []core.AttributeHandle{1}, []byte{1, 0x80, 0xff}); err != nil {
		t.Fatal(err)
	}
	if err := m.Acquire(ctx, "fed", 2, 100, []core.AttributeHandle{1}, []byte{2, 0x80, 0xff}); err != nil {
		t.Fatal(err)
	}
	if err := c.ConfirmDivestitureWithTag(ctx, "fed", 1, 100, []core.AttributeHandle{1}, []byte{0, 0x80, 0xff}); err != nil {
		t.Fatal(err)
	}
	sent := out.Sent()
	e := sent[len(sent)-1].Event.(*CallbackEvent)
	if e.Kind != CallbackAcquisitionNotification || !bytes.Equal(e.Tag, []byte{0, 0x80, 0xff}) {
		t.Fatalf("callback = %+v", e)
	}
}

func TestN04DivestCommitsBeforeObserver(t *testing.T) {
	l := &n04Log{}
	m, _ := newMgr(t, Options{EventLog: l})
	ctx := context.Background()
	m.RegisterInitialOwnership("fed", 1, 100, []core.AttributeHandle{1})
	if err := m.Acquire(ctx, "fed", 2, 100, []core.AttributeHandle{1}, nil); err != nil {
		t.Fatal(err)
	}
	l.onDivest = func() {
		if got, _ := m.QueryOwnership("fed", 100, 1); got != 2 {
			t.Errorf("observer saw owner %d before commit", got)
		}
		if err := m.CancelAcquire(ctx, "fed", 2, 100, []core.AttributeHandle{1}); !errors.Is(err, core.ErrFederateOwnsAttributes) {
			t.Errorf("cancel after commit = %v", err)
		}
		// A subsequent operation must never be overwritten by the earlier one.
		m.RegisterInitialOwnership("fed", 3, 100, []core.AttributeHandle{1})
	}
	if err := m.DivestIfWanted(ctx, "fed", 1, 100, []core.AttributeHandle{1}); err != nil {
		t.Fatal(err)
	}
	if got, _ := m.QueryOwnership("fed", 100, 1); got != 3 {
		t.Fatalf("later owner overwritten: %d", got)
	}
}

func TestN04DivestResultIsOperationNotCurrentState(t *testing.T) {
	l := &n04Log{}
	m, _ := newMgr(t, Options{EventLog: l})
	r, ok := any(m).(n04Result)
	if !ok {
		t.Fatal("missing atomic divest result")
	}
	ctx := context.Background()
	m.RegisterInitialOwnership("fed", 1, 100, []core.AttributeHandle{1, 2})
	if err := m.Acquire(ctx, "fed", 2, 100, []core.AttributeHandle{1}, nil); err != nil {
		t.Fatal(err)
	}
	l.onDivest = func() {
		if err := m.UnconditionalDivest(ctx, "fed", 2, 100, []core.AttributeHandle{1}); err != nil {
			t.Fatal(err)
		}
		if err := m.Acquire(ctx, "fed", 1, 100, []core.AttributeHandle{1}, nil); err != nil {
			t.Fatal(err)
		}
	}
	got, err := r.DivestIfWantedWithResult(ctx, "fed", 1, 100, []core.AttributeHandle{2, 1, 1})
	if err != nil || !reflect.DeepEqual(got, []core.AttributeHandle{1}) {
		t.Fatalf("result = %v, %v", got, err)
	}
}

func TestN04CancelledCandidateNotTransferred(t *testing.T) {
	m, _ := newMgr(t, Options{})
	ctx := context.Background()
	m.RegisterInitialOwnership("fed", 1, 100, []core.AttributeHandle{1})
	for _, h := range []core.FederateHandle{2, 3} {
		if err := m.Acquire(ctx, "fed", h, 100, []core.AttributeHandle{1}, nil); err != nil {
			t.Fatal(err)
		}
	}
	if err := m.CancelAcquire(ctx, "fed", 2, 100, []core.AttributeHandle{1}); err != nil {
		t.Fatal(err)
	}
	if err := m.DivestIfWanted(ctx, "fed", 1, 100, []core.AttributeHandle{1}); err != nil {
		t.Fatal(err)
	}
	if got, _ := m.QueryOwnership("fed", 100, 1); got != 3 {
		t.Fatalf("owner = %d", got)
	}
}

type n04BarrierStrategy struct {
	defaultNegotiation
	selected chan struct{}
	commit   chan struct{}
}

func (s n04BarrierStrategy) SelectAcquirer(ctx SelectAcquirerContext) core.FederateHandle {
	winner := s.defaultNegotiation.SelectAcquirer(ctx)
	if ctx.Phase == PhaseDivestIfWanted {
		close(s.selected)
		<-s.commit
	}
	return winner
}

func TestN04SelectionCannotRaceCancellationOrCompetingDivest(t *testing.T) {
	for _, operation := range []string{"cancel", "competing-divest"} {
		t.Run(operation, func(t *testing.T) {
			strategy := n04BarrierStrategy{selected: make(chan struct{}), commit: make(chan struct{})}
			m, _ := newMgr(t, Options{Strategy: strategy})
			ctx := context.Background()
			attrs := []core.AttributeHandle{1}
			m.RegisterInitialOwnership("fed", 1, 100, attrs)
			if err := m.Acquire(ctx, "fed", 2, 100, attrs, nil); err != nil {
				t.Fatal(err)
			}
			first := make(chan error, 1)
			go func() { first <- m.DivestIfWanted(ctx, "fed", 1, 100, attrs) }()
			<-strategy.selected
			started := make(chan struct{})
			second := make(chan error, 1)
			go func() {
				close(started)
				if operation == "cancel" {
					second <- m.CancelAcquire(ctx, "fed", 2, 100, attrs)
				} else {
					second <- m.UnconditionalDivest(ctx, "fed", 1, 100, attrs)
				}
			}()
			<-started
			close(strategy.commit)
			if err := <-first; err != nil {
				t.Fatal(err)
			}
			want := core.ErrFederateOwnsAttributes
			if operation != "cancel" {
				want = core.ErrAttributeNotOwned
			}
			if err := <-second; !errors.Is(err, want) {
				t.Fatalf("competing operation = %v, want %v", err, want)
			}
			if got, _ := m.QueryOwnership("fed", 100, 1); got != 2 {
				t.Fatalf("owner = %d", got)
			}
		})
	}
}

func TestN04QueuedTagsStayPerAttribute(t *testing.T) {
	m, out := newMgr(t, Options{})
	ctx := context.Background()
	m.RegisterInitialOwnership("fed", 1, 100, []core.AttributeHandle{1, 2})
	for _, attr := range []core.AttributeHandle{1, 2} {
		tag := []byte{byte(attr), 0, 0x80, 0xff}
		if err := m.Acquire(ctx, "fed", 2, 100, []core.AttributeHandle{attr}, tag); err != nil {
			t.Fatal(err)
		}
		tag[0] = 99
	}
	if err := m.DivestIfWanted(ctx, "fed", 1, 100, []core.AttributeHandle{1, 2}); err != nil {
		t.Fatal(err)
	}
	var count int
	for _, sent := range out.Sent() {
		e := sent.Event.(*CallbackEvent)
		if e.Kind == CallbackAcquisitionNotification || e.Kind == CallbackReleaseRequest {
			count++
			if !bytes.Equal(e.Tag, []byte{byte(e.Attributes[0]), 0, 0x80, 0xff}) {
				t.Fatalf("callback = %+v", e)
			}
		}
	}
	if count != 4 {
		t.Fatalf("tagged callbacks = %d", count)
	}
}
