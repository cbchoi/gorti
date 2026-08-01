package object

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/internal/core"
	"github.com/cbchoi/gorti/rti/internal/declaration"
	timepkg "github.com/cbchoi/gorti/rti/internal/time"
)

type strictInteractionFOM struct{}

func (*strictInteractionFOM) IsValid() bool { return true }
func (*strictInteractionFOM) LookupObjectClass(string) (core.ObjectClassHandle, bool) {
	return 0, false
}
func (*strictInteractionFOM) LookupInteractionClass(string) (core.InteractionClassHandle, bool) {
	return 7, true
}
func (*strictInteractionFOM) LookupAttribute(core.ObjectClassHandle, string) (core.AttributeHandle, bool) {
	return 0, false
}
func (*strictInteractionFOM) LookupParameter(core.InteractionClassHandle, string) (core.ParameterHandle, bool) {
	return 1, true
}
func (*strictInteractionFOM) ObjectClassName(core.ObjectClassHandle) (string, bool) {
	return "", false
}
func (*strictInteractionFOM) InteractionClassName(cls core.InteractionClassHandle) (string, bool) {
	return "Message", cls == 7
}
func (*strictInteractionFOM) AttributeName(core.ObjectClassHandle, core.AttributeHandle) (string, bool) {
	return "", false
}
func (*strictInteractionFOM) ParameterName(cls core.InteractionClassHandle, parameter core.ParameterHandle) (string, bool) {
	return "Payload", cls == 7 && parameter == 1
}
func (*strictInteractionFOM) LookupDimension(string) (core.DimensionHandle, bool) {
	return 0, false
}
func (*strictInteractionFOM) DimensionName(core.DimensionHandle) (string, bool) {
	return "", false
}
func (*strictInteractionFOM) DimensionUpperBound(core.DimensionHandle) (uint64, bool) {
	return 0, false
}

type strictInteractionFOMRepo struct{ handle core.FOMHandle }

func (r *strictInteractionFOMRepo) Load(context.Context, []core.FOMModule) (core.FOMHandle, error) {
	return r.handle, nil
}
func (r *strictInteractionFOMRepo) Get(context.Context, core.FederationName) (core.FOMHandle, error) {
	return r.handle, nil
}

type failingInteractionOutbox struct{ err error }

func (o *failingInteractionOutbox) Send(context.Context, core.FederationName, core.FederateHandle, core.OutboundEvent) error {
	return o.err
}

type atomicInteractionOutbox struct {
	reserveErr error
	records    []outboxRecord
}

func (o *atomicInteractionOutbox) Send(_ context.Context, fed core.FederationName, h core.FederateHandle, event core.OutboundEvent) error {
	o.records = append(o.records, outboxRecord{Federation: fed, Federate: h, Event: event})
	return nil
}

func (o *atomicInteractionOutbox) Reserve(_ context.Context, fed core.FederationName, deliveries []core.OutboxDelivery) (core.OutboxReservation, error) {
	if o.reserveErr != nil {
		return nil, o.reserveErr
	}
	return &atomicInteractionReservation{outbox: o, fed: fed, deliveries: append([]core.OutboxDelivery(nil), deliveries...)}, nil
}

type atomicInteractionReservation struct {
	outbox     *atomicInteractionOutbox
	fed        core.FederationName
	deliveries []core.OutboxDelivery
	done       bool
}

func (r *atomicInteractionReservation) Commit() error {
	if r.done {
		return nil
	}
	for _, delivery := range r.deliveries {
		r.outbox.records = append(r.outbox.records, outboxRecord{Federation: r.fed, Federate: delivery.Recipient, Event: delivery.Event})
	}
	r.done = true
	return nil
}

func (r *atomicInteractionReservation) Release() { r.done = true }

func newP0InteractionRegistry(t *testing.T, outbox core.Outbox) (*Registry, *declaration.Manager, *recordingEventLog) {
	t.Helper()
	decl := declaration.New()
	log := &recordingEventLog{}
	reg, err := New(Options{
		EventLog: log, Declarations: decl, Outbox: outbox,
		FOMs:  &strictInteractionFOMRepo{handle: &strictInteractionFOM{}},
		Clock: core.NewFakeClock(time.Unix(0, 0)),
	})
	if err != nil {
		t.Fatal(err)
	}
	return reg, decl, log
}

