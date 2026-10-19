package federate

import (
	"context"
	"errors"
	"io"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
)

type callbackReportWrite struct {
	first uint64
	tasks []*callbackReportTask
	done  chan error
}

// One writer per stream allows ACK reads during sends, including flow control
// on large reports. Each submitted window is bounded by the report queue limit.
func writeCallbackReportBatches(ctx context.Context, cancel context.CancelFunc, stream rtiv1.CallbackReportService_ExchangeClient, writes <-chan callbackReportWrite, done chan<- struct{}) {
	defer close(done)
	for {
		select {
		case <-ctx.Done():
			return
		case write := <-writes:
			var err error
			sequence := write.first
			for offset := 0; offset < len(write.tasks); offset += callbackReportBatchSize {
				end := min(offset+callbackReportBatchSize, len(write.tasks))
				reports := make([]*rtiv1.ReportCallbackInvocationRequest, end-offset)
				for i, task := range write.tasks[offset:end] {
					reports[i] = task.request
				}
				err = stream.Send(&rtiv1.CallbackReportBatch{FirstSequence: sequence, Reports: reports})
				if err != nil {
					break
				}
				sequence += uint64(len(reports))
			}
			write.done <- err
			if err != nil {
				// A client interceptor can fail before gRPC closes the stream.
				// EOF instead requires Recv to recover the server's final status.
				if !errors.Is(err, io.EOF) {
					cancel()
				}
				return
			}
		}
	}
}
