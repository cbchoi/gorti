package federate

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/genproto/rti/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	defaultLocalLRCQueueCapacity = 1024
	defaultLocalLRCAckEvery      = 32
	defaultLocalLRCBatchSize     = 32
	maxLocalLRCClientBatchSize   = 256
	localLRCBatchDelay           = 50 * time.Microsecond
)

type localLRCQueueItem struct {
	operation *rtiv1.LocalLRCOperation
	flush     *rtiv1.LocalLRCFlush
	sequence  uint64
}

type localLRCCounters struct {
	enqueued, sent, acked, operationFrames, maxFrameOperations, flushes, failures atomic.Uint64
}

type localLRC struct {
	stream rtiv1.LocalLRCService_ExchangeClient
	cancel context.CancelFunc

	queue              chan localLRCQueueItem
	slots              chan struct{}
	ackEvery           uint32
	requestedBatchSize uint32
	peerBatchLimit     uint32
	batchSize          int

	admitMu     sync.Mutex
	closing     bool
	closingFlag atomic.Bool
	next        uint64

	stateMu         sync.Mutex
	acked           uint64
	failure         error
	failureSequence uint64
	changed         chan struct{}

	stop     chan struct{}
	stopOnce sync.Once
	sendDone chan struct{}
	recvDone chan struct{}

	counters localLRCCounters
}

// LocalLRCStats is a point-in-time view of the bounded queued transport.
type LocalLRCStats struct {
	Enqueued, Sent, Acked, OperationFrames, Flushes, Failures uint64
	MaxFrameOperations                                        uint64
	QueueDepth, QueueCapacity                                 int
	AckEvery                                                  uint32
	RequestedBatchSize, PeerBatchLimit                        uint32
	BatchSize                                                 int
}

// QueueAttributeValuesByHandle admits one receive-order attribute update to
// the in-process LocalLRC. The returned sequence is locally ordered; call
// FlushLocalLRC to establish server completion.
func (f *Federate) QueueAttributeValuesByHandle(
	ctx context.Context,
	objectHandle uint64,
	attributes map[uint64][]byte,
) (uint64, error) {
	if err := f.validateQueuedAttributeUpdate(objectHandle, attributes); err != nil {
		return 0, err
	}
	lrc, err := f.ensureLocalLRC(ctx)
	if err != nil {
		return 0, err
	}
	owned := cloneLocalLRCPayloads(attributes)
	return lrc.enqueue(ctx, func(sequence uint64) *rtiv1.LocalLRCOperation {
		return &rtiv1.LocalLRCOperation{Body: &rtiv1.LocalLRCOperation_AttributeUpdate{
			AttributeUpdate: &rtiv1.LocalLRCAttributeUpdate{
				Sequence: sequence, ObjectHandle: objectHandle, Attributes: owned,
			},
		}}
	})
}

// QueueInteractionByHandle admits one receive-order interaction to the
// in-process LocalLRC. Payload bytes are copied before the call returns.
func (f *Federate) QueueInteractionByHandle(
	ctx context.Context,
	classHandle uint64,
	parameters map[uint64][]byte,
) (uint64, error) {
	if err := f.validateQueuedInteraction(classHandle, parameters); err != nil {
		return 0, err
	}
	lrc, err := f.ensureLocalLRC(ctx)
	if err != nil {
		return 0, err
	}
	owned := cloneLocalLRCPayloads(parameters)
	return lrc.enqueue(ctx, func(sequence uint64) *rtiv1.LocalLRCOperation {
		return &rtiv1.LocalLRCOperation{Body: &rtiv1.LocalLRCOperation_Interaction{
			Interaction: &rtiv1.LocalLRCInteraction{
				Sequence: sequence, InteractionClassHandle: classHandle, Parameters: owned,
			},
		}}
	})
}

// FlushLocalLRC waits until the server cumulatively ACKs every queued
// operation admitted before this call's ordered flush marker.
func (f *Federate) FlushLocalLRC(ctx context.Context) error {
	f.localLRCMu.Lock()
	lrc := f.localLRC
	f.localLRCMu.Unlock()
	if lrc == nil {
		return nil
	}
	return lrc.flush(ctx)
}

