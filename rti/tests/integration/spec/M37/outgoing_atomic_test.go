package m37spec

import (
	"context"
	"errors"
	"sync"
	"testing"
	stdtime "time"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/ddm"
	"github.com/cbchoi/gorti/rti/internal/hla/object"
	hlatime "github.com/cbchoi/gorti/rti/internal/hla/time"
)

type pausedOutgoingValidator struct {
	manager           *hlatime.Manager
	validated, resume chan struct{}
	once              sync.Once
}

func initializeOutgoingRegion(t *testing.T, regions *ddm.Manager, owner core.FederateHandle, region core.DDMRegionHandleCore, dim core.DDMDimensionHandle) {
	t.Helper()
	if err := regions.SetRangeBounds("f", owner, region, dim, ddm.Range{Upper: ^uint64(0)}); err != nil {
		t.Fatal(err)
	}
	if err := regions.CommitRegionModifications(context.Background(), "f", owner, []core.DDMRegionHandleCore{region}); err != nil {
		t.Fatal(err)
	}
}

func (v *pausedOutgoingValidator) ValidateOutgoingTSO(f core.FederationName, h core.FederateHandle, ts core.LogicalTime) error {
	err := v.manager.ValidateOutgoingTSO(f, h, ts)
	v.once.Do(func() { close(v.validated); <-v.resume })
	return err
}

type outgoingAdmissionLog struct {
	mu sync.Mutex
	n  int
}

func (l *outgoingAdmissionLog) Append(context.Context, core.FederationName, core.EventRecord) error {
	l.mu.Lock()
	l.n++
	l.mu.Unlock()
	return nil
}
func (*outgoingAdmissionLog) Sync(context.Context, core.FederationName) error { return nil }
func (*outgoingAdmissionLog) OpenReader(context.Context, string) (core.EventLogReader, error) {
	return nil, errors.New("not used")
}
func (l *outgoingAdmissionLog) count() int { l.mu.Lock(); defer l.mu.Unlock(); return l.n }

type outgoingAdmissionFOM struct{ stubFOMHandle }

func (*outgoingAdmissionFOM) AttributeName(cls core.ObjectClassHandle, attr core.AttributeHandle) (string, bool) {
	return "attribute", cls == 1 && attr == 1
}

func (*outgoingAdmissionFOM) InteractionClassName(cls core.InteractionClassHandle) (string, bool) {
	return "interaction", cls == 1
}

type outgoingAdmissionFOMRepo struct{ stubFOMRepo }

func (*outgoingAdmissionFOMRepo) Get(context.Context, core.FederationName) (core.FOMHandle, error) {
	return &outgoingAdmissionFOM{}, nil
}

