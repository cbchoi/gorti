package federate

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/genproto/rti/v1"
)

type captureInteractionClient struct {
	rtiv1.ObjectServiceClient
	send *rtiv1.SendInteractionRequest
}

const interactionStreamFOM = `<?xml version="1.0" encoding="UTF-8"?>
<objectModel xmlns="http://standards.ieee.org/IEEE1516-2010">
  <modelIdentification><name>interaction-stream</name><type>FOM</type></modelIdentification>
  <objects><objectClass><name>HLAobjectRoot</name></objectClass></objects>
  <interactions>
    <interactionClass>
      <name>HLAinteractionRoot</name>
      <interactionClass>
        <name>Tick</name>
        <sharing>PublishSubscribe</sharing>
        <transportation>HLAreliable</transportation>
        <order>Receive</order>
        <parameter><name>seq</name><dataType>HLAinteger32BE</dataType></parameter>
      </interactionClass>
    </interactionClass>
  </interactions>
</objectModel>`

type unaryOnlyInteractionServer struct {
	rtiv1.UnimplementedObjectServiceServer
	sends int
}

type ackBoundaryServer struct {
	rtiv1.UnimplementedFederationServiceServer
	received chan struct{}
	release  chan struct{}
	resigned chan struct{}
}

func (s *ackBoundaryServer) ResignFederation(context.Context, *rtiv1.ResignFederationRequest) (*rtiv1.Empty, error) {
	close(s.resigned)
	return &rtiv1.Empty{}, nil
}

func (s *ackBoundaryServer) SendInteractions(stream grpc.ServerStream) error {
	if err := stream.SendHeader(nil); err != nil {
		return err
	}
	handshake := new(rtiv1.SendInteractionRequest)
	if err := stream.RecvMsg(handshake); err != nil {
		return err
	}
	if err := stream.SendMsg(&rtiv1.Empty{}); err != nil {
		return err
	}
	request := new(rtiv1.SendInteractionRequest)
	if err := stream.RecvMsg(request); err != nil {
		return err
	}
	close(s.received)
	<-s.release
	if err := stream.SendMsg(&rtiv1.Empty{}); err != nil {
		return err
	}
	return nil
}

type ackBoundaryInteractionService interface {
	SendInteractions(grpc.ServerStream) error
}

var ackBoundaryInteractionDescription = grpc.ServiceDesc{
	ServiceName: "rti.v1.InteractionStreamService",
	HandlerType: (*ackBoundaryInteractionService)(nil),
	Streams: []grpc.StreamDesc{{
		StreamName: "SendInteractions", ClientStreams: true, ServerStreams: true,
		Handler: func(service any, stream grpc.ServerStream) error {
			return service.(ackBoundaryInteractionService).SendInteractions(stream)
		},
	}},
}

func (s *unaryOnlyInteractionServer) SendInteraction(
	_ context.Context, _ *rtiv1.SendInteractionRequest,
) (*rtiv1.Empty, error) {
	s.sends++
	return &rtiv1.Empty{}, nil
}

func (c *captureInteractionClient) SendInteraction(
	_ context.Context, in *rtiv1.SendInteractionRequest, _ ...grpc.CallOption,
) (*rtiv1.Empty, error) {
	c.send = in
	return &rtiv1.Empty{}, nil
}

func interactionTables() *fomTables {
	return &fomTables{
		interactionByName:   map[string]uint64{"Tick": 7},
		interactionByHandle: map[uint64]string{7: "Tick"},
		paramByName: map[string]map[string]uint64{
			"Tick": {"seq": 3, "source": 4},
		},
	}
}

func TestInteractionHandleLookups(t *testing.T) {
	fed := &Federate{handles: interactionTables()}

	classHandle, found := fed.InteractionClassHandle("Tick")
	if !found || classHandle != 7 {
		t.Fatalf("InteractionClassHandle(Tick) = (%d, %v), want (7, true)", classHandle, found)
	}
	parameterHandle, found := fed.ParameterHandle(classHandle, "seq")
	if !found || parameterHandle != 3 {
		t.Fatalf("ParameterHandle(7, seq) = (%d, %v), want (3, true)", parameterHandle, found)
	}

	if handle, ok := fed.InteractionClassHandle("Missing"); ok || handle != 0 {
		t.Fatalf("InteractionClassHandle(Missing) = (%d, %v), want (0, false)", handle, ok)
	}
	if handle, ok := fed.ParameterHandle(classHandle, "missing"); ok || handle != 0 {
		t.Fatalf("ParameterHandle(7, missing) = (%d, %v), want (0, false)", handle, ok)
	}
	if handle, ok := fed.ParameterHandle(999, "seq"); ok || handle != 0 {
		t.Fatalf("ParameterHandle(999, seq) = (%d, %v), want (0, false)", handle, ok)
	}
}

