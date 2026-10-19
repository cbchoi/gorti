package grpc

import (
	"context"
	"math"
	"strconv"
	"testing"
	"time"

	wire "github.com/cbchoi/gorti/rti/internal/adapters/protobuf"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/mom"
	"github.com/cbchoi/gorti/rti/pkg/logicaltime"
	grpcgo "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

func momExactManager(t *testing.T) *mom.Manager {
	t.Helper()
	manager, err := mom.New(mom.Options{Outbox: stubOutboxForServerTest{}})
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.FederateJoined(context.Background(), "fed", 1, "one", "model"); err != nil {
		t.Fatal(err)
	}
	return manager
}

func momExactQueryPaths(t *testing.T, manager core.ManagementObjectModel, check func(*testing.T, *rtiv1.QueryFederateAttributesResponse)) {
	t.Helper()
	service := newMomService(manager)
	conn := saveDDMExactRPCConn(t, func(server *grpcgo.Server) {
		rtiv1.RegisterMomServiceServer(server, service)
	})
	request := &rtiv1.QueryFederateAttributesRequest{WireVersion: 1, FederationName: "fed", FederateHandle: 1}
	for _, path := range []string{"direct", "bufconn"} {
		t.Run(path, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			var response *rtiv1.QueryFederateAttributesResponse
			var err error
			if path == "direct" {
				response, err = service.QueryFederateAttributes(ctx, request)
			} else {
				response, err = rtiv1.NewMomServiceClient(conn).QueryFederateAttributes(ctx, request)
			}
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := proto.Marshal(response)
			if err != nil {
				t.Fatal(err)
			}
			roundTrip := new(rtiv1.QueryFederateAttributesResponse)
			if err := proto.Unmarshal(encoded, roundTrip); err != nil {
				t.Fatal(err)
			}
			if !roundTrip.Found || roundTrip.FederateHandle != 1 || roundTrip.FederateName != "one" || roundTrip.FederateType != "model" {
				t.Fatalf("identity lost: %v", roundTrip)
			}
			check(t, roundTrip)
		})
	}
}

func TestMOMExactBoundaryInteger64(t *testing.T) {
	for _, number := range []int64{0, 1<<53 + 1, math.MaxInt64} {
		t.Run(strconv.FormatInt(number, 10), func(t *testing.T) {
			manager := momExactManager(t)
			value, _ := logicaltime.Integer64(number)
			interval, _ := logicaltime.Integer64Interval(number)
			if err := manager.TimeStateChangedValue(context.Background(), "fed", 1, true, true, interval, value); err != nil {
				t.Fatal(err)
			}
			momExactQueryPaths(t, manager, func(t *testing.T, response *rtiv1.QueryFederateAttributesResponse) {
				if response.LogicalTime == nil || response.Lookahead == nil || !response.TimeRegulating || !response.TimeConstrained {
					t.Fatal("time snapshot missing")
				}
				for _, message := range []*rtiv1.LogicalTime{response.LogicalTime, response.Lookahead} {
					if math.Float64bits(message.Value) != 0 || message.LogicalTimeValue == nil {
						t.Fatalf("Integer64 lost exact selector or populated legacy scalar: %v", message)
					}
					selector, ok := message.LogicalTimeValue.Value.(*rtiv1.LogicalTimeValue_Integer64Value)
					if !ok || selector.Integer64Value != number {
						t.Fatalf("Integer64 changed %d: %v", number, message)
					}
				}
				at, err := wire.DecodeLogicalTimePair(logicaltime.HLAinteger64Time, response.LogicalTime.Value, response.LogicalTime.LogicalTimeValue)
				if err != nil || at != value {
					t.Fatalf("time pair changed: %v %v", at, err)
				}
				ahead, err := wire.DecodeLogicalTimeIntervalPair(logicaltime.HLAinteger64Time, response.Lookahead.Value, response.Lookahead.LogicalTimeValue)
				if err != nil || ahead != interval {
					t.Fatalf("interval pair changed: %v %v", ahead, err)
				}
			})
		})
	}
}

func TestMOMExactBoundaryLegacyFloat64(t *testing.T) {
	for _, number := range []float64{0, 3.5, math.MaxFloat64, math.Inf(1)} {
		t.Run(strconv.FormatFloat(number, 'g', -1, 64), func(t *testing.T) {
			manager := momExactManager(t)
			if err := manager.TimeStateChanged(context.Background(), "fed", 1, true, true, 2, core.LogicalTime(number)); err != nil {
				t.Fatal(err)
			}
			momExactQueryPaths(t, manager, func(t *testing.T, response *rtiv1.QueryFederateAttributesResponse) {
				if response.LogicalTime == nil || response.Lookahead == nil || math.Float64bits(response.LogicalTime.Value) != math.Float64bits(number) || response.Lookahead.Value != 2 {
					t.Fatalf("legacy Float64 changed: %v", response)
				}
				if response.LogicalTime.LogicalTimeValue != nil || response.Lookahead.LogicalTimeValue != nil {
					t.Fatal("legacy snapshot fabricated exact values")
				}
			})
		})
	}
}

