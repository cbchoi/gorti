package federate

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/pkg/logicaltime"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

func TestLegacyFloatFinalTimeRemainsLegacyOnly(t *testing.T) {
	f := remainingServicesFederate(t, func(_ context.Context, request any, info *grpc.UnaryServerInfo, _ grpc.UnaryHandler) (any, error) {
		if strings.HasSuffix(info.FullMethod, "QueryLogicalTime") {
			return &rtiv1.QueryFederateTimeResponse{LogicalTime: math.Inf(1)}, nil
		}
		message := request.(proto.Message).ProtoReflect()
		legacy := message.Descriptor().Fields().ByName("logical_time")
		exact := message.Descriptor().Fields().ByName("logical_time_value")
		if legacy == nil || !math.IsInf(message.Get(legacy).Float(), 1) || message.Has(exact) {
			t.Fatalf("legacy final changed: %v", request)
		}
		return &rtiv1.Empty{}, nil
	})
	ctx := context.Background()
	for _, operation := range []func() error{
		func() error { return f.TimeAdvanceRequest(ctx, math.Inf(1)) },
		func() error { return f.TimeAdvanceRequestAvailable(ctx, math.Inf(1)) },
		func() error { return f.NextMessageRequest(ctx, math.Inf(1)) },
		func() error { return f.NextMessageRequestAvailable(ctx, math.Inf(1)) },
		func() error { return f.FlushQueueRequest(ctx, math.Inf(1)) },
	} {
		if err := operation(); err != nil {
			t.Fatal(err)
		}
	}
	value, err := f.QueryLogicalTime(ctx)
	if err != nil || !math.IsInf(value, 1) {
		t.Fatalf("final query %v %v", value, err)
	}
	event := f.translate(&rtiv1.FederateEvent{Event: &rtiv1.FederateEvent_Grant{Grant: &rtiv1.TimeAdvanceGrant{LogicalTime: math.Inf(1)}}})
	if grant, ok := event.(TimeAdvanceGrant); !ok || !math.IsInf(grant.Time, 1) {
		t.Fatalf("final grant %v", event)
	}
	f.logicalTimeImplementationName = HLAinteger64Time
	if err := f.TimeAdvanceRequest(ctx, math.Inf(1)); err == nil {
		t.Fatal("Integer64 accepted Float final")
	}
}

func TestExactInteger64MOMNestedTime(t *testing.T) {
	const n = int64(1<<53 + 1)
	exact := &rtiv1.LogicalTimeValue{Value: &rtiv1.LogicalTimeValue_Integer64Value{Integer64Value: n}}
	f := remainingServicesFederate(t, func(_ context.Context, _ any, _ *grpc.UnaryServerInfo, _ grpc.UnaryHandler) (any, error) {
		return &rtiv1.QueryFederateAttributesResponse{Found: true, LogicalTime: &rtiv1.LogicalTime{LogicalTimeValue: exact}, Lookahead: &rtiv1.LogicalTime{LogicalTimeValue: exact}}, nil
	})
	f.logicalTimeImplementationName = HLAinteger64Time
	result, err := f.QueryFederateAttributes(context.Background(), f.CurrentHandle())
	if err != nil {
		t.Fatal(err)
	}
	if result.LogicalTime != nil || result.Lookahead != nil || result.LogicalTimeValue == nil || result.LookaheadValue == nil {
		t.Fatalf("rounded legacy MOM result %#v", result)
	}
	value, _ := result.LogicalTimeValue.Integer64()
	interval, _ := result.LookaheadValue.Integer64()
	if value != n || interval != n {
		t.Fatalf("MOM precision %d %d", value, interval)
	}
}

