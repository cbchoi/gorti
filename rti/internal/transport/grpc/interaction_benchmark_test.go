package grpc

import (
	"context"
	"net"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/core"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/genproto/rti/v1"
	stdgrpc "google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type benchmarkObjectRegistry struct {
	*stubObjectRegistry
}

type benchmarkInteractionStreamService interface {
	SendInteractions(stdgrpc.ServerStream) error
}

type benchmarkInteractionStreamHandler struct {
	preparedAck bool
}

func (h benchmarkInteractionStreamHandler) SendInteractions(stream stdgrpc.ServerStream) error {
	var ack any = &rtiv1.Empty{}
	if h.preparedAck {
		prepared := new(stdgrpc.PreparedMsg)
		if err := prepared.Encode(stream, ack); err != nil {
			return err
		}
		ack = prepared
	}
	for {
		request := new(rtiv1.SendInteractionRequest)
		if err := stream.RecvMsg(request); err != nil {
			return err
		}
		if err := stream.SendMsg(ack); err != nil {
			return err
		}
	}
}

var benchmarkInteractionStreamDescription = stdgrpc.ServiceDesc{
	ServiceName: "benchmark.InteractionStream",
	HandlerType: (*benchmarkInteractionStreamService)(nil),
	Streams: []stdgrpc.StreamDesc{{
		StreamName:    "SendInteractions",
		Handler:       benchmarkInteractionStreamServerHandler,
		ServerStreams: true,
		ClientStreams: true,
	}},
}

func benchmarkInteractionStreamServerHandler(service any, stream stdgrpc.ServerStream) error {
	return service.(benchmarkInteractionStreamService).SendInteractions(stream)
}

func (r *benchmarkObjectRegistry) SendInteraction(
	context.Context,
	core.FederationName,
	core.FederateHandle,
	core.InteractionClassHandle,
	map[core.ParameterHandle][]byte,
	*core.LogicalTime,
) error {
	return nil
}

func benchmarkInteractionRequest() *rtiv1.SendInteractionRequest {
	timestamp := 1.0
	return &rtiv1.SendInteractionRequest{
		WireVersion:            rtiv1.WireVersion_WIRE_VERSION_V1,
		FederationName:         "benchmark",
		FederateHandle:         1,
		InteractionClassHandle: 7,
		Parameters: map[uint64][]byte{
			3: {0, 0, 0, 1},
			4: {0, 16, '0', '1', '2', '3', '4', '5', '6', '7'},
		},
		LogicalTime: &timestamp,
	}
}

func BenchmarkSendInteractionDirectHandler(b *testing.B) {
	service := newObjectService(&benchmarkObjectRegistry{&stubObjectRegistry{}})
	request := benchmarkInteractionRequest()
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		if _, err := service.SendInteraction(ctx, request); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSendInteractionUnaryTCP(b *testing.B) {
	benchmarkSendInteractionUnaryTCP(b, nil, nil)
}

func BenchmarkSendInteractionUnaryTCPNoBuffers(b *testing.B) {
	benchmarkSendInteractionUnaryTCP(
		b,
		[]stdgrpc.ServerOption{stdgrpc.ReadBufferSize(0), stdgrpc.WriteBufferSize(0)},
		[]stdgrpc.DialOption{stdgrpc.WithReadBufferSize(0), stdgrpc.WithWriteBufferSize(0)},
	)
}

func BenchmarkSendInteractionUnaryTCPSharedWriteBuffer(b *testing.B) {
	benchmarkSendInteractionUnaryTCP(
		b,
		[]stdgrpc.ServerOption{stdgrpc.SharedWriteBuffer(true)},
		[]stdgrpc.DialOption{stdgrpc.WithSharedWriteBuffer(true)},
	)
}

func BenchmarkSendInteractionPersistentStreamTCP(b *testing.B) {
	benchmarkSendInteractionPersistentStreamTCP(b, nil, nil, false)
}

func BenchmarkSendInteractionPersistentStreamTCPSharedWriteBuffer(b *testing.B) {
	benchmarkSendInteractionPersistentStreamTCP(
		b,
		[]stdgrpc.ServerOption{stdgrpc.SharedWriteBuffer(true)},
		[]stdgrpc.DialOption{stdgrpc.WithSharedWriteBuffer(true)},
		false,
	)
}

func BenchmarkSendInteractionPersistentStreamTCPPreparedAck(b *testing.B) {
	benchmarkSendInteractionPersistentStreamTCP(b, nil, nil, true)
}

func benchmarkSendInteractionPersistentStreamTCP(
	b *testing.B,
	serverOptions []stdgrpc.ServerOption,
	dialOptions []stdgrpc.DialOption,
	preparedAck bool,
) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		b.Fatal(err)
	}
	server := stdgrpc.NewServer(serverOptions...)
	server.RegisterService(
		&benchmarkInteractionStreamDescription,
		benchmarkInteractionStreamHandler{preparedAck: preparedAck},
	)
	go func() { _ = server.Serve(listener) }()
	b.Cleanup(func() {
		server.Stop()
		_ = listener.Close()
	})

	dialOptions = append(
		[]stdgrpc.DialOption{stdgrpc.WithTransportCredentials(insecure.NewCredentials())},
		dialOptions...,
	)
	connection, err := stdgrpc.NewClient(listener.Addr().String(), dialOptions...)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = connection.Close() })
	stream, err := connection.NewStream(
		context.Background(),
		&benchmarkInteractionStreamDescription.Streams[0],
		"/benchmark.InteractionStream/SendInteractions",
	)
	if err != nil {
		b.Fatal(err)
	}
	request := benchmarkInteractionRequest()
	if err := stream.SendMsg(request); err != nil {
		b.Fatal(err)
	}
	if err := stream.RecvMsg(new(rtiv1.Empty)); err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		if err := stream.SendMsg(request); err != nil {
			b.Fatal(err)
		}
		if err := stream.RecvMsg(new(rtiv1.Empty)); err != nil {
			b.Fatal(err)
		}
	}
}

func benchmarkSendInteractionUnaryTCP(
	b *testing.B, serverOptions []stdgrpc.ServerOption, dialOptions []stdgrpc.DialOption,
) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		b.Fatal(err)
	}
	server := stdgrpc.NewServer(serverOptions...)
	rtiv1.RegisterObjectServiceServer(
		server, newObjectService(&benchmarkObjectRegistry{&stubObjectRegistry{}}),
	)
	go func() { _ = server.Serve(listener) }()
	b.Cleanup(func() {
		server.Stop()
		_ = listener.Close()
	})

	dialOptions = append(
		[]stdgrpc.DialOption{stdgrpc.WithTransportCredentials(insecure.NewCredentials())},
		dialOptions...,
	)
	connection, err := stdgrpc.NewClient(listener.Addr().String(), dialOptions...)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = connection.Close() })
	client := rtiv1.NewObjectServiceClient(connection)
	request := benchmarkInteractionRequest()
	ctx := context.Background()
	if _, err := client.SendInteraction(ctx, request); err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		if _, err := client.SendInteraction(ctx, request); err != nil {
			b.Fatal(err)
		}
	}
}
