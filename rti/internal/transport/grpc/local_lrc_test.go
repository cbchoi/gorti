package grpc

import (
	"context"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/core"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/genproto/rti/v1"
	stdgrpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type fixedLocalLRCMembership struct {
	generation uint64
	federation core.FederationName
	handle     core.FederateHandle
	acquired   int
	released   int
}

func (m *fixedLocalLRCMembership) GenerationFor(fed core.FederationName) (uint64, bool) {
	return m.generation, fed == m.federation
}

func (m *fixedLocalLRCMembership) ValidateMember(
	fed core.FederationName,
	handle core.FederateHandle,
) error {
	if fed != m.federation || handle != m.handle {
		return core.ErrFederateNotJoined
	}
	return nil
}

func (m *fixedLocalLRCMembership) AcquireMember(
	fed core.FederationName,
	handle core.FederateHandle,
) (func(), error) {
	if err := m.ValidateMember(fed, handle); err != nil {
		return nil, err
	}
	m.acquired++
	return func() { m.released++ }, nil
}

type orderedLocalLRCRegistry struct {
	*stubObjectRegistry
	order []string
}

func (r *orderedLocalLRCRegistry) UpdateAttributes(
	ctx context.Context,
	fed core.FederationName,
	producer core.FederateHandle,
	obj core.ObjectHandle,
	attrs map[core.AttributeHandle][]byte,
	ts *core.LogicalTime,
) error {
	r.order = append(r.order, "update")
	return r.stubObjectRegistry.UpdateAttributes(ctx, fed, producer, obj, attrs, ts)
}

func (r *orderedLocalLRCRegistry) SendInteraction(
	ctx context.Context,
	fed core.FederationName,
	producer core.FederateHandle,
	class core.InteractionClassHandle,
	parameters map[core.ParameterHandle][]byte,
	ts *core.LogicalTime,
) error {
	r.order = append(r.order, "interaction")
	return r.stubObjectRegistry.SendInteraction(ctx, fed, producer, class, parameters, ts)
}

type scriptedLocalLRCServerStream struct {
	stdgrpc.ServerStream
	ctx       context.Context
	requests  []*rtiv1.LocalLRCRequest
	responses []*rtiv1.LocalLRCAck
	next      int
}

func (s *scriptedLocalLRCServerStream) Context() context.Context { return s.ctx }

func (s *scriptedLocalLRCServerStream) Recv() (*rtiv1.LocalLRCRequest, error) {
	if s.next >= len(s.requests) {
		return nil, io.EOF
	}
	request := s.requests[s.next]
	s.next++
	return request, nil
}

func (s *scriptedLocalLRCServerStream) Send(ack *rtiv1.LocalLRCAck) error {
	s.responses = append(s.responses, ack)
	return nil
}

func localLRCOpenRequest(generation uint64, ackEvery uint32) *rtiv1.LocalLRCRequest {
	return localLRCOpenRequestWithBatch(generation, ackEvery, 0)
}

func localLRCOpenRequestWithBatch(
	generation uint64,
	ackEvery uint32,
	requestedBatch uint32,
) *rtiv1.LocalLRCRequest {
	return &rtiv1.LocalLRCRequest{Body: &rtiv1.LocalLRCRequest_Open{
		Open: &rtiv1.LocalLRCOpen{
			WireVersion:                  rtiv1.WireVersion_WIRE_VERSION_V1,
			FederationName:               "fed",
			FederateHandle:               7,
			ExpectedFederationGeneration: generation,
			AckEvery:                     ackEvery,
			RequestedMaxBatchOperations:  requestedBatch,
		},
	}}
}

func localLRCUpdateOperation(sequence uint64, value byte) *rtiv1.LocalLRCOperation {
	return &rtiv1.LocalLRCOperation{Body: &rtiv1.LocalLRCOperation_AttributeUpdate{
		AttributeUpdate: &rtiv1.LocalLRCAttributeUpdate{
			Sequence: sequence, ObjectHandle: 11, Attributes: map[uint64][]byte{2: {value}},
		},
	}}
}

func localLRCInteractionOperation(sequence uint64) *rtiv1.LocalLRCOperation {
	return &rtiv1.LocalLRCOperation{Body: &rtiv1.LocalLRCOperation_Interaction{
		Interaction: &rtiv1.LocalLRCInteraction{
			Sequence: sequence, InteractionClassHandle: 12, Parameters: map[uint64][]byte{4: {5}},
		},
	}}
}

func localLRCBatchRequest(operations ...*rtiv1.LocalLRCOperation) *rtiv1.LocalLRCRequest {
	return &rtiv1.LocalLRCRequest{Body: &rtiv1.LocalLRCRequest_Batch{
		Batch: &rtiv1.LocalLRCBatch{Operations: operations},
	}}
}

func TestLocalLRCExchangeOrdersOperationsAndCumulativelyAcks(t *testing.T) {
	registry := &orderedLocalLRCRegistry{stubObjectRegistry: &stubObjectRegistry{}}
	membership := &fixedLocalLRCMembership{generation: 9, federation: "fed", handle: 7}
	server := &Server{objService: newObjectService(registry), membership: membership}
	stream := &scriptedLocalLRCServerStream{
		ctx: context.Background(),
		requests: []*rtiv1.LocalLRCRequest{
			localLRCOpenRequest(9, 2),
			localLRCBatchRequest(
				localLRCUpdateOperation(1, 3),
				localLRCInteractionOperation(2),
				localLRCUpdateOperation(3, 6),
			),
			{Body: &rtiv1.LocalLRCRequest_Flush{Flush: &rtiv1.LocalLRCFlush{ThroughSequence: 3}}},
		},
	}

	if err := newLocalLRCService(server).Exchange(stream); err != nil {
		t.Fatalf("Exchange: %v", err)
	}
	if !reflect.DeepEqual(registry.order, []string{"update", "interaction", "update"}) {
		t.Fatalf("operation order = %v", registry.order)
	}
	// W2: the membership lease is frame-scoped — one 3-op batch frame
	// acquires exactly once, not once per operation.
	if membership.acquired != 1 || membership.released != 1 {
		t.Fatalf("membership leases = %d/%d, want 1/1 (per frame)", membership.acquired, membership.released)
	}
	var got []uint64
	for _, ack := range stream.responses {
		got = append(got, ack.GetCommittedThrough())
	}
	if !reflect.DeepEqual(got, []uint64{0, 2, 3}) {
		t.Fatalf("ACKs = %v, want [0 2 3]", got)
	}
	if stream.responses[0].GetAckEvery() != 2 {
		t.Fatalf("negotiated ack_every = %d", stream.responses[0].GetAckEvery())
	}
	if stream.responses[0].GetMaxBatchOperations() != defaultLocalLRCBatchOperations {
		t.Fatalf("negotiated max_batch_operations = %d", stream.responses[0].GetMaxBatchOperations())
	}
}

func TestLocalLRCExchangeNegotiatesRequestedBatchLimit(t *testing.T) {
	registry := &orderedLocalLRCRegistry{stubObjectRegistry: &stubObjectRegistry{}}
	membership := &fixedLocalLRCMembership{generation: 9, federation: "fed", handle: 7}
	server := &Server{objService: newObjectService(registry), membership: membership}
	operations := make([]*rtiv1.LocalLRCOperation, 0, 64)
	for sequence := uint64(1); sequence <= 64; sequence++ {
		operations = append(operations, localLRCInteractionOperation(sequence))
	}
	stream := &scriptedLocalLRCServerStream{
		ctx: context.Background(),
		requests: []*rtiv1.LocalLRCRequest{
			localLRCOpenRequestWithBatch(9, 32, 64),
			localLRCBatchRequest(operations...),
		},
	}

	if err := newLocalLRCService(server).Exchange(stream); err != nil {
		t.Fatalf("Exchange: %v", err)
	}
	if got := stream.responses[0].GetMaxBatchOperations(); got != 64 {
		t.Fatalf("negotiated max_batch_operations = %d, want 64", got)
	}
	if len(registry.order) != 64 {
		t.Fatalf("registry operations = %d, want 64", len(registry.order))
	}
	var committed []uint64
	for _, ack := range stream.responses {
		committed = append(committed, ack.GetCommittedThrough())
	}
	if !reflect.DeepEqual(committed, []uint64{0, 32, 64}) {
		t.Fatalf("committed ACKs = %v, want [0 32 64]", committed)
	}
}

func TestLocalLRCExchangeRejectsBatchAboveNegotiatedLimit(t *testing.T) {
	registry := &orderedLocalLRCRegistry{stubObjectRegistry: &stubObjectRegistry{}}
	membership := &fixedLocalLRCMembership{generation: 9, federation: "fed", handle: 7}
	server := &Server{objService: newObjectService(registry), membership: membership}
	operations := make([]*rtiv1.LocalLRCOperation, 0, 65)
	for sequence := uint64(1); sequence <= 65; sequence++ {
		operations = append(operations, localLRCInteractionOperation(sequence))
	}
	stream := &scriptedLocalLRCServerStream{
		ctx: context.Background(),
		requests: []*rtiv1.LocalLRCRequest{
			localLRCOpenRequestWithBatch(9, 32, 64),
			localLRCBatchRequest(operations...),
		},
	}

	err := newLocalLRCService(server).Exchange(stream)
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("Exchange status = %v, want InvalidArgument", status.Code(err))
	}
	if len(stream.responses) != 1 || stream.responses[0].GetMaxBatchOperations() != 64 {
		t.Fatalf("responses = %+v, want opening ACK with negotiated limit 64", stream.responses)
	}
	if len(registry.order) != 0 {
		t.Fatalf("registry operations = %d, want none", len(registry.order))
	}
}

