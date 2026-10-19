package object

import (
	"context"
	"errors"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	timepkg "github.com/cbchoi/gorti/rti/internal/hla/time"
)

type roObservedGate struct {
	*timepkg.Manager
	calls           int
	deliveries      []core.MessageDelivery
	beforeTimestamp func()
}

type roFOM struct{ stubFOMHandle }

func (*roFOM) AttributeName(cls core.ObjectClassHandle, attr core.AttributeHandle) (string, bool) {
	if cls == 9 && attr == 101 {
		return testValueName, true
	}
	if cls == 1 && attr == 1 {
		return testValueName, true
	}
	return testOther, cls == 1 && attr == 2
}

func (*roFOM) InteractionClassName(cls core.InteractionClassHandle) (string, bool) {
	if cls == 6 {
		return "event", true
	}
	return "event.child", cls == 7
}

func (*roFOM) ObjectClassAncestors(cls core.ObjectClassHandle) []core.ObjectClassHandle {
	if cls == 1 {
		return []core.ObjectClassHandle{9}
	}
	return nil
}

func (*roFOM) InteractionClassAncestors(cls core.InteractionClassHandle) []core.InteractionClassHandle {
	if cls == 7 {
		return []core.InteractionClassHandle{6}
	}
	return nil
}

func (f *roFOM) LookupAttribute(cls core.ObjectClassHandle, name string) (core.AttributeHandle, bool) {
	if cls == 9 && name == testValueName {
		return 101, true
	}
	if cls == 1 && name == testValueName {
		return 1, true
	}
	return 2, cls == 1 && name == testOther
}

type roFOMRepository struct{ stubFOMRepo }

func (*roFOMRepository) Get(context.Context, core.FederationName) (core.FOMHandle, error) {
	return &roFOM{}, nil
}

func (g *roObservedGate) ReserveTSO(fed core.FederationName, deliveries []core.TSOBufferedDelivery) core.TSOBufferReservation {
	g.calls++
	g.deliveries = append([]core.MessageDelivery(nil), deliveries...)
	return g.Manager.ReserveTSO(fed, deliveries)
}

func (g *roObservedGate) ReserveOutgoingTSO(fed core.FederationName, sender core.FederateHandle, timestamp core.LogicalTime, deliveries []core.TSOBufferedDelivery) (core.TSOBufferReservation, error) {
	g.calls++
	g.deliveries = append([]core.MessageDelivery(nil), deliveries...)
	if g.beforeTimestamp != nil {
		g.beforeTimestamp()
	}
	return g.Manager.ReserveOutgoingTSO(fed, sender, timestamp, deliveries)
}

func roFixture(t *testing.T, recipients ...core.FederateHandle) (*Registry, *timepkg.Manager, *atomicUpdateOutbox, *roObservedGate, core.ObjectHandle) {
	t.Helper()
	ctx := context.Background()
	r, decl, _ := newTestRegistry(t, nil)
	r.opts.FOMs = &roFOMRepository{}
	if err := decl.PublishObjectClassAttributes(ctx, "fed", 1, 1, []core.AttributeHandle{1, 2}); err != nil {
		t.Fatal(err)
	}
	if err := decl.PublishInteractionClass(ctx, "fed", 1, 7); err != nil {
		t.Fatal(err)
	}
	for _, h := range recipients {
		if err := decl.SubscribeObjectClassAttributes(ctx, "fed", h, 1, []core.AttributeHandle{1, 2}); err != nil {
			t.Fatal(err)
		}
		if err := decl.SubscribeInteractionClass(ctx, "fed", h, 7); err != nil {
			t.Fatal(err)
		}
	}
	reserveFixtureName(t, r, "fed", 1, "ro-routing")
	obj, _, err := r.Register(ctx, "fed", 1, 1, "ro-routing")
	if err != nil {
		t.Fatal(err)
	}
	out := &atomicUpdateOutbox{}
	tm, err := timepkg.New(timepkg.Options{Clock: r.opts.Clock, Outbox: out})
	if err != nil {
		t.Fatal(err)
	}
	gate := &roObservedGate{Manager: tm}
	r.opts.Outbox, r.opts.TSOGate, r.opts.TSOValidator, r.opts.Retractions = out, gate, tm, tm
	tm.SetDeletionLifecycle(r)
	return r, tm, out, gate, obj
}