func TestSendInteractionByHandle(t *testing.T) {
	client := &captureInteractionClient{}
	fed := &Federate{
		conn:           &Connection{obj: client},
		federationName: "benchmark",
		federateHandle: 11,
	}
	parameters := map[uint64][]byte{3: {1, 2, 3}}
	timestamp := 0.0

	if err := fed.SendInteractionByHandle(context.Background(), 7, parameters, &timestamp); err != nil {
		t.Fatalf("SendInteractionByHandle: %v", err)
	}
	if client.send.GetFederationName() != "benchmark" || client.send.GetFederateHandle() != 11 {
		t.Fatalf("send identity = %#v", client.send)
	}
	if client.send.GetInteractionClassHandle() != 7 {
		t.Fatalf("class handle = %d, want 7", client.send.GetInteractionClassHandle())
	}
	if got := client.send.GetParameters()[3]; len(got) != 3 || got[0] != 1 {
		t.Fatalf("parameters[3] = %v, want [1 2 3]", got)
	}
	if client.send.LogicalTime == nil || client.send.GetLogicalTime() != 0 {
		t.Fatalf("logical time = %v, want present zero", client.send.LogicalTime)
	}
}

func TestSendInteractionResolvesNamesAndDelegates(t *testing.T) {
	client := &captureInteractionClient{}
	fed := &Federate{
		conn:           &Connection{obj: client},
		federationName: "benchmark",
		federateHandle: 11,
		handles:        interactionTables(),
	}

	err := fed.SendInteraction(context.Background(), "Tick", map[string][]byte{
		"seq":     {9},
		"unknown": {8},
	}, nil)
	if err != nil {
		t.Fatalf("SendInteraction: %v", err)
	}
	if client.send.GetInteractionClassHandle() != 7 {
		t.Fatalf("class handle = %d, want 7", client.send.GetInteractionClassHandle())
	}
	if got := client.send.GetParameters(); len(got) != 1 || len(got[3]) != 1 || got[3][0] != 9 {
		t.Fatalf("parameters = %v, want only handle 3", got)
	}
	if client.send.LogicalTime != nil {
		t.Fatalf("receive-order logical time = %v, want nil", client.send.LogicalTime)
	}

	client.send = nil
	err = fed.SendInteraction(context.Background(), "Missing", nil, nil)
	if err == nil || !strings.Contains(err.Error(), `interaction class "Missing"`) {
		t.Fatalf("unknown class error = %v", err)
	}
	if client.send != nil {
		t.Fatal("unknown class unexpectedly sent an interaction")
	}
}

func TestSendInteractionStreamFallsBackToUnaryServer(t *testing.T) {
	listener := bufconn.Listen(1 << 20)
	server := grpc.NewServer()
	objects := &unaryOnlyInteractionServer{}
	rtiv1.RegisterObjectServiceServer(server, objects)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() {
		server.Stop()
		_ = listener.Close()
	})
	connection, err := grpc.NewClient(
		"passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return listener.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	fed := &Federate{conn: &Connection{
		cc: connection, obj: rtiv1.NewObjectServiceClient(connection), interactionStreamEnabled: true,
	}}

	for range 2 {
		if err := fed.SendInteractionByHandle(context.Background(), 7, nil, nil); err != nil {
			t.Fatalf("SendInteractionByHandle: %v", err)
		}
	}
	if !fed.interactionStreamUnsupported {
		t.Fatal("server without stream support was not cached as unary-only")
	}
	if objects.sends != 2 {
		t.Fatalf("unary sends = %d, want 2", objects.sends)
	}
	stats := fed.InteractionTransportStats()
	if stats.Total != 2 || stats.UnarySent != 2 || stats.UnaryAcked != 2 ||
		stats.StreamSent != 0 || stats.FallbackUnsupported != 2 {
		t.Fatalf("fallback transport stats = %+v", stats)
	}
}

