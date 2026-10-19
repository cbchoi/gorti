package grpc

import (
	"context"
	"io"
	"math"

	"github.com/cbchoi/gorti/rti/internal/adapters/delivery/callbackprofile"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	grpcgo "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

const callbackReportBatchLimit = 64

type callbackInvocationReporter interface {
	ReportCallbackInvocation(context.Context, *rtiv1.ReportCallbackInvocationRequest) (*rtiv1.Empty, error)
}

func localCallbackEntrySupported(reporter callbackInvocationReporter) bool {
	capability, ok := reporter.(interface{ localCallbackEntrySupported() bool })
	return ok && capability.localCallbackEntrySupported()
}

func (s *supportService) localCallbackEntrySupported() bool {
	return s.callbackReceipts != nil && s.callbackReceipts.opts.LocalDataEntry && s.callbackReceipts.opts.ObserveEntry != nil
}

func (s *routedSupportService) localCallbackEntrySupported() bool {
	return localCallbackEntrySupported(s.floats) && localCallbackEntrySupported(s.integers)
}

type callbackReportService struct {
	rtiv1.UnimplementedCallbackReportServiceServer
	reporter callbackInvocationReporter
}

func registerCallbackReportService(registrar grpcgo.ServiceRegistrar, reporter callbackInvocationReporter) {
	if callbackprofile.Async {
		rtiv1.RegisterCallbackReportServiceServer(registrar, &callbackReportService{reporter: reporter})
	}
}

func (s *callbackReportService) Exchange(stream rtiv1.CallbackReportService_ExchangeServer) error {
	header := metadata.Pairs("gorti-callback-report-stream", "1")
	if localCallbackEntrySupported(s.reporter) {
		header.Set("gorti-local-ro-callback-entry", "1")
	}
	if err := stream.SendHeader(header); err != nil {
		return err
	}
	state := callbackReportStreamState{}
	for {
		batch, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		ack, err := s.applyReportBatch(stream.Context(), &state, batch)
		if err != nil {
			return err
		}
		if err := stream.Send(ack); err != nil {
			return err
		}
		if ack.FailedSequence != 0 {
			return nil
		}
	}
}

type callbackReportStreamState struct {
	acknowledged uint64
	federation   string
	federate     uint64
}

func (s *callbackReportService) applyReportBatch(ctx context.Context, state *callbackReportStreamState, batch *rtiv1.CallbackReportBatch) (*rtiv1.CallbackReportAck, error) {
	if len(batch.Reports) == 0 || len(batch.Reports) > callbackReportBatchLimit ||
		state.acknowledged == math.MaxUint64 || batch.FirstSequence != state.acknowledged+1 ||
		uint64(len(batch.Reports)) > math.MaxUint64-state.acknowledged {
		return nil, status.Error(codes.InvalidArgument, "invalid callback report batch sequence or size")
	}
	ack := &rtiv1.CallbackReportAck{AcknowledgedSequence: state.acknowledged}
	for index := 0; index < len(batch.Reports); index++ {
		count, reportErr := s.applyReportPrefix(ctx, state, batch.Reports[index:])
		if count > 0 {
			state.acknowledged += uint64(count)
			ack.AcknowledgedSequence = state.acknowledged
			index += count - 1
			continue
		}
		if reportErr != nil {
			st := status.Convert(reportErr)
			ack.FailedSequence, ack.ErrorCode, ack.ErrorMessage = state.acknowledged+1, uint32(st.Code()), st.Message()
			ack.ErrorStatus, _ = proto.Marshal(st.Proto())
			break
		}
		state.acknowledged++
		ack.AcknowledgedSequence = state.acknowledged
	}
	return ack, nil
}

func (s *callbackReportService) applyReportPrefix(ctx context.Context, state *callbackReportStreamState, reports []*rtiv1.ReportCallbackInvocationRequest) (int, error) {
	report := reports[0]
	if report == nil || report.FederationName == "" || report.FederateHandle == 0 {
		return 0, status.Error(codes.InvalidArgument, "callback report recipient required")
	}
	if state.federation == "" {
		state.federation, state.federate = report.FederationName, report.FederateHandle
	}
	if report.FederationName != state.federation || report.FederateHandle != state.federate {
		return 0, status.Error(codes.InvalidArgument, "callback report stream recipient changed")
	}
	if batcher, ok := s.reporter.(dataReportBatcher); ok {
		if count := batcher.tryDataReportPrefix(ctx, reports); count > 0 {
			return count, nil
		}
	}
	_, err := s.reporter.ReportCallbackInvocation(ctx, report)
	return 0, err
}
