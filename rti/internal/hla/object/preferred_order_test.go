package object

import (
	"context"
	"errors"
	"fmt"
	"math"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

type preferredOrderMode struct {
	mode  core.Mode
	known bool
}

func (m preferredOrderMode) ModeFor(core.FederationName) (core.Mode, bool) {
	return m.mode, m.known
}

type preferredOrderTable struct {
	attributes  map[core.AttributeHandle]Order
	interaction Order
}

func (o preferredOrderTable) AttributeOrder(_ core.FederationName, _ core.ObjectClassHandle, attr core.AttributeHandle) (Order, bool) {
	order, ok := o.attributes[attr]
	return order, ok
}

func (o preferredOrderTable) InteractionOrder(core.FederationName, core.InteractionClassHandle) (Order, bool) {
	return o.interaction, true
}

// Use a distinct privilege handle so deletion cannot accidentally use the
// first updated attribute's order or the object's registration order.
type preferredOrderFOM struct{ roFOM }

func (f *preferredOrderFOM) LookupAttribute(cls core.ObjectClassHandle, name string) (core.AttributeHandle, bool) {
	if cls == 1 && name == "HLAprivilegeToDeleteObject" {
		return 2, true
	}
	return f.roFOM.LookupAttribute(cls, name)
}

type preferredOrderRepository struct{ stubFOMRepo }

func (*preferredOrderRepository) Get(context.Context, core.FederationName) (core.FOMHandle, error) {
	return &preferredOrderFOM{}, nil
}

func preferredOrderSend(t *testing.T, r *Registry, obj core.ObjectHandle, kind string) func(*core.LogicalTime, uint64) error {
	t.Helper()
	ctx := context.Background()
	var regions []core.DDMRegionHandleCore
	if kind == testRegional {
		_, region := roRegions(t, r)
		regions = []core.DDMRegionHandleCore{region(1, 0, 10)}
	}
	return func(ts *core.LogicalTime, id uint64) error {
		options := core.ObjectMessageOptions{Tag: []byte{0, 255}, RetractionHandle: id}
		switch kind {
		case "reflect":
			return r.UpdateAttributesWithMetadata(ctx, "fed", 1, obj, map[core.AttributeHandle][]byte{1: {7}}, ts, options)
		case testReflectWire:
			return r.UpdateAttributesWire(ctx, "fed", 1, obj, map[uint64][]byte{1: {7}}, ts)
		case testInteraction:
			return r.SendInteractionWithMetadata(ctx, "fed", 1, 7, map[core.ParameterHandle][]byte{1: {7}}, ts, options)
		case testInteractionWire:
			return r.SendInteractionWire(ctx, "fed", 1, 7, map[uint64][]byte{1: {7}}, ts)
		case testRegional:
			return r.SendInteractionWithRegionsMetadata(ctx, "fed", 1, 7, map[core.ParameterHandle][]byte{1: {7}}, ts, regions, options)
		case testRemove:
			return r.DeleteWithMetadata(ctx, "fed", 1, obj, ts, options)
		default:
			panic(kind)
		}
	}
}

func TestPreferredOrderProfilesAndOverrides(t *testing.T) {
	profiles := []struct {
		name   string
		lookup FederationModeLookup
	}{
		{"verbose", preferredOrderMode{core.ModeVerbose, true}},
		{"best-effort", preferredOrderMode{core.ModeBestEffort, true}},
		{"unspecified", preferredOrderMode{core.ModeUnspecified, true}},
		{"unknown", preferredOrderMode{core.ModeVerbose, false}},
		{"no-mode-lookup", nil},
	}
	cases := []struct {
		name     string
		fom      Order
		override core.OrderType
		want     core.OrderType
	}{
		{"FOM-receive", OrderReceive, core.OrderUnspecified, core.OrderReceive},
		{"FOM-timestamp", OrderTimeStamp, core.OrderUnspecified, core.OrderTimestamp},
		{"override-timestamp", OrderReceive, core.OrderTimestamp, core.OrderTimestamp},
		{"override-receive", OrderTimeStamp, core.OrderReceive, core.OrderReceive},
	}
	for _, profile := range profiles {
		for _, kind := range []string{"reflect", testReflectWire, testInteraction, testInteractionWire, testRegional, testRemove} {
			for _, tc := range cases {
				t.Run(profile.name+"/"+kind+"/"+tc.name, func(t *testing.T) {
					ctx := context.Background()
					r, tm, out, gate, obj := roFixture(t, 2, 3)
					r.opts.FOMs = &preferredOrderRepository{}
					r.opts.Federations = profile.lookup
					r.opts.Orders = preferredOrderTable{map[core.AttributeHandle]Order{1: tc.fom, 2: tc.fom}, tc.fom}
					if kind == testRemove {
						// Deletion must consult attribute 2, not attribute 1.
						r.opts.Orders = preferredOrderTable{map[core.AttributeHandle]Order{1: Order(1 - tc.fom), 2: tc.fom}, tc.fom}
					}
					if tc.override != core.OrderUnspecified {
						if err := r.ChangeAttributeOrderType(ctx, "fed", 1, obj, []core.AttributeHandle{1, 2}, tc.override); err != nil {
							t.Fatal(err)
						}
						if err := r.ChangeInteractionOrderType(ctx, "fed", 1, 7, tc.override); err != nil {
							t.Fatal(err)
						}
					}
					if err := tm.EnableRegulation(ctx, "fed", 1, 1); err != nil {
						t.Fatal(err)
					}
					if err := tm.EnableConstrained(ctx, "fed", 2); err != nil {
						t.Fatal(err)
					}
					if err := tm.EnableAsynchronousDelivery(ctx, "fed", 2); err != nil {
						t.Fatal(err)
					}
					send := preferredOrderSend(t, r, obj, kind)
					out.records = nil
					admission := &recordingRetractionAdmission{}
					r.opts.Retractions = admission
					log := &recordingEventLog{}
					r.opts.EventLog = log
					ts := core.LogicalTime(10)
					if err := send(&ts, 901); err != nil {
						t.Fatal(err)
					}
					wantID := uint64(0)
					if tc.want == core.OrderTimestamp && kind != testReflectWire && kind != testInteractionWire {
						wantID = 901
					}
					if admission.calls != int(wantID/901) {
						t.Errorf("origin reservations=%d, sent order=%v", admission.calls, tc.want)
					}
					if gate.calls != 1 {
						t.Fatalf("fanout reservations=%d, want one", gate.calls)
					}
					callbacks := 0
					for _, d := range gate.deliveries {
						e := d.Event.(*CallbackEvent)
						if e.Kind == CallbackDeleteMarker {
							if tc.want != core.OrderTimestamp {
								t.Error("RO deletion created a timed lifecycle marker")
							}
							continue
						}
						callbacks++
						wantReceived := core.OrderReceive
						if d.Recipient == 2 && tc.want == core.OrderTimestamp {
							wantReceived = core.OrderTimestamp
						}
						if d.ReceiveOrder != (tc.want == core.OrderReceive) || d.RetractionHandle != wantID || e.Metadata.SentOrder != tc.want || e.Metadata.ReceivedOrder != wantReceived || e.Metadata.RetractionHandle != wantID {
							t.Errorf("delivery=%+v metadata=%+v, want sent=%v received=%v ID=%d", d, e.Metadata, tc.want, wantReceived, wantID)
						}
						if e.LogicalTime == nil || *e.LogicalTime != ts {
							t.Errorf("original timestamp lost: %+v", e)
						}
						if kind != testReflectWire && kind != testInteractionWire && string(e.UserSuppliedTag) != string([]byte{0, 255}) {
							t.Error("tag lost")
						}
						if kind == testRegional && len(e.Metadata.Regions) != 1 {
							t.Error("regional metadata lost")
						}
					}
					if callbacks != 2 {
						t.Errorf("recipient callbacks=%d, want two", callbacks)
					}
					wantImmediate := 2
					if tc.want == core.OrderTimestamp {
						wantImmediate = 1
					}
					if len(out.records) != wantImmediate {
						t.Errorf("immediate callbacks=%d, want %d", len(out.records), wantImmediate)
					}
					if records := log.Records(); len(records) != 1 || records[0].Event.(*eventRecord).LogicalTime == nil || *records[0].Event.(*eventRecord).LogicalTime != ts {
						t.Errorf("journal lost original timestamp: %v", records)
					}
				})
			}
		}
	}
}

func TestPreferredOrderMixedPasselsAtomicRetry(t *testing.T) {
	for _, wire := range []bool{false, true} {
		for _, phase := range []string{"none", testReserve, testJournal, testCommit} {
			t.Run(fmt.Sprintf("wire=%t/%s", wire, phase), func(t *testing.T) {
				ctx := context.Background()
				r, tm, out, gate, obj := roFixture(t, 2, 3)
				r.opts.Orders = preferredOrderTable{map[core.AttributeHandle]Order{1: OrderReceive, 2: OrderTimeStamp}, OrderReceive}
				if err := tm.EnableRegulation(ctx, "fed", 1, 1); err != nil {
					t.Fatal(err)
				}
				if err := tm.EnableConstrained(ctx, "fed", 2); err != nil {
					t.Fatal(err)
				}
				out.records = nil
				admission := &recordingRetractionAdmission{}
				r.opts.Retractions = admission
				log := &recordingEventLog{}
				r.opts.EventLog = log
				failure := errors.New("preferred-order fanout failure")
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
					if wire {
						return r.UpdateAttributesWire(ctx, "fed", 1, obj, map[uint64][]byte{1: {1}, 2: {2}}, &ts)
					}
					return r.UpdateAttributesWithMetadata(ctx, "fed", 1, obj, map[core.AttributeHandle][]byte{1: {1}, 2: {2}}, &ts, core.ObjectMessageOptions{RetractionHandle: 902})
				}
				if phase != "none" {
					if err := send(); !errors.Is(err, failure) {
						t.Fatalf("failure=%v", err)
					}
					if len(out.records) != 0 || len(admission.pending) != 0 || len(admission.accepted) != 0 {
						t.Fatal("failed fanout leaked callbacks or originating ID")
					}
					out.reserveErr = nil
					if err := tm.EnableAsynchronousDelivery(ctx, "fed", 2); err != nil {
						t.Fatal(err)
					}
					if err := tm.DisableConstrained(ctx, "fed", 2); err != nil {
						t.Fatal(err)
					}
					if len(out.records) != 0 {
						t.Fatal("failed fanout left queued RO/TSO callbacks")
					}
					if err := tm.EnableConstrained(ctx, "fed", 2); err != nil {
						t.Fatal(err)
					}
					if err := tm.DisableAsynchronousDelivery(ctx, "fed", 2); err != nil {
						t.Fatal(err)
					}
					out.records = nil
				}
				before := gate.calls
				if err := send(); err != nil {
					t.Fatal(err)
				}
				if gate.calls != before+1 || len(gate.deliveries) != 4 {
					t.Fatalf("mixed passels did not share a reservation: %+v", gate.deliveries)
				}
				for _, d := range gate.deliveries {
					e := d.Event.(*CallbackEvent)
					attr := uint64(2)
					wantOrder, wantID := core.OrderTimestamp, uint64(902)
					if d.ReceiveOrder {
						attr, wantOrder, wantID = 1, core.OrderReceive, 0
					}
					if wire {
						wantID = 0
					}
					if len(e.Attributes) != 1 || len(e.Attributes[attr]) != 1 || e.Attributes[attr][0] != byte(attr) || e.Metadata.SentOrder != wantOrder || e.Metadata.RetractionHandle != wantID || d.RetractionHandle != wantID || e.LogicalTime == nil || *e.LogicalTime != ts {
						t.Fatalf("incorrect passel: %+v metadata=%+v", e, e.Metadata)
					}
				}
				if len(out.records) != 2 {
					t.Fatalf("immediate passels=%d, want two for unconstrained receiver", len(out.records))
				}
				if err := tm.EnableAsynchronousDelivery(ctx, "fed", 2); err != nil {
					t.Fatal(err)
				}
				if len(out.records) != 3 {
					t.Fatalf("RO release count=%d, want three", len(out.records))
				}
				if err := tm.DisableRegulation(ctx, "fed", 1); err != nil {
					t.Fatal(err)
				}
				if err := tm.TimeAdvanceRequest(ctx, "fed", 2, ts); err != nil {
					t.Fatal(err)
				}
				if len(out.records) != 5 {
					t.Fatalf("TSO release and grant count=%d, want five", len(out.records))
				}
				e := out.records[3].Event.(*CallbackEvent)
				if e.Metadata.SentOrder != core.OrderTimestamp || e.Metadata.ReceivedOrder != core.OrderTimestamp || len(e.Attributes[2]) != 1 {
					t.Fatalf("TSO release=%+v", e)
				}
			})
		}
	}
}