func TestOutgoingAdmissionDistinguishesTSOValidationFromRO(t *testing.T) {
	for _, operation := range []struct {
		name, kind   string
		receiveOrder bool
		retraction   uint64
	}{
		{name: "interaction", kind: "interaction"},
		{name: "wire-interaction", kind: "wire-interaction"},
		{name: updateServiceName, kind: updateServiceName},
		{name: wireUpdateServiceName, kind: wireUpdateServiceName},
		{name: "delete", kind: "delete"},
		{name: "ro-interaction", kind: "interaction", receiveOrder: true},
		{name: "ro-wire-interaction", kind: "wire-interaction", receiveOrder: true},
		{name: "ro-update", kind: updateServiceName, receiveOrder: true},
		{name: "ro-wire-update", kind: wireUpdateServiceName, receiveOrder: true},
		{name: "scoped-interaction", kind: "scoped-interaction"},
		{name: "ro-scoped-interaction", kind: "scoped-interaction", receiveOrder: true},
		{name: "retractable-interaction", kind: "interaction", retraction: 42},
		{name: "retractable-update", kind: updateServiceName, retraction: 42},
		{name: "retractable-delete", kind: "delete", retraction: 42},
		{name: "retractable-scoped-interaction", kind: "scoped-interaction", retraction: 42},
	} {
		for _, subscribed := range []bool{false, true} {
			for _, mutation := range []string{"lookahead", "advance"} {
				name := operation.name + "/" + map[bool]string{false: "no-recipient", true: "recipient"}[subscribed] + "/" + mutation
				t.Run(name, func(t *testing.T) {
					ctx := context.Background()
					m, timeOut := newTimeManager(t)
					v := &pausedOutgoingValidator{manager: m, validated: make(chan struct{}), resume: make(chan struct{})}
					var release sync.Once
					defer release.Do(func() { close(v.resume) })
					journal := &outgoingAdmissionLog{}
					var regions *ddm.Manager
					r, decl, out := newRegistry(t, func(o *object.Options) {
						o.TSOGate, o.TSOValidator, o.EventLog = m, v, journal
						o.FOMs = &outgoingAdmissionFOMRepo{}
						if operation.kind == "scoped-interaction" {
							var err error
							regions, err = ddm.New(ddm.Options{Outbox: o.Outbox, FOMs: o.FOMs})
							if err != nil {
								t.Fatal(err)
							}
							o.RegistrationRegions = regions
						}
					})
					for _, err := range []error{
						decl.PublishInteractionClass(ctx, "f", 1, 1),
						decl.PublishObjectClassAttributes(ctx, "f", 1, 1, []core.AttributeHandle{1}),
						m.EnableRegulation(ctx, "f", 1, 1),
						m.EnableRegulation(ctx, "f", 2, 10),
						m.EnableConstrained(ctx, "f", 2),
					} {
						if err != nil {
							t.Fatal(err)
						}
					}
					var sendRegions []core.DDMRegionHandleCore
					if regions != nil {
						space, ok := regions.LookupRoutingSpace("f", ddm.DefaultRoutingSpace)
						if !ok {
							t.Fatal("missing routing space")
						}
						dimension, ok := regions.LookupDimension("f", space, "dimension")
						if !ok {
							t.Fatal("missing routing dimension")
						}
						pub, err := regions.CreateRegion(ctx, "f", 1, space, []core.DDMDimensionHandle{dimension})
						if err != nil {
							t.Fatal(err)
						}
						sendRegions = []core.DDMRegionHandleCore{pub}
						initializeOutgoingRegion(t, regions, 1, pub, dimension)
						if subscribed {
							sub, err := regions.CreateRegion(ctx, "f", 2, space, []core.DDMDimensionHandle{dimension})
							if err != nil {
								t.Fatal(err)
							}
							initializeOutgoingRegion(t, regions, 2, sub, dimension)
							if err := regions.SubscribeInteractionClassWithRegions(ctx, "f", 2, 1, []core.DDMRegionHandleCore{sub}); err != nil {
								t.Fatal(err)
							}
						}
					}
					if subscribed {
						// Scoped cases must reach the receiver through DDM, not the default subscription.
						if regions == nil {
							if err := decl.SubscribeInteractionClass(ctx, "f", 2, 1); err != nil {
								t.Fatal(err)
							}
						}
						if err := decl.SubscribeObjectClassAttributes(ctx, "f", 2, 1, []core.AttributeHandle{1}); err != nil {
							t.Fatal(err)
						}
					}
					obj, _, err := r.Register(ctx, "f", 1, 1, "")
					if err != nil {
						t.Fatal(err)
					}
					if operation.receiveOrder {
						if operation.kind == updateServiceName || operation.kind == wireUpdateServiceName {
							err = r.ChangeAttributeOrderType(ctx, "f", 1, obj, []core.AttributeHandle{1}, core.OrderReceive)
						} else {
							err = r.ChangeInteractionOrderType(ctx, "f", 1, 1, core.OrderReceive)
						}
						if err != nil {
							t.Fatal(err)
						}
					}
					beforeLog, beforeEvents := journal.count(), len(out.snapshot())
					send := func(ts core.LogicalTime) error {
						switch operation.kind {
						case "interaction":
							if operation.retraction != 0 {
								return r.SendInteractionRetractable(ctx, "f", 1, 1, nil, &ts, operation.retraction)
							}
							return r.SendInteraction(ctx, "f", 1, 1, nil, &ts)
						case "wire-interaction":
							return r.SendInteractionWire(ctx, "f", 1, 1, nil, &ts)
						case "scoped-interaction":
							return r.SendInteractionWithRegionsMetadata(ctx, "f", 1, 1, nil, &ts, sendRegions, core.ObjectMessageOptions{RetractionHandle: operation.retraction})
						case updateServiceName:
							if operation.retraction != 0 {
								return r.UpdateAttributesRetractable(ctx, "f", 1, obj, map[core.AttributeHandle][]byte{1: {1}}, &ts, operation.retraction)
							}
							return r.UpdateAttributes(ctx, "f", 1, obj, map[core.AttributeHandle][]byte{1: {1}}, &ts)
						case wireUpdateServiceName:
							return r.UpdateAttributesWire(ctx, "f", 1, obj, map[uint64][]byte{1: {1}}, &ts)
						default:
							if operation.retraction != 0 {
								return r.DeleteWithMetadata(ctx, "f", 1, obj, &ts, core.ObjectMessageOptions{RetractionHandle: operation.retraction})
							}
							return r.Delete(ctx, "f", 1, obj, &ts, nil)
						}
					}
					if err := m.ValidateOutgoingTSO("f", 1, 1); err != nil {
						t.Fatalf("initial timestamp must be valid: %v", err)
					}
					done := make(chan error, 1)
					if !operation.receiveOrder {
						go func() { done <- send(1) }()
						select {
						case <-v.validated:
						case <-stdtime.After(3 * stdtime.Second):
							t.Fatal("validation did not arrive")
						}
					}
					if mutation == "lookahead" {
						err = m.ModifyLookahead(ctx, "f", 1, 10)
					} else {
						err = m.TimeAdvanceRequest(ctx, "f", 1, 5)
					}
					if err != nil {
						t.Fatal(err)
					}
					if err := m.TimeAdvanceRequest(ctx, "f", 2, 5); err != nil {
						t.Fatal(err)
					}
					if got := m.Metrics("f", 2).CurrentTime; got != 5 {
						t.Fatalf("receiver did not advance: %v", got)
					}
					beforeTimeEvents := len(timeOut.snapshot())
					release.Do(func() { close(v.resume) })
					if !operation.receiveOrder {
						select {
						case err := <-done:
							if !errors.Is(err, core.ErrTimeInvalidLogicalTime) {
								t.Fatalf("stale validation admitted: %v", err)
							}
						case <-stdtime.After(3 * stdtime.Second):
							t.Fatal("send did not return")
						}
						if journal.count() != beforeLog || len(out.snapshot()) != beforeEvents || len(timeOut.snapshot()) != beforeTimeEvents {
							t.Fatal("rejected send produced journal/callback effects")
						}
						if queued := m.Metrics("f", 2).TSOQueued; queued != 0 {
							t.Fatalf("rejected send left %d queued callbacks", queued)
						}
						if queued := m.Metrics("f", 2).ROQueued; queued != 0 {
							t.Fatalf("rejected send left %d queued RO callbacks", queued)
						}
						if operation.retraction != 0 {
							history, err := m.ExportRetractionHistory("f")
							if err != nil {
								t.Fatalf("rejection left a pending retraction ID: %v", err)
							}
							if !history.Complete || len(history.Producers) != 0 {
								t.Fatalf("rejection consumed a retraction ID: %+v", history)
							}
						}
					}
					// RO retains its timestamp but is not subject to TSO's time floor.
					acceptedTime := core.LogicalTime(20)
					if operation.receiveOrder {
						acceptedTime = 1
					}
					if err := send(acceptedTime); err != nil {
						t.Fatalf("valid control with the same retraction ID after rejection: %v", err)
					}
					if got := journal.count(); got != beforeLog+1 {
						t.Fatalf("successful retry journal count = %d, want %d", got, beforeLog+1)
					}
					if operation.retraction != 0 {
						reservation, err := m.ReserveRetractionID(ctx, "f", 1, operation.retraction)
						if reservation != nil {
							reservation.Release()
						}
						if !errors.Is(err, core.ErrRetractionIDAlreadyUsed) {
							t.Fatalf("successful retry did not consume retraction ID: %v", err)
						}
					}
					wantQueued := uint64(0)
					if subscribed && !operation.receiveOrder {
						wantQueued = 1
					}
					if got := m.Metrics("f", 2).TSOQueued; got != wantQueued {
						t.Fatalf("valid retry queued %d callbacks, want %d", got, wantQueued)
					}
					if !operation.receiveOrder {
						if err := m.ModifyLookahead(ctx, "f", 1, 30); err != nil {
							t.Fatal(err)
						}
						if err := m.TimeAdvanceRequest(ctx, "f", 2, 20); err != nil {
							t.Fatal(err)
						}
						if got := m.Metrics("f", 2).CurrentTime; got != 20 {
							t.Fatalf("receiver did not advance to retry timestamp: %v", got)
						}
					} else {
						wantRO := uint64(0)
						if subscribed {
							wantRO = 1
						}
						if got := m.Metrics("f", 2).ROQueued; got != wantRO {
							t.Fatalf("idle receiver queued %d RO callbacks, want %d", got, wantRO)
						}
						immediate := out.snapshot()[beforeEvents:]
						immediate = append(immediate, timeOut.snapshot()[beforeTimeEvents:]...)
						wantScope := 0
						if subscribed && (operation.kind == updateServiceName || operation.kind == wireUpdateServiceName) {
							wantScope = 1
						}
						if len(immediate) != wantScope {
							t.Fatalf("idle receiver callbacks=%d, want %d scope advisories only", len(immediate), wantScope)
						}
						for _, delivery := range immediate {
							event, ok := delivery.evt.(*object.CallbackEvent)
							if !ok || delivery.h != 2 || event.Kind != object.CallbackAttributesInScope || event.Object != obj || event.LogicalTime != nil || len(event.AttributeHandles) != 1 || event.AttributeHandles[0] != 1 {
								t.Fatalf("idle constrained receiver got a non-scope callback before async enable: %+v", delivery)
							}
						}
						if err := m.EnableAsynchronousDelivery(ctx, "f", 2); err != nil {
							t.Fatal(err)
						}
						if got := m.Metrics("f", 2).ROQueued; got != 0 {
							t.Fatalf("async release retained %d RO callbacks", got)
						}
					}
					wantKind := object.CallbackReceive
					if operation.kind == updateServiceName || operation.kind == wireUpdateServiceName {
						wantKind = object.CallbackReflect
					} else if operation.kind == "delete" {
						wantKind = object.CallbackRemove
					}
					deliveries := out.snapshot()[beforeEvents:]
					deliveries = append(deliveries, timeOut.snapshot()[beforeTimeEvents:]...)
					received := 0
					for _, delivery := range deliveries {
						event, ok := delivery.evt.(*object.CallbackEvent)
						if !ok || delivery.h != 2 || event.Kind != wantKind {
							continue
						}
						received++
						wantOrder := core.OrderTimestamp
						if operation.receiveOrder {
							wantOrder = core.OrderReceive
						}
						if event.LogicalTime == nil || *event.LogicalTime != acceptedTime {
							t.Fatalf("retry callback has wrong timestamp: %+v", event)
						}
						if event.Metadata == nil || event.Metadata.SentOrder != wantOrder || event.Metadata.ReceivedOrder != wantOrder || event.Metadata.RetractionHandle != operation.retraction {
							t.Fatalf("retry callback metadata = %+v, want order %v and retraction %d", event.Metadata, wantOrder, operation.retraction)
						}
						if regions != nil && (len(event.Metadata.Regions) != 1 || event.Metadata.Regions[0] != sendRegions[0]) {
							t.Fatalf("scoped callback lost sending region: %+v", event.Metadata.Regions)
						}
					}
					wantReceived := 0
					if subscribed {
						wantReceived = 1
					}
					if received != wantReceived {
						t.Fatalf("retry delivered %d callbacks, want %d", received, wantReceived)
					}
				})
			}
		}
	}
}

const updateServiceName = "update"

const wireUpdateServiceName = "wire-update"
