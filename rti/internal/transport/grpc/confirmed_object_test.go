package grpc

import (
	"context"
	"io"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/core"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/genproto/rti/v1"
	statuspb "google.golang.org/genproto/googleapis/rpc/status"
	stdgrpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

type scriptedConfirmedObjectServerStream struct {
	stdgrpc.ServerStream
	ctx       context.Context
	requests  []*rtiv1.ConfirmedObjectRequest
	responses []*rtiv1.ConfirmedObjectResult
	next      int
}

func (s *scriptedConfirmedObjectServerStream) Context() context.Context     { return s.ctx }
func (s *scriptedConfirmedObjectServerStream) SetHeader(metadata.MD) error  { return nil }
func (s *scriptedConfirmedObjectServerStream) SendHeader(metadata.MD) error { return nil }
func (s *scriptedConfirmedObjectServerStream) SetTrailer(metadata.MD)       {}

func (s *scriptedConfirmedObjectServerStream) Recv() (*rtiv1.ConfirmedObjectRequest, error) {
	if s.next >= len(s.requests) {
		return nil, io.EOF
	}
	request := s.requests[s.next]
	s.next++
	return request, nil
}

func (s *scriptedConfirmedObjectServerStream) Send(result *rtiv1.ConfirmedObjectResult) error {
	s.responses = append(s.responses, result)
	return nil
}

func (s *scriptedConfirmedObjectServerStream) RecvMsg(message any) error {
	request, err := s.Recv()
	if err != nil {
		return err
	}
	// proto messages embed a mutex-bearing MessageState: never copy the
	// value (vet copylocks) — reset the target and merge instead.
	target := message.(*rtiv1.ConfirmedObjectRequest)
	proto.Reset(target)
	proto.Merge(target, request)
	return nil
}

func (s *scriptedConfirmedObjectServerStream) SendMsg(message any) error {
	return s.Send(message.(*rtiv1.ConfirmedObjectResult))
}

type interceptedConfirmedObjectServerStream struct {
	*membershipServerStream
}

func (s *interceptedConfirmedObjectServerStream) Recv() (*rtiv1.ConfirmedObjectRequest, error) {
	request := new(rtiv1.ConfirmedObjectRequest)
	if err := s.RecvMsg(request); err != nil {
		return nil, err
	}
	return request, nil
}

func (s *interceptedConfirmedObjectServerStream) Send(result *rtiv1.ConfirmedObjectResult) error {
	return s.SendMsg(result)
}

type nonReentrantConfirmedMembership struct {
	held, nested       bool
	acquired, released int
}

func (*nonReentrantConfirmedMembership) GenerationFor(core.FederationName) (uint64, bool) {
	return 1, true
}

func (m *nonReentrantConfirmedMembership) ValidateMember(
	core.FederationName,
	core.FederateHandle,
) error {
	if m.held {
		m.nested = true
	}
	return nil
}

func (m *nonReentrantConfirmedMembership) AcquireMember(
	core.FederationName,
	core.FederateHandle,
) (func(), error) {
	m.held = true
	m.acquired++
	return func() {
		m.held = false
		m.released++
	}, nil
}

func TestConfirmedObjectExchangeKeepsStreamAfterServiceError(t *testing.T) {
	registry := &stubObjectRegistry{sendErr: core.ErrInteractionClassNotPublished}
	stream := &scriptedConfirmedObjectServerStream{
		ctx: context.Background(),
		requests: []*rtiv1.ConfirmedObjectRequest{
			{Sequence: 0, Operation: &rtiv1.ConfirmedObjectRequest_Open{Open: confirmedObjectTestOpen()}},
			{Sequence: 1, Operation: &rtiv1.ConfirmedObjectRequest_AttributeUpdate{
				AttributeUpdate: &rtiv1.UpdateAttributeValuesRequest{
					WireVersion: rtiv1.WireVersion_WIRE_VERSION_V1, FederationName: "fed",
					FederateHandle: 7, ObjectHandle: 11, Attributes: map[uint64][]byte{3: {1}},
				},
			}},
			{Sequence: 2, Operation: &rtiv1.ConfirmedObjectRequest_Interaction{
				Interaction: &rtiv1.SendInteractionRequest{
					WireVersion: rtiv1.WireVersion_WIRE_VERSION_V1, FederationName: "fed",
					FederateHandle: 7, InteractionClassHandle: 5,
				},
			}},
			{Sequence: 3, Operation: &rtiv1.ConfirmedObjectRequest_AttributeUpdate{
				AttributeUpdate: &rtiv1.UpdateAttributeValuesRequest{
					WireVersion: rtiv1.WireVersion_WIRE_VERSION_V1, FederationName: "fed",
					FederateHandle: 7, ObjectHandle: 11, Attributes: map[uint64][]byte{3: {2}},
				},
			}},
		},
	}

	server := &Server{objService: newObjectService(registry)}
	if err := newConfirmedObjectService(server).Exchange(stream); err != nil {
		t.Fatalf("Exchange: %v", err)
	}
	if len(stream.responses) != 4 {
		t.Fatalf("responses = %d, want 4", len(stream.responses))
	}
	if len(stream.responses[1].GetErrorStatus()) != 0 || len(stream.responses[3].GetErrorStatus()) != 0 {
		t.Fatal("successful updates carried an error status")
	}
	encoded := stream.responses[2].GetErrorStatus()
	if len(encoded) == 0 {
		t.Fatal("failed interaction did not carry its status")
	}
	message := new(statuspb.Status)
	if err := proto.Unmarshal(encoded, message); err != nil {
		t.Fatal(err)
	}
	if code := status.Code(status.ErrorProto(message)); code != codes.FailedPrecondition {
		t.Fatalf("interaction status = %v, want FailedPrecondition", code)
	}
	if len(registry.updCalls) != 2 || len(registry.sendCalls) != 1 {
		t.Fatalf("registry calls: updates=%d interactions=%d", len(registry.updCalls), len(registry.sendCalls))
	}
}

func TestConfirmedObjectExchangeRejectsSequenceGap(t *testing.T) {
	stream := &scriptedConfirmedObjectServerStream{
		ctx: context.Background(),
		requests: []*rtiv1.ConfirmedObjectRequest{
			{Sequence: 0, Operation: &rtiv1.ConfirmedObjectRequest_Open{Open: confirmedObjectTestOpen()}},
			{
				Sequence:  2,
				Operation: &rtiv1.ConfirmedObjectRequest_Interaction{Interaction: &rtiv1.SendInteractionRequest{}},
			},
		},
	}
	err := newConfirmedObjectService(&Server{
		objService: newObjectService(&stubObjectRegistry{}),
	}).Exchange(stream)
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("sequence gap error = %v, want FailedPrecondition", err)
	}
}

