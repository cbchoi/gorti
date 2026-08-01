// TASK-205½ (M21) — SendInteraction wire dispatcher.

package federate

import (
	"context"
	"errors"
	"fmt"
	"io"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/genproto/rti/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const interactionStreamMethod = "/rti.v1.InteractionStreamService/SendInteractions"

var interactionStreamDescription = grpc.StreamDesc{
	StreamName:    "SendInteractions",
	ServerStreams: true,
	ClientStreams: true,
}

type interactionRPCStream struct {
	stream grpc.ClientStream
	cancel context.CancelFunc
	ack    rtiv1.Empty
}

// SendInteraction sends an interaction. parameters is keyed by
// parameter name; the SDK resolves to wire handles via the FOM tables.
// timestamp is optional (nil → untimed).
//
// Unknown parameter names are silently dropped (the wire surface
// only carries handle-keyed entries; unrepresentable names produce
// no wire bytes). Pass at-least-one valid parameter to actually
// transmit anything.
func (f *Federate) SendInteraction(
	ctx context.Context, className string, parameters map[string][]byte, timestamp *float64,
) error {
	classHandle, wireParams, err := f.resolveInteractionValues(className, parameters)
	if err != nil {
		return err
	}
	return f.SendInteractionByHandle(ctx, classHandle, wireParams, timestamp)
}

// SendInteractionConfirmed waits for the RTI server to accept or reject the
// interaction, bypassing LocalLRC for receive-order traffic.
func (f *Federate) SendInteractionConfirmed(
	ctx context.Context, className string, parameters map[string][]byte, timestamp *float64,
) error {
	classHandle, wireParams, err := f.resolveInteractionValues(className, parameters)
	if err != nil {
		return err
	}
	return f.SendInteractionByHandleConfirmed(ctx, classHandle, wireParams, timestamp)
}

func (f *Federate) resolveInteractionValues(
	className string, parameters map[string][]byte,
) (uint64, map[uint64][]byte, error) {
	classHandle, ok := f.InteractionClassHandle(className)
	if !ok {
		return 0, nil, fmt.Errorf("federate: interaction class %q not in FOM", className)
	}
	wireParams := make(map[uint64][]byte, len(parameters))
	for name, payload := range parameters {
		ph, pok := f.handles.parameterHandle(className, name)
		if !pok {
			continue // unknown param name — drop on the floor
		}
		wireParams[ph] = payload
	}
	return classHandle, wireParams, nil
}

// SendInteractionByHandle sends an interaction using handles previously
// resolved with InteractionClassHandle and ParameterHandle. It performs no FOM
// name lookups, allowing callers to prepare handles and parameter maps outside
// latency-sensitive regions.
func (f *Federate) SendInteractionByHandle(
	ctx context.Context, classHandle uint64, parameters map[uint64][]byte, timestamp *float64,
) error {
	if timestamp == nil && f.useLocalLRCForReceiveOrder() {
		_, err := f.QueueInteractionByHandle(ctx, classHandle, parameters)
		if !errors.Is(err, ErrLocalLRCUnavailable) {
			return err
		}
	}
	return f.SendInteractionByHandleConfirmed(ctx, classHandle, parameters, timestamp)
}

// SendInteractionByHandleConfirmed waits for the RTI server result and does
// not use LocalLRC, even for receive-order traffic.
func (f *Federate) SendInteractionByHandleConfirmed(
	ctx context.Context, classHandle uint64, parameters map[uint64][]byte, timestamp *float64,
) error {
	f.interactionStats.total.Add(1)
	if f.interactionClosing.Load() {
		return ErrNotJoined
	}
	req := &rtiv1.SendInteractionRequest{
		WireVersion:            rtiv1.WireVersion_WIRE_VERSION_V1,
		FederationName:         f.federationName,
		FederateHandle:         f.federateHandle,
		InteractionClassHandle: classHandle,
		Parameters:             parameters,
	}
	if timestamp != nil {
		req.LogicalTime = timestamp
	}
	// Keep update/interaction stream use and every pre-send fallback in one
	// ordered critical section so confirmed Object Management calls cannot
	// overtake by selecting different transports.
	f.interactionStreamMu.Lock()
	defer f.interactionStreamMu.Unlock()
	if f.interactionClosing.Load() {
		return ErrNotJoined
	}
	if err := ctx.Err(); err != nil {
		return status.FromContextError(err).Err()
	}
	if f.interactionContext != nil {
		select {
		case <-f.interactionContext.Done():
			return ErrNotJoined
		default:
		}
	}
	confirmed := f.sendConfirmedObjectStreamLocked(ctx, &rtiv1.ConfirmedObjectRequest{
		Operation: &rtiv1.ConfirmedObjectRequest_Interaction{Interaction: req},
	})
	f.recordConfirmedObjectOutcome(confirmed)
	if confirmed.handled {
		f.recordConfirmedInteractionOutcome(confirmed)
		return confirmed.err
	}
	if handled, err := f.sendInteractionStreamLocked(ctx, req); handled {
		return err
	}
	f.interactionStats.unarySent.Add(1)
	f.confirmedObjectStats.unarySent.Add(1)
	// Tie the fallback RPC to the federate lifecycle as well as the caller.
	// Resign can then interrupt an in-flight unary call before waiting for the
	// ordered transport mutex.
	unaryCtx, finishUnary := f.confirmedUnaryContext(ctx)
	_, err := f.conn.obj.SendInteraction(
		unaryCtx, req, grpc.MaxRetryRPCBufferSize(0),
	)
	finishUnary()
	if err == nil {
		f.interactionStats.unaryAcked.Add(1)
		f.confirmedObjectStats.unaryAcked.Add(1)
	} else if interactionResultIndeterminate(err) {
		f.interactionStats.indeterminate.Add(1)
	}
	return wrapStatusErr(err)
}

// sendInteractionStreamLocked attempts the persistent path. The caller holds
// interactionStreamMu through any pre-send unary fallback.
func (f *Federate) sendInteractionStreamLocked(
	ctx context.Context, request *rtiv1.SendInteractionRequest,
) (bool, error) {
	if f.conn == nil || f.conn.cc == nil || !f.conn.interactionStreamEnabled {
		f.interactionStats.fallbackDisabled.Add(1)
		return false, nil
	}
	if outgoing, ok := metadata.FromOutgoingContext(ctx); ok && len(outgoing) > 0 {
		f.interactionStats.fallbackMetadata.Add(1)
		return false, nil
	}
	if f.interactionStreamUnsupported {
		f.interactionStats.fallbackUnsupported.Add(1)
		return false, nil
	}
	if err := ctx.Err(); err != nil {
		return true, status.FromContextError(err).Err()
	}
	if f.interactionStream == nil {
		f.interactionStats.openAttempts.Add(1)
		stream, err := f.openInteractionStream(ctx)
		if err != nil {
			if status.Code(err) == codes.Unimplemented || errors.Is(err, io.EOF) {
				f.interactionStreamUnsupported = true
				f.interactionStats.fallbackUnsupported.Add(1)
				return false, nil
			}
			return true, wrapStatusErr(err)
		}
		f.interactionStream = stream
		f.interactionStats.openSuccesses.Add(1)
	}
	stream := f.interactionStream
	stopCancellation := context.AfterFunc(ctx, stream.cancel)
	if err := stream.stream.SendMsg(request); err != nil {
		stopCancellation()
		f.resetInteractionStreamLocked()
		f.interactionStats.resets.Add(1)
		if interactionResultIndeterminate(err) {
			f.interactionStats.indeterminate.Add(1)
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return true, status.FromContextError(ctxErr).Err()
		}
		return true, wrapStatusErr(err)
	}
	f.interactionStats.streamSent.Add(1)
	err := stream.stream.RecvMsg(&stream.ack)
	cancellationStopped := stopCancellation()
	// An ACK is the transport commit point. A cancellation observed after a
	// successful receive must not overwrite that completed result.
	if err == nil {
		if !cancellationStopped {
			f.resetInteractionStreamLocked()
			f.interactionStats.resets.Add(1)
		}
		f.interactionStats.streamAcked.Add(1)
		return true, nil
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		f.resetInteractionStreamLocked()
		f.interactionStats.resets.Add(1)
		f.interactionStats.indeterminate.Add(1)
		return true, status.FromContextError(ctxErr).Err()
	}
	f.resetInteractionStreamLocked()
	f.interactionStats.resets.Add(1)
	if interactionResultIndeterminate(err) {
		f.interactionStats.indeterminate.Add(1)
	}
	return true, wrapStatusErr(err)
}

func interactionResultIndeterminate(err error) bool {
	switch status.Code(err) {
	case codes.InvalidArgument, codes.NotFound, codes.AlreadyExists,
		codes.PermissionDenied, codes.FailedPrecondition, codes.Unauthenticated,
		codes.Unimplemented:
		return false
	case codes.OK:
		return false
	default:
		return true
	}
}

func (f *Federate) openInteractionStream(ctx context.Context) (*interactionRPCStream, error) {
	parent := f.interactionContext
	if parent == nil {
		parent = context.Background()
	}
	streamCtx, cancel := context.WithCancel(parent)
	stream, err := f.conn.cc.NewStream(
		streamCtx, &interactionStreamDescription, interactionStreamMethod, grpc.StaticMethod(),
	)
	if err != nil {
		cancel()
		return nil, err
	}
	stopCancellation := context.AfterFunc(ctx, cancel)
	if err := stream.SendMsg(&rtiv1.SendInteractionRequest{}); err != nil {
		stopCancellation()
		cancel()
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, status.FromContextError(ctxErr).Err()
		}
		return nil, err
	}
	if err := stream.RecvMsg(new(rtiv1.Empty)); err != nil {
		stopCancellation()
		cancel()
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, status.FromContextError(ctxErr).Err()
		}
		return nil, err
	}
	stopCancellation()
	if ctxErr := ctx.Err(); ctxErr != nil {
		cancel()
		return nil, status.FromContextError(ctxErr).Err()
	}
	return &interactionRPCStream{stream: stream, cancel: cancel}, nil
}

func (f *Federate) resetInteractionStreamLocked() {
	if f.interactionStream == nil {
		return
	}
	f.interactionStream.cancel()
	_ = f.interactionStream.stream.CloseSend()
	f.interactionStream = nil
}

func (f *Federate) drainAndCloseInteractionStream(ctx context.Context) error {
	locked := make(chan struct{})
	go func() {
		f.interactionStreamMu.Lock()
		close(locked)
	}()
	select {
	case <-locked:
		f.resetConfirmedObjectStreamLocked()
		f.resetInteractionStreamLocked()
		f.interactionStreamMu.Unlock()
		return nil
	case <-ctx.Done():
		// Force the active stream or lifecycle-linked unary fallback to leave
		// its wait, then take the lock so the helper goroutine is never leaked.
		if f.interactionCancel != nil {
			f.interactionCancel()
		}
		<-locked
		f.resetConfirmedObjectStreamLocked()
		f.resetInteractionStreamLocked()
		f.interactionStreamMu.Unlock()
		return status.FromContextError(ctx.Err()).Err()
	}
}