func TestSendInteractionStreamReusesConnectionAndResigns(t *testing.T) {
	rtid := newTestRtid(t)
	connection := rtid.connect(t)
	connection.interactionStreamEnabled = true
	t.Cleanup(func() { _ = connection.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	fed, err := connection.JoinFederation(ctx, FederationSpec{
		Name: "interaction-stream", FOMModules: []FOMModule{{Path: "stream.xml", XML: []byte(interactionStreamFOM)}},
	}, "sender")
	if err != nil {
		t.Fatal(err)
	}
	if err := fed.PublishInteractionClass(ctx, "Tick"); err != nil {
		t.Fatal(err)
	}
	if err := fed.SendInteraction(ctx, "Tick", map[string][]byte{"seq": {1}}, nil); err != nil {
		t.Fatalf("SendInteraction(1): %v", err)
	}
	firstStream := fed.interactionStream
	if firstStream == nil {
		t.Fatal("first interaction did not open a stream")
	}
	if err := fed.SendInteraction(ctx, "Tick", map[string][]byte{"seq": {2}}, nil); err != nil {
		t.Fatalf("SendInteraction(2): %v", err)
	}
	if fed.interactionStream != firstStream {
		t.Fatal("second interaction did not reuse the first stream")
	}
	if fed.interactionStream == nil || fed.interactionStreamUnsupported {
		t.Fatal("persistent interaction stream was not reused")
	}
	stats := fed.InteractionTransportStats()
	if stats.Total != 2 || stats.StreamSent != 2 || stats.StreamAcked != 2 ||
		stats.OpenAttempts != 1 || stats.OpenSuccesses != 1 || stats.UnarySent != 0 ||
		stats.Resets != 0 || stats.Indeterminate != 0 {
		t.Fatalf("persistent transport stats = %+v", stats)
	}
	if err := fed.Resign(ctx); err != nil {
		t.Fatal(err)
	}
	if err := fed.SendInteraction(ctx, "Tick", nil, nil); !errors.Is(err, ErrNotJoined) {
		t.Fatalf("post-resign interaction error = %v, want ErrNotJoined", err)
	}
}

func TestP0ResignWaitsForWireACK(t *testing.T) {
	listener := bufconn.Listen(1 << 20)
	server := grpc.NewServer()
	service := &ackBoundaryServer{
		received: make(chan struct{}), release: make(chan struct{}), resigned: make(chan struct{}),
	}
	rtiv1.RegisterFederationServiceServer(server, service)
	server.RegisterService(&ackBoundaryInteractionDescription, service)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() {
		server.Stop()
		_ = listener.Close()
	})
	cc, err := grpc.NewClient(
		"passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return listener.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cc.Close() })
	interactionContext, interactionCancel := context.WithCancel(context.Background())
	drainDone := make(chan struct{})
	close(drainDone)
	fed := &Federate{
		conn:           &Connection{cc: cc, fed: rtiv1.NewFederationServiceClient(cc), interactionStreamEnabled: true},
		federationName: "fed", federateHandle: 1,
		interactionContext: interactionContext, interactionCancel: interactionCancel,
		streamCancel: func() {}, drainDone: drainDone,
	}
	sendDone := make(chan error, 1)
	go func() { sendDone <- fed.SendInteractionByHandle(context.Background(), 7, nil, nil) }()
	select {
	case <-service.received:
	case <-time.After(time.Second):
		t.Fatal("server did not receive interaction")
	}
	resignDone := make(chan error, 1)
	go func() { resignDone <- fed.ResignWithAction(context.Background(), ResignActionNoAction) }()
	deadline := time.Now().Add(time.Second)
	for !fed.interactionClosing.Load() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !fed.interactionClosing.Load() {
		t.Fatal("resign did not close interaction admission")
	}
	if err := fed.SendInteractionByHandle(context.Background(), 7, nil, nil); !errors.Is(err, ErrNotJoined) {
		t.Fatalf("send during drain = %v, want ErrNotJoined", err)
	}
	select {
	case <-service.resigned:
		t.Fatal("resign RPC crossed unacknowledged interaction")
	default:
	}
	close(service.release)
	if err := <-sendDone; err != nil {
		t.Fatalf("interaction after ACK: %v", err)
	}
	select {
	case <-service.resigned:
	case <-time.After(time.Second):
		t.Fatal("resign RPC did not follow interaction ACK")
	}
	if err := <-resignDone; err != nil {
		t.Fatal(err)
	}
}

func TestDrainAndCloseInteractionStreamWaitsForAckBoundary(t *testing.T) {
	interactionContext, cancel := context.WithCancel(context.Background())
	defer cancel()
	fed := &Federate{interactionContext: interactionContext, interactionCancel: cancel}
	fed.interactionStreamMu.Lock()
	done := make(chan error, 1)
	go func() { done <- fed.drainAndCloseInteractionStream(context.Background()) }()
	select {
	case err := <-done:
		t.Fatalf("drain returned before active send boundary: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	fed.interactionStreamMu.Unlock()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("drain did not complete after ACK boundary")
	}
}

func TestDrainAndCloseInteractionStreamDeadlineCancelsActiveTransport(t *testing.T) {
	interactionContext, cancel := context.WithCancel(context.Background())
	fed := &Federate{interactionContext: interactionContext, interactionCancel: cancel}
	fed.interactionStreamMu.Lock()
	ctx, stop := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer stop()
	done := make(chan error, 1)
	go func() { done <- fed.drainAndCloseInteractionStream(ctx) }()
	select {
	case <-interactionContext.Done():
	case <-time.After(time.Second):
		t.Fatal("deadline did not cancel active interaction transport")
	}
	fed.interactionStreamMu.Unlock()
	select {
	case err := <-done:
		if status.Code(err) != codes.DeadlineExceeded {
			t.Fatalf("drain error = %v, want DeadlineExceeded", err)
		}
	case <-time.After(time.Second):
		t.Fatal("deadline drain leaked its lock waiter")
	}
}
