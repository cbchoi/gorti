package grpc

import (
	"math"
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	syncpkg "github.com/cbchoi/gorti/rti/internal/hla/synchronization"
)

func TestWireOrderConversionRejectsWrappedValues(t *testing.T) {
	for _, tc := range []struct {
		wire rtiv1.OrderType
		want core.OrderType
	}{
		{rtiv1.OrderType_ORDER_TYPE_UNSPECIFIED, core.OrderUnspecified},
		{rtiv1.OrderType_ORDER_TYPE_RECEIVE, core.OrderReceive},
		{rtiv1.OrderType_ORDER_TYPE_TIMESTAMP, core.OrderTimestamp},
		{-1, core.OrderUnspecified},
		{257, core.OrderUnspecified},
		{258, core.OrderUnspecified},
		{math.MinInt32, core.OrderUnspecified},
		{math.MaxInt32, core.OrderUnspecified},
	} {
		if got := protoOrderTypeToCore(tc.wire); got != tc.want {
			t.Errorf("order %d: got %d, want %d", tc.wire, got, tc.want)
		}
		order := tc.wire
		event := &rtiv1.FederateEvent{Event: &rtiv1.FederateEvent_Receive{Receive: &rtiv1.ReceiveInteraction{
			Metadata: &rtiv1.ObjectCallbackMetadata{ReceivedOrder: &order},
		}}}
		if got, data := dataCallbackOrder(event); !data || got != tc.want {
			t.Errorf("data order %d: got %d, data=%v", tc.wire, got, data)
		}
	}
}

func TestWireAdvisoryConversionRejectsWrappedValues(t *testing.T) {
	for _, tc := range []struct {
		wire rtiv1.AdvisorySwitchKind
		want core.AdvisorySwitchKind
	}{
		{rtiv1.AdvisorySwitchKind_ADVISORY_SWITCH_KIND_OBJECT_CLASS_RELEVANCE, core.AdvisoryObjectClassRelevance},
		{rtiv1.AdvisorySwitchKind_ADVISORY_SWITCH_KIND_ATTRIBUTE_RELEVANCE, core.AdvisoryAttributeRelevance},
		{rtiv1.AdvisorySwitchKind_ADVISORY_SWITCH_KIND_ATTRIBUTE_SCOPE, core.AdvisoryAttributeScope},
		{rtiv1.AdvisorySwitchKind_ADVISORY_SWITCH_KIND_INTERACTION_RELEVANCE, core.AdvisoryInteractionRelevance},
		{0, 0},
		{-1, 0},
		{257, 0},
		{math.MaxInt32, 0},
	} {
		if got := protoAdvisorySwitchToCore(tc.wire); got != tc.want {
			t.Errorf("advisory %d: got %d, want %d", tc.wire, got, tc.want)
		}
	}
}

func TestCheckpointSyncReasonConversionRejectsWrappedValues(t *testing.T) {
	for _, tc := range []struct {
		wire rtiv1.SyncPointFailureReason
		want syncpkg.RegistrationFailureReason
	}{
		{rtiv1.SyncPointFailureReason_SYNC_POINT_FAILURE_REASON_LABEL_NOT_UNIQUE, syncpkg.RegistrationFailureLabelNotUnique},
		{rtiv1.SyncPointFailureReason_SYNC_POINT_FAILURE_REASON_SET_MEMBER_NOT_JOINED, syncpkg.RegistrationFailureSetMemberNotJoined},
	} {
		if got, err := checkpointSyncFailureReason(tc.wire); err != nil || got != tc.want {
			t.Errorf("reason %d: got %d, error=%v", tc.wire, got, err)
		}
	}
	for _, reason := range []rtiv1.SyncPointFailureReason{0, -1, 257, 258, math.MinInt32, math.MaxInt32} {
		if _, err := checkpointSyncFailureReason(reason); err == nil {
			t.Errorf("invalid synchronization failure reason %d accepted", reason)
		}
	}
}
