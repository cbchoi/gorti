package federate

import (
	"context"
	"errors"
	"io"
	"sync"

	"github.com/cbchoi/gorti/rti/internal/adapters/delivery/callbackprofile"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	statuspb "google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

const callbackReportQueueLimit = 256
const callbackReportBatchSize = 64

type callbackReportRejection struct{ error }

func (e callbackReportRejection) Unwrap() error { return e.error }

// CallbackReportingProfile reports the compiled Go SDK transport profile.
// The async profile can negotiate local entry for ordinary receive-order data;
// unsupported peers retain confirmed entry and asynchronous data outcomes.
func CallbackReportingProfile() string {
	if callbackprofile.Async {
		return "stream-async-outcome"
	}
	return "unary-sync"
}

type CallbackReportTransportStats struct {
	StreamBatches uint64
	StreamReports uint64
	AsyncOutcomes uint64
	LocalEntries  uint64
}

func (f *Federate) CallbackReportTransportStats() CallbackReportTransportStats {
	return CallbackReportTransportStats{
		StreamBatches: f.callbackReportBatches.Load(),
		StreamReports: f.callbackReportCount.Load(),
		AsyncOutcomes: f.callbackReportAsync.Load(),
		LocalEntries:  f.callbackLocalEntries.Load(),
	}
}

type callbackReportTask struct {
	ctx      context.Context
	request  *rtiv1.ReportCallbackInvocationRequest
	done     callbackReportSignal
	ack      callbackReportSignal
	err      error
	complete func(error)
}

type callbackReportKey struct {
	receipt string
	entry   bool
}

func reportKey(request *rtiv1.ReportCallbackInvocationRequest) callbackReportKey {
	return callbackReportKey{receipt: string(request.CallbackReceipt), entry: request.InvocationEntry}
}

type callbackReportTransport struct {
	f                 *Federate
	ctx               context.Context
	cancel            context.CancelFunc
	mu                sync.Mutex
	changed           chan struct{}
	queue             []*callbackReportTask
	completing        map[*callbackReportTask]struct{}
	completionQueue   []*callbackReportTask
	completionPending int
	completionStarted bool
	running           bool
	unsupported       bool
	asyncError        error
	failedReports     map[callbackReportKey]error
	localEntry        bool
	peerLocalEntry    bool
	stream            rtiv1.CallbackReportService_ExchangeClient
	streamCancel      context.CancelFunc
	writes            chan callbackReportWrite
	writerDone        chan struct{}
	sequence          uint64
}

func (f *Federate) callbackReportTransport() *callbackReportTransport {
	f.callbackReportMu.Lock()
	defer f.callbackReportMu.Unlock()
	if f.callbackReports == nil || f.callbackReports.ctx.Err() != nil {
		ctx, cancel := context.WithCancel(f.transportLifetime().ctx)
		f.callbackReports = &callbackReportTransport{f: f, ctx: ctx, cancel: cancel, changed: make(chan struct{})}
		f.conn.registerCallbackReports(f.callbackReports)
	}
	return f.callbackReports
}

func (q *callbackReportTransport) signalLocked() {
	close(q.changed)
	q.changed = make(chan struct{})
}

func (q *callbackReportTransport) enqueue(ctx context.Context, request *rtiv1.ReportCallbackInvocationRequest, complete func(error)) (*callbackReportTask, error) {
	if complete != nil {
		if err := q.reserveCompletion(ctx); err != nil {
			return nil, err
		}
	}
	task, err := q.enqueueReserved(ctx, request, complete)
	if err != nil && complete != nil {
		q.releaseCompletion()
	}
	return task, err
}

// An asynchronous caller owns a completion slot before entering this method.
func (q *callbackReportTransport) enqueueReserved(ctx context.Context, request *rtiv1.ReportCallbackInvocationRequest, complete func(error)) (*callbackReportTask, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for len(q.queue) >= callbackReportQueueLimit {
		changed := q.changed
		q.mu.Unlock()
		select {
		case <-changed:
		case <-ctx.Done():
			q.mu.Lock()
			return nil, ctx.Err()
		case <-q.ctx.Done():
			q.mu.Lock()
			return nil, q.ctx.Err()
		}
		q.mu.Lock()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := q.ctx.Err(); err != nil {
		return nil, err
	}
	if q.asyncError != nil && (complete != nil || request.InvocationEntry && q.failedReports[reportKey(request)] == nil) {
		return nil, q.asyncError
	}
	task := &callbackReportTask{ctx: ctx, request: request, complete: complete}
	if complete != nil {
		// Admission may wait on the caller's deadline; accepted reports belong
		// to the session and must survive that deadline afterwards.
		task.ctx = q.ctx
	}
	q.queue = append(q.queue, task)
	if !q.running {
		q.running = true
		go q.run()
	}
	return task, nil
}

func (q *callbackReportTransport) open(caller context.Context) error {
	if q.stream != nil {
		return nil
	}
	ctx, cancel := context.WithCancel(q.f.sessionContext(q.ctx))
	stop := context.AfterFunc(caller, cancel)
	defer stop()
	stream, err := rtiv1.NewCallbackReportServiceClient(q.f.conn.cc).Exchange(ctx)
	if err != nil {
		cancel()
		return err
	}
	header, err := stream.Header()
	if err == nil && len(header.Get("gorti-callback-report-stream")) == 0 {
		// Header may be empty when an older server immediately returns a status.
		_, err = stream.Recv()
		if err == nil {
			err = status.Error(codes.Internal, "callback report stream lacks capability header")
		}
	}
	if err != nil {
		cancel()
		return err
	}
	if values := header.Get("gorti-callback-report-stream"); len(values) != 1 || values[0] != "1" {
		cancel()
		return status.Error(codes.FailedPrecondition, "unsupported callback report stream version")
	}
	q.stream, q.streamCancel, q.sequence = stream, cancel, 0
	q.writes, q.writerDone = make(chan callbackReportWrite), make(chan struct{})
	go writeCallbackReportBatches(ctx, cancel, stream, q.writes, q.writerDone)
	values := header.Get("gorti-local-ro-callback-entry")
	q.mu.Lock()
	q.peerLocalEntry = len(values) == 1 && values[0] == "1"
	q.mu.Unlock()
	return nil
}

func (q *callbackReportTransport) resetStream() {
	if q.streamCancel != nil {
		q.streamCancel()
		<-q.writerDone
	}
	q.stream, q.streamCancel, q.sequence = nil, nil, 0
	q.writes, q.writerDone = nil, nil
	q.mu.Lock()
	q.localEntry, q.peerLocalEntry = false, false
	q.mu.Unlock()
}

func (q *callbackReportTransport) exchange(tasks []*callbackReportTask) (count int, resultErr error) {
	unsupported, err := q.prepareExchange(tasks[0].ctx)
	if err != nil {
		return 0, err
	}
	if unsupported {
		return q.exchangeUnary(tasks)
	}
	first, last := q.sequence+1, q.sequence+uint64(len(tasks))
	if first == 0 || last < first {
		return 0, errors.New("callback report sequence exhausted")
	}
	stop := q.watchReportCancellation(tasks)
	defer stop()
	write := callbackReportWrite{first: first, tasks: tasks, done: make(chan error, 1)}
	select {
	case q.writes <- write:
	case <-q.stream.Context().Done():
		return 0, q.stream.Context().Err()
	}
	defer func() {
		if resultErr != nil {
			q.streamCancel()
		}
		if err := <-write.done; err != nil {
			if resultErr == nil || status.Code(resultErr) == codes.Canceled && !errors.Is(err, io.EOF) {
				resultErr = err
			}
		}
	}()
	for count < len(tasks) {
		batchSize := min(uint64(len(tasks[count:])), uint64(callbackReportBatchSize))
		ack, err := q.stream.Recv()
		if err != nil {
			return count, err
		}
		q.f.callbackReportBatches.Add(1)
		accepted, err := q.acceptReportAcknowledgement(ack, batchSize)
		count += accepted
		if err != nil {
			return count, err
		}
	}
	q.mu.Lock()
	// A header alone is not authenticated acceptance of this federate's reports.
	q.localEntry = q.peerLocalEntry
	q.mu.Unlock()
	return count, nil
}

func (q *callbackReportTransport) run() {
	for {
		q.mu.Lock()
		if len(q.queue) == 0 {
			q.running = false
			q.mu.Unlock()
			return
		}
		batch := append([]*callbackReportTask(nil), q.queue[:min(len(q.queue), callbackReportQueueLimit)]...)
		q.mu.Unlock()
		count, err := q.exchangeWithRetry(batch)
		q.mu.Lock()
		finished := q.finishReportBatchLocked(batch, count, err)
		// Publish errors and ACKs before making tasks visible to the completion
		// worker, so that worker never needs a per-task acknowledgement channel.
		for i, task := range finished {
			if i >= count {
				task.err = err
			}
			task.ack.finish()
			if task.complete == nil {
				task.done.finish()
			}
		}
		// Keep acknowledged tasks discoverable until explicit drain can observe
		// their local bookkeeping, even after the wire queue releases capacity.
		for _, task := range finished {
			if task.complete != nil {
				if q.completing == nil {
					q.completing = make(map[*callbackReportTask]struct{})
				}
				q.completing[task] = struct{}{}
				q.completionQueue = append(q.completionQueue, task)
			}
		}
		if len(q.completionQueue) != 0 && !q.completionStarted {
			q.completionStarted = true
			go q.completeReports()
		}
		q.mu.Unlock()
		// Local retirement never blocks wire progress on checkpointMu;
		// confirmed control reports do not consume completion slots.
		q.mu.Lock()
		q.queue = q.queue[len(finished):]
		q.signalLocked()
		q.mu.Unlock()
	}
}

func (f *Federate) sendCallbackReport(ctx context.Context, request *rtiv1.ReportCallbackInvocationRequest) error {
	if !callbackprofile.Async || len(f.sessionToken) == 0 {
		_, err := rtiv1.NewSupportServiceClient(f.conn.cc).ReportCallbackInvocation(f.sessionContext(ctx), request)
		return err
	}
	task, err := f.callbackReportTransport().enqueue(ctx, request, nil)
	if err != nil {
		return err
	}
	select {
	case <-task.done.wait():
		return task.err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func asyncDataOutcome(event Event) bool {
	var metadata *ObjectCallbackMetadata
	switch e := event.(type) {
	case ReflectAttributeValues:
		metadata = e.Metadata
	case ReflectAttributeValuesByHandle:
		metadata = e.Metadata
	case ReceiveInteraction:
		metadata = e.Metadata
	case ReceiveInteractionByHandle:
		metadata = e.Metadata
	default:
		return false
	}
	return metadata != nil && len(metadata.ReceivedRegions) == 0 && metadata.ReceivedOrder != nil &&
		*metadata.ReceivedOrder == OrderReceive && metadata.SentOrder != nil && *metadata.SentOrder == OrderReceive
}

func (f *Federate) asyncCallbackOutcomeEnabled(event Event) bool {
	if !callbackprofile.Async || len(f.sessionToken) == 0 || !f.callbackEntrySupported || !asyncDataOutcome(event) {
		return false
	}
	q := f.callbackReportTransport()
	q.mu.Lock()
	defer q.mu.Unlock()
	return !q.unsupported
}

func (f *Federate) enqueueCallbackOutcome(ctx context.Context, event Event, success bool, exception string) error {
	info, ok := event.(interface {
		CallbackReceipt() []byte
		callbackReportingHandle() uint64
	})
	if !ok || len(info.CallbackReceipt()) == 0 {
		return ErrCallbackReceiptUnavailable
	}
	request := &rtiv1.ReportCallbackInvocationRequest{WireVersion: 1, FederationName: f.federationName,
		FederateHandle: info.callbackReportingHandle(), CallbackReceipt: info.CallbackReceipt(), Success: success, Exception: exception}
	q := f.callbackReportTransport()
	// Accepted outcomes outlive the per-dispatch caller deadline. They are
	// retained by the SDK until ACK, explicit drain, restore, or teardown.
	if err := ctx.Err(); err != nil {
		return err
	}
	_, err := q.enqueue(q.ctx, request, func(err error) {
		if err != nil {
			return
		}
		f.checkpointMu.Lock()
		defer f.checkpointMu.Unlock()
		if f.validateRuntimeEventLocked(event) == nil {
			f.retainCallbackOutcome(event, success, exception, true)
			f.retireRuntimeEventLocked(event)
		}
	})
	if err == nil {
		f.callbackReportAsync.Add(1)
	}
	return err
}

func (f *Federate) closeCallbackReports() {
	f.callbackReportMu.Lock()
	defer f.callbackReportMu.Unlock()
	if f.callbackReports != nil {
		f.callbackReports.cancel()
	}
}

func (f *Federate) hasCallbackReports() bool {
	f.callbackReportMu.Lock()
	defer f.callbackReportMu.Unlock()
	return f.callbackReports != nil
}

func (q *callbackReportTransport) prepareExchange(ctx context.Context) (bool, error) {
	q.mu.Lock()
	unsupported := q.unsupported
	q.mu.Unlock()
	if !unsupported {
		if err := q.open(ctx); err != nil {
			if status.Code(err) != codes.Unimplemented {
				return false, err
			}
			q.mu.Lock()
			q.unsupported = true
			q.mu.Unlock()
			unsupported = true
		}
	}
	return unsupported, nil
}

func (q *callbackReportTransport) exchangeUnary(tasks []*callbackReportTask) (int, error) {
	for i, task := range tasks {
		_, err := rtiv1.NewSupportServiceClient(q.f.conn.cc).ReportCallbackInvocation(q.f.sessionContext(task.ctx), task.request)
		if err != nil {
			return i, err
		}
	}
	return len(tasks), nil
}

func (q *callbackReportTransport) watchReportCancellation(tasks []*callbackReportTask) func() {
	var stops []func() bool
	for _, task := range tasks {
		if task.complete == nil {
			stops = append(stops, context.AfterFunc(task.ctx, q.streamCancel))
		}
	}
	return func() {
		for _, stop := range stops {
			stop()
		}
	}
}

func (q *callbackReportTransport) acceptReportAcknowledgement(ack *rtiv1.CallbackReportAck, batchSize uint64) (int, error) {
	last := q.sequence + batchSize
	if ack.AcknowledgedSequence < q.sequence || ack.AcknowledgedSequence > last {
		return 0, errors.New("invalid callback report cumulative acknowledgement")
	}
	accepted := ack.AcknowledgedSequence - q.sequence
	if accepted > callbackReportBatchSize {
		return 0, errors.New("invalid callback report cumulative acknowledgement")
	}
	if ack.FailedSequence != 0 {
		err := callbackReportFailure(ack, last)
		var rejection callbackReportRejection
		if !errors.As(err, &rejection) {
			return 0, err
		}
		q.f.callbackReportCount.Add(accepted)
		return int(accepted), err
	}
	if accepted != batchSize || ack.ErrorCode != 0 || ack.ErrorMessage != "" || len(ack.ErrorStatus) != 0 {
		return 0, errors.New("incomplete callback report acknowledgement")
	}
	q.sequence = last
	q.f.callbackReportCount.Add(accepted)
	return int(accepted), nil
}

func callbackReportFailure(ack *rtiv1.CallbackReportAck, last uint64) error {
	if ack.FailedSequence != ack.AcknowledgedSequence+1 || ack.FailedSequence > last || ack.ErrorCode == 0 || ack.ErrorCode > uint32(codes.Unauthenticated) {
		return errors.New("invalid callback report failure acknowledgement")
	}
	rejection := status.Error(codes.Code(ack.ErrorCode), ack.ErrorMessage)
	if len(ack.ErrorStatus) != 0 {
		var wire statuspb.Status
		if proto.Unmarshal(ack.ErrorStatus, &wire) != nil || int64(wire.Code) != int64(ack.ErrorCode) || wire.Message != ack.ErrorMessage {
			return errors.New("invalid callback report error details")
		}
		rejection = status.FromProto(&wire).Err()
	}
	return callbackReportRejection{rejection}
}

func (q *callbackReportTransport) exchangeWithRetry(batch []*callbackReportTask) (int, error) {
	count, err := q.exchange(batch)
	if err != nil {
		q.resetStream()
	}
	// A lost acknowledgement is retried using the same opaque receipts.
	// Domain failures are returned, not retried as successful execution.
	q.mu.Lock()
	canRetry := !q.unsupported
	q.mu.Unlock()
	var rejection callbackReportRejection
	if canRetry && !errors.As(err, &rejection) && count == 0 && status.Code(err) == codes.Unavailable && q.ctx.Err() == nil {
		count, err = q.exchange(batch)
		if err != nil {
			q.resetStream()
		}
	}
	return count, err
}

// finishReportBatchLocked retains failures before exposing acknowledgements.
func (q *callbackReportTransport) finishReportBatchLocked(batch []*callbackReportTask, count int, err error) []*callbackReportTask {
	finished := batch
	for _, task := range batch[:count] {
		delete(q.failedReports, reportKey(task.request))
	}
	if len(q.failedReports) == 0 {
		q.asyncError = nil
	}
	if err != nil {
		finished = append([]*callbackReportTask(nil), q.queue...)
		for i, task := range finished {
			if i >= count && task.complete != nil {
				if q.failedReports == nil {
					q.failedReports = make(map[callbackReportKey]error)
				}
				q.failedReports[reportKey(task.request)] = err
				q.asyncError = err
			}
		}
	}
	return finished
}
