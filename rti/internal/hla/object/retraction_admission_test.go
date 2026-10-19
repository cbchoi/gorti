package object

import (
	"context"
	"errors"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

type recordingRetractionAdmission struct {
	calls             int
	pending, accepted map[uint64]bool
}

func (a *recordingRetractionAdmission) ReserveRetractionID(_ context.Context, _ core.FederationName, _ core.FederateHandle, id uint64) (core.OutboxReservation, error) {
	a.calls++
	if a.accepted[id] || a.pending[id] {
		return nil, core.ErrRetractionIDAlreadyUsed
	}
	if a.pending == nil {
		a.pending, a.accepted = map[uint64]bool{}, map[uint64]bool{}
	}
	a.pending[id] = true
	return &recordingRetractionReservation{admission: a, id: id}, nil
}

type recordingRetractionReservation struct {
	admission *recordingRetractionAdmission
	id        uint64
	done      bool
}

type retractionAdmissionGate struct {
	*updateTSOGate
	*recordingRetractionAdmission
}

func (r *recordingRetractionReservation) Commit() error {
	if !r.done {
		r.admission.accepted[r.id] = true
		delete(r.admission.pending, r.id)
		r.done = true
	}
	return nil
}

func (r *recordingRetractionReservation) Release() {
	if !r.done {
		delete(r.admission.pending, r.id)
		r.done = true
	}
}

func TestRetractionAdmissionFailedFanoutRetry(t *testing.T) {
	for _, interaction := range []bool{false, true} {
		for _, phase := range []string{testReserve, testJournal, testCommit} {
			name := "update/" + phase
			if interaction {
				name = "interaction/" + phase
			}
			t.Run(name, func(t *testing.T) {
				ctx := context.Background()
				log := &recordingEventLog{}
				r, decl, _ := newTestRegistry(t, log)
				if err := decl.PublishObjectClassAttributes(ctx, "fed", 1, 1, []core.AttributeHandle{1}); err != nil {
					t.Fatal(err)
				}
				reserveFixtureName(t, r, "fed", 1, "retry-object")
				obj, _, err := r.Register(ctx, "fed", 1, 1, "retry-object")
				if err != nil {
					t.Fatal(err)
				}
				if err := decl.PublishInteractionClass(ctx, "fed", 1, 1); err != nil {
					t.Fatal(err)
				}
				for _, sub := range []core.FederateHandle{2, 3} {
					if err := decl.SubscribeObjectClassAttributes(ctx, "fed", sub, 1, []core.AttributeHandle{1}); err != nil {
						t.Fatal(err)
					}
					if err := decl.SubscribeInteractionClass(ctx, "fed", sub, 1); err != nil {
						t.Fatal(err)
					}
				}
				out := &atomicUpdateOutbox{}
				admission := &recordingRetractionAdmission{}
				r.opts.Outbox, r.opts.Retractions = out, admission
				failure := errors.New("injected admission failure")
				switch phase {
				case testReserve:
					out.reserveErr = failure
				case testJournal:
					log.failNext = failure
				case testCommit:
					out.commitErr = failure
				}
				ts := core.LogicalTime(10)
				send := func() error {
					if interaction {
						return r.sendInteraction(ctx, "fed", 1, 1, nil, &ts, 17)
					}
					return r.updateAttributes(ctx, "fed", 1, obj, map[core.AttributeHandle][]byte{1: {7}}, &ts, 17)
				}
				if err := send(); !errors.Is(err, failure) {
					t.Fatalf("failed send=%v", err)
				}
				if admission.calls != 1 || len(admission.pending) != 0 || len(admission.accepted) != 0 || len(out.records) != 0 {
					t.Fatalf("failed origin leaked state: calls=%d pending=%v accepted=%v recipients=%d", admission.calls, admission.pending, admission.accepted, len(out.records))
				}
				out.reserveErr = nil
				if err := send(); err != nil {
					t.Fatalf("retry=%v", err)
				}
				if admission.calls != 2 || len(admission.pending) != 0 || !admission.accepted[17] {
					t.Fatalf("retry did not commit one origin: %+v", admission)
				}
				counts := map[core.FederateHandle]int{}
				for _, row := range out.records {
					if e := row.Event.(*CallbackEvent); e.Kind == CallbackReflect || e.Kind == CallbackReceive {
						counts[row.Federate]++
					}
				}
				if counts[2] != 1 || counts[3] != 1 {
					t.Fatalf("retry recipient counts=%v", counts)
				}
				beforeSeq := r.ExportState("fed").LastOutboundSequence
				beforeCallbacks, beforeJournal := len(out.records), len(log.Records())
				if err := send(); !errors.Is(err, core.ErrRetractionIDAlreadyUsed) {
					t.Fatalf("duplicate=%v", err)
				}
				if len(out.records) != beforeCallbacks || len(log.Records()) != beforeJournal || r.ExportState("fed").LastOutboundSequence != beforeSeq {
					t.Fatal("duplicate admission caused callback, journal, or sequence effects")
				}
			})
		}
	}
}

func TestRetractionAdmissionMissingCapabilityAndReceiveOrder(t *testing.T) {
	r, decl, _ := newTestRegistry(t, nil)
	ctx := context.Background()
	if err := decl.PublishInteractionClass(ctx, "fed", 1, 1); err != nil {
		t.Fatal(err)
	}
	ts := core.LogicalTime(10)
	if err := r.sendInteraction(ctx, "fed", 1, 1, nil, &ts, 0); err != nil {
		t.Fatalf("zero ID=%v", err)
	}
	if err := r.sendInteraction(ctx, "fed", 1, 1, nil, nil, 9); err != nil {
		t.Fatalf("receive-order ID=%v", err)
	}
	if err := r.sendInteraction(ctx, "fed", 1, 1, nil, &ts, 9); !errors.Is(err, core.ErrRetractionHistoryUnavailable) {
		t.Fatalf("missing admission capability=%v", err)
	}
	st := r.stateFor("fed")
	st.interactionOrders = map[core.FederateHandle]map[core.InteractionClassHandle]core.OrderType{1: {1: core.OrderReceive}}
	if err := r.sendInteraction(ctx, "fed", 1, 1, nil, &ts, 9); err != nil {
		t.Fatalf("effective receive-order ID=%v", err)
	}
}

func TestRetractionAdmissionUnsubscribedTimestampAttribute(t *testing.T) {
	ctx := context.Background()
	r, decl, _ := newTestRegistry(t, nil)
	if err := decl.PublishObjectClassAttributes(ctx, "fed", 1, 1, []core.AttributeHandle{1, 2}); err != nil {
		t.Fatal(err)
	}
	reserveFixtureName(t, r, "fed", 1, "mixed-retraction")
	obj, _, err := r.Register(ctx, "fed", 1, 1, "mixed-retraction")
	if err != nil {
		t.Fatal(err)
	}
	if err := decl.SubscribeObjectClassAttributes(ctx, "fed", 2, 1, []core.AttributeHandle{1}); err != nil {
		t.Fatal(err)
	}
	r.stateFor("fed").attributeOrders = map[core.ObjectHandle]map[core.AttributeHandle]core.OrderType{obj: {1: core.OrderReceive, 2: core.OrderTimestamp}}
	r.opts.DDM = &updateDDMFilter{}
	admission := &recordingRetractionAdmission{}
	r.opts.Retractions = admission
	ts := core.LogicalTime(10)
	if err := r.updateAttributes(ctx, "fed", 1, obj, map[core.AttributeHandle][]byte{1: {1}, 2: {2}}, &ts, 20); err != nil {
		t.Fatal(err)
	}
	if admission.calls != 1 || !admission.accepted[20] {
		t.Fatal("timestamped attribute with no recipients lost its originating ID")
	}
}

func TestRetractionAdmissionTimeGateFallback(t *testing.T) {
	r, decl, _ := newTestRegistry(t, nil)
	ctx := context.Background()
	if err := decl.PublishInteractionClass(ctx, "fed", 1, 1); err != nil {
		t.Fatal(err)
	}
	admission := &recordingRetractionAdmission{}
	r.opts.TSOGate = &retractionAdmissionGate{&updateTSOGate{}, admission}
	ts := core.LogicalTime(10)
	if err := r.sendInteraction(ctx, "fed", 1, 1, nil, &ts, 18); err != nil {
		t.Fatal(err)
	}
	if admission.calls != 1 || !admission.accepted[18] {
		t.Fatal("time gate admission capability was ignored")
	}
}
