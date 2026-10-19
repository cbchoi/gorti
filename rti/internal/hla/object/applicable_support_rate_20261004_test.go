package object_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/internal/adapters/delivery"
	"github.com/cbchoi/gorti/rti/internal/application/composition"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/declaration"
	"github.com/cbchoi/gorti/rti/internal/hla/object"
)

func TestApplicableSupportBestEffortRateReduction(t *testing.T) {
	for _, active := range []bool{true, false} {
		t.Run(fmt.Sprintf("active=%v", active), func(t *testing.T) {
			runApplicableSupportRate(t, active, false, false)
		})
	}
}

func TestApplicableSupportReliableRateDoesNotDrop(t *testing.T) {
	runApplicableSupportRate(t, true, true, false)
}

func TestApplicableSupportRateFailedAdmissionCanRetry(t *testing.T) {
	runApplicableSupportRate(t, true, false, true)
}

var errSupportRateAdmission = errors.New("support rate admission rejected")

type supportRateOutbox struct {
	*delivery.MultiOutbox
	reject       bool
	rejectCommit bool
}

type supportRateReservation struct {
	core.OutboxReservation
}

func (r supportRateReservation) Commit() error {
	return errSupportRateAdmission
}

func (o *supportRateOutbox) Reserve(ctx context.Context, fed core.FederationName, events []core.OutboxDelivery) (core.OutboxReservation, error) {
	if o.reject {
		return nil, errSupportRateAdmission
	}
	reservation, err := o.MultiOutbox.Reserve(ctx, fed, events)
	if err == nil && o.rejectCommit {
		return supportRateReservation{reservation}, nil
	}
	return reservation, err
}