func roSend(r *Registry, obj core.ObjectHandle, kind string, timestamp *core.LogicalTime) error {
	ctx := context.Background()
	switch kind {
	case testInteraction:
		return r.SendInteractionWithMetadata(ctx, "fed", 1, 7, map[core.ParameterHandle][]byte{1: {7}}, timestamp, core.ObjectMessageOptions{Tag: []byte{0, 255}})
	case testInteractionWire:
		return r.SendInteractionWire(ctx, "fed", 1, 7, map[uint64][]byte{1: {7}}, timestamp)
	case "reflect":
		return r.UpdateAttributesWithMetadata(ctx, "fed", 1, obj, map[core.AttributeHandle][]byte{1: {7}}, timestamp, core.ObjectMessageOptions{Tag: []byte{0, 255}})
	case testReflectWire:
		return r.UpdateAttributesWire(ctx, "fed", 1, obj, map[uint64][]byte{1: {7}}, timestamp)
	case testRemove:
		return r.DeleteWithMetadata(ctx, "fed", 1, obj, timestamp, core.ObjectMessageOptions{Tag: []byte{0, 255}})
	default:
		panic(kind)
	}
}

func TestROObjectMessagesWaitAndRelease(t *testing.T) {
	for _, kind := range []string{testInteraction, testInteractionWire, "reflect", testReflectWire, testRemove} {
		for _, release := range []string{"async", testAdvance, "unconstrain"} {
			t.Run(kind+"/"+release, func(t *testing.T) {
				ctx := context.Background()
				r, tm, out, gate, obj := roFixture(t, 2)
				if err := tm.EnableConstrained(ctx, "fed", 2); err != nil {
					t.Fatal(err)
				}
				out.records = nil
				if err := roSend(r, obj, kind, nil); err != nil {
					t.Fatal(err)
				}
				if gate.calls != 1 || len(gate.deliveries) != 1 {
					t.Fatalf("RO admission calls=%d deliveries=%+v", gate.calls, gate.deliveries)
				}
				d := gate.deliveries[0]
				if !d.ReceiveOrder || d.Timestamp != 0 || d.RetractionHandle != 0 || d.Sender != 1 {
					t.Fatalf("RO admission=%+v", d)
				}
				if len(out.records) != 0 {
					t.Fatalf("idle constrained recipient received %v", out.records)
				}
				var err error
				switch release {
				case "async":
					err = tm.EnableAsynchronousDelivery(ctx, "fed", 2)
				case testAdvance:
					err = tm.TimeAdvanceRequest(ctx, "fed", 2, 1)
				case "unconstrain":
					err = tm.DisableConstrained(ctx, "fed", 2)
				}
				if err != nil {
					t.Fatal(err)
				}
				want := 1
				if release == testAdvance {
					want++
				}
				if len(out.records) != want {
					t.Fatalf("released callbacks=%+v, want %d", out.records, want)
				}
				event, ok := out.records[0].Event.(*CallbackEvent)
				if !ok || event.LogicalTime != nil || event.Metadata == nil || event.Metadata.ReceivedOrder != core.OrderReceive || event.Metadata.SentOrder != core.OrderReceive || event.Metadata.Producer != 1 {
					t.Fatalf("released RO event=%+v", out.records[0].Event)
				}
				if release == testAdvance {
					if _, ok := out.records[1].Event.(*timepkg.TimeAdvanceGrant); !ok {
						t.Fatalf("RO did not precede grant: %v", out.records)
					}
				}
				if kind == testRemove && r.stateFor("fed").instances[obj] != nil {
					t.Fatal("accepted RO deletion retained the authoritative object")
				}
			})
		}
	}
}

