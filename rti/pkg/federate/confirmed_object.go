package federate

import (
	"context"
	"errors"
	"fmt"
	"io"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/genproto/rti/v1"
	statuspb "google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

type confirmedObjectRPCStream struct {
	stream       rtiv1.ConfirmedObjectService_ExchangeClient
	cancel       context.CancelFunc
	nextSequence uint64
}

type confirmedObjectOutcome struct {
	handled, sent, acked                            bool
	openAttempted, opened                           bool
	reset, indeterminate                            bool
	fallbackDisabled, fallbackMetadata, unsupported bool
	err                                             error
}

func (f *Federate) sendConfirmedObjectStreamLocked(
	ctx context.Context,
	request *rtiv1.ConfirmedObjectRequest,
) confirmedObjectOutcome {
	if f.conn == nil || f.conn.confirmedObject == nil || !f.conn.confirmedObjectStreamEnabled {
		return confirmedObjectOutcome{fallbackDisabled: true}
	}
	if outgoing, ok := metadata.FromOutgoingContext(ctx); ok && len(outgoing) > 0 {
		// Per-call metadata cannot safely be changed after a persistent stream is
		// opened. Connection-level credentials are still applied by gRPC.
		return confirmedObjectOutcome{fallbackMetadata: true}
	}
	if f.confirmedObjectStreamUnsupported {
		return confirmedObjectOutcome{unsupported: true}
	}
	if err := ctx.Err(); err != nil {
		return confirmedObjectOutcome{handled: true, err: status.FromContextError(err).Err()}
	}

	outcome := confirmedObjectOutcome{handled: true}
	if f.confirmedObjectStream == nil {
		outcome.openAttempted = true
		stream, err := f.openConfirmedObjectStream(ctx)
		if err != nil {
			if status.Code(err) == codes.Unimplemented || errors.Is(err, io.EOF) {
				f.confirmedObjectStreamUnsupported = true
				outcome.handled = false
				outcome.unsupported = true
				return outcome
			}
			outcome.err = wrapStatusErr(err)
			return outcome
		}
		f.confirmedObjectStream = stream
		outcome.opened = true
	}

	stream := f.confirmedObjectStream
	request.Sequence = stream.nextSequence
	stopCancellation := context.AfterFunc(ctx, stream.cancel)
	if err := stream.stream.Send(request); err != nil {
		stopCancellation()
		f.resetConfirmedObjectStreamLocked()
		outcome.reset = true
		outcome.indeterminate = interactionResultIndeterminate(err)
		if ctxErr := ctx.Err(); ctxErr != nil {
			outcome.indeterminate = true
			outcome.err = status.FromContextError(ctxErr).Err()
			return outcome
		}
		outcome.err = wrapStatusErr(err)
		return outcome
	}
	outcome.sent = true

	result, err := stream.stream.Recv()
	cancellationStopped := stopCancellation()
	if err != nil {
		f.resetConfirmedObjectStreamLocked()
		outcome.reset = true
		outcome.indeterminate = interactionResultIndeterminate(err)
		if ctxErr := ctx.Err(); ctxErr != nil {
			outcome.indeterminate = true
			outcome.err = status.FromContextError(ctxErr).Err()
			return outcome
		}
		outcome.err = wrapStatusErr(err)
		return outcome
	}
	outcome.acked = true
	if result.GetSequence() != request.GetSequence() {
		f.resetConfirmedObjectStreamLocked()
		outcome.reset = true
		outcome.indeterminate = true
		outcome.err = status.Error(codes.DataLoss, fmt.Sprintf(
			"confirmed object response sequence %d, expected %d",
			result.GetSequence(), request.GetSequence()))
		return outcome
	}
	stream.nextSequence++
	if !cancellationStopped {
		// The operation crossed its response boundary, but the canceled caller's
		// stream cannot be reused safely for a later request.
		f.resetConfirmedObjectStreamLocked()
		outcome.reset = true
	}

	operationErr, decodeErr := decodeConfirmedObjectError(result.GetErrorStatus())
	if decodeErr != nil {
		f.resetConfirmedObjectStreamLocked()
		outcome.reset = true
		outcome.indeterminate = true
		outcome.err = decodeErr
		return outcome
	}
	outcome.err = operationErr
	return outcome
}

func (f *Federate) recordConfirmedObjectOutcome(outcome confirmedObjectOutcome) {
	stats := &f.confirmedObjectStats
	stats.total.Add(1)
	if outcome.openAttempted {
		stats.openAttempts.Add(1)
	}
	if outcome.opened {
		stats.openSuccesses.Add(1)
	}
	if outcome.sent {
		stats.streamSent.Add(1)
	}
	if outcome.acked {
		stats.streamAcked.Add(1)
	}
	if outcome.reset {
		stats.resets.Add(1)
	}
	if outcome.indeterminate {
		stats.indeterminate.Add(1)
	}
	if outcome.fallbackDisabled {
		stats.fallbackDisabled.Add(1)
	}
	if outcome.fallbackMetadata {
		stats.fallbackMetadata.Add(1)
	}
	if outcome.unsupported {
		stats.fallbackUnsupported.Add(1)
	}
}

func decodeConfirmedObjectError(encoded []byte) (error, error) {
	if len(encoded) == 0 {
		return nil, nil
	}
	message := new(statuspb.Status)
	if err := proto.Unmarshal(encoded, message); err != nil {
		return nil, status.Error(codes.DataLoss,
			fmt.Sprintf("decode confirmed object status: %v", err))
	}
	return wrapStatusErr(status.ErrorProto(message)), nil
}

func (f *Federate) openConfirmedObjectStream(ctx context.Context) (*confirmedObjectRPCStream, error) {
	parent := f.interactionContext
	if parent == nil {
		parent = context.Background()
	}
	streamCtx, cancel := context.WithCancel(parent)
	stream, err := f.conn.confirmedObject.Exchange(streamCtx)
	if err != nil {
		cancel()
		return nil, err
	}
	stopCancellation := context.AfterFunc(ctx, cancel)
	if _, err := stream.Header(); err != nil {
		stopCancellation()
		cancel()
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, status.FromContextError(ctxErr).Err()
		}
		return nil, err
	}
	if err := stream.Send(&rtiv1.ConfirmedObjectRequest{
		Sequence: 0,
		Operation: &rtiv1.ConfirmedObjectRequest_Open{Open: &rtiv1.ConfirmedObjectOpen{
			WireVersion:                  rtiv1.WireVersion_WIRE_VERSION_V1,
			FederationName:               f.federationName,
			FederateHandle:               f.federateHandle,
			ExpectedFederationGeneration: f.federationGeneration,
		}},
	}); err != nil {
		stopCancellation()
		cancel()
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, status.FromContextError(ctxErr).Err()
		}
		return nil, err
	}
	openingResult, err := stream.Recv()
	if err != nil {
		stopCancellation()
		cancel()
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, status.FromContextError(ctxErr).Err()
		}
		return nil, err
	}
	if openingResult.GetSequence() != 0 || len(openingResult.GetErrorStatus()) != 0 {
		stopCancellation()
		cancel()
		return nil, status.Error(codes.DataLoss, "invalid confirmed object opening ACK")
	}
	stopCancellation()
	if ctxErr := ctx.Err(); ctxErr != nil {
		cancel()
		return nil, status.FromContextError(ctxErr).Err()
	}
	return &confirmedObjectRPCStream{stream: stream, cancel: cancel, nextSequence: 1}, nil
}