func TestPreferredOrderUntimestampedAndUnknownDefaults(t *testing.T) {
	r, _, _ := newTestRegistry(t, nil)
	inst := &objectInstance{handle: 1, cls: 1}
	ts := core.LogicalTime(10)
	for _, orders := range []AttributeOrderLookup{nil, preferredOrderTable{nil, OrderTimeStamp}} {
		r.opts.Orders = orders
		if r.attributeDeliveryTime("fed", inst, 99, &ts) != &ts || r.interactionDeliveryTime("fed", 1, 7, &ts) != &ts {
			t.Fatal("missing order declaration did not retain timestamp default")
		}
		if r.attributeDeliveryTime("fed", inst, 99, nil) != nil || r.interactionDeliveryTime("fed", 1, 7, nil) != nil {
			t.Fatal("timestamp default invented an absent timestamp")
		}
	}
}

func TestPreferredOrderOnlyROSkipsTSOBound(t *testing.T) {
	for _, kind := range []string{"reflect", testReflectWire, testInteraction, testInteractionWire, testRegional, testRemove} {
		for _, override := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/override=%t", kind, override), func(t *testing.T) {
				ctx := context.Background()
				r, tm, out, gate, obj := roFixture(t, 2)
				r.opts.FOMs = &preferredOrderRepository{}
				order := OrderReceive
				if override {
					order = OrderTimeStamp
					if err := r.ChangeAttributeOrderType(ctx, "fed", 1, obj, []core.AttributeHandle{1, 2}, core.OrderReceive); err != nil {
						t.Fatal(err)
					}
					if err := r.ChangeInteractionOrderType(ctx, "fed", 1, 7, core.OrderReceive); err != nil {
						t.Fatal(err)
					}
				}
				r.opts.Orders = preferredOrderTable{map[core.AttributeHandle]Order{1: order, 2: order}, order}
				if err := tm.EnableRegulation(ctx, "fed", 1, 20); err != nil {
					t.Fatal(err)
				}
				send := preferredOrderSend(t, r, obj, kind)
				out.records = nil
				ts := core.LogicalTime(10)
				if err := send(&ts, 903); err != nil {
					t.Fatalf("RO timestamp incorrectly subjected to TSO bound: %v", err)
				}
				if gate.calls != 1 || len(gate.deliveries) != 1 || !gate.deliveries[0].ReceiveOrder || len(out.records) != 1 {
					t.Fatalf("RO delivery=%+v callbacks=%v", gate.deliveries, out.records)
				}
				e := out.records[0].Event.(*CallbackEvent)
				if e.LogicalTime == nil || *e.LogicalTime != ts || e.Metadata.SentOrder != core.OrderReceive || e.Metadata.ReceivedOrder != core.OrderReceive || e.Metadata.RetractionHandle != 0 {
					t.Fatalf("RO timestamp/metadata=%+v", e)
				}
			})
		}
	}
}

