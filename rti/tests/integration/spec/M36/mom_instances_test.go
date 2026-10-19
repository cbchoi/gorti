package m36spec

import (
	"context"
	"sync"
	"testing"
	"time"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/declaration"
	"github.com/cbchoi/gorti/rti/internal/hla/mom"
	"github.com/cbchoi/gorti/rti/internal/hla/object"
)

const fedName = core.FederationName("mom_federation_lifecycle")

func (o *fakeOutbox) Reserve(_ context.Context, fed core.FederationName, deliveries []core.OutboxDelivery) (core.OutboxReservation, error) {
	staged := make([]sentRecord, len(deliveries))
	for i, delivery := range deliveries {
		staged[i] = sentRecord{fed, delivery.Recipient, delivery.Event}
	}
	o.mu.Lock()
	return &momInstanceReservation{out: o, sent: staged}, nil
}

type momInstanceReservation struct {
	once sync.Once
	out  *fakeOutbox
	sent []sentRecord
}

func (r *momInstanceReservation) Commit() error {
	r.once.Do(func() { r.out.sent = append(r.out.sent, r.sent...); r.sent = nil; r.out.mu.Unlock() })
	return nil
}

func (r *momInstanceReservation) Release() {
	r.once.Do(func() { r.sent = nil; r.out.mu.Unlock() })
}

// newMOMStack composes the production declaration.Manager, object.Registry
// and mom.Manager exactly the way cmd/rtid does for the M36 instance
// fan-out (EnableInstanceFanout + SetOnSubscribeObjectClass).
func newMOMStack(t *testing.T) (*mom.Manager, *declaration.Manager, *fakeOutbox) {
	t.Helper()
	declMgr := declaration.New()
	outbox := newFakeOutbox()

	momMgr, err := mom.New(mom.Options{Outbox: outbox})
	if err != nil {
		t.Fatalf("mom.New: %v", err)
	}
	objReg, err := object.New(object.Options{
		Declarations: declMgr,
		Outbox:       outbox,
		FOMs:         momFOMRepo{},
		Clock:        core.NewManualClock(time.Unix(0, 0)),
	})
	if err != nil {
		t.Fatalf("object.New: %v", err)
	}
	momMgr.EnableInstanceFanout(objReg, declMgr, momFOMRepo{})
	declMgr.SetOnSubscribeObjectClass(momMgr.ObjectClassSubscribed)
	return momMgr, declMgr, outbox
}

// driveLifecycle replays the mom_federation_lifecycle fixture scenario up
// to the point where federate 1 ("observer") has joined and subscribed to
// HLAmanager.HLAfederate attributes [HLAfederateHandle, HLAfederateName].
func driveLifecycle(t *testing.T, momMgr *mom.Manager, declMgr *declaration.Manager) context.Context {
	t.Helper()
	ctx := context.Background()
	if err := momMgr.FederationCreated(ctx, fedName, nil); err != nil {
		t.Fatalf("FederationCreated: %v", err)
	}
	if err := momMgr.FederateJoined(ctx, fedName, 1, "observer", ""); err != nil {
		t.Fatalf("FederateJoined(observer): %v", err)
	}
	if err := declMgr.SubscribeObjectClassAttributes(
		ctx, fedName, 1, clsHLAfederate,
		[]core.AttributeHandle{attrFederateHandle, attrFederateName},
	); err != nil {
		t.Fatalf("SubscribeObjectClassAttributes: %v", err)
	}
	return ctx
}

// kindOf compresses a FederateEvent to a comparable label.
func kindOf(ev *rtiv1.FederateEvent) string {
	switch {
	case ev.GetDiscover() != nil:
		return "DISCOVER"
	case ev.GetReflect() != nil:
		return "REFLECT"
	case ev.GetRemove() != nil:
		return "REMOVE"
	case ev.GetAttributesInScope() != nil:
		return "SCOPE_IN"
	default:
		return "OTHER"
	}
}

