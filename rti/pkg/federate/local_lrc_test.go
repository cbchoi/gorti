package federate

import (
	"context"
	"errors"
	"io"
	"net"
	"reflect"
	"sync"
	"testing"
	"time"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/genproto/rti/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

type recordingLocalLRCServer struct {
	rtiv1.UnimplementedLocalLRCServiceServer

	ackEvery       uint32
	maxBatch       uint32
	requestedBatch uint32
	firstSeen      chan struct{}
	releaseFirst   chan struct{}
	failFirst      bool

	firstOnce sync.Once
	mu        sync.Mutex
	requests  []*rtiv1.LocalLRCRequest
	frames    int
}

type resignBoundaryLocalLRCServer struct {
	rtiv1.UnimplementedLocalLRCServiceServer
	rtiv1.UnimplementedFederationServiceServer

	operationSeen chan struct{}
	flushSeen     chan struct{}
	releaseACK    chan struct{}
	ackSent       chan struct{}
	exchangeDone  chan struct{}
	resigned      chan struct{}
	resignOnce    sync.Once
}

func newResignBoundaryLocalLRCServer() *resignBoundaryLocalLRCServer {
	return &resignBoundaryLocalLRCServer{
		operationSeen: make(chan struct{}),
		flushSeen:     make(chan struct{}),
		releaseACK:    make(chan struct{}),
		ackSent:       make(chan struct{}),
		exchangeDone:  make(chan struct{}),
		resigned:      make(chan struct{}),
	}
}

func (s *resignBoundaryLocalLRCServer) Exchange(stream rtiv1.LocalLRCService_ExchangeServer) error {
	defer close(s.exchangeDone)
	open, err := stream.Recv()
	if err != nil {
		return err
	}
	if open.GetOpen() == nil {
		return status.Error(codes.InvalidArgument, "missing open")
	}
	if err := stream.Send(&rtiv1.LocalLRCAck{AckEvery: 32, MaxBatchOperations: 32}); err != nil {
		return err
	}

	operation, err := stream.Recv()
	if err != nil {
		return err
	}
	if sequence := localLRCRequestSequence(operation); sequence != 1 {
		return status.Errorf(codes.FailedPrecondition, "operation sequence = %d, want 1", sequence)
	}
	close(s.operationSeen)

	flushRequest, err := stream.Recv()
	if err != nil {
		return err
	}
	flush := flushRequest.GetFlush()
	if flush == nil || flush.GetThroughSequence() != 1 {
		return status.Error(codes.FailedPrecondition, "missing flush through sequence 1")
	}
	close(s.flushSeen)
	select {
	case <-s.releaseACK:
	case <-stream.Context().Done():
		return status.FromContextError(stream.Context().Err()).Err()
	}
	if err := stream.Send(&rtiv1.LocalLRCAck{CommittedThrough: 1}); err != nil {
		return err
	}
	close(s.ackSent)

	_, err = stream.Recv()
	if errors.Is(err, io.EOF) || status.Code(err) == codes.Canceled {
		return nil
	}
	return err
}

func (s *resignBoundaryLocalLRCServer) ResignFederation(
	context.Context,
	*rtiv1.ResignFederationRequest,
) (*rtiv1.Empty, error) {
	s.resignOnce.Do(func() { close(s.resigned) })
	return &rtiv1.Empty{}, nil
}

type manualDoneContext struct {
	context.Context
	done chan struct{}
	err  error
}

func newManualDoneContext(err error) (*manualDoneContext, func()) {
	ctx := &manualDoneContext{Context: context.Background(), done: make(chan struct{}), err: err}
	var once sync.Once
	return ctx, func() { once.Do(func() { close(ctx.done) }) }
}

func (c *manualDoneContext) Done() <-chan struct{} { return c.done }

func (c *manualDoneContext) Err() error {
	select {
	case <-c.done:
		return c.err
	default:
		return nil
	}
}

func (s *recordingLocalLRCServer) Exchange(stream rtiv1.LocalLRCService_ExchangeServer) error {
	open, err := stream.Recv()
	if err != nil {
		return err
	}
	if open.GetOpen() == nil {
		return status.Error(codes.InvalidArgument, "missing open")
	}
	s.mu.Lock()
	s.requestedBatch = open.GetOpen().GetRequestedMaxBatchOperations()
	s.mu.Unlock()
	if err := stream.Send(&rtiv1.LocalLRCAck{
		AckEvery: s.ackEvery, MaxBatchOperations: s.maxBatch,
	}); err != nil {
		return err
	}

	var committed, lastAck uint64
	for {
		request, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if flush := request.GetFlush(); flush != nil {
			if flush.GetThroughSequence() > committed {
				return status.Error(codes.FailedPrecondition, "flush exceeds committed")
			}
			if err := stream.Send(&rtiv1.LocalLRCAck{CommittedThrough: committed}); err != nil {
				return err
			}
			lastAck = committed
			continue
		}

		operations := localLRCFrameOperations(request)
		if len(operations) == 0 {
			return status.Error(codes.InvalidArgument, "operation frame is empty")
		}
		s.mu.Lock()
		s.frames++
		s.mu.Unlock()
		for _, operation := range operations {
			sequence := localLRCRequestSequence(operation)
			s.mu.Lock()
			s.requests = append(s.requests, operation)
			s.mu.Unlock()
			s.firstOnce.Do(func() {
				if s.firstSeen != nil {
					close(s.firstSeen)
				}
				if s.releaseFirst != nil {
					<-s.releaseFirst
				}
			})
			if s.failFirst && committed == 0 {
				return status.Error(codes.FailedPrecondition, "interaction class not published")
			}
			if sequence != committed+1 {
				return status.Error(codes.FailedPrecondition, "sequence gap")
			}
			committed = sequence
			if committed-lastAck >= uint64(s.ackEvery) {
				if err := stream.Send(&rtiv1.LocalLRCAck{CommittedThrough: committed}); err != nil {
					return err
				}
				lastAck = committed
			}
		}
	}
}

func localLRCFrameOperations(request *rtiv1.LocalLRCRequest) []*rtiv1.LocalLRCRequest {
	if batch := request.GetBatch(); batch != nil {
		operations := make([]*rtiv1.LocalLRCRequest, 0, len(batch.GetOperations()))
		for _, operation := range batch.GetOperations() {
			operations = append(operations, localLRCRequestForOperation(operation))
		}
		return operations
	}
	if localLRCRequestSequence(request) != 0 {
		return []*rtiv1.LocalLRCRequest{request}
	}
	return nil
}

func localLRCRequestSequence(request *rtiv1.LocalLRCRequest) uint64 {
	if update := request.GetAttributeUpdate(); update != nil {
		return update.GetSequence()
	}
	if interaction := request.GetInteraction(); interaction != nil {
		return interaction.GetSequence()
	}
	return 0
}

func (s *recordingLocalLRCServer) snapshot() []*rtiv1.LocalLRCRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]*rtiv1.LocalLRCRequest(nil), s.requests...)
}