func (f *Federate) resetConfirmedObjectStreamLocked() {
	if f.confirmedObjectStream == nil {
		return
	}
	f.confirmedObjectStream.cancel()
	_ = f.confirmedObjectStream.stream.CloseSend()
	f.confirmedObjectStream = nil
}

func (f *Federate) confirmedUnaryContext(ctx context.Context) (context.Context, func()) {
	unaryCtx, cancel := context.WithCancel(ctx)
	var stopLifecycleCancel func() bool
	if f.interactionContext != nil {
		stopLifecycleCancel = context.AfterFunc(f.interactionContext, cancel)
	}
	return unaryCtx, func() {
		if stopLifecycleCancel != nil {
			stopLifecycleCancel()
		}
		cancel()
	}
}

func (f *Federate) recordConfirmedInteractionOutcome(outcome confirmedObjectOutcome) {
	stats := &f.interactionStats
	if outcome.openAttempted {
		stats.openAttempts.Add(1)
	}
	if outcome.opened {
		stats.openSuccesses.Add(1)
	}
	if outcome.sent {
		stats.streamSent.Add(1)
	}
	if outcome.acked {
		stats.streamAcked.Add(1)
	}
	if outcome.reset {
		stats.resets.Add(1)
	}
	if outcome.indeterminate {
		stats.indeterminate.Add(1)
	}
}
