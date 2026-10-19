package ownership

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

// IEEE 1516.1-2010 7.1.5, 7.2 and 7.4.3, supplied PDF pp. 160-163.
func TestIEEE2010UnconditionalProgressesSeparatePendingRequests(t *testing.T) {
	ctx := context.Background()
	m, out := newMgr(t, Options{})
	m.RegisterInitialOwnership("fed", 1, 10, []core.AttributeHandle{1, 2, 3})
	for _, attr := range []core.AttributeHandle{1, 2} {
		if err := m.Acquire(ctx, "fed", 2, 10, []core.AttributeHandle{attr}, []byte("same-tag-separate-request")); err != nil {
			t.Fatal(err)
		}
		if err := m.Acquire(ctx, "fed", 3, 10, []core.AttributeHandle{attr}, []byte{byte(attr), 0, 255}); err != nil {
			t.Fatal(err)
		}
	}
	if err := m.NegotiatedDivestTwoPhase(ctx, "fed", 1, 10, []core.AttributeHandle{1, 2}, nil); err != nil {
		t.Fatal(err)
	}
	before := len(out.Sent())
	if err := m.UnconditionalDivest(ctx, "fed", 1, 10, []core.AttributeHandle{3, 2, 1, 1}); err != nil {
		t.Fatal(err)
	}
	for _, attr := range []core.AttributeHandle{1, 2} {
		if !m.IsOwnedBy("fed", 2, 10, attr) {
			t.Errorf("attribute %d was not granted to the waiting acquirer", attr)
		}
	}
	if _, owned := m.QueryOwnership("fed", 10, 3); owned {
		t.Fatal("attribute without a waiting acquirer remains owned")
	}
	state := m.ExportState("fed")
	if len(state.PendingDivests) != 0 || len(state.PendingAcquires) != 2 {
		t.Fatalf("offers/waiters not advanced: %+v", state)
	}
	var acquired, requested []core.AttributeHandle
	for _, sent := range out.Sent()[before:] {
		e := sent.Event.(*CallbackEvent)
		if sent.Federate != 2 || len(e.Attributes) != 1 {
			t.Fatalf("callback mixed requests or targeted the former owner: %+v / %+v", sent, e)
		}
		switch e.Kind {
		case CallbackAcquisitionNotification:
			acquired = append(acquired, e.Attributes[0])
			if len(e.Tag) != 0 {
				t.Fatal("unconditional divestiture invented a confirmation tag")
			}
		case CallbackReleaseRequest:
			requested = append(requested, e.Attributes[0])
			if !bytes.Equal(e.Tag, []byte{byte(e.Attributes[0]), 0, 255}) {
				t.Fatalf("remaining request tag lost: %+v", e)
			}
		default:
			t.Fatalf("unexpected callback after unconditional divestiture: %+v", e)
		}
	}
	if !reflect.DeepEqual(acquired, []core.AttributeHandle{1, 2}) || !reflect.DeepEqual(requested, acquired) {
		t.Fatalf("acquisition/release notifications=%v/%v", acquired, requested)
	}
	if err := m.DivestIfWanted(ctx, "fed", 2, 10, []core.AttributeHandle{1, 2}); err != nil {
		t.Fatal(err)
	}
	if !m.IsOwnedBy("fed", 3, 10, 1) || !m.IsOwnedBy("fed", 3, 10, 2) {
		t.Fatal("remaining requests were lost during the first handoff")
	}
}