func (s *recordingLocalLRCServer) frameCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.frames
}

func (s *recordingLocalLRCServer) requestedBatchSize() uint32 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.requestedBatch
}

func newLocalLRCTestFederate(
	t *testing.T,
	server rtiv1.LocalLRCServiceServer,
	queueCapacity int,
) *Federate {
	t.Helper()
	listener := bufconn.Listen(1 << 20)
	grpcServer := grpc.NewServer()
	rtiv1.RegisterLocalLRCServiceServer(grpcServer, server)
	if federationServer, ok := server.(rtiv1.FederationServiceServer); ok {
		rtiv1.RegisterFederationServiceServer(grpcServer, federationServer)
	}
	go func() { _ = grpcServer.Serve(listener) }()
	t.Cleanup(func() {
		grpcServer.Stop()
		_ = listener.Close()
	})

	cc, err := grpc.NewClient(
		"passthrough:///local-lrc-test",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return listener.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cc.Close() })
	connection := WrapGRPCClientConn(cc)
	connection.localLRCQueueCapacity = queueCapacity
	connection.localLRCAckEvery = uint32(queueCapacity)
	fed := &Federate{
		conn:                 connection,
		federationName:       "fed",
		federateHandle:       7,
		federationGeneration: 9,
		handles:              testLocalStateTables(),
		streamCancel:         func() {},
		drainDone:            make(chan struct{}),
	}
	close(fed.drainDone)
	fed.rememberPublishedObjectAttributes(10, []uint64{2})
	fed.rememberPublishedInteraction(12)
	fed.rememberRegisteredObject(11, 10)
	return fed
}