func TestROOriginalTimestampSurvivesOrderChange(t *testing.T) {
	for _, kind := range []string{testInteraction, testInteractionWire, "reflect", testReflectWire} {
		t.Run(kind, func(t *testing.T) {
			ctx := context.Background()
			r, tm, out, gate, obj := roFixture(t, 2)
			if err := r.ChangeInteractionOrderType(ctx, "fed", 1, 7, core.OrderReceive); err != nil {
				t.Fatal(err)
			}
			if err := r.ChangeAttributeOrderType(ctx, "fed", 1, obj, []core.AttributeHandle{1}, core.OrderReceive); err != nil {
				t.Fatal(err)
			}
			if err := tm.EnableConstrained(ctx, "fed", 2); err != nil {
				t.Fatal(err)
			}
			if err := tm.EnableAsynchronousDelivery(ctx, "fed", 2); err != nil {
				t.Fatal(err)
			}
			out.records = nil
			ts := core.LogicalTime(10)
			if err := roSend(r, obj, kind, &ts); err != nil {
				t.Fatal(err)
			}
			if gate.calls != 1 || len(gate.deliveries) != 1 {
				t.Fatalf("RO reservation=%+v", gate.deliveries)
			}
			d := gate.deliveries[0]
			if !d.ReceiveOrder || d.Timestamp != 0 || d.RetractionHandle != 0 {
				t.Fatalf("timestamp used as RO ordering key: %+v", d)
			}
			if len(out.records) != 1 {
				t.Fatalf("callbacks=%v", out.records)
			}
			event := out.records[0].Event.(*CallbackEvent)
			if event.LogicalTime == nil || *event.LogicalTime != ts {
				t.Fatalf("original timestamp lost: %+v", event)
			}
			if event.Metadata.SentOrder != core.OrderReceive || event.Metadata.ReceivedOrder != core.OrderReceive || event.Metadata.RetractionHandle != 0 {
				t.Fatalf("RO metadata=%+v", event.Metadata)
			}
		})
	}
}

func TestROMixedRecipientsAbortAtomically(t *testing.T) {
	for _, kind := range []string{testInteraction, testInteractionWire, "reflect", testReflectWire, testRemove} {
		for _, failure := range []string{testReserve, testCommit, testJournal} {
			t.Run(kind+"/"+failure, func(t *testing.T) {
				ctx := context.Background()
				r, tm, out, _, obj := roFixture(t, 2, 3)
				if err := tm.EnableConstrained(ctx, "fed", 2); err != nil {
					t.Fatal(err)
				}
				out.records = nil
				injected := errors.New("RO fanout failure")
				switch failure {
				case testReserve:
					out.reserveErr = injected
				case testCommit:
					out.commitErr = injected
				case testJournal:
					r.opts.EventLog = &recordingEventLog{failNext: injected}
				}
				if err := roSend(r, obj, kind, nil); !errors.Is(err, injected) {
					t.Fatalf("send failure=%v", err)
				}
				if len(out.records) != 0 {
					t.Fatal("failed fanout delivered a recipient")
				}
				out.reserveErr, out.commitErr = nil, nil
				if err := tm.EnableAsynchronousDelivery(ctx, "fed", 2); err != nil {
					t.Fatal(err)
				}
				if len(out.records) != 0 {
					t.Fatal("failed fanout left a buffered RO callback")
				}
				if err := tm.DisableAsynchronousDelivery(ctx, "fed", 2); err != nil {
					t.Fatal(err)
				}
				if err := roSend(r, obj, kind, nil); err != nil {
					t.Fatalf("retry=%v", err)
				}
				if len(out.records) != 1 || out.records[0].Federate != 3 {
					t.Fatalf("retry immediate recipients=%v", out.records)
				}
				if err := tm.EnableAsynchronousDelivery(ctx, "fed", 2); err != nil {
					t.Fatal(err)
				}
				if len(out.records) != 2 || out.records[1].Federate != 2 {
					t.Fatalf("retry buffered recipients=%v", out.records)
				}
			})
		}
	}
}