func runApplicableSupportRate(t *testing.T, active, reliable, failedAdmission bool) {
	t.Helper()
	ctx := context.Background()
	const fed = "support-rate"
	repo := composition.NewFOMRepository()
	transportName, transport := "HLAbestEffort", core.TransportTypeBestEffort
	if reliable {
		transportName, transport = "HLAreliable", core.TransportTypeReliable
	}
	fom, err := repo.Load(ctx, []core.FOMModule{{XML: []byte(fmt.Sprintf(`<objectModel><objects><objectClass><name>HLAobjectRoot</name><objectClass><name>Item</name><attribute><name>value</name><dataType>HLAinteger32BE</dataType><transportation>%s</transportation><order>Receive</order></attribute><attribute><name>companion</name><dataType>HLAinteger32BE</dataType><transportation>%s</transportation><order>Receive</order></attribute></objectClass></objectClass></objects><updateRates><updateRate><name>TwoHz</name><rate>2</rate></updateRate></updateRates></objectModel>`, transportName, transportName))}})
	knownOK(t, err)
	repo.RememberFor(fed, fom)
	cls, _ := fom.LookupObjectClass("Item")
	attr, _ := fom.LookupAttribute(cls, testValueName)
	companion, _ := fom.LookupAttribute(cls, "companion")
	decl := declaration.New()
	decl.SetFOMRepository(repo)
	out := &supportRateOutbox{MultiOutbox: delivery.NewWithBatch(32, 1, 0)}
	queues := map[core.FederateHandle]<-chan []core.OutboundEvent{}
	for _, h := range []core.FederateHandle{2, 3} {
		q, stop, err := out.Subscribe(ctx, fed, h)
		knownOK(t, err)
		t.Cleanup(func() { _ = stop() })
		queues[h] = q
	}
	clock := core.NewManualClock(time.Unix(100, 0))
	reg, err := object.New(object.Options{Clock: clock, FOMs: repo, Declarations: decl, Outbox: out})
	knownOK(t, err)
	decl.SetOnSubscribeObjectClass(reg.ObjectClassSubscribed)
	knownOK(t, decl.PublishObjectClassAttributes(ctx, fed, 1, cls, []core.AttributeHandle{attr, companion}))
	knownOK(t, decl.SubscribeObjectClassAttributesWithOptions(ctx, fed, 2, cls, []core.AttributeHandle{attr}, core.SubscriptionOptions{Active: active, UpdateRateDesignator: "TwoHz"}))
	knownOK(t, decl.SubscribeObjectClassAttributesWithOptions(ctx, fed, 2, cls, []core.AttributeHandle{companion}, core.SubscriptionOptions{Active: active}))
	knownOK(t, decl.SubscribeObjectClassAttributesWithOptions(ctx, fed, 3, cls, []core.AttributeHandle{attr, companion}, core.SubscriptionOptions{Active: true}))
	obj, _, err := reg.Register(ctx, fed, 1, cls, "")
	knownOK(t, err)
	drain := func(h core.FederateHandle) []byte {
		var values []byte
		for {
			select {
			case batch := <-queues[h]:
				for _, event := range batch {
					if e, ok := event.(*object.CallbackEvent); ok && e.Kind == object.CallbackReflect {
						if e.Metadata == nil || e.Metadata.Transportation != transport {
							t.Fatal("configured transportation not exercised")
						}
						b := e.Attributes[uint64(attr)]
						if len(b) != 4 {
							t.Fatal("value framing")
						}
						other := e.Attributes[uint64(companion)]
						if len(e.Attributes) != 2 || len(other) != 4 || other[3] != b[3] {
							t.Fatal("passel was split or contains stale companion value")
						}
						values = append(values, b[3])
					}
				}
			default:
				return values
			}
		}
	}
	drain(2)
	drain(3)
	send := func(value byte) {
		knownOK(t, reg.UpdateAttributes(ctx, fed, 1, obj, map[core.AttributeHandle][]byte{attr: {0, 0, 0, value}, companion: {0, 0, 0, value}}, nil))
	}
	send(1)
	if got := drain(2); len(got) != 1 || got[0] != 1 {
		t.Fatalf("initial delivery=%v", got)
	}
	if got := drain(3); len(got) != 1 || got[0] != 1 {
		t.Fatalf("default initial=%v", got)
	}
	clock.Advance(100 * time.Millisecond)
	send(2)
	if got := drain(2); reliable {
		if len(got) != 1 || got[0] != 2 {
			t.Errorf("reliable update dropped: %v", got)
		}
	} else if len(got) != 0 {
		t.Errorf("2 Hz subscriber received excess update at 100 ms: %v", got)
	}
	if got := drain(3); len(got) != 1 || got[0] != 2 {
		t.Errorf("default subscription was reduced: %v", got)
	}
	clock.Advance(400 * time.Millisecond)
	if got := drain(2); len(got) != 0 {
		t.Errorf("RTI synthesized update without publisher: %v", got)
	}
	if failedAdmission {
		for _, commitFailure := range []bool{false, true} {
			out.reject, out.rejectCommit = !commitFailure, commitFailure
			err := reg.UpdateAttributes(ctx, fed, 1, obj, map[core.AttributeHandle][]byte{attr: {0, 0, 0, 99}, companion: {0, 0, 0, 99}}, nil)
			if !errors.Is(err, errSupportRateAdmission) {
				t.Fatalf("expected exact admission rejection, got %v", err)
			}
			out.reject, out.rejectCommit = false, false
			for _, h := range []core.FederateHandle{2, 3} {
				if got := drain(h); len(got) != 0 {
					t.Fatalf("failed admission delivered to %d: %v", h, got)
				}
			}
		}
	}
	send(3)
	if got := drain(2); len(got) != 1 || got[0] != 3 {
		t.Errorf("boundary delivery must contain current value, not queued excess: %v", got)
	}
	if got := drain(3); len(got) != 1 || got[0] != 3 {
		t.Errorf("default boundary=%v", got)
	}
	knownOK(t, decl.SubscribeObjectClassAttributesWithOptions(ctx, fed, 2, cls, []core.AttributeHandle{attr}, core.SubscriptionOptions{Active: active}))
	send(4)
	if got := drain(2); len(got) != 1 || got[0] != 4 {
		t.Errorf("reset to default retained named rate: %v", got)
	}
	if got := drain(3); len(got) != 1 || got[0] != 4 {
		t.Errorf("default reset peer=%v", got)
	}
}
