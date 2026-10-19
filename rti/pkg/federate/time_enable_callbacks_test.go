package federate

import (
	"context"
	"errors"
	"testing"
	"time"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func nextEnableTestEvent(t *testing.T, f *Federate) Event {
	t.Helper()
	select {
	case event := <-f.Events():
		return event
	case <-time.After(3 * time.Second):
		t.Fatal("server callback was not received")
		return nil
	}
}

func expectTimeEnable(t *testing.T, f *Federate, regulation bool, at float64) {
	t.Helper()
	event := nextEnableTestEvent(t, f)
	if regulation {
		e, ok := event.(TimeRegulationEnabled)
		if !ok || e.Time != at {
			t.Fatalf("callback=%#v, want regulation enabled at %v", event, at)
		}
	} else {
		e, ok := event.(TimeConstrainedEnabled)
		if !ok || e.Time != at {
			t.Fatalf("callback=%#v, want constraint enabled at %v", event, at)
		}
	}
}

func TestTimeEnableCallbacksCompleteRequests(t *testing.T) {
	_, _, fed := timeFixture(t)
	ctx := context.Background()
	if err := fed.EnableTimeRegulation(ctx, 2); err != nil {
		t.Fatal(err)
	}
	if event, ok := nextEnableTestEvent(t, fed).(TimeRegulationEnabled); !ok || event.Time != 0 {
		t.Fatalf("regulation completion = %#v", event)
	}
	if err := fed.EnableTimeConstrained(ctx); err != nil {
		t.Fatal(err)
	}
	if event, ok := nextEnableTestEvent(t, fed).(TimeConstrainedEnabled); !ok || event.Time != 0 {
		t.Fatalf("constraint completion = %#v", event)
	}
	if current, err := fed.QueryLogicalTime(ctx); err != nil || current != 0 {
		t.Fatalf("current=%v err=%v", current, err)
	}
}

func TestTimeEnablePendingIsNotActivation(t *testing.T) {
	_, conn, fed := timeFixture(t)
	ctx := context.Background()
	peer, err := conn.JoinFederation(ctx, FederationSpec{Name: "test-fed"}, "blocker")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = peer.Resign(ctx) })
	if err := peer.EnableTimeRegulation(ctx, 1); err != nil {
		t.Fatal(err)
	}
	if event, ok := nextEnableTestEvent(t, peer).(TimeRegulationEnabled); !ok || event.Time != 0 {
		t.Fatalf("peer completion=%#v", event)
	}
	if err := fed.TimeAdvanceRequest(ctx, 10); err != nil {
		t.Fatal(err)
	}
	if event, ok := nextEnableTestEvent(t, fed).(TimeAdvanceGrant); !ok || event.Time != 10 {
		t.Fatalf("initial grant=%#v", event)
	}
	if err := fed.EnableTimeConstrained(ctx); err != nil {
		t.Fatal(err)
	}
	if err := fed.EnableTimeConstrained(ctx); !errors.Is(err, ErrTimeConstrainedPending) {
		t.Fatalf("duplicate pending request=%v", err)
	}
	if err := fed.TimeAdvanceRequest(ctx, 11); !errors.Is(err, ErrTimeConstrainedPending) {
		t.Fatalf("advance while pending=%v", err)
	}
	select {
	case event := <-fed.Events():
		t.Fatalf("request acceptance fabricated activation: %#v", event)
	default:
	}
	if current, err := fed.QueryLogicalTime(ctx); err != nil || current != 10 {
		t.Fatalf("pending current=%v err=%v", current, err)
	}
	if err := peer.TimeAdvanceRequest(ctx, 12); err != nil {
		t.Fatal(err)
	}
	if event, ok := nextEnableTestEvent(t, fed).(TimeConstrainedEnabled); !ok || event.Time != 10 {
		t.Fatalf("delayed completion=%#v", event)
	}
	if current, err := fed.QueryLogicalTime(ctx); err != nil || current != 10 {
		t.Fatalf("enabled current=%v err=%v", current, err)
	}
}

func TestTimeEnableCallbackTranslationPreservesReceipt(t *testing.T) {
	f := &Federate{federateHandle: 7}
	for _, at := range []float64{0, 7.25} {
		for _, wire := range []*rtiv1.FederateEvent{
			{CallbackReceipt: []byte(testCallbackReceipt), Event: &rtiv1.FederateEvent_TimeRegulationEnabled{TimeRegulationEnabled: &rtiv1.TimeRegulationEnabled{LogicalTime: at}}},
			{CallbackReceipt: []byte(testCallbackReceipt), Event: &rtiv1.FederateEvent_TimeConstrainedEnabled{TimeConstrainedEnabled: &rtiv1.TimeConstrainedEnabled{LogicalTime: at}}},
		} {
			event := f.translate(wire)
			switch e := event.(type) {
			case TimeRegulationEnabled:
				if e.Time != at || string(e.CallbackReceipt()) != testCallbackReceipt {
					t.Fatalf("invalid regulation event: %#v", e)
				}
			case TimeConstrainedEnabled:
				if e.Time != at || string(e.CallbackReceipt()) != testCallbackReceipt {
					t.Fatalf("invalid constraint event: %#v", e)
				}
			default:
				t.Fatalf("activation callback dropped: %T", event)
			}
		}
	}
}

func TestTimeEnablePendingSDKErrorIdentity(t *testing.T) {
	for _, tc := range []struct {
		detail string
		want   error
	}{
		{"request for time-regulation pending", ErrTimeRegulationPending},
		{"request for time-constrained pending", ErrTimeConstrainedPending},
		{"message retraction designator is invalid for this sender", ErrInvalidMessageRetractionHandle},
		{"object deletion has already been delivered", ErrMessageCanNoLongerBeRetracted},
	} {
		if got := wrapStatusErr(status.Error(codes.FailedPrecondition, tc.detail)); !errors.Is(got, tc.want) {
			t.Errorf("%q -> %v, want %v", tc.detail, got, tc.want)
		}
	}
}
