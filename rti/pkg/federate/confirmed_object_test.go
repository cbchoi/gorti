package federate

import (
	"context"
	"net"
	"sync"
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/genproto/rti/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"
)

type recordingConfirmedObjectService struct {
	rtiv1.UnimplementedConfirmedObjectServiceServer
	mu       sync.Mutex
	streams  int
	requests []*rtiv1.ConfirmedObjectRequest
	failAt   uint64
}

func (s *recordingConfirmedObjectService) Exchange(
	stream rtiv1.ConfirmedObjectService_ExchangeServer,
) error {
	s.mu.Lock()
	s.streams++
	s.mu.Unlock()
	if err := stream.SendHeader(metadata.MD{}); err != nil {
		return err
	}
	for {
		request, err := stream.Recv()
		if err != nil {
			return nil
		}
		if request.GetSequence() == 0 && request.GetOpen() != nil {
			if err := stream.Send(&rtiv1.ConfirmedObjectResult{}); err != nil {
				return err
			}
			continue
		}
		s.mu.Lock()
		s.requests = append(s.requests, proto.Clone(request).(*rtiv1.ConfirmedObjectRequest))
		s.mu.Unlock()
		result := &rtiv1.ConfirmedObjectResult{Sequence: request.GetSequence()}
		if request.GetSequence() == s.failAt {
			result.ErrorStatus, _ = proto.Marshal(status.New(
				codes.FailedPrecondition, "scripted rejection").Proto())
		}
		if err := stream.Send(result); err != nil {
			return err
		}
	}
}

func TestConfirmedObjectStreamSharesOrderingAndSurvivesRejection(t *testing.T) {
	listener := bufconn.Listen(1 << 20)
	server := grpc.NewServer()
	service := &recordingConfirmedObjectService{failAt: 1}
	rtiv1.RegisterConfirmedObjectServiceServer(server, service)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() {
		server.Stop()
		_ = listener.Close()
	})

	cc, err := grpc.NewClient(
		"passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return listener.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cc.Close() })
	lifecycle, cancel := context.WithCancel(context.Background())
	defer cancel()
	fed := &Federate{
		conn: &Connection{
			confirmedObject:              rtiv1.NewConfirmedObjectServiceClient(cc),
			confirmedObjectStreamEnabled: true,
		},
		federationName: "fed", federateHandle: 7,
		interactionContext: lifecycle, interactionCancel: cancel,
	}

	err = fed.UpdateAttributeValuesByHandleConfirmed(
		context.Background(), 11, map[uint64][]byte{3: {1}}, nil,
	)
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("first result = %v, want FailedPrecondition", err)
	}
	if err := fed.SendInteractionByHandleConfirmed(
		context.Background(), 5, map[uint64][]byte{9: {2}}, nil,
	); err != nil {
		t.Fatalf("second result: %v", err)
	}

	service.mu.Lock()
	defer service.mu.Unlock()
	if service.streams != 1 || len(service.requests) != 2 {
		t.Fatalf("streams=%d requests=%d, want 1 and 2", service.streams, len(service.requests))
	}
	if service.requests[0].GetSequence() != 1 || service.requests[0].GetAttributeUpdate() == nil {
		t.Fatalf("first request = %#v", service.requests[0])
	}
	if service.requests[1].GetSequence() != 2 || service.requests[1].GetInteraction() == nil {
		t.Fatalf("second request = %#v", service.requests[1])
	}
}