// AwaitLocalLRC waits for the final result of one locally admitted sequence.
// A server-side HLA rejection is returned for the failed sequence; later
// admitted sequences are indeterminate because the stream stops at failure.
func (f *Federate) AwaitLocalLRC(ctx context.Context, sequence uint64) error {
	if sequence == 0 {
		return nil
	}
	f.localLRCMu.Lock()
	lrc := f.localLRC
	f.localLRCMu.Unlock()
	if lrc == nil {
		return fmt.Errorf("%w: %d", ErrLocalLRCSequenceNotAdmitted, sequence)
	}
	return lrc.await(ctx, sequence)
}

func (f *Federate) LocalLRCStats() LocalLRCStats {
	f.localLRCMu.Lock()
	lrc := f.localLRC
	f.localLRCMu.Unlock()
	if lrc == nil {
		return LocalLRCStats{}
	}
	return LocalLRCStats{
		Enqueued:           lrc.counters.enqueued.Load(),
		Sent:               lrc.counters.sent.Load(),
		Acked:              lrc.counters.acked.Load(),
		OperationFrames:    lrc.counters.operationFrames.Load(),
		MaxFrameOperations: lrc.counters.maxFrameOperations.Load(),
		Flushes:            lrc.counters.flushes.Load(),
		Failures:           lrc.counters.failures.Load(),
		QueueDepth:         len(lrc.queue),
		QueueCapacity:      cap(lrc.queue),
		AckEvery:           lrc.ackEvery,
		RequestedBatchSize: lrc.requestedBatchSize,
		PeerBatchLimit:     lrc.peerBatchLimit,
		BatchSize:          lrc.batchSize,
	}
}

func (f *Federate) ensureLocalLRC(ctx context.Context) (*localLRC, error) {
	if f.localLRCUnsupported.Load() {
		return nil, ErrLocalLRCUnavailable
	}
	if f.localLRCClosing.Load() {
		return nil, ErrLocalLRCClosed
	}
	f.localLRCMu.Lock()
	defer f.localLRCMu.Unlock()
	if f.localLRCClosing.Load() {
		return nil, ErrLocalLRCClosed
	}
	if f.localLRC != nil {
		return f.localLRC, nil
	}
	if f.conn == nil || f.conn.localLRC == nil || !f.conn.localLRCEnabled {
		return nil, ErrLocalLRCUnavailable
	}
	lrc, err := openLocalLRC(ctx, f)
	if err != nil {
		if status.Code(err) == codes.Unimplemented {
			f.localLRCUnsupported.Store(true)
			return nil, ErrLocalLRCUnavailable
		}
		return nil, wrapStatusErr(err)
	}
	f.localLRC = lrc
	return lrc, nil
}

func (f *Federate) useLocalLRCForReceiveOrder() bool {
	return f != nil && f.conn != nil &&
		f.conn.receiveOrderTransport == ReceiveOrderTransportLocalLRC &&
		f.conn.localLRCEnabled && f.conn.localLRC != nil &&
		!f.localLRCUnsupported.Load()
}

func openLocalLRC(ctx context.Context, f *Federate) (*localLRC, error) {
	streamCtx, cancel := context.WithCancel(context.Background())
	stopCaller := context.AfterFunc(ctx, cancel)
	stream, err := f.conn.localLRC.Exchange(streamCtx)
	if err != nil {
		stopCaller()
		cancel()
		return nil, err
	}
	if err := stream.Send(&rtiv1.LocalLRCRequest{Body: &rtiv1.LocalLRCRequest_Open{
		Open: &rtiv1.LocalLRCOpen{
			WireVersion:                  rtiv1.WireVersion_WIRE_VERSION_V1,
			FederationName:               f.federationName,
			FederateHandle:               f.federateHandle,
			ExpectedFederationGeneration: f.federationGeneration,
			AckEvery:                     f.conn.localLRCAckEvery,
			RequestedMaxBatchOperations:  f.conn.localLRCBatchSize,
		},
	}}); err != nil {
		stopCaller()
		cancel()
		return nil, err
	}
	openingAck, err := stream.Recv()
	if err != nil {
		stopCaller()
		cancel()
		return nil, err
	}
	if !stopCaller() && ctx.Err() != nil {
		cancel()
		return nil, ctx.Err()
	}
	if openingAck.GetCommittedThrough() != 0 || openingAck.GetAckEvery() == 0 {
		cancel()
		return nil, errors.New("federate: invalid LocalLRC opening ACK")
	}
	lrc := &localLRC{
		stream:             stream,
		cancel:             cancel,
		queue:              make(chan localLRCQueueItem, f.conn.localLRCQueueCapacity),
		slots:              make(chan struct{}, f.conn.localLRCQueueCapacity),
		ackEvery:           openingAck.GetAckEvery(),
		requestedBatchSize: f.conn.localLRCBatchSize,
		peerBatchLimit:     openingAck.GetMaxBatchOperations(),
		batchSize: localLRCBatchSize(
			f.conn.localLRCBatchSize, openingAck.GetMaxBatchOperations(), f.conn.localLRCQueueCapacity,
		),
		changed:  make(chan struct{}),
		stop:     make(chan struct{}),
		sendDone: make(chan struct{}),
		recvDone: make(chan struct{}),
	}
	go lrc.sendLoop()
	go lrc.receiveLoop()
	return lrc, nil
}