func TestExactInteger64SDKRequestsAndQueries(t *testing.T) {
	const n = int64(1<<53 + 1)
	value, _ := logicaltime.Integer64(n)
	interval, _ := logicaltime.Integer64Interval(n)
	calls := 0
	f := remainingServicesFederate(t, func(_ context.Context, request any, info *grpc.UnaryServerInfo, _ grpc.UnaryHandler) (any, error) {
		calls++
		exact := &rtiv1.LogicalTimeValue{Value: &rtiv1.LogicalTimeValue_Integer64Value{Integer64Value: n}}
		switch {
		case strings.HasSuffix(info.FullMethod, "QueryLogicalTime"):
			return &rtiv1.QueryFederateTimeResponse{LogicalTimeValue: exact}, nil
		case strings.HasSuffix(info.FullMethod, "QueryLookahead"):
			return &rtiv1.QueryLookaheadResponse{LookaheadValue: exact}, nil
		case strings.HasSuffix(info.FullMethod, "QueryLBTS"):
			return &rtiv1.QueryLBTSResponse{Finite: true, LbtsValue: exact}, nil
		case strings.HasSuffix(info.FullMethod, "QueryGALT"):
			return &rtiv1.QueryGALTResponse{Finite: true, GaltValue: exact}, nil
		case strings.HasSuffix(info.FullMethod, "QueryLITS"):
			return &rtiv1.QueryLITSResponse{Finite: true, LitsValue: exact}, nil
		}
		message := request.(proto.Message).ProtoReflect()
		var found bool
		for _, name := range []protoreflect.Name{"logical_time", "lookahead", "save_time"} {
			legacy := message.Descriptor().Fields().ByName(name)
			if legacy == nil {
				continue
			}
			field := message.Descriptor().Fields().ByName(name + "_value")
			if field == nil || !message.Has(field) {
				t.Fatalf("missing exact field in %s", info.FullMethod)
			}
			if legacy.HasPresence() {
				if message.Has(legacy) {
					t.Fatal("Integer64 wrote legacy double")
				}
			} else if message.Get(legacy).Float() != 0 {
				t.Fatal("Integer64 wrote legacy double")
			}
			if message.Get(field).Message().Interface().(*rtiv1.LogicalTimeValue).GetInteger64Value() != n {
				t.Fatal("precision lost")
			}
			found = true
		}
		if !found {
			t.Fatalf("no exact time in %s", info.FullMethod)
		}
		return &rtiv1.Empty{}, nil
	})
	f.logicalTimeImplementationName = HLAinteger64Time
	ctx := context.Background()
	operations := []func() error{
		func() error { return f.EnableTimeRegulationValue(ctx, interval) }, func() error { return f.ModifyLookaheadValue(ctx, interval) },
		func() error { return f.NextMessageRequestValue(ctx, value) }, func() error { return f.NextMessageRequestAvailableValue(ctx, value) },
		func() error { return f.TimeAdvanceRequestValue(ctx, value) }, func() error { return f.TimeAdvanceRequestAvailableValue(ctx, value) }, func() error { return f.FlushQueueRequestValue(ctx, value) },
		func() error { return f.SendInteractionByHandleValue(ctx, 1, nil, &value, MessageOptions{}) },
		func() error { return f.UpdateAttributeValuesByHandleValue(ctx, 2, nil, &value, MessageOptions{}) },
		func() error { return f.DeleteObjectInstanceValue(ctx, 2, &value, MessageOptions{}) },
		func() error { return f.SendInteractionWithRegionsValue(ctx, 1, nil, nil, &value, MessageOptions{}) },
		func() error { return f.RequestFederationSaveValue(ctx, "save", &value) },
	}
	for _, operation := range operations {
		if err := operation(); err != nil {
			t.Fatal(err)
		}
	}
	if got, err := f.QueryLogicalTimeValue(ctx); err != nil || got != value {
		t.Fatalf("query %v %v", got, err)
	}
	if got, err := f.QueryLookaheadValue(ctx); err != nil || got != interval {
		t.Fatalf("lookahead %v %v", got, err)
	}
	for _, query := range []func(context.Context) (logicaltime.Time, bool, error){f.QueryLBTSValue, f.QueryGALTValue, f.QueryLITSValue} {
		if got, finite, err := query(ctx); err != nil || !finite || got != value {
			t.Fatalf("bound %v %t %v", got, finite, err)
		}
	}
	before := calls
	floating, _ := logicaltime.Float64(1)
	if err := f.TimeAdvanceRequestValue(ctx, floating); !errors.Is(err, logicaltime.ErrRepresentationMismatch) {
		t.Fatal(err)
	}
	if err := f.TimeAdvanceRequest(ctx, 1); !errors.Is(err, logicaltime.ErrRepresentationMismatch) {
		t.Fatal(err)
	}
	if _, err := f.QueryLogicalTime(ctx); !errors.Is(err, logicaltime.ErrRepresentationMismatch) {
		t.Fatal(err)
	}
	if calls != before {
		t.Fatal("mixed representations reached RPC")
	}
}