func TestLocalLRCExchangeRejectsSequenceGapWithoutAck(t *testing.T) {
	registry := &orderedLocalLRCRegistry{stubObjectRegistry: &stubObjectRegistry{}}
	membership := &fixedLocalLRCMembership{generation: 9, federation: "fed", handle: 7}
	server := &Server{objService: newObjectService(registry), membership: membership}
	stream := &scriptedLocalLRCServerStream{
		ctx: context.Background(),
		requests: []*rtiv1.LocalLRCRequest{
			localLRCOpenRequest(9, 1),
			{Body: &rtiv1.LocalLRCRequest_Interaction{Interaction: &rtiv1.LocalLRCInteraction{
				Sequence: 2, InteractionClassHandle: 12,
			}}},
		},
	}

	err := newLocalLRCService(server).Exchange(stream)
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("Exchange status = %v, want FailedPrecondition", status.Code(err))
	}
	if len(stream.responses) != 1 || stream.responses[0].GetCommittedThrough() != 0 {
		t.Fatalf("responses = %+v, want opening ACK only", stream.responses)
	}
	if len(registry.order) != 0 {
		t.Fatalf("registry calls = %v, want none", registry.order)
	}
}

func TestLocalLRCExchangeDoesNotAckFailedOperation(t *testing.T) {
	registry := &orderedLocalLRCRegistry{stubObjectRegistry: &stubObjectRegistry{
		sendErr: core.ErrInteractionClassNotPublished,
	}}
	membership := &fixedLocalLRCMembership{generation: 9, federation: "fed", handle: 7}
	server := &Server{objService: newObjectService(registry), membership: membership}
	stream := &scriptedLocalLRCServerStream{
		ctx: context.Background(),
		requests: []*rtiv1.LocalLRCRequest{
			localLRCOpenRequest(9, 1),
			{Body: &rtiv1.LocalLRCRequest_Interaction{Interaction: &rtiv1.LocalLRCInteraction{
				Sequence: 1, InteractionClassHandle: 12,
			}}},
		},
	}

	err := newLocalLRCService(server).Exchange(stream)
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("Exchange status = %v, want FailedPrecondition", status.Code(err))
	}
	if len(stream.responses) != 1 {
		t.Fatalf("responses = %d, want opening ACK only", len(stream.responses))
	}
}