func waitLocalLRCSignal(t *testing.T, ctx context.Context, name string, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-ctx.Done():
		t.Fatalf("timed out waiting for %s: %v", name, ctx.Err())
	}
}

func requireLocalLRCSignalOpen(t *testing.T, name string, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
		t.Fatalf("%s completed before the LocalLRC flush ACK", name)
	default:
	}
}

func TestLocalLRCQueuesMixedOperationsAndFlushesCumulativeAck(t *testing.T) {
	server := &recordingLocalLRCServer{
		ackEvery: 2, firstSeen: make(chan struct{}), releaseFirst: make(chan struct{}),
	}
	fed := newLocalLRCTestFederate(t, server, 4)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	attributes := map[uint64][]byte{2: {3, 4}}
	first, err := fed.QueueAttributeValuesByHandle(ctx, 11, attributes)
	if err != nil {
		t.Fatal(err)
	}
	if first != 1 {
		t.Fatalf("first sequence = %d, want 1", first)
	}
	select {
	case <-server.firstSeen:
	case <-ctx.Done():
		t.Fatal("server did not receive first queued operation")
	}
	if got := fed.LocalLRCStats().Acked; got != 0 {
		t.Fatalf("ACK advanced through blocked operation: %d", got)
	}

	second, err := fed.QueueInteractionByHandle(ctx, 12, map[uint64][]byte{5: {6}})
	if err != nil {
		t.Fatal(err)
	}
	if second != 2 {
		t.Fatalf("second sequence = %d, want 2", second)
	}
	attributes[2][0] = 99
	close(server.releaseFirst)
	if err := fed.FlushLocalLRC(ctx); err != nil {
		t.Fatal(err)
	}

	requests := server.snapshot()
	if len(requests) != 2 {
		t.Fatalf("operation count = %d, want 2", len(requests))
	}
	if requests[0].GetAttributeUpdate() == nil || requests[1].GetInteraction() == nil {
		t.Fatalf("operation order = %T, %T", requests[0].GetBody(), requests[1].GetBody())
	}
	if got := requests[0].GetAttributeUpdate().GetAttributes()[2]; !reflect.DeepEqual(got, []byte{3, 4}) {
		t.Fatalf("owned attribute payload = %v, want [3 4]", got)
	}
	stats := fed.LocalLRCStats()
	if stats.Enqueued != 2 || stats.Sent != 2 || stats.Acked != 2 || stats.Flushes != 1 {
		t.Fatalf("LocalLRC stats = %+v", stats)
	}
	if err := fed.closeLocalLRC(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestFederateResignWaitsForLocalLRCFlushACKAndStopsStream(t *testing.T) {
	server := newResignBoundaryLocalLRCServer()
	fed := newLocalLRCTestFederate(t, server, 4)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if _, err := fed.QueueInteractionByHandle(ctx, 12, nil); err != nil {
		t.Fatal(err)
	}
	waitLocalLRCSignal(t, ctx, "queued operation", server.operationSeen)
	lrc := fed.localLRC
	if lrc == nil {
		t.Fatal("queued operation did not open LocalLRC")
	}

	resignDone := make(chan error, 1)
	go func() { resignDone <- fed.Resign(ctx) }()
	waitLocalLRCSignal(t, ctx, "resign flush", server.flushSeen)
	requireLocalLRCSignalOpen(t, "wire resign", server.resigned)
	requireLocalLRCSignalOpen(t, "LocalLRC sender", lrc.sendDone)
	requireLocalLRCSignalOpen(t, "LocalLRC receiver", lrc.recvDone)
	requireLocalLRCSignalOpen(t, "LocalLRC server stream", server.exchangeDone)

	close(server.releaseACK)
	waitLocalLRCSignal(t, ctx, "flush ACK", server.ackSent)
	select {
	case err := <-resignDone:
		if err != nil {
			t.Fatalf("Resign: %v", err)
		}
	case <-ctx.Done():
		t.Fatalf("Resign did not finish after flush ACK: %v", ctx.Err())
	}
	waitLocalLRCSignal(t, ctx, "wire resign", server.resigned)
	waitLocalLRCSignal(t, ctx, "LocalLRC sender shutdown", lrc.sendDone)
	waitLocalLRCSignal(t, ctx, "LocalLRC receiver shutdown", lrc.recvDone)
	waitLocalLRCSignal(t, ctx, "LocalLRC server stream shutdown", server.exchangeDone)
	if got := fed.LocalLRCStats().Acked; got != 1 {
		t.Fatalf("acked sequence = %d, want 1", got)
	}
}

func TestFederateResignCanceledLocalLRCReturnsContextErrorAndStopsStream(t *testing.T) {
	for _, cause := range []error{context.Canceled, context.DeadlineExceeded} {
		t.Run(cause.Error(), func(t *testing.T) {
			server := newResignBoundaryLocalLRCServer()
			fed := newLocalLRCTestFederate(t, server, 4)
			waitCtx, cancelWait := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancelWait()

			if _, err := fed.QueueInteractionByHandle(waitCtx, 12, nil); err != nil {
				t.Fatal(err)
			}
			waitLocalLRCSignal(t, waitCtx, "queued operation", server.operationSeen)
			lrc := fed.localLRC
			if lrc == nil {
				t.Fatal("queued operation did not open LocalLRC")
			}

			resignCtx, trigger := newManualDoneContext(cause)
			resignDone := make(chan error, 1)
			go func() { resignDone <- fed.Resign(resignCtx) }()
			waitLocalLRCSignal(t, waitCtx, "resign flush", server.flushSeen)
			requireLocalLRCSignalOpen(t, "wire resign", server.resigned)
			trigger()

			select {
			case err := <-resignDone:
				if !errors.Is(err, cause) {
					t.Fatalf("Resign error = %v, want %v", err, cause)
				}
			case <-waitCtx.Done():
				t.Fatalf("canceled Resign did not finish: %v", waitCtx.Err())
			}
			waitLocalLRCSignal(t, waitCtx, "best-effort wire resign", server.resigned)
			waitLocalLRCSignal(t, waitCtx, "LocalLRC sender shutdown", lrc.sendDone)
			waitLocalLRCSignal(t, waitCtx, "LocalLRC receiver shutdown", lrc.recvDone)
			waitLocalLRCSignal(t, waitCtx, "LocalLRC server stream shutdown", server.exchangeDone)
			if got := fed.LocalLRCStats().Acked; got != 0 {
				t.Fatalf("acked sequence = %d after %v, want 0", got, cause)
			}
		})
	}
}

func TestLocalLRCAdmissionBlocksAtUnackedCapacity(t *testing.T) {
	server := &recordingLocalLRCServer{
		ackEvery: 1, firstSeen: make(chan struct{}), releaseFirst: make(chan struct{}),
	}
	fed := newLocalLRCTestFederate(t, server, 2)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if _, err := fed.QueueInteractionByHandle(ctx, 12, nil); err != nil {
		t.Fatal(err)
	}
	select {
	case <-server.firstSeen:
	case <-ctx.Done():
		t.Fatal("server did not receive first operation")
	}
	if _, err := fed.QueueInteractionByHandle(ctx, 12, nil); err != nil {
		t.Fatal(err)
	}
	thirdDone := make(chan error, 1)
	go func() {
		_, err := fed.QueueInteractionByHandle(ctx, 12, nil)
		thirdDone <- err
	}()
	select {
	case err := <-thirdDone:
		t.Fatalf("third admission crossed two-operation capacity: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	close(server.releaseFirst)
	select {
	case err := <-thirdDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("cumulative ACK did not release admission capacity")
	}
	if err := fed.FlushLocalLRC(ctx); err != nil {
		t.Fatal(err)
	}
	if err := fed.closeLocalLRC(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestLocalLRCAwaitReturnsExactOperationFailure(t *testing.T) {
	server := &recordingLocalLRCServer{ackEvery: 1, firstSeen: make(chan struct{}), failFirst: true}
	fed := newLocalLRCTestFederate(t, server, 2)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	sequence, err := fed.QueueInteractionByHandle(ctx, 12, nil)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-server.firstSeen:
	case <-ctx.Done():
		t.Fatal("server did not receive failed operation")
	}
	deadline := time.Now().Add(time.Second)
	for fed.LocalLRCStats().Failures == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if err := fed.AwaitLocalLRC(ctx, sequence); !errors.Is(err, ErrInteractionClassNotPublished) {
		t.Fatalf("AwaitLocalLRC error = %v, want ErrInteractionClassNotPublished", err)
	}
	if err := fed.FlushLocalLRC(ctx); !errors.Is(err, ErrInteractionClassNotPublished) {
		t.Fatalf("FlushLocalLRC error = %v, want ErrInteractionClassNotPublished", err)
	}
}

func TestLocalLRCAwaitRejectsUnadmittedSequence(t *testing.T) {
	server := &recordingLocalLRCServer{ackEvery: 1}
	fed := newLocalLRCTestFederate(t, server, 2)
	if err := fed.AwaitLocalLRC(context.Background(), 1); !errors.Is(err, ErrLocalLRCSequenceNotAdmitted) {
		t.Fatalf("AwaitLocalLRC error = %v, want ErrLocalLRCSequenceNotAdmitted", err)
	}
}

func TestLocalLRCRejectsInvalidWorkBeforeOpeningStream(t *testing.T) {
	server := &recordingLocalLRCServer{ackEvery: 1}
	fed := newLocalLRCTestFederate(t, server, 2)

	fed.mu.Lock()
	delete(fed.publishedInteractions, 12)
	fed.mu.Unlock()
	if sequence, err := fed.QueueInteractionByHandle(context.Background(), 12, nil); sequence != 0 || !errors.Is(err, ErrInteractionClassNotPublished) {
		t.Fatalf("unpublished interaction = (%d, %v)", sequence, err)
	}
	if fed.localLRC != nil {
		t.Fatal("invalid work opened the LocalLRC stream")
	}

	if sequence, err := fed.QueueAttributeValuesByHandle(context.Background(), 99, map[uint64][]byte{2: nil}); sequence != 0 || !errors.Is(err, ErrObjectInstanceNotKnown) {
		t.Fatalf("unknown object update = (%d, %v)", sequence, err)
	}
}

func TestStandardReceiveOrderAPIsUseLocalLRCByDefault(t *testing.T) {
	server := &recordingLocalLRCServer{ackEvery: 2}
	fed := newLocalLRCTestFederate(t, server, 4)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := fed.UpdateAttributeValuesByHandle(ctx, 11, map[uint64][]byte{2: {3}}, nil); err != nil {
		t.Fatalf("UpdateAttributeValuesByHandle: %v", err)
	}
	if err := fed.SendInteractionByHandle(ctx, 12, map[uint64][]byte{5: {6}}, nil); err != nil {
		t.Fatalf("SendInteractionByHandle: %v", err)
	}
	if err := fed.FlushLocalLRC(ctx); err != nil {
		t.Fatalf("FlushLocalLRC: %v", err)
	}

	requests := server.snapshot()
	if len(requests) != 2 || requests[0].GetAttributeUpdate() == nil || requests[1].GetInteraction() == nil {
		t.Fatalf("standard receive-order operations did not use LocalLRC: %+v", requests)
	}
}

func TestLocalLRCNegotiatesAndBatchesOrderedOperations(t *testing.T) {
	server := &recordingLocalLRCServer{ackEvery: 32, maxBatch: 32}
	fed := newLocalLRCTestFederate(t, server, 128)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	const operationCount = 128
	for i := 0; i < operationCount; i++ {
		sequence, err := fed.QueueInteractionByHandle(ctx, 12, map[uint64][]byte{5: {byte(i)}})
		if err != nil {
			t.Fatal(err)
		}
		if sequence != uint64(i+1) {
			t.Fatalf("sequence[%d] = %d", i, sequence)
		}
	}
	if err := fed.FlushLocalLRC(ctx); err != nil {
		t.Fatal(err)
	}

	requests := server.snapshot()
	if len(requests) != operationCount {
		t.Fatalf("operations = %d, want %d", len(requests), operationCount)
	}
	for i, request := range requests {
		if sequence := localLRCRequestSequence(request); sequence != uint64(i+1) {
			t.Fatalf("operation[%d] sequence = %d", i, sequence)
		}
	}
	if frames := server.frameCount(); frames >= operationCount {
		t.Fatalf("operation frames = %d, want fewer than %d", frames, operationCount)
	}
	stats := fed.LocalLRCStats()
	if stats.BatchSize != 32 || stats.Sent != operationCount || stats.Acked != operationCount ||
		stats.OperationFrames >= operationCount || stats.RequestedBatchSize != 32 ||
		stats.PeerBatchLimit != 32 || stats.MaxFrameOperations > 32 {
		t.Fatalf("LocalLRC batch stats = %+v", stats)
	}
	if got := server.requestedBatchSize(); got != 32 {
		t.Fatalf("requested batch size = %d, want 32", got)
	}
}

func TestLocalLRCRequestsAndUsesLargerBatch(t *testing.T) {
	server := &recordingLocalLRCServer{ackEvery: 32, maxBatch: 256}
	fed := newLocalLRCTestFederate(t, server, 1024)
	fed.conn.localLRCBatchSize = 256
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	const operationCount = 1024
	for i := 0; i < operationCount; i++ {
		if _, err := fed.QueueInteractionByHandle(ctx, 12, map[uint64][]byte{5: {byte(i)}}); err != nil {
			t.Fatal(err)
		}
	}
	if err := fed.FlushLocalLRC(ctx); err != nil {
		t.Fatal(err)
	}

	stats := fed.LocalLRCStats()
	if stats.RequestedBatchSize != 256 || stats.PeerBatchLimit != 256 ||
		stats.BatchSize != 256 || stats.MaxFrameOperations <= 32 ||
		stats.MaxFrameOperations > 256 || stats.Sent != operationCount ||
		stats.Acked != operationCount {
		t.Fatalf("LocalLRC large-batch stats = %+v", stats)
	}
	if got := server.requestedBatchSize(); got != 256 {
		t.Fatalf("requested batch size = %d, want 256", got)
	}
}

func TestLocalLRCFallsBackWhenServerDoesNotAdvertiseBatching(t *testing.T) {
	server := &recordingLocalLRCServer{ackEvery: 2}
	fed := newLocalLRCTestFederate(t, server, 4)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if _, err := fed.QueueInteractionByHandle(ctx, 12, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := fed.QueueInteractionByHandle(ctx, 12, nil); err != nil {
		t.Fatal(err)
	}
	if err := fed.FlushLocalLRC(ctx); err != nil {
		t.Fatal(err)
	}
	if stats := fed.LocalLRCStats(); stats.BatchSize != 1 {
		t.Fatalf("fallback batch size = %d, want 1", stats.BatchSize)
	}
	if frames := server.frameCount(); frames != 2 {
		t.Fatalf("fallback operation frames = %d, want 2", frames)
	}
}

func TestLocalLRCBatchSizeClampsPeerAdvertisement(t *testing.T) {
	tests := []struct {
		name                         string
		requested, advertised        uint32
		queueCapacity, wantBatchSize int
	}{
		{name: "unsupported peer", requested: 256, advertised: 0, queueCapacity: 1024, wantBatchSize: 1},
		{name: "peer limit", requested: 256, advertised: 32, queueCapacity: 1024, wantBatchSize: 32},
		{name: "request limit", requested: 64, advertised: 256, queueCapacity: 1024, wantBatchSize: 64},
		{name: "queue capacity", requested: 256, advertised: 256, queueCapacity: 4, wantBatchSize: 4},
		{name: "client maximum", requested: 512, advertised: 512, queueCapacity: 1024, wantBatchSize: 256},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := localLRCBatchSize(test.requested, test.advertised, test.queueCapacity); got != test.wantBatchSize {
				t.Fatalf("batch size = %d, want %d", got, test.wantBatchSize)
			}
		})
	}
}

func TestConfirmedReceiveOrderModeBypassesLocalLRC(t *testing.T) {
	server := &recordingLocalLRCServer{ackEvery: 1}
	fed := newLocalLRCTestFederate(t, server, 2)
	fed.conn.receiveOrderTransport = ReceiveOrderTransportConfirmed
	fed.conn.confirmedObjectStreamEnabled = false

	err := fed.SendInteractionByHandle(context.Background(), 12, nil, nil)
	if status.Code(err) != codes.Unimplemented {
		t.Fatalf("confirmed mode error = %v (code %v), want Unimplemented test service", err, status.Code(err))
	}
	if fed.localLRC != nil || len(server.snapshot()) != 0 {
		t.Fatal("confirmed mode opened or used LocalLRC")
	}
}

func TestCloneLocalLRCPayloadsOwnsMapAndBytes(t *testing.T) {
	original := map[uint64][]byte{1: {2, 3}}
	owned := cloneLocalLRCPayloads(original)
	original[1][0] = 9
	original[4] = []byte{5}
	if !reflect.DeepEqual(owned, map[uint64][]byte{1: {2, 3}}) {
		t.Fatalf("owned payloads changed with caller data: %v", owned)
	}
}

func BenchmarkLocalLRCQueueInteractionAdmission(b *testing.B) {
	lrc := &localLRC{
		cancel:   func() {},
		queue:    make(chan localLRCQueueItem, defaultLocalLRCQueueCapacity),
		slots:    make(chan struct{}, defaultLocalLRCQueueCapacity),
		changed:  make(chan struct{}),
		stop:     make(chan struct{}),
		ackEvery: defaultLocalLRCAckEvery,
	}
	fed := &Federate{conn: &Connection{}, localLRC: lrc, handles: testLocalStateTables()}
	fed.rememberPublishedInteraction(12)
	drainDone := make(chan struct{})
	go func() {
		defer close(drainDone)
		for {
			select {
			case item := <-lrc.queue:
				if item.sequence > 0 {
					<-lrc.slots
				}
			case <-lrc.stop:
				return
			}
		}
	}()
	b.Cleanup(func() {
		lrc.shutdown()
		<-drainDone
	})
	parameters := map[uint64][]byte{
		3: {0, 0, 0, 1},
		4: {0, 16, '0', '1', '2', '3', '4', '5', '6', '7'},
	}

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := fed.QueueInteractionByHandle(context.Background(), 12, parameters); err != nil {
			b.Fatal(err)
		}
	}
}