func TestP0SendInteractionRejectsInvalidHandlesBeforeAppend(t *testing.T) {
	outbox := &recordingOutbox{}
	reg, decl, log := newP0InteractionRegistry(t, outbox)
	ctx := context.Background()
	if err := decl.PublishInteractionClass(ctx, "fed", 1, 7); err != nil {
		t.Fatal(err)
	}

	if err := reg.SendInteraction(ctx, "fed", 1, 99, nil, nil); !errors.Is(err, core.ErrInteractionClassNotFound) {
		t.Fatalf("invalid class error = %v", err)
	}
	if err := reg.SendInteraction(ctx, "fed", 1, 7, map[core.ParameterHandle][]byte{9: {1}}, nil); !errors.Is(err, core.ErrInteractionParameterNotFound) {
		t.Fatalf("invalid parameter error = %v", err)
	}
	if got := len(log.Records()); got != 0 {
		t.Fatalf("event-log appends = %d, want 0", got)
	}
	if got := len(outbox.Records()); got != 0 {
		t.Fatalf("outbox records = %d, want 0", got)
	}
}

func TestP0SendInteractionPropagatesFanoutFailure(t *testing.T) {
	want := core.ErrFederateOverflow
	reg, decl, log := newP0InteractionRegistry(t, &failingInteractionOutbox{err: want})
	ctx := context.Background()
	if err := decl.PublishInteractionClass(ctx, "fed", 1, 7); err != nil {
		t.Fatal(err)
	}
	if err := decl.SubscribeInteractionClass(ctx, "fed", 2, 7); err != nil {
		t.Fatal(err)
	}
	delivered, sent := 0, 0
	reg.opts.OnInteractionDelivered = func(core.FederationName, core.FederateHandle) { delivered++ }
	reg.opts.OnInteractionSent = func(core.FederationName, core.FederateHandle) { sent++ }

	err := reg.SendInteraction(ctx, "fed", 1, 7, map[core.ParameterHandle][]byte{1: {1}}, nil)
	if !errors.Is(err, want) {
		t.Fatalf("fanout error = %v, want ErrFederateOverflow", err)
	}
	if got := len(log.Records()); got != 1 {
		t.Fatalf("event-log appends = %d, want committed write-ahead record", got)
	}
	if delivered != 0 || sent != 0 {
		t.Fatalf("accounting = delivered %d sent %d, want 0/0", delivered, sent)
	}
}

func TestP0SendInteractionDeepCopiesPayload(t *testing.T) {
	outbox := &recordingOutbox{}
	reg, decl, _ := newP0InteractionRegistry(t, outbox)
	ctx := context.Background()
	if err := decl.PublishInteractionClass(ctx, "fed", 1, 7); err != nil {
		t.Fatal(err)
	}
	if err := decl.SubscribeInteractionClass(ctx, "fed", 2, 7); err != nil {
		t.Fatal(err)
	}
	payload := []byte{1, 2, 3}
	if err := reg.SendInteraction(ctx, "fed", 1, 7, map[core.ParameterHandle][]byte{1: payload}, nil); err != nil {
		t.Fatal(err)
	}
	payload[0] = 9
	records := outbox.Records()
	received := records[0].Event.(*outboundEvent).pb.GetReceive().GetParameters()[1]
	if received[0] != 1 {
		t.Fatalf("owned payload changed to %v after caller mutation", received)
	}
}

func TestP0SendInteractionMixedFanoutAdmissionIsAtomic(t *testing.T) {
	outbox := &atomicInteractionOutbox{reserveErr: core.ErrFederateOverflow}
	timeManager, err := timepkg.New(timepkg.Options{Clock: core.NewFakeClock(time.Unix(0, 0)), Outbox: outbox})
	if err != nil {
		t.Fatal(err)
	}
	decl := declaration.New()
	log := &recordingEventLog{}
	reg, err := New(Options{
		EventLog: log, Declarations: decl, Outbox: outbox, TSOGate: timeManager,
		FOMs: &strictInteractionFOMRepo{handle: &strictInteractionFOM{}}, Clock: core.NewFakeClock(time.Unix(0, 0)),
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := decl.PublishInteractionClass(ctx, "fed", 1, 7); err != nil {
		t.Fatal(err)
	}
	for _, subscriber := range []core.FederateHandle{2, 3} {
		if err := decl.SubscribeInteractionClass(ctx, "fed", subscriber, 7); err != nil {
			t.Fatal(err)
		}
	}
	if err := timeManager.EnableRegulation(ctx, "fed", 2, 1); err != nil {
		t.Fatal(err)
	}
	timestamp := core.LogicalTime(5)
	err = reg.SendInteraction(ctx, "fed", 1, 7, map[core.ParameterHandle][]byte{1: {1}}, &timestamp)
	if !errors.Is(err, core.ErrFederateOverflow) {
		t.Fatalf("SendInteraction = %v, want ErrFederateOverflow", err)
	}
	if len(log.Records()) != 0 || len(outbox.records) != 0 {
		t.Fatalf("failed transaction side effects: WAL=%d outbox=%d", len(log.Records()), len(outbox.records))
	}
	if logicalTime, ok := timeManager.QueryLITS("fed", 2); ok {
		t.Fatalf("failed transaction retained TSO at %v", logicalTime)
	}
}