func TestLocalLRCExchangeAcksPriorSuccessBeforeFailedOperation(t *testing.T) {
	registry := &orderedLocalLRCRegistry{stubObjectRegistry: &stubObjectRegistry{
		sendErr: core.ErrInteractionClassNotPublished,
	}}
	membership := &fixedLocalLRCMembership{generation: 9, federation: "fed", handle: 7}
	server := &Server{objService: newObjectService(registry), membership: membership}
	stream := &scriptedLocalLRCServerStream{
		ctx: context.Background(),
		requests: []*rtiv1.LocalLRCRequest{
			localLRCOpenRequest(9, 32),
			localLRCBatchRequest(
				localLRCUpdateOperation(1, 3),
				localLRCInteractionOperation(2),
			),
		},
	}

	err := newLocalLRCService(server).Exchange(stream)
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("Exchange status = %v, want FailedPrecondition", status.Code(err))
	}
	var committed []uint64
	for _, ack := range stream.responses {
		committed = append(committed, ack.GetCommittedThrough())
	}
	if !reflect.DeepEqual(committed, []uint64{0, 1}) {
		t.Fatalf("committed ACKs = %v, want [0 1]", committed)
	}
}

// TestLocalLRCExchangeSequenceErrorPrecedesMembershipError pins main's
// observable error precedence: a wrong-sequence op racing the
// federate's own resign yields the SEQUENCE error, not the membership
// error — sequence validation runs before the frame lease acquire. The
// beforeRecv hook flips the roster between the open handshake and the
// bad frame, simulating the resign landing first.
func TestLocalLRCExchangeSequenceErrorPrecedesMembershipError(t *testing.T) {
	cases := []struct {
		name  string
		frame *rtiv1.LocalLRCRequest
	}{
		{"single op", &rtiv1.LocalLRCRequest{Body: &rtiv1.LocalLRCRequest_Interaction{
			Interaction: &rtiv1.LocalLRCInteraction{Sequence: 5, InteractionClassHandle: 12},
		}}},
		{"batch", localLRCBatchRequest(
			localLRCUpdateOperation(1, 3),
			localLRCInteractionOperation(9), // in-frame gap: want 2
		)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			membership := &fixedLocalLRCMembership{generation: 9, federation: "fed", handle: 7}
			registry := &orderedLocalLRCRegistry{stubObjectRegistry: &stubObjectRegistry{}}
			server := &Server{objService: newObjectService(registry), membership: membership}
			stream := &leaseHookedServerStream{
				scriptedLocalLRCServerStream: scriptedLocalLRCServerStream{
					ctx:      context.Background(),
					requests: []*rtiv1.LocalLRCRequest{localLRCOpenRequest(9, 32), tc.frame},
				},
				// After the open handshake, the federate is gone from the
				// roster: every membership acquire now fails.
				beforeRecv: map[int]func(){1: func() { membership.handle = 8 }},
			}

			err := newLocalLRCService(server).Exchange(stream)
			if status.Code(err) != codes.FailedPrecondition {
				t.Fatalf("Exchange status = %v (%v), want FailedPrecondition", status.Code(err), err)
			}
			if !strings.Contains(status.Convert(err).Message(), "local LRC sequence") {
				t.Fatalf("Exchange error = %v, want the sequence error to precede the membership error", err)
			}
			// Pre-fence validation failed the whole frame: no lease was
			// taken and nothing applied.
			if membership.acquired != 0 {
				t.Fatalf("membership acquires = %d, want 0 (validation precedes fencing)", membership.acquired)
			}
			if len(registry.order) != 0 {
				t.Fatalf("registry calls = %v, want none", registry.order)
			}
		})
	}
}