func (l *localLRC) enqueue(
	ctx context.Context,
	build func(sequence uint64) *rtiv1.LocalLRCOperation,
) (uint64, error) {
	l.admitMu.Lock()
	defer l.admitMu.Unlock()
	if l.closing {
		return 0, ErrLocalLRCClosed
	}
	if err := l.failureErr(); err != nil {
		return 0, fmt.Errorf("%w: %v", ErrLocalLRCIndeterminate, err)
	}
	select {
	case l.slots <- struct{}{}:
	case <-ctx.Done():
		return 0, ctx.Err()
	case <-l.stop:
		return 0, fmt.Errorf("%w: %v", ErrLocalLRCIndeterminate, l.failureErr())
	}
	if err := l.failureErr(); err != nil {
		<-l.slots
		return 0, fmt.Errorf("%w: %v", ErrLocalLRCIndeterminate, err)
	}
	sequence := l.next + 1
	item := localLRCQueueItem{operation: build(sequence), sequence: sequence}
	select {
	case l.queue <- item:
		l.next = sequence
		l.counters.enqueued.Add(1)
		if err := l.failureErr(); err != nil {
			return sequence, fmt.Errorf("%w: %v", ErrLocalLRCIndeterminate, err)
		}
		return sequence, nil
	case <-ctx.Done():
		<-l.slots
		return 0, ctx.Err()
	case <-l.stop:
		<-l.slots
		return 0, fmt.Errorf("%w: %v", ErrLocalLRCIndeterminate, l.failureErr())
	}
}

func (l *localLRC) flush(ctx context.Context) error {
	l.admitMu.Lock()
	if l.closing {
		l.admitMu.Unlock()
		return ErrLocalLRCClosed
	}
	target := l.next
	if target <= l.committed() {
		l.admitMu.Unlock()
		return nil
	}
	if l.failureErr() != nil {
		l.admitMu.Unlock()
		return l.waitCommitted(ctx, target)
	}
	select {
	case l.queue <- localLRCQueueItem{flush: &rtiv1.LocalLRCFlush{ThroughSequence: target}}:
		l.counters.flushes.Add(1)
		l.admitMu.Unlock()
	case <-ctx.Done():
		l.admitMu.Unlock()
		return ctx.Err()
	case <-l.stop:
		l.admitMu.Unlock()
		return fmt.Errorf("%w: %v", ErrLocalLRCIndeterminate, l.failureErr())
	}
	return l.waitCommitted(ctx, target)
}

