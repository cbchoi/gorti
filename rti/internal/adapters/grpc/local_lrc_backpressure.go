package grpc

import (
	"context"
	"errors"
	"time"

	"github.com/cbchoi/gorti/rti/internal/adapters/delivery"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type localLRCPressure struct {
	ready      <-chan struct{}
	retryAfter time.Duration
}

func localLRCCapacityPressure(err error) *localLRCPressure {
	var uncommitted *core.UncommittedWireError
	var pressure *callbackCapacityPressure
	var queue *delivery.ReservationPressure
	if errors.As(err, &uncommitted) {
		if errors.As(uncommitted.Err, &pressure) {
			return &localLRCPressure{ready: pressure.ready}
		}
		if errors.As(uncommitted.Err, &queue) {
			return &localLRCPressure{retryAfter: time.Millisecond}
		}
	}
	return nil
}

func localLRCWireError(ctx context.Context, err error) error {
	if localLRCCapacityPressure(err) != nil {
		return err
	}
	return errToStatus(ctx, err)
}

// Normally a frame owns one lease. Receipt pressure introduces a boundary at
// the last committed operation: publish its ACK, release all leases, and retry
// only the next uncommitted operation after capacity or lifecycle changes.
func (s *localLRCService) applyLocalLRCFrame(stream rtiv1.LocalLRCService_ExchangeServer,
	f *localLRCFrameFencer, request *rtiv1.LocalLRCRequest, operations []*rtiv1.LocalLRCOperation,
	generation uint64, committed, lastAck *uint64, ackEvery uint32) error {
	ctx, lease, err := f.acquireFrame(stream.Context(), request)
	if err != nil {
		return err
	}
	end := func() { lease.end(); lease = localLRCFrameLease{} }
	defer end()
	for _, operation := range operations {
		for {
			sequence, operationErr := s.applyLocalLRCOperation(ctx, f.fed, f.handle, operation, *committed+1)
			pressure := localLRCCapacityPressure(operationErr)
			if pressure == nil {
				if operationErr != nil {
					return operationErr
				}
				*committed = sequence
				break
			}
			if lease.management {
				s.observeLocalLRCWaitFailure(ctx, f, generation, operation, operationErr)
				return errToStatus(ctx, operationErr)
			}
			// Capture authenticated incarnation and callback epoch only on the
			// slow path. Restore can reuse a handle without changing generation.
			var before core.TransportationScope
			scope := s.server.objService.transportationScope
			if scope != nil {
				before, err = scope(ctx, f.fed, f.handle)
				if err != nil {
					return err
				}
			}
			end()
			// Flush the complete prefix, including a short batch, before waiting.
			// The sender can release its bounded local queue while reports arrive.
			err = flushLocalLRCAcknowledgements(stream, *committed, lastAck, ackEvery)
			if err == nil {
				err = waitLocalLRCCapacity(stream.Context(), f, generation, pressure)
			}
			if err == nil {
				ctx, lease, err = f.acquireFrame(stream.Context(), request)
			}
			if err == nil {
				// Check again under the reacquired lease, not only while waiting.
				err = validateLocalLRCGeneration(ctx, f, generation)
			}
			if err == nil {
				if scopeErr := validateLocalLRCRetryScope(ctx, f, before, scope); scopeErr != nil {
					return scopeErr
				}
			}
			if err != nil {
				s.observeLocalLRCWaitFailure(stream.Context(), f, generation, operation, err)
				return err
			}
		}
	}
	return nil
}

func validateLocalLRCGeneration(ctx context.Context, f *localLRCFrameFencer, expected uint64) error {
	actual, exists := f.membership.GenerationFor(f.fed)
	if !exists {
		return errToStatus(ctx, core.ErrFederationNotFound)
	}
	if actual != expected {
		return errToStatus(ctx, core.ErrFederationGenerationMismatch)
	}
	return errToStatus(ctx, f.membership.ValidateMember(f.fed, f.handle))
}

func waitLocalLRCCapacity(ctx context.Context, f *localLRCFrameFencer, generation uint64, pressure *localLRCPressure) error {
	// A producer may resign without owning any pending receipt, so capacity
	// notification alone cannot observe every teardown. No RTI lease is held.
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	var retry <-chan time.Time
	if pressure.retryAfter > 0 {
		timer := time.NewTimer(pressure.retryAfter)
		defer timer.Stop()
		retry = timer.C
	}
	for {
		if err := ctx.Err(); err != nil {
			return status.FromContextError(err).Err()
		}
		if err := validateLocalLRCGeneration(ctx, f, generation); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return status.FromContextError(ctx.Err()).Err()
		case <-pressure.ready:
			return nil
		case <-retry:
			return nil
		case <-ticker.C:
		}
	}
}

func (s *localLRCService) observeLocalLRCWaitFailure(ctx context.Context, f *localLRCFrameFencer, generation uint64, op *rtiv1.LocalLRCOperation, err error) {
	if s.server.momObserver == nil || validateLocalLRCGeneration(ctx, f, generation) != nil {
		return
	}
	if update := op.GetAttributeUpdate(); update != nil {
		s.server.momObserver.Observe(ctx, "/rti.v1.ObjectService/UpdateAttributeValues", &rtiv1.UpdateAttributeValuesRequest{WireVersion: 1, FederationName: string(f.fed), FederateHandle: uint64(f.handle), ObjectHandle: update.ObjectHandle, Attributes: update.Attributes}, emptyResponse, err)
	} else if interaction := op.GetInteraction(); interaction != nil {
		s.server.momObserver.Observe(ctx, "/rti.v1.ObjectService/SendInteraction", &rtiv1.SendInteractionRequest{WireVersion: 1, FederationName: string(f.fed), FederateHandle: uint64(f.handle), InteractionClassHandle: interaction.InteractionClassHandle, Parameters: interaction.Parameters}, emptyResponse, err)
	}
}
func validateLocalLRCRetryScope(ctx context.Context, f *localLRCFrameFencer, before core.TransportationScope, scope func(context.Context, core.FederationName, core.FederateHandle) (core.TransportationScope, error)) error {
	if scope == nil {
		return nil
	}
	after, err := scope(ctx, f.fed, f.handle)
	if err != nil {
		return err
	}
	if after != before {
		return status.Error(codes.FailedPrecondition, "local LRC execution scope changed while waiting for callback capacity")
	}
	return nil
}
func flushLocalLRCAcknowledgements(stream rtiv1.LocalLRCService_ExchangeServer, committed uint64, lastAck *uint64, ackEvery uint32) error {
	for committed > *lastAck {
		boundary := min(committed, *lastAck+uint64(ackEvery))
		if err := stream.Send(&rtiv1.LocalLRCAck{CommittedThrough: boundary}); err != nil {
			return err
		}
		*lastAck = boundary
	}
	return nil
}