// TestSpec_M36_LateSubscriber_ReceivesRetroactiveDiscoverReflect: a
// federate that subscribes to HLAmanager.HLAfederate AFTER its own join
// receives Discover + Reflect for the already-registered HLAfederate
// instance (its own). IEEE 1516.1-2010 §11 + §5.6/§6.9/§6.11.
func TestSpec_M36_LateSubscriber_ReceivesRetroactiveDiscoverReflect(t *testing.T) {
	momMgr, declMgr, outbox := newMOMStack(t)
	driveLifecycle(t, momMgr, declMgr)

	events := outbox.SentTo(1)
	if len(events) != 2 {
		t.Fatalf("observer events after late subscribe = %d, want 2 (Discover+Reflect); got %+v", len(events), events)
	}
	discover := events[0].GetDiscover()
	if discover == nil {
		t.Fatalf("first event = %s, want DISCOVER", kindOf(events[0]))
	}
	if discover.GetObjectClassHandle() != uint64(clsHLAfederate) {
		t.Errorf("discover class = %d, want %d (HLAfederate)", discover.GetObjectClassHandle(), clsHLAfederate)
	}
	if discover.GetObjectName() != "HLAfederate.1" {
		t.Errorf("discover name = %q, want %q", discover.GetObjectName(), "HLAfederate.1")
	}
	reflect := events[1].GetReflect()
	if reflect == nil {
		t.Fatalf("second event = %s, want REFLECT", kindOf(events[1]))
	}
	name, ok := decodeHLAunicodeString(reflect.GetAttributes()[uint64(attrFederateName)])
	if !ok || name != "observer" {
		t.Errorf("retro reflect HLAfederateName = %q (ok=%v), want %q", name, ok, "observer")
	}
}

// TestSpec_M36_FederateJoin_FansOutDiscoverAndReflect: after the observer
// subscribes, a second federate's join produces Discover + Reflect for
// the new HLAfederate instance via the STANDARD registry fan-out, with
// HLAfederateName HLAunicodeString-encoded (IEEE 1516.2 §4.13.9).
func TestSpec_M36_FederateJoin_FansOutDiscoverAndReflect(t *testing.T) {
	momMgr, declMgr, outbox := newMOMStack(t)
	ctx := driveLifecycle(t, momMgr, declMgr)

	before := len(outbox.SentTo(1))
	if err := momMgr.FederateJoined(ctx, fedName, 2, "alice", "test-type"); err != nil {
		t.Fatalf("FederateJoined(alice): %v", err)
	}
	events := outbox.SentTo(1)[before:]
	if len(events) != 3 {
		t.Fatalf("observer events for alice join = %d, want 3 (Discover+Scope+Reflect); kinds=%v", len(events), kinds(events))
	}
	discover := events[0].GetDiscover()
	if discover == nil {
		t.Fatalf("first event = %s, want DISCOVER", kindOf(events[0]))
	}
	if discover.GetObjectName() != "HLAfederate.2" {
		t.Errorf("discover name = %q, want %q", discover.GetObjectName(), "HLAfederate.2")
	}
	scope := events[1].GetAttributesInScope()
	if scope == nil || scope.GetObjectHandle() != discover.GetObjectHandle() || len(scope.GetAttributeHandles()) != 2 {
		t.Fatalf("scope callback for alice: %+v", scope)
	}
	if scope.GetAttributeHandles()[0] != uint64(attrFederateHandle) || scope.GetAttributeHandles()[1] != uint64(attrFederateName) {
		t.Fatalf("scope attributes for alice: %v", scope.GetAttributeHandles())
	}
	reflect := events[2].GetReflect()
	if reflect == nil {
		t.Fatalf("third event = %s, want REFLECT", kindOf(events[2]))
	}
	name, ok := decodeHLAunicodeString(reflect.GetAttributes()[uint64(attrFederateName)])
	if !ok || name != "alice" {
		t.Errorf("reflect HLAfederateName = %q (ok=%v), want %q", name, ok, "alice")
	}

	// The joining federate itself must not receive Discover for its own
	// instance's registration burst (it is not subscribed at all here).
	if got := outbox.SentTo(2); len(got) != 0 {
		t.Errorf("alice received %d events, want 0 (not subscribed)", len(got))
	}
}