func TestPreferredOrderMixedUnsubscribedTSOStillValidates(t *testing.T) {
	for _, wire := range []bool{false, true} {
		t.Run(fmt.Sprintf("wire=%t", wire), func(t *testing.T) {
			ctx := context.Background()
			r, tm, out, gate, obj := roFixture(t)
			r.opts.Orders = preferredOrderTable{map[core.AttributeHandle]Order{1: OrderReceive, 2: OrderTimeStamp}, OrderReceive}
			if err := tm.EnableRegulation(ctx, "fed", 1, 20); err != nil {
				t.Fatal(err)
			}
			out.records = nil
			log := &recordingEventLog{}
			r.opts.EventLog = log
			ts := core.LogicalTime(10)
			var err error
			if wire {
				err = r.UpdateAttributesWire(ctx, "fed", 1, obj, map[uint64][]byte{1: {1}, 2: {2}}, &ts)
			} else {
				err = r.UpdateAttributesWithMetadata(ctx, "fed", 1, obj, map[core.AttributeHandle][]byte{1: {1}, 2: {2}}, &ts, core.ObjectMessageOptions{RetractionHandle: 904})
			}
			if !errors.Is(err, core.ErrTimeInvalidLogicalTime) {
				t.Fatalf("unsubscribed TSO attribute bypassed bound: %v", err)
			}
			if gate.calls != 0 || len(out.records) != 0 || len(log.Records()) != 0 {
				t.Fatal("invalid TSO send had effects")
			}
		})
	}
}