func (l *localLRC) sendLoop() {
	defer close(l.sendDone)
	timer := time.NewTimer(time.Hour)
	if !timer.Stop() {
		<-timer.C
	}
	defer timer.Stop()
	var pending localLRCQueueItem
	hasPending := false
	for {
		var item localLRCQueueItem
		if hasPending {
			item = pending
			hasPending = false
		} else {
			select {
			case item = <-l.queue:
			case <-l.stop:
				return
			}
		}
		if item.operation == nil {
			if item.flush == nil {
				l.fail(errors.New("federate: invalid LocalLRC queue item"))
				return
			}
			if err := l.stream.Send(&rtiv1.LocalLRCRequest{Body: &rtiv1.LocalLRCRequest_Flush{Flush: item.flush}}); err != nil {
				l.fail(err)
				return
			}
			continue
		}
		if l.batchSize <= 1 {
			l.counters.sent.Store(item.sequence)
			if err := l.stream.Send(localLRCRequestForOperation(item.operation)); err != nil {
				l.fail(err)
				return
			}
			l.counters.operationFrames.Add(1)
			recordLocalLRCMaxFrameOperations(&l.counters.maxFrameOperations, 1)
			continue
		}

		operations := make([]*rtiv1.LocalLRCOperation, 0, l.batchSize)
		operations = append(operations, item.operation)
		sentThrough := item.sequence
		timer.Reset(localLRCBatchDelay)
		timerFired := false
	collect:
		for len(operations) < l.batchSize {
			select {
			case next := <-l.queue:
				if next.operation == nil {
					pending = next
					hasPending = true
					break collect
				}
				operations = append(operations, next.operation)
				sentThrough = next.sequence
			case <-timer.C:
				timerFired = true
				break collect
			case <-l.stop:
				stopAndDrainLocalLRCTimer(timer, timerFired)
				return
			}
		}
		stopAndDrainLocalLRCTimer(timer, timerFired)
		// Publish the upper bound before Send: the peer may process and ACK
		// the frame before grpc.Send returns to this goroutine.
		l.counters.sent.Store(sentThrough)
		request := &rtiv1.LocalLRCRequest{Body: &rtiv1.LocalLRCRequest_Batch{
			Batch: &rtiv1.LocalLRCBatch{Operations: operations},
		}}
		if len(operations) == 1 {
			request = localLRCRequestForOperation(operations[0])
		}
		if err := l.stream.Send(request); err != nil {
			l.fail(err)
			return
		}
		l.counters.operationFrames.Add(1)
		recordLocalLRCMaxFrameOperations(&l.counters.maxFrameOperations, uint64(len(operations)))
	}
}

func localLRCRequestForOperation(operation *rtiv1.LocalLRCOperation) *rtiv1.LocalLRCRequest {
	switch body := operation.GetBody().(type) {
	case *rtiv1.LocalLRCOperation_AttributeUpdate:
		return &rtiv1.LocalLRCRequest{Body: &rtiv1.LocalLRCRequest_AttributeUpdate{
			AttributeUpdate: body.AttributeUpdate,
		}}
	case *rtiv1.LocalLRCOperation_Interaction:
		return &rtiv1.LocalLRCRequest{Body: &rtiv1.LocalLRCRequest_Interaction{
			Interaction: body.Interaction,
		}}
	default:
		return &rtiv1.LocalLRCRequest{}
	}
}

func stopAndDrainLocalLRCTimer(timer *time.Timer, fired bool) {
	if fired || timer.Stop() {
		return
	}
	select {
	case <-timer.C:
	default:
	}
}

func localLRCBatchSize(requested, advertised uint32, queueCapacity int) int {
	if requested == 0 || advertised == 0 || queueCapacity <= 1 {
		return 1
	}
	batchSize := int(advertised)
	if batchSize > maxLocalLRCClientBatchSize {
		batchSize = maxLocalLRCClientBatchSize
	}
	if batchSize > int(requested) {
		batchSize = int(requested)
	}
	if batchSize > queueCapacity {
		batchSize = queueCapacity
	}
	if batchSize < 1 {
		return 1
	}
	return batchSize
}

func recordLocalLRCMaxFrameOperations(counter *atomic.Uint64, value uint64) {
	for current := counter.Load(); value > current; current = counter.Load() {
		if counter.CompareAndSwap(current, value) {
			return
		}
	}
}

