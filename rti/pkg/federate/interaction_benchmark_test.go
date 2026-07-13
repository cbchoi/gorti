package federate

import (
	"context"
	"io"
	"net"
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/genproto/rti/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

type benchmarkInteractionStreamServer struct {
	preparedAck bool
}

func (s benchmarkInteractionStreamServer) SendInteractions(stream grpc.ServerStream) error {
	if err := stream.SendHeader(metadata.MD{}); err != nil {
		return err
	}
	var ack any = &rtiv1.Empty{}
	if s.preparedAck {
		prepared := new(grpc.PreparedMsg)
		if err := prepared.Encode(stream, ack); err != nil {
			return err
		}
		ack = prepared
	}
	for {
		request := new(rtiv1.SendInteractionRequest)
		if err := stream.RecvMsg(request); err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		if err := stream.SendMsg(ack); err != nil {
			return err
		}
	}
}

type benchmarkInteractionStreamService interface {
	SendInteractions(grpc.ServerStream) error
}

var benchmarkInteractionStreamServiceDescription = grpc.ServiceDesc{
	ServiceName: "rti.v1.InteractionStreamService",
	HandlerType: (*benchmarkInteractionStreamService)(nil),
	Streams: []grpc.StreamDesc{{
		StreamName:    "SendInteractions",
		ClientStreams: true,
		ServerStreams: true,
		Handler: func(service any, stream grpc.ServerStream) error {
			return service.(benchmarkInteractionStreamService).SendInteractions(stream)
		},
	}},
}

func BenchmarkSendInteractionSDKPersistentStreamTCP(b *testing.B) {
	benchmarkSendInteractionSDKPersistentStreamTCP(b, nil, nil, false)
}

func BenchmarkSendInteractionSDKPersistentStreamTCPSharedWriteBuffer(b *testing.B) {
	benchmarkSendInteractionSDKPersistentStreamTCP(
		b,
		[]grpc.ServerOption{grpc.SharedWriteBuffer(true)},
		[]grpc.DialOption{grpc.WithSharedWriteBuffer(true)},
		false,
	)
}

func BenchmarkSendInteractionSDKPersistentStreamTCPPreparedAck(b *testing.B) {
	benchmarkSendInteractionSDKPersistentStreamTCP(b, nil, nil, true)
}

func benchmarkSendInteractionSDKPersistentStreamTCP(
	b *testing.B,
	serverOptions []grpc.ServerOption,
	dialOptions []grpc.DialOption,
	preparedAck bool,
) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		b.Fatal(err)
	}
	server := grpc.NewServer(serverOptions...)
	server.RegisterService(
		&benchmarkInteractionStreamServiceDescription,
		benchmarkInteractionStreamServer{preparedAck: preparedAck},
	)
	go func() { _ = server.Serve(listener) }()
	b.Cleanup(func() {
		server.Stop()
		_ = listener.Close()
	})

	dialOptions = append(
		[]grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())},
		dialOptions...,
	)
	connection, err := grpc.NewClient(listener.Addr().String(), dialOptions...)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = connection.Close() })

	lifecycleCtx, stopLifecycle := context.WithCancel(context.Background())
	b.Cleanup(stopLifecycle)
	fed := &Federate{
		conn: &Connection{
			cc:                       connection,
			interactionStreamEnabled: true,
		},
		federationName:     "benchmark",
		federateHandle:     1,
		interactionContext: lifecycleCtx,
		interactionCancel:  stopLifecycle,
	}
	callCtx, stopCall := context.WithCancel(context.Background())
	b.Cleanup(stopCall)
	timestamp := 1.0
	parameters := map[uint64][]byte{
		3: {0, 0, 0, 1},
		4: {0, 16, '0', '1', '2', '3', '4', '5', '6', '7'},
	}
	if err := fed.SendInteractionByHandle(callCtx, 7, parameters, &timestamp); err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if err := fed.SendInteractionByHandle(callCtx, 7, parameters, &timestamp); err != nil {
			b.Fatal(err)
		}
	}
}