func TestROMixedAttributeOrdersUseOneReservation(t *testing.T) {
	ctx := context.Background()
	r, tm, out, gate, obj := roFixture(t, 2)
	if err := r.ChangeAttributeOrderType(ctx, "fed", 1, obj, []core.AttributeHandle{1}, core.OrderReceive); err != nil {
		t.Fatal(err)
	}
	if err := tm.EnableConstrained(ctx, "fed", 2); err != nil {
		t.Fatal(err)
	}
	out.records = nil
	ts := core.LogicalTime(10)
	if err := r.UpdateAttributes(ctx, "fed", 1, obj, map[core.AttributeHandle][]byte{1: {1}, 2: {2}}, &ts); err != nil {
		t.Fatal(err)
	}
	if gate.calls != 1 || len(gate.deliveries) != 2 {
		t.Fatalf("mixed admission calls=%d deliveries=%v", gate.calls, gate.deliveries)
	}
	if len(out.records) != 0 {
		t.Fatalf("idle mixed callbacks=%v", out.records)
	}
	if err := tm.EnableAsynchronousDelivery(ctx, "fed", 2); err != nil {
		t.Fatal(err)
	}
	if len(out.records) != 1 {
		t.Fatalf("async callbacks=%v", out.records)
	}
	ro := out.records[0].Event.(*CallbackEvent)
	if len(ro.Attributes) != 1 || ro.Attributes[1] == nil || ro.LogicalTime == nil || *ro.LogicalTime != ts || ro.Metadata.ReceivedOrder != core.OrderReceive {
		t.Fatalf("RO partition=%+v", ro)
	}
	if err := tm.TimeAdvanceRequest(ctx, "fed", 2, ts); err != nil {
		t.Fatal(err)
	}
	if len(out.records) != 3 {
		t.Fatalf("advance callbacks=%v", out.records)
	}
	tso := out.records[1].Event.(*CallbackEvent)
	if len(tso.Attributes) != 1 || tso.Attributes[2] == nil || tso.Metadata.ReceivedOrder != core.OrderTimestamp {
		t.Fatalf("TSO partition=%+v", tso)
	}
	if _, ok := out.records[2].Event.(*timepkg.TimeAdvanceGrant); !ok {
		t.Fatalf("last callback=%T", out.records[2].Event)
	}
}

func TestROTimestampExemptAndTSORevalidatedAtReservation(t *testing.T) {
	for _, kind := range []string{testInteraction, testInteractionWire, "reflect", testReflectWire} {
		for _, tc := range []struct {
			name      string
			order     core.OrderType
			lookahead core.LogicalTime
		}{
			{"RO-below-bound", core.OrderReceive, 20},
			{"TSO-stale-bound", core.OrderTimestamp, 1},
		} {
			t.Run(kind+"/"+tc.name, func(t *testing.T) {
				ctx := context.Background()
				r, tm, out, gate, obj := roFixture(t, 2)
				if err := r.ChangeInteractionOrderType(ctx, "fed", 1, 7, tc.order); err != nil {
					t.Fatal(err)
				}
				if err := r.ChangeAttributeOrderType(ctx, "fed", 1, obj, []core.AttributeHandle{1}, tc.order); err != nil {
					t.Fatal(err)
				}
				if err := tm.EnableRegulation(ctx, "fed", 1, tc.lookahead); err != nil {
					t.Fatal(err)
				}
				out.records = nil
				log := &recordingEventLog{}
				r.opts.EventLog = log
				revalidated := false
				gate.beforeTimestamp = func() {
					revalidated = true
					if err := tm.ModifyLookahead(ctx, "fed", 1, 20); err != nil {
						t.Fatal(err)
					}
				}
				ts := core.LogicalTime(10)
				err := roSend(r, obj, kind, &ts)
				if tc.order == core.OrderTimestamp {
					// The first check accepts 10 >= 1; admission must reject
					// after the lookahead increases to 20.
					if !errors.Is(err, core.ErrTimeInvalidLogicalTime) {
						t.Fatalf("stale TSO timestamp accepted: %v", err)
					}
					if !revalidated || gate.calls != 1 || len(out.records) != 0 || len(log.Records()) != 0 {
						t.Fatalf("TSO admission revalidated=%t calls=%d callbacks=%v journal=%v", revalidated, gate.calls, out.records, log.Records())
					}
					return
				}
				if err != nil {
					t.Fatalf("RO timestamp subjected to TSO bound: %v", err)
				}
				if revalidated || gate.calls != 1 || len(gate.deliveries) != 1 || len(out.records) != 1 || len(log.Records()) != 1 {
					t.Fatalf("RO admission revalidated=%t calls=%d deliveries=%v callbacks=%v journal=%v", revalidated, gate.calls, gate.deliveries, out.records, log.Records())
				}
				d := gate.deliveries[0]
				if !d.ReceiveOrder || d.Timestamp != 0 || d.RetractionHandle != 0 {
					t.Fatalf("RO timestamp used as ordering key: %+v", d)
				}
				e := out.records[0].Event.(*CallbackEvent)
				if e.LogicalTime == nil || *e.LogicalTime != ts || e.Metadata == nil || e.Metadata.SentOrder != core.OrderReceive || e.Metadata.ReceivedOrder != core.OrderReceive || e.Metadata.RetractionHandle != 0 {
					t.Fatalf("RO callback lost timestamp or metadata: %+v", e)
				}
			})
		}
	}
}