// TestSpec_M36_FederateResign_FansOutRemove: the resigning federate's
// HLAfederate instance is deleted through the standard registry path so
// subscribers receive RemoveObjectInstance (IEEE 1516.1-2010 §6.15).
func TestSpec_M36_FederateResign_FansOutRemove(t *testing.T) {
	momMgr, declMgr, outbox := newMOMStack(t)
	ctx := driveLifecycle(t, momMgr, declMgr)
	if err := momMgr.FederateJoined(ctx, fedName, 2, "alice", ""); err != nil {
		t.Fatalf("FederateJoined(alice): %v", err)
	}

	before := len(outbox.SentTo(1))
	if err := momMgr.FederateResigned(ctx, fedName, 2); err != nil {
		t.Fatalf("FederateResigned(alice): %v", err)
	}
	events := outbox.SentTo(1)[before:]
	if len(events) != 1 || events[0].GetRemove() == nil {
		t.Fatalf("observer events for alice resign = %v, want exactly [REMOVE]", kinds(events))
	}
}

// TestSpec_M36_FederationClassSubscriber_SeesFederatesInFederation: a
// subscriber to HLAmanager.HLAfederation retroactively discovers the
// singleton federation instance and receives HLAfederatesInFederation
// updates as joins occur (HLAvariableArray of HLAhandle).
func TestSpec_M36_FederationClassSubscriber_SeesFederatesInFederation(t *testing.T) {
	momMgr, declMgr, outbox := newMOMStack(t)
	ctx := driveLifecycle(t, momMgr, declMgr)

	if err := declMgr.SubscribeObjectClassAttributes(
		ctx, fedName, 1, clsHLAfederation,
		[]core.AttributeHandle{attrFederationName, attrFederatesInFederation},
	); err != nil {
		t.Fatalf("subscribe HLAfederation: %v", err)
	}
	before := outbox.SentTo(1)
	// Retro pair for the singleton federation instance.
	n := len(before)
	if n < 2 || before[n-2].GetDiscover() == nil || before[n-1].GetReflect() == nil {
		t.Fatalf("federation-class late subscribe: last events = %v, want [... DISCOVER REFLECT]", kinds(before))
	}
	if got := before[n-2].GetDiscover().GetObjectName(); got != "HLAfederation."+string(fedName) {
		t.Errorf("federation discover name = %q, want %q", got, "HLAfederation."+string(fedName))
	}

	// A join updates HLAfederatesInFederation on the federation instance.
	if err := momMgr.FederateJoined(ctx, fedName, 2, "alice", ""); err != nil {
		t.Fatalf("FederateJoined(alice): %v", err)
	}
	events := outbox.SentTo(1)[n:]
	var sawList []byte
	for _, ev := range events {
		if r := ev.GetReflect(); r != nil && r.GetObjectClassHandle() == uint64(clsHLAfederation) {
			sawList = r.GetAttributes()[uint64(attrFederatesInFederation)]
		}
	}
	// Two HLAhandle arrays, each containing the SDK's eight-byte payload.
	want := []byte{
		0, 0, 0, 2,
		0, 0, 0, 8, 0, 0, 0, 0, 0, 0, 0, 1,
		0, 0, 0, 8, 0, 0, 0, 0, 0, 0, 0, 2,
	}
	if string(sawList) != string(want) {
		t.Errorf("HLAfederatesInFederation after alice join = % x, want % x", sawList, want)
	}
}

// TestSpec_M36_FanoutDisabled_KeepsSnapshotOnlyBehavior: a Manager
// without EnableInstanceFanout must behave exactly as pre-M36 — hooks
// succeed, no outbox traffic.
func TestSpec_M36_FanoutDisabled_KeepsSnapshotOnlyBehavior(t *testing.T) {
	outbox := newFakeOutbox()
	momMgr, err := mom.New(mom.Options{Outbox: outbox})
	if err != nil {
		t.Fatalf("mom.New: %v", err)
	}
	ctx := context.Background()
	if err := momMgr.FederationCreated(ctx, fedName, nil); err != nil {
		t.Fatalf("FederationCreated: %v", err)
	}
	if err := momMgr.FederateJoined(ctx, fedName, 1, "observer", ""); err != nil {
		t.Fatalf("FederateJoined: %v", err)
	}
	if err := momMgr.FederateResigned(ctx, fedName, 1); err != nil {
		t.Fatalf("FederateResigned: %v", err)
	}
	if got := outbox.Sent(); len(got) != 0 {
		t.Errorf("snapshot-only Manager sent %d events, want 0", len(got))
	}
}

func kinds(events []*rtiv1.FederateEvent) []string {
	out := make([]string, len(events))
	for i, ev := range events {
		out[i] = kindOf(ev)
	}
	return out
}