func TestExactInteger64CallbacksAndRemovalState(t *testing.T) {
	f := &Federate{logicalTimeImplementationName: HLAinteger64Time, objectClasses: map[uint64]uint64{4: 2}, timeAdvances: []*sdkAdvanceRequest{{flush: true}}}
	n := int64(1<<53 + 1)
	wire := &rtiv1.LogicalTimeValue{Value: &rtiv1.LogicalTimeValue_Integer64Value{Integer64Value: n}}
	removal := &rtiv1.FederateEvent{Event: &rtiv1.FederateEvent_Remove{Remove: &rtiv1.RemoveObjectInstance{ObjectHandle: 4, LogicalTimeValue: wire, Metadata: &rtiv1.ObjectCallbackMetadata{SentOrder: rtiv1.OrderType_ORDER_TYPE_TIMESTAMP.Enum(), ReceivedOrder: rtiv1.OrderType_ORDER_TYPE_TIMESTAMP.Enum()}}}}
	got := f.translate(removal).(RemoveObjectInstance)
	if got.Timestamp != nil || got.TimestampValue == nil {
		t.Fatal("rounded callback or missing exact time")
	}
	integer, _ := got.TimestampValue.Integer64()
	if integer != n {
		t.Fatal(integer)
	}
	below, _ := logicaltime.Integer64(n - 1)
	f.observeSDKEnabledTimeValue(below)
	if _, known := f.objectClasses[4]; !known {
		t.Fatal("removed before exact timestamp")
	}
	grant := f.translate(&rtiv1.FederateEvent{Event: &rtiv1.FederateEvent_Grant{Grant: &rtiv1.TimeAdvanceGrant{LogicalTimeValue: wire}}}).(TimeAdvanceGrant)
	integer, _ = grant.TimeValue.Integer64()
	if integer != n {
		t.Fatal(integer)
	}
	if _, known := f.objectClasses[4]; known {
		t.Fatal("did not retire at exact timestamp")
	}
	if _, ok := f.translate(&rtiv1.FederateEvent{Event: &rtiv1.FederateEvent_Grant{Grant: &rtiv1.TimeAdvanceGrant{LogicalTime: float64(n)}}}).(CallbackProtocolError); !ok {
		t.Fatal("accepted rounded legacy callback")
	}
}

func TestLegacyFinalFloatRemovalRetainsFQRKnowledgeUntilFinalGrant(t *testing.T) {
	for _, flush := range []bool{false, true} {
		t.Run(fmt.Sprint(flush), func(t *testing.T) {
			f := &Federate{objectClasses: map[uint64]uint64{4: 2}, timeAdvances: []*sdkAdvanceRequest{{flush: flush}}}
			final := math.Inf(1)
			f.applyObjectRemoval(&rtiv1.RemoveObjectInstance{ObjectHandle: 4, LogicalTime: &final, Metadata: &rtiv1.ObjectCallbackMetadata{SentOrder: rtiv1.OrderType_ORDER_TYPE_TIMESTAMP.Enum(), ReceivedOrder: rtiv1.OrderType_ORDER_TYPE_TIMESTAMP.Enum()}})
			_, known := f.objectClasses[4]
			if known != flush {
				t.Fatalf("knowledge after removal: %t; flush %t", known, flush)
			}
			f.observeSDKEnabledTime(math.MaxFloat64)
			if _, known := f.objectClasses[4]; known != flush {
				t.Fatal("finite maximum treated as unbounded final time")
			}
			f.completeSDKGrant(final)
			if _, known := f.objectClasses[4]; known || len(f.pendingFinalObjectRemovals) != 0 {
				t.Fatal("final grant did not retire removal knowledge")
			}
		})
	}
}