func (l *localLRC) receiveLoop() {
	defer close(l.recvDone)
	for {
		ack, err := l.stream.Recv()
		if err != nil {
			closing := l.closingFlag.Load()
			if closing && (errors.Is(err, io.EOF) || status.Code(err) == codes.Canceled) {
				return
			}
			l.fail(err)
			return
		}
		committed := ack.GetCommittedThrough()
		if committed > l.counters.sent.Load() {
			l.fail(fmt.Errorf("LocalLRC ACK %d exceeds sent sequence %d", committed, l.counters.sent.Load()))
			return
		}
		l.stateMu.Lock()
		if committed < l.acked {
			previous := l.acked
			l.stateMu.Unlock()
			l.fail(fmt.Errorf("LocalLRC ACK regressed from %d to %d", previous, committed))
			return
		}
		if committed > l.acked {
			advanced := committed - l.acked
			l.acked = committed
			l.counters.acked.Store(committed)
			close(l.changed)
			l.changed = make(chan struct{})
			for range advanced {
				<-l.slots
			}
		}
		l.stateMu.Unlock()
	}
}

func (l *localLRC) waitCommitted(ctx context.Context, target uint64) error {
	for {
		l.stateMu.Lock()
		acked := l.acked
		failure := l.failure
		failureSequence := l.failureSequence
		changed := l.changed
		l.stateMu.Unlock()
		if acked >= target {
			return nil
		}
		if failure != nil {
			if failureSequence == target {
				return wrapStatusErr(failure)
			}
			return fmt.Errorf("%w: %v", ErrLocalLRCIndeterminate, failure)
		}
		select {
		case <-changed:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (l *localLRC) await(ctx context.Context, target uint64) error {
	l.admitMu.Lock()
	admitted := l.next
	l.admitMu.Unlock()
	if target > admitted {
		return fmt.Errorf("%w: %d (latest %d)", ErrLocalLRCSequenceNotAdmitted, target, admitted)
	}
	return l.waitCommitted(ctx, target)
}

func (l *localLRC) committed() uint64 {
	l.stateMu.Lock()
	defer l.stateMu.Unlock()
	return l.acked
}

func (l *localLRC) failureErr() error {
	l.stateMu.Lock()
	defer l.stateMu.Unlock()
	return l.failure
}

func (l *localLRC) fail(err error) {
	if err == nil {
		return
	}
	l.stateMu.Lock()
	if l.failure == nil {
		l.failure = err
		if !interactionResultIndeterminate(err) {
			l.failureSequence = l.acked + 1
		}
		l.counters.failures.Add(1)
		close(l.changed)
		l.changed = make(chan struct{})
	}
	l.stateMu.Unlock()
	l.shutdown()
}

func (l *localLRC) shutdown() {
	l.stopOnce.Do(func() {
		close(l.stop)
		l.cancel()
	})
}

func (f *Federate) closeLocalLRC(ctx context.Context) error {
	f.localLRCMu.Lock()
	lrc := f.localLRC
	f.localLRCMu.Unlock()
	if lrc == nil {
		return nil
	}
	return lrc.close(ctx)
}

func (l *localLRC) close(ctx context.Context) error {
	l.admitMu.Lock()
	if l.closing {
		l.admitMu.Unlock()
		return nil
	}
	l.closing = true
	l.closingFlag.Store(true)
	target := l.next
	needsFlush := target > l.committed()
	if needsFlush {
		select {
		case l.queue <- localLRCQueueItem{flush: &rtiv1.LocalLRCFlush{ThroughSequence: target}}:
			l.counters.flushes.Add(1)
		case <-ctx.Done():
			l.admitMu.Unlock()
			l.shutdown()
			return ctx.Err()
		case <-l.stop:
			l.admitMu.Unlock()
			return fmt.Errorf("%w: %v", ErrLocalLRCIndeterminate, l.failureErr())
		}
	}
	l.admitMu.Unlock()

	var flushErr error
	if needsFlush {
		flushErr = l.waitCommitted(ctx, target)
	}
	_ = l.stream.CloseSend()
	l.shutdown()
	for _, done := range []<-chan struct{}{l.sendDone, l.recvDone} {
		select {
		case <-done:
		case <-ctx.Done():
			if flushErr == nil {
				flushErr = ctx.Err()
			}
		}
	}
	return flushErr
}

func cloneLocalLRCPayloads(input map[uint64][]byte) map[uint64][]byte {
	output := make(map[uint64][]byte, len(input))
	for handle, payload := range input {
		output[handle] = append([]byte(nil), payload...)
	}
	return output
}