func TestLocalLRCExchangeRejectsStaleGeneration(t *testing.T) {
	membership := &fixedLocalLRCMembership{generation: 10, federation: "fed", handle: 7}
	server := &Server{objService: newObjectService(&stubObjectRegistry{}), membership: membership}
	stream := &scriptedLocalLRCServerStream{
		ctx: context.Background(), requests: []*rtiv1.LocalLRCRequest{localLRCOpenRequest(9, 32)},
	}

	err := newLocalLRCService(server).Exchange(stream)
	if status.Code(err) != codes.FailedPrecondition ||
		!errors.Is(status.Convert(err).Err(), err) {
		// The errors.Is expression intentionally exercises status round-tripping;
		// the status code remains the stable wire assertion.
	}
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("Exchange status = %v, want FailedPrecondition", status.Code(err))
	}
	if len(stream.responses) != 0 {
		t.Fatalf("stale generation received %d ACKs", len(stream.responses))
	}
}

func TestClampLocalLRCAckEvery(t *testing.T) {
	if got := clampLocalLRCAckEvery(0); got != defaultLocalLRCAckEvery {
		t.Fatalf("default = %d", got)
	}
	if got := clampLocalLRCAckEvery(maxLocalLRCAckEvery + 1); got != maxLocalLRCAckEvery {
		t.Fatalf("clamped = %d", got)
	}
}

func TestClampLocalLRCBatchOperations(t *testing.T) {
	if got := clampLocalLRCBatchOperations(0); got != defaultLocalLRCBatchOperations {
		t.Fatalf("default = %d", got)
	}
	if got := clampLocalLRCBatchOperations(maxLocalLRCBatchOperations + 1); got != maxLocalLRCBatchOperations {
		t.Fatalf("clamped = %d", got)
	}
	if got := clampLocalLRCBatchOperations(64); got != 64 {
		t.Fatalf("requested value = %d, want 64", got)
	}
}

func BenchmarkLocalLRCInteractionExecution(b *testing.B) {
	registry := &benchmarkObjectRegistry{stubObjectRegistry: &stubObjectRegistry{}}
	membership := &fixedLocalLRCMembership{generation: 9, federation: "fed", handle: 7}
	service := newLocalLRCService(&Server{
		objService: newObjectService(registry), membership: membership,
	})
	interaction := &rtiv1.LocalLRCInteraction{
		Sequence: 1, InteractionClassHandle: 12,
		Parameters: map[uint64][]byte{4: {5, 6, 7, 8}},
	}
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := service.applyLocalLRCInteraction(ctx, "fed", 7, interaction, 1); err != nil {
			b.Fatal(err)
		}
	}
}