func TestROPendingAdvanceAllowsMessageWithoutEarlyGrant(t *testing.T) {
	ctx := context.Background()
	r, tm, out, _, obj := roFixture(t, 2)
	if err := tm.EnableRegulation(ctx, "fed", 1, 1); err != nil {
		t.Fatal(err)
	}
	if err := tm.EnableConstrained(ctx, "fed", 2); err != nil {
		t.Fatal(err)
	}
	out.records = nil
	if err := tm.TimeAdvanceRequest(ctx, "fed", 2, 10); err != nil {
		t.Fatal(err)
	}
	if len(out.records) != 0 {
		t.Fatal("peer regulation should withhold the grant")
	}
	if err := roSend(r, obj, testInteraction, nil); err != nil {
		t.Fatal(err)
	}
	if len(out.records) != 1 {
		t.Fatalf("pending advance RO callbacks=%v", out.records)
	}
	event, ok := out.records[0].Event.(*CallbackEvent)
	if !ok || event.Kind != CallbackReceive || event.Metadata.ReceivedOrder != core.OrderReceive {
		t.Fatalf("pending advance callback=%+v", out.records[0].Event)
	}
}

func TestROGateDoesNotHoldDiscovery(t *testing.T) {
	ctx := context.Background()
	r, tm, out, gate, _ := roFixture(t, 2)
	if err := tm.EnableConstrained(ctx, "fed", 2); err != nil {
		t.Fatal(err)
	}
	out.records = nil
	reserveFixtureName(t, r, "fed", 1, "discovery-is-control")
	if _, _, err := r.Register(ctx, "fed", 1, 1, "discovery-is-control"); err != nil {
		t.Fatal(err)
	}
	if gate.calls != 0 || len(out.records) != 1 {
		t.Fatalf("discovery admission calls=%d callbacks=%v", gate.calls, out.records)
	}
	if event, ok := out.records[0].Event.(*CallbackEvent); !ok || event.Kind != CallbackDiscover {
		t.Fatalf("discovery callback=%T", out.records[0].Event)
	}
}

func TestROMixedOrdersFanoutFailureLeavesNeitherQueue(t *testing.T) {
	ctx := context.Background()
	r, tm, out, gate, obj := roFixture(t, 2, 3)
	if err := r.ChangeAttributeOrderType(ctx, "fed", 1, obj, []core.AttributeHandle{1}, core.OrderReceive); err != nil {
		t.Fatal(err)
	}
	if err := tm.EnableConstrained(ctx, "fed", 2); err != nil {
		t.Fatal(err)
	}
	out.records = nil
	failure := errors.New("mixed RO/TSO admission refused")
	out.reserveErr = failure
	ts := core.LogicalTime(10)
	if err := r.UpdateAttributes(ctx, "fed", 1, obj, map[core.AttributeHandle][]byte{1: {1}, 2: {2}}, &ts); !errors.Is(err, failure) {
		t.Fatalf("send=%v", err)
	}
	if gate.calls != 1 || len(gate.deliveries) != 4 {
		t.Fatalf("mixed fanout reservation calls=%d entries=%d", gate.calls, len(gate.deliveries))
	}
	out.reserveErr = nil
	if err := tm.EnableAsynchronousDelivery(ctx, "fed", 2); err != nil {
		t.Fatal(err)
	}
	if len(out.records) != 0 {
		t.Fatal("failed mixed fanout retained RO work")
	}
	if err := tm.TimeAdvanceRequest(ctx, "fed", 2, ts); err != nil {
		t.Fatal(err)
	}
	if len(out.records) != 1 {
		t.Fatalf("failed mixed fanout retained TSO work: %v", out.records)
	}
	if _, ok := out.records[0].Event.(*timepkg.TimeAdvanceGrant); !ok {
		t.Fatalf("callback=%T", out.records[0].Event)
	}
}