func TestPreferredOrderRORejectsMalformedOriginalTime(t *testing.T) {
	for _, kind := range []string{"reflect", testReflectWire, testInteraction, testInteractionWire, testRegional, testRemove} {
		for _, value := range []float64{math.NaN(), -1, math.Inf(-1)} {
			for _, regulating := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%v/regulating=%t", kind, value, regulating), func(t *testing.T) {
					ctx := context.Background()
					r, tm, out, gate, obj := roFixture(t, 2)
					r.opts.FOMs = &preferredOrderRepository{}
					r.opts.Orders = preferredOrderTable{map[core.AttributeHandle]Order{1: OrderReceive, 2: OrderReceive}, OrderReceive}
					if regulating {
						if err := tm.EnableRegulation(ctx, "fed", 1, 20); err != nil {
							t.Fatal(err)
						}
					}
					if err := tm.EnableConstrained(ctx, "fed", 2); err != nil {
						t.Fatal(err)
					}
					send := preferredOrderSend(t, r, obj, kind)
					out.records = nil
					log := &recordingEventLog{}
					r.opts.EventLog = log
					ts := core.LogicalTime(value)
					if err := send(&ts, 0); !errors.Is(err, core.ErrTimeInvalidLogicalTime) {
						t.Fatalf("malformed RO timestamp accepted: %v", err)
					}
					state := tm.Metrics("fed", 2)
					if gate.calls != 0 || len(out.records) != 0 || len(log.Records()) != 0 || state.ROQueued != 0 || state.TSOQueued != 0 {
						t.Fatal("rejected original timestamp changed delivery or journal state")
					}
					// A valid RO timestamp below the TSO floor remains acceptable.
					ts = 10
					if err := send(&ts, 0); err != nil {
						t.Fatalf("valid RO control after rejection: %v", err)
					}
					if tm.Metrics("fed", 2).ROQueued != 1 || len(log.Records()) != 1 {
						t.Fatal("valid RO control did not queue exactly once")
					}
				})
			}
		}
	}
}
