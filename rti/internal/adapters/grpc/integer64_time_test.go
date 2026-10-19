package grpc

import (
	"context"
	"math"
	"testing"
	"time"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	timepkg "github.com/cbchoi/gorti/rti/internal/hla/time"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

type integerTimeOutbox struct{ events []core.OutboundEvent }

func (o *integerTimeOutbox) Send(_ context.Context, _ core.FederationName, _ core.FederateHandle, event core.OutboundEvent) error {
	o.events = append(o.events, event)
	return nil
}
func integerWire(value int64) *rtiv1.LogicalTimeValue {
	return &rtiv1.LogicalTimeValue{Value: &rtiv1.LogicalTimeValue_Integer64Value{Integer64Value: value}}
}

func TestInteger64TimeServiceExactRequestQueryAndGrant(t *testing.T) {
	ctx := context.Background()
	out := &integerTimeOutbox{}
	mgr, err := timepkg.NewInteger(timepkg.IntegerOptions{Outbox: out, Clock: core.NewManualClock(time.Unix(0, 0))})
	if err != nil {
		t.Fatal(err)
	}
	service := newTimeServiceOf[int64](mgr)
	for _, target := range []int64{1<<53 + 1, 1<<53 + 19, math.MaxInt64} {
		_, err = service.TimeAdvanceRequest(ctx, &rtiv1.TARRequest{WireVersion: wireV1(), FederationName: "integer", FederateHandle: 1, LogicalTimeValue: integerWire(target)})
		if err != nil {
			t.Fatal(err)
		}
		response, err := service.QueryLogicalTime(ctx, &rtiv1.QueryFederateTimeRequest{WireVersion: wireV1(), FederationName: "integer", FederateHandle: 1})
		if err != nil {
			t.Fatal(err)
		}
		if response.LogicalTime != 0 || response.GetLogicalTimeValue().GetInteger64Value() != target {
			t.Fatalf("query=%v", response)
		}
		encoded, err := proto.Marshal(response)
		if err != nil {
			t.Fatal(err)
		}
		var decoded rtiv1.QueryFederateTimeResponse
		if err = proto.Unmarshal(encoded, &decoded); err != nil {
			t.Fatal(err)
		}
		if decoded.GetLogicalTimeValue().GetInteger64Value() != target {
			t.Fatal("wire lost precision")
		}
		event := out.events[len(out.events)-1]
		wire, err := toFederateEvent(event)
		if err != nil {
			t.Fatal(err)
		}
		if wire.GetGrant().GetLogicalTime() != 0 || wire.GetGrant().GetLogicalTimeValue().GetInteger64Value() != target {
			t.Fatalf("grant=%v", wire)
		}
		if !isTimeGrantEvent(event) {
			t.Fatal("integer grant did not delimit callback batch")
		}
	}
	bound, err := service.QueryLBTS(ctx, &rtiv1.QueryLBTSRequest{WireVersion: wireV1(), FederationName: "integer"})
	if err != nil {
		t.Fatal(err)
	}
	if bound.Finite || bound.LbtsValue != nil {
		t.Fatal("unbounded horizon became finite integer final time")
	}
}

func TestInteger64TimeServiceRejectsLegacyAndMixedTimeWithoutEffects(t *testing.T) {
	ctx := context.Background()
	out := &integerTimeOutbox{}
	mgr, err := timepkg.NewInteger(timepkg.IntegerOptions{Outbox: out, Clock: core.NewManualClock(time.Unix(0, 0))})
	if err != nil {
		t.Fatal(err)
	}
	service := newTimeServiceOf[int64](mgr)
	for _, req := range []*rtiv1.TARRequest{
		{LogicalTime: 1}, {LogicalTime: 1, LogicalTimeValue: integerWire(1)},
		{LogicalTimeValue: &rtiv1.LogicalTimeValue{Value: &rtiv1.LogicalTimeValue_Float64Value{Float64Value: 1}}},
		{LogicalTimeValue: integerWire(-1)},
	} {
		req.WireVersion = wireV1()
		req.FederationName = "integer"
		req.FederateHandle = 1
		if _, err := service.TimeAdvanceRequest(ctx, req); status.Code(err) != codes.InvalidArgument {
			t.Fatalf("error=%v", err)
		}
	}
	if len(out.events) != 0 {
		t.Fatal("invalid requests produced callbacks")
	}
	if len(mgr.Snapshot("integer").Federates) != 0 {
		t.Fatal("invalid requests changed authoritative state")
	}
}
