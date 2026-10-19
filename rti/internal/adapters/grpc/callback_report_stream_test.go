package grpc

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	grpcgo "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

type reportStreamTestReporter func(context.Context, *rtiv1.ReportCallbackInvocationRequest) (*rtiv1.Empty, error)

func (fn reportStreamTestReporter) ReportCallbackInvocation(ctx context.Context, r *rtiv1.ReportCallbackInvocationRequest) (*rtiv1.Empty, error) {
	return fn(ctx, r)
}

func reportStreamTestClient(t *testing.T, reporter callbackInvocationReporter) (context.Context, rtiv1.CallbackReportServiceClient) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpcgo.NewServer()
	rtiv1.RegisterCallbackReportServiceServer(server, &callbackReportService{reporter: reporter})
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.Serve(listener) }()
	t.Cleanup(func() {
		server.Stop()
		if err := <-serveDone; err != nil && !errors.Is(err, grpcgo.ErrServerStopped) {
			t.Errorf("serve: %v", err)
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	conn, err := grpcgo.NewClient(listener.Addr().String(), grpcgo.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return ctx, rtiv1.NewCallbackReportServiceClient(conn)
}

func TestCallbackReportStreamOrderedPrefixAndFailure(t *testing.T) {
	var observed []bool
	var mu sync.Mutex
	ctx, client := reportStreamTestClient(t, reportStreamTestReporter(func(_ context.Context, r *rtiv1.ReportCallbackInvocationRequest) (*rtiv1.Empty, error) {
		mu.Lock()
		defer mu.Unlock()
		observed = append(observed, r.InvocationEntry)
		if r.Exception == "reject" {
			return nil, status.Error(codes.FailedPrecondition, "retryable lifecycle")
		}
		return &rtiv1.Empty{}, nil
	}))
	stream, err := client.Exchange(ctx)
	if err != nil {
		t.Fatal(err)
	}
	report := func(entry bool, exception string) *rtiv1.ReportCallbackInvocationRequest {
		return &rtiv1.ReportCallbackInvocationRequest{WireVersion: 1, FederationName: "f", FederateHandle: 7, InvocationEntry: entry, Success: exception == "", Exception: exception}
	}
	if err := stream.Send(&rtiv1.CallbackReportBatch{FirstSequence: 1, Reports: []*rtiv1.ReportCallbackInvocationRequest{report(true, ""), report(false, ""), report(true, "reject"), report(false, "")}}); err != nil {
		t.Fatal(err)
	}
	ack, err := stream.Recv()
	if err != nil {
		t.Fatal(err)
	}
	if ack.AcknowledgedSequence != 2 || ack.FailedSequence != 3 || ack.ErrorCode != uint32(codes.FailedPrecondition) {
		t.Fatalf("bad prefix: %v", ack)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(observed) != 3 || !observed[0] || observed[1] || !observed[2] {
		t.Fatalf("applied suffix or reordered reports: %v", observed)
	}
}

func TestCallbackReportStreamRejectsSequenceSizeAndRecipientChanges(t *testing.T) {
	for _, kind := range []string{"gap", "empty", "oversize", "recipient"} {
		t.Run(kind, func(t *testing.T) {
			ctx, client := reportStreamTestClient(t, reportStreamTestReporter(func(context.Context, *rtiv1.ReportCallbackInvocationRequest) (*rtiv1.Empty, error) {
				return &rtiv1.Empty{}, nil
			}))
			stream, err := client.Exchange(ctx)
			if err != nil {
				t.Fatal(err)
			}
			r := &rtiv1.ReportCallbackInvocationRequest{WireVersion: 1, FederationName: "f", FederateHandle: 7, InvocationEntry: true, Success: true}
			batch := &rtiv1.CallbackReportBatch{FirstSequence: 1, Reports: []*rtiv1.ReportCallbackInvocationRequest{r}}
			switch kind {
			case "gap":
				batch.FirstSequence = 2
			case "empty":
				batch.Reports = nil
			case "oversize":
				for len(batch.Reports) <= callbackReportBatchLimit {
					batch.Reports = append(batch.Reports, r)
				}
			case "recipient":
				batch.Reports = append(batch.Reports, &rtiv1.ReportCallbackInvocationRequest{FederationName: "f", FederateHandle: 8})
			}
			if err := stream.Send(batch); err != nil {
				t.Fatal(err)
			}
			ack, err := stream.Recv()
			if kind == "recipient" {
				if err != nil || ack.AcknowledgedSequence != 1 || ack.FailedSequence != 2 || ack.ErrorCode != uint32(codes.InvalidArgument) {
					t.Fatalf("recipient fence: %v/%v", ack, err)
				}
			} else if status.Code(err) != codes.InvalidArgument {
				t.Fatalf("bad batch accepted: %v/%v", ack, err)
			}
		})
	}
}
func checkTransportCleanup(t *testing.T, cleanup func() error) {
	t.Helper()
	if err := cleanup(); err != nil {
		t.Errorf("transport cleanup: %v", err)
	}
}
