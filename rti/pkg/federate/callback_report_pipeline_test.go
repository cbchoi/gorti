package federate

import (
	"context"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/internal/adapters/delivery/callbackprofile"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type failedReportSendStream struct{ grpc.ClientStream }

func (s failedReportSendStream) SendMsg(any) error {
	return status.Error(codes.Unavailable, "report not sent")
}

func TestCallbackReportPipelineSendFailureUnblocksACKReader(t *testing.T) {
	if !callbackprofile.Async {
		t.Skip("async report pipeline")
	}
	intercept := grpc.WithStreamInterceptor(func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		stream, err := streamer(ctx, desc, cc, method, opts...)
		if err != nil {
			return nil, err
		}
		return failedReportSendStream{stream}, nil
	})
	f, ctx := newSDKReportFixture(t, &sdkReportServer{report: func(context.Context, *rtiv1.ReportCallbackInvocationRequest) error { return nil }}, true, intercept)
	task, err := f.callbackReportTransport().enqueue(ctx, &rtiv1.ReportCallbackInvocationRequest{}, func(error) {})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-task.done.wait():
		if status.Code(task.err) != codes.Unavailable {
			t.Fatalf("send failure was replaced by cancellation: %v", task.err)
		}
	case <-time.After(time.Second):
		t.Fatal("reader awaited an ACK for an unsent report")
	}
}

func TestCallbackReportPipelineSendsBeforeFirstACK(t *testing.T) {
	testCallbackReportPipeline(t, false)
}

func TestCallbackReportPipelineKeepsCommittedPrefixOnFailure(t *testing.T) {
	testCallbackReportPipeline(t, true)
}

func testCallbackReportPipeline(t *testing.T, reject bool) {
	t.Helper()
	if !callbackprofile.Async {
		t.Skip("async report pipeline")
	}
	server := &sdkReportServer{exchange: func(stream rtiv1.CallbackReportService_ExchangeServer) error {
		if err := stream.SendHeader(metadata.Pairs("gorti-callback-report-stream", "1")); err != nil {
			return err
		}
		// Require both frames before acknowledging either frame.
		for i := 0; i < 2; i++ {
			batch, err := stream.Recv()
			if err != nil {
				return err
			}
			if batch.FirstSequence != uint64(i*callbackReportBatchSize+1) || len(batch.Reports) != callbackReportBatchSize {
				return status.Error(codes.InvalidArgument, "unexpected pipeline framing")
			}
		}
		if err := stream.Send(&rtiv1.CallbackReportAck{AcknowledgedSequence: callbackReportBatchSize}); err != nil {
			return err
		}
		ack := &rtiv1.CallbackReportAck{AcknowledgedSequence: 2 * callbackReportBatchSize}
		if reject {
			ack.AcknowledgedSequence = callbackReportBatchSize + 13
			ack.FailedSequence = ack.AcknowledgedSequence + 1
			ack.ErrorCode, ack.ErrorMessage = uint32(codes.FailedPrecondition), "deferred outcome"
		}
		return stream.Send(ack)
	}}
	f, ctx := newSDKReportFixture(t, server, true)
	q := f.callbackReportTransport()
	// Assemble a full window without starting its transport early.
	q.mu.Lock()
	q.running = true
	q.mu.Unlock()
	var tasks []*callbackReportTask
	for i := 0; i < 2*callbackReportBatchSize; i++ {
		task, err := q.enqueue(ctx, &rtiv1.ReportCallbackInvocationRequest{}, func(error) {})
		if err != nil {
			t.Fatal(err)
		}
		tasks = append(tasks, task)
	}
	go q.run()
	for i, task := range tasks {
		select {
		case <-task.done.wait():
		case <-ctx.Done():
			t.Fatal("pipeline waited for the first ACK before sending the second frame")
		}
		if reject && i >= callbackReportBatchSize+13 {
			if status.Code(task.err) != codes.FailedPrecondition {
				t.Fatalf("task %d lost the precise rejection: %v", i, task.err)
			}
		} else if task.err != nil {
			t.Fatalf("committed task %d failed: %v", i, task.err)
		}
	}
	if !reject {
		if err := f.flushLocalCallbackEntries(ctx); err != nil {
			t.Fatal(err)
		}
	}
}