func TestIEEE2010UnconditionalMixedOwnershipDoesNotProgressAnyRequest(t *testing.T) {
	for _, attrs := range [][]core.AttributeHandle{{1, 2}, {2, 1}} {
		m, out := newMgr(t, Options{})
		ctx := context.Background()
		m.RegisterInitialOwnership("fed", 1, 10, []core.AttributeHandle{1})
		m.RegisterInitialOwnership("fed", 3, 10, []core.AttributeHandle{2})
		if err := m.Acquire(ctx, "fed", 2, 10, []core.AttributeHandle{1}, nil); err != nil {
			t.Fatal(err)
		}
		before, callbacks := m.ExportState("fed"), len(out.Sent())
		if err := m.UnconditionalDivest(ctx, "fed", 1, 10, attrs); !errors.Is(err, core.ErrAttributeNotOwned) {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(before, m.ExportState("fed")) || len(out.Sent()) != callbacks {
			t.Fatal("invalid mixed set progressed ownership or callbacks")
		}
	}
}

func TestIEEE2010AssumptionsValidateEachAttributeAndExcludePendingAcquirers(t *testing.T) {
	for _, unconditional := range []bool{false, true} {
		t.Run(map[bool]string{false: "negotiated", true: "unconditional"}[unconditional], func(t *testing.T) {
			ctx := context.Background()
			m, out := newMgr(t, Options{})
			m.RegisterInitialOwnership("fed", 1, 10, []core.AttributeHandle{1, 2})
			if !unconditional {
				if err := m.Acquire(ctx, "fed", 2, 10, []core.AttributeHandle{1}, nil); err != nil {
					t.Fatal(err)
				}
			}
			m.opts.Subscribers = func(_ context.Context, _ core.FederationName, _ core.ObjectHandle, attrs []core.AttributeHandle) []core.FederateHandle {
				if len(attrs) != 1 {
					t.Errorf("candidate lookup mixed attributes: %v", attrs)
				}
				return []core.FederateHandle{5, 4, 3, 2, 2, 1, 0, ^core.FederateHandle(0)}
			}
			m.opts.PrepareAcquisition = func(_ context.Context, _ core.FederationName, h core.FederateHandle, _ core.ObjectHandle, attrs []core.AttributeHandle) (func(), error) {
				if h == 5 {
					return nil, core.ErrObjectHandleInvalid
				}
				if len(attrs) != 1 || h == 4 || (h == 2 && attrs[0] != 1) || (h == 3 && attrs[0] != 2) {
					return nil, core.ErrAttributeNotPublished
				}
				return func() {}, nil
			}
			before := len(out.Sent())
			tag := []byte{9, 0, 255}
			var err error
			if unconditional {
				err = m.UnconditionalDivest(ctx, "fed", 1, 10, []core.AttributeHandle{1, 2})
				tag = nil
			} else {
				err = m.NegotiatedDivestTwoPhase(ctx, "fed", 1, 10, []core.AttributeHandle{1, 2}, tag)
			}
			if err != nil {
				t.Fatal(err)
			}
			got := map[core.FederateHandle][]core.AttributeHandle{}
			for _, sent := range out.Sent()[before:] {
				e := sent.Event.(*CallbackEvent)
				if e.Kind != CallbackAssumption {
					continue
				}
				if len(e.Attributes) != 1 || !bytes.Equal(e.Tag, tag) {
					t.Fatalf("incorrect assumption: %+v", e)
				}
				got[sent.Federate] = append(got[sent.Federate], e.Attributes...)
			}
			want := map[core.FederateHandle][]core.AttributeHandle{3: {2}}
			if unconditional {
				want[2] = []core.AttributeHandle{1}
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("assumptions=%v, want %v", got, want)
			}
		})
	}
}

type assumptionActionOutbox struct {
	out    core.Outbox
	action func(core.FederateHandle, *CallbackEvent)
}

func (o assumptionActionOutbox) Send(ctx context.Context, fed core.FederationName, h core.FederateHandle, event core.OutboundEvent) error {
	if err := o.out.Send(ctx, fed, h, event); err != nil {
		return err
	}
	if callback, ok := event.(*CallbackEvent); ok && callback.Kind == CallbackAssumption {
		o.action(h, callback)
	}
	return nil
}

func TestIEEE2010UnconditionalStopsAssumptionsAfterAcquisition(t *testing.T) {
	ctx := context.Background()
	m, out := newMgr(t, Options{Subscribers: func(context.Context, core.FederationName, core.ObjectHandle, []core.AttributeHandle) []core.FederateHandle {
		return []core.FederateHandle{2, 3}
	}})
	m.RegisterInitialOwnership("fed", 1, 10, []core.AttributeHandle{1})
	m.opts.Outbox = assumptionActionOutbox{out: out, action: func(h core.FederateHandle, e *CallbackEvent) {
		if err := m.Acquire(ctx, "fed", h, e.Object, e.Attributes, nil); err != nil {
			t.Fatal(err)
		}
	}}
	if err := m.UnconditionalDivest(ctx, "fed", 1, 10, []core.AttributeHandle{1}); err != nil {
		t.Fatal(err)
	}
	sent := out.Sent()
	if len(sent) != 2 || sent[0].Federate != 2 || sent[0].Event.(*CallbackEvent).Kind != CallbackAssumption || sent[1].Event.(*CallbackEvent).Kind != CallbackAcquisitionNotification {
		t.Fatalf("assumption fan-out did not stop at acquisition: %+v", sent)
	}
	if !m.IsOwnedBy("fed", 2, 10, 1) {
		t.Fatal("responding candidate did not acquire")
	}
}

func TestIEEE2010NegotiatedStopsAssumptionsAfterCancellation(t *testing.T) {
	ctx := context.Background()
	m, out := newMgr(t, Options{Subscribers: func(context.Context, core.FederationName, core.ObjectHandle, []core.AttributeHandle) []core.FederateHandle {
		return []core.FederateHandle{2, 3}
	}})
	m.RegisterInitialOwnership("fed", 1, 10, []core.AttributeHandle{1})
	m.opts.Outbox = assumptionActionOutbox{out: out, action: func(_ core.FederateHandle, e *CallbackEvent) {
		if err := m.CancelDivest(ctx, "fed", 1, e.Object, e.Attributes); err != nil {
			t.Fatal(err)
		}
	}}
	if err := m.NegotiatedDivestTwoPhase(ctx, "fed", 1, 10, []core.AttributeHandle{1}, []byte("offer")); err != nil {
		t.Fatal(err)
	}
	if sent := out.Sent(); len(sent) != 1 || sent[0].Federate != 2 {
		t.Fatalf("cancelled offer was solicited again: %+v", sent)
	}
	if !m.IsOwnedBy("fed", 1, 10, 1) {
		t.Fatal("cancelled offer lost ownership")
	}
}

type invalidUnconditionalStrategy struct{ defaultNegotiation }

func (s invalidUnconditionalStrategy) SelectAcquirer(c SelectAcquirerContext) core.FederateHandle {
	if c.Phase == PhaseUnconditionalDivest && c.Attribute == 2 {
		return 999
	}
	return s.defaultNegotiation.SelectAcquirer(c)
}

func TestIEEE2010UnconditionalRejectsNoncandidateWithoutPartialDivest(t *testing.T) {
	ctx := context.Background()
	m, out := newMgr(t, Options{Strategy: invalidUnconditionalStrategy{}})
	m.RegisterInitialOwnership("fed", 1, 10, []core.AttributeHandle{1, 2})
	if err := m.Acquire(ctx, "fed", 2, 10, []core.AttributeHandle{1, 2}, nil); err != nil {
		t.Fatal(err)
	}
	before, callbacks := m.ExportState("fed"), len(out.Sent())
	if err := m.UnconditionalDivest(ctx, "fed", 1, 10, []core.AttributeHandle{1, 2}); err == nil {
		t.Fatal("noncandidate was accepted")
	}
	if !reflect.DeepEqual(before, m.ExportState("fed")) || len(out.Sent()) != callbacks {
		t.Fatal("invalid strategy result partially divested the set")
	}
}