func TestMOMExactBoundaryTypedFloat64(t *testing.T) {
	manager := momExactManager(t)
	value, _ := logicaltime.Float64(3.5)
	interval, _ := logicaltime.Float64Interval(2)
	// The live Float64 hook uses legacy-only state; restored state can retain both.
	state := manager.ExportState("fed")
	state.Federates[0].LogicalTime, state.Federates[0].Lookahead = 3.5, 2
	state.Federates[0].LogicalTimeValue, state.Federates[0].LookaheadValue = &value, &interval
	commit, err := manager.PrepareImportState("fed", state)
	if err != nil {
		t.Fatal(err)
	}
	commit()
	momExactQueryPaths(t, manager, func(t *testing.T, response *rtiv1.QueryFederateAttributesResponse) {
		if response.LogicalTime == nil || response.Lookahead == nil || response.LogicalTime.Value != 3.5 || response.Lookahead.Value != 2 || response.LogicalTime.LogicalTimeValue == nil || response.Lookahead.LogicalTimeValue == nil {
			t.Fatalf("typed Float64 pair lost: %v", response)
		}
		at, err := wire.DecodeLogicalTimePair(logicaltime.HLAfloat64Time, response.LogicalTime.Value, response.LogicalTime.LogicalTimeValue)
		if err != nil || at != value {
			t.Fatalf("Float64 time pair changed: %v %v", at, err)
		}
		ahead, err := wire.DecodeLogicalTimeIntervalPair(logicaltime.HLAfloat64Time, response.Lookahead.Value, response.Lookahead.LogicalTimeValue)
		if err != nil || ahead != interval {
			t.Fatalf("Float64 interval pair changed: %v %v", ahead, err)
		}
	})
}

type momExactInvalidSnapshot struct {
	core.ManagementObjectModel
	attrs core.MOMFederateAttributes
}

func (m momExactInvalidSnapshot) QueryFederate(core.FederationName, core.FederateHandle) (core.MOMFederateAttributes, bool) {
	return m.attrs, true
}

func TestMOMExactBoundaryRejectsInvalidSnapshots(t *testing.T) {
	integer, _ := logicaltime.Integer64(1<<53 + 1)
	integerInterval, _ := logicaltime.Integer64Interval(2)
	floating, _ := logicaltime.Float64(3.5)
	floatInterval, _ := logicaltime.Float64Interval(2)
	unknown := logicaltime.Time{}
	unknownInterval := logicaltime.Interval{}
	for _, test := range []struct {
		name  string
		attrs core.MOMFederateAttributes
	}{
		{"missing-lookahead", core.MOMFederateAttributes{LogicalTimeValue: &integer}},
		{"missing-time", core.MOMFederateAttributes{LookaheadValue: &integerInterval}},
		{"mixed-integer-time", core.MOMFederateAttributes{LogicalTimeValue: &integer, LookaheadValue: &floatInterval}},
		{"mixed-float-time", core.MOMFederateAttributes{LogicalTimeValue: &floating, LookaheadValue: &integerInterval}},
		{"unknown", core.MOMFederateAttributes{LogicalTimeValue: &unknown, LookaheadValue: &unknownInterval}},
		{"integer-legacy-time", core.MOMFederateAttributes{LogicalTime: 1, LogicalTimeValue: &integer, LookaheadValue: &integerInterval}},
		{"integer-legacy-lookahead", core.MOMFederateAttributes{Lookahead: 1, LogicalTimeValue: &integer, LookaheadValue: &integerInterval}},
		{"float-legacy-conflict", core.MOMFederateAttributes{LogicalTime: 4, Lookahead: 2, LogicalTimeValue: &floating, LookaheadValue: &floatInterval}},
		{"float-lookahead-conflict", core.MOMFederateAttributes{LogicalTime: 3.5, Lookahead: 4, LogicalTimeValue: &floating, LookaheadValue: &floatInterval}},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := newMomService(momExactInvalidSnapshot{attrs: test.attrs})
			request := &rtiv1.QueryFederateAttributesRequest{WireVersion: 1, FederationName: "fed", FederateHandle: 1}
			response, err := service.QueryFederateAttributes(context.Background(), request)
			if status.Code(err) != codes.Internal || response != nil {
				t.Fatalf("invalid snapshot fabricated output: %v %v", response, err)
			}
			conn := saveDDMExactRPCConn(t, func(server *grpcgo.Server) { rtiv1.RegisterMomServiceServer(server, service) })
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			response, err = rtiv1.NewMomServiceClient(conn).QueryFederateAttributes(ctx, request)
			if status.Code(err) != codes.Internal || response != nil {
				t.Fatalf("invalid wire snapshot fabricated output: %v %v", response, err)
			}
		})
	}
}

func TestMOMExactBoundaryUnknownFederate(t *testing.T) {
	service := newMomService(momExactManager(t))
	request := &rtiv1.QueryFederateAttributesRequest{WireVersion: 1, FederationName: "fed", FederateHandle: 2}
	response, err := service.QueryFederateAttributes(context.Background(), request)
	if err != nil || response == nil || response.Found || response.LogicalTime != nil || response.Lookahead != nil {
		t.Fatalf("unknown federate fabricated time: %v %v", response, err)
	}
	conn := saveDDMExactRPCConn(t, func(server *grpcgo.Server) { rtiv1.RegisterMomServiceServer(server, service) })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	response, err = rtiv1.NewMomServiceClient(conn).QueryFederateAttributes(ctx, request)
	if err != nil || response == nil || response.Found || response.LogicalTime != nil || response.Lookahead != nil {
		t.Fatalf("unknown federate fabricated wire time: %v %v", response, err)
	}
}