func TestConfirmedObjectExchangeRejectsStaleGeneration(t *testing.T) {
	membership := &fixedLocalLRCMembership{generation: 10, federation: "fed", handle: 7}
	stream := &scriptedConfirmedObjectServerStream{
		ctx: context.Background(),
		requests: []*rtiv1.ConfirmedObjectRequest{{
			Sequence: 0,
			Operation: &rtiv1.ConfirmedObjectRequest_Open{Open: &rtiv1.ConfirmedObjectOpen{
				WireVersion:    rtiv1.WireVersion_WIRE_VERSION_V1,
				FederationName: "fed", FederateHandle: 7,
				ExpectedFederationGeneration: 9,
			}},
		}},
	}
	err := newConfirmedObjectService(&Server{
		objService: newObjectService(&stubObjectRegistry{}), membership: membership,
	}).Exchange(stream)
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("stale generation error = %v, want FailedPrecondition", err)
	}
}

func TestConfirmedObjectExchangeRejectsIdentityChange(t *testing.T) {
	stream := &scriptedConfirmedObjectServerStream{
		ctx: context.Background(),
		requests: []*rtiv1.ConfirmedObjectRequest{
			{Sequence: 0, Operation: &rtiv1.ConfirmedObjectRequest_Open{Open: confirmedObjectTestOpen()}},
			{Sequence: 1, Operation: &rtiv1.ConfirmedObjectRequest_Interaction{
				Interaction: &rtiv1.SendInteractionRequest{
					WireVersion:    rtiv1.WireVersion_WIRE_VERSION_V1,
					FederationName: "fed", FederateHandle: 8,
				},
			}},
		},
	}
	err := newConfirmedObjectService(&Server{
		objService: newObjectService(&stubObjectRegistry{}),
	}).Exchange(stream)
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("identity change error = %v, want PermissionDenied", err)
	}
}

func TestConfirmedObjectOpenDoesNotReenterMembershipLease(t *testing.T) {
	membership := new(nonReentrantConfirmedMembership)
	base := &scriptedConfirmedObjectServerStream{
		ctx: context.Background(),
		requests: []*rtiv1.ConfirmedObjectRequest{{
			Sequence: 0,
			Operation: &rtiv1.ConfirmedObjectRequest_Open{Open: &rtiv1.ConfirmedObjectOpen{
				WireVersion:    rtiv1.WireVersion_WIRE_VERSION_V1,
				FederationName: "fed", FederateHandle: 7,
				ExpectedFederationGeneration: 1,
			}},
		}},
	}
	stream := &interceptedConfirmedObjectServerStream{membershipServerStream: &membershipServerStream{
		ServerStream:    base,
		server:          &Server{membership: membership},
		confirmedObject: true,
	}}
	service := newConfirmedObjectService(&Server{
		objService: newObjectService(&stubObjectRegistry{}), membership: membership,
	})
	if err := service.Exchange(stream); err != nil {
		t.Fatal(err)
	}
	if membership.nested {
		t.Fatal("confirmed open re-entered membership validation while its lease was held")
	}
	if membership.acquired != 1 || membership.released != 1 {
		t.Fatalf("membership leases = acquired %d released %d", membership.acquired, membership.released)
	}
}

func confirmedObjectTestOpen() *rtiv1.ConfirmedObjectOpen {
	return &rtiv1.ConfirmedObjectOpen{
		WireVersion:    rtiv1.WireVersion_WIRE_VERSION_V1,
		FederationName: "fed",
		FederateHandle: 7,
	}
}
