package grpc

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/cbchoi/gorti/rti/internal/core"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/genproto/rti/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	defaultLocalLRCAckEvery        uint32 = 32
	maxLocalLRCAckEvery            uint32 = 256
	defaultLocalLRCBatchOperations uint32 = 32
	maxLocalLRCBatchOperations     uint32 = 256
)

type localLRCService struct {
	rtiv1.UnimplementedLocalLRCServiceServer
	server *Server
	// wire is the W3 direct-apply fast path: the composed object
	// registry asserted ONCE here to core.WireApplier. When non-nil,
	// applyLocalLRC* hands the decoded proto maps straight to the
	// registry (documented ownership transfer — see core.WireApplier)
	// instead of re-boxing a unary request through objService. nil
	// (registry without the optional interface, e.g. test stubs) falls
	// back to the copying unary path.
	wire core.WireApplier
}

func newLocalLRCService(server *Server) *localLRCService {
	s := &localLRCService{server: server}
	// DECORATOR CONTRACT (load-bearing): the fast path is resolved by
	// asserting on server.objService.obj — the SAME core.ObjectRegistry
	// interface value the unary dispatch path (objectService handlers)
	// calls — NEVER by unwrapping to the concrete *object.Registry. A
	// future decorator that wraps the registry (metrics, tracing,
	// policy, ...) therefore has exactly two valid outcomes here:
	// either it implements core.WireApplier itself (forwarding after
	// its own concern), or this assertion fails and every stream op
	// falls back to the copying unary-equivalent path THROUGH the
	// decorator. The fast path must never bypass a decorator — do not
	// add an Unwrap/inner-registry escape hatch to this assertion.
	if applier, ok := server.objService.obj.(core.WireApplier); ok {
		s.wire = applier
	}
	return s
}

func (s *localLRCService) Exchange(
	stream rtiv1.LocalLRCService_ExchangeServer,
) error {
	first, err := stream.Recv()
	if err != nil {
		return err
	}
	open := first.GetOpen()
	if open == nil {
		return status.Error(codes.InvalidArgument, "local LRC stream must start with open")
	}
	if !validWireVersion(open.GetWireVersion()) {
		return status.Error(codes.FailedPrecondition, "unsupported wire version")
	}
	fed := core.FederationName(open.GetFederationName())
	handle := core.FederateHandle(open.GetFederateHandle())
	if err := s.validateOpen(stream.Context(), fed, handle, open.GetExpectedFederationGeneration()); err != nil {
		return err
	}
	ackEvery := clampLocalLRCAckEvery(open.GetAckEvery())
	batchOperations := clampLocalLRCBatchOperations(open.GetRequestedMaxBatchOperations())
	if err := stream.Send(&rtiv1.LocalLRCAck{
		AckEvery:           ackEvery,
		MaxBatchOperations: batchOperations,
	}); err != nil {
		return err
	}

	// W2: resolve the membership fencing tier ONCE at stream open —
	// the per-op core.FederationMembershipGuard type assertion that
	// withMember paid is hoisted here, and the lease itself is
	// acquired once per received FRAME (single op, or one whole Batch
	// of <=256 ops) instead of once per operation. Resign/destroy take
	// the exclusive side of the same gate, so teardown now lands on
	// frame boundaries: every op of an in-flight frame applies, and no
	// op of a later frame applies after teardown.
	fencer := newLocalLRCFrameFencer(s.server.membership, fed, handle)

	var committed uint64
	var lastAck uint64
	for {
		request, recvErr := stream.Recv()
		if recvErr != nil {
			if errors.Is(recvErr, io.EOF) {
				if committed > lastAck {
					return stream.Send(&rtiv1.LocalLRCAck{CommittedThrough: committed})
				}
				return nil
			}
			return recvErr
		}

		switch body := request.GetBody().(type) {
		case *rtiv1.LocalLRCRequest_AttributeUpdate:
			// Sequence validation runs BEFORE the frame lease acquire
			// (main's observable precedence): a wrong-sequence op sent
			// concurrently with the federate's own resign must surface
			// the sequence error, not the membership error. The sequence
			// is already decoded, so this costs one compare.
			if seqErr := validateLocalLRCSequence(body.AttributeUpdate.GetSequence(), committed+1); seqErr != nil {
				return sendLocalLRCAckBeforeError(stream, committed, lastAck, seqErr)
			}
			lease, leaseErr := fencer.acquire(stream.Context())
			if leaseErr != nil {
				return sendLocalLRCAckBeforeError(stream, committed, lastAck, leaseErr)
			}
			sequence, operationErr := s.applyLocalLRCAttributeUpdate(
				stream.Context(), fed, handle, body.AttributeUpdate, committed+1,
			)
			lease.end()
			if operationErr != nil {
				return sendLocalLRCAckBeforeError(stream, committed, lastAck, operationErr)
			}
			committed = sequence
		case *rtiv1.LocalLRCRequest_Interaction:
			// Pre-fence sequence validation — see the AttributeUpdate case.
			if seqErr := validateLocalLRCSequence(body.Interaction.GetSequence(), committed+1); seqErr != nil {
				return sendLocalLRCAckBeforeError(stream, committed, lastAck, seqErr)
			}
			lease, leaseErr := fencer.acquire(stream.Context())
			if leaseErr != nil {
				return sendLocalLRCAckBeforeError(stream, committed, lastAck, leaseErr)
			}
			sequence, operationErr := s.applyLocalLRCInteraction(
				stream.Context(), fed, handle, body.Interaction, committed+1,
			)
			lease.end()
			if operationErr != nil {
				return sendLocalLRCAckBeforeError(stream, committed, lastAck, operationErr)
			}
			committed = sequence
		case *rtiv1.LocalLRCRequest_Batch:
			operations := body.Batch.GetOperations()
			if len(operations) == 0 || len(operations) > int(batchOperations) {
				return sendLocalLRCAckBeforeError(stream, committed, lastAck,
					status.Errorf(codes.InvalidArgument,
						"local LRC batch contains %d operations; want 1..%d",
						len(operations), batchOperations))
			}
			// Pre-fence validation of every sequence in the frame (all
			// are known from the decoded batch): frame-decodable errors
			// surface before membership fencing, matching main's
			// sequence-before-membership precedence. A frame that fails
			// here applies NONE of its operations — an in-frame sequence
			// gap is a client protocol violation, and failing the whole
			// frame before fencing keeps the error independent of
			// concurrent teardown.
			for i, operation := range operations {
				opSequence, seqErr := localLRCOperationSequence(operation)
				if seqErr == nil {
					seqErr = validateLocalLRCSequence(opSequence, committed+uint64(i)+1)
				}
				if seqErr != nil {
					return sendLocalLRCAckBeforeError(stream, committed, lastAck, seqErr)
				}
			}
			lease, leaseErr := fencer.acquire(stream.Context())
			if leaseErr != nil {
				return sendLocalLRCAckBeforeError(stream, committed, lastAck, leaseErr)
			}
			// Apply the whole frame under the lease WITHOUT touching the
			// stream: stream.Send can park on client flow control, and the
			// shared side of the operation gate must never be held across
			// it — resign/destroy take the exclusive side, so a slow
			// client's ack window could otherwise stall teardown
			// indefinitely. The lease ends after the frame's last op and
			// BEFORE any ack Send; committed is final by then, so ack
			// content is unchanged.
			var frameErr error
			for _, operation := range operations {
				sequence, operationErr := s.applyLocalLRCOperation(
					stream.Context(), fed, handle, operation, committed+1,
				)
				if operationErr != nil {
					frameErr = operationErr
					break
				}
				committed = sequence
			}
			lease.end()
			// Emit the exact ack cadence the in-loop sends produced: one
			// ack per ack-every boundary crossed within this frame, with
			// the same CommittedThrough values (each op advances committed
			// by exactly 1, and committed-lastAck < ackEvery holds at
			// every frame start, so boundaries land on lastAck+k*ackEvery
			// precisely as before). Acks are NOT batched differently —
			// only moved after the lease release.
			for committed-lastAck >= uint64(ackEvery) {
				boundary := lastAck + uint64(ackEvery)
				if sendErr := stream.Send(&rtiv1.LocalLRCAck{CommittedThrough: boundary}); sendErr != nil {
					return sendErr
				}
				lastAck = boundary
			}
			if frameErr != nil {
				return sendLocalLRCAckBeforeError(stream, committed, lastAck, frameErr)
			}
			continue
		case *rtiv1.LocalLRCRequest_Flush:
			through := body.Flush.GetThroughSequence()
			if through > committed {
				return sendLocalLRCAckBeforeError(stream, committed, lastAck,
					status.Errorf(codes.FailedPrecondition,
						"local LRC flush through %d exceeds committed sequence %d", through, committed))
			}
			if err := stream.Send(&rtiv1.LocalLRCAck{CommittedThrough: committed}); err != nil {
				return err
			}
			lastAck = committed
			continue
		case *rtiv1.LocalLRCRequest_Open:
			return status.Error(codes.FailedPrecondition, "local LRC stream is already open")
		default:
			return status.Error(codes.InvalidArgument, "local LRC request has no operation")
		}

		lastAck, err = sendLocalLRCAckIfDue(stream, committed, lastAck, ackEvery)
		if err != nil {
			return err
		}
	}
}

// localLRCOperationSequence extracts the client-declared sequence of a
// batch operation for pre-fence validation, returning the same
// InvalidArgument errors the apply path reports for malformed
// operations. Kept in lockstep with applyLocalLRCOperation.
func localLRCOperationSequence(operation *rtiv1.LocalLRCOperation) (uint64, error) {
	if operation == nil {
		return 0, status.Error(codes.InvalidArgument, "local LRC batch contains a nil operation")
	}
	switch body := operation.GetBody().(type) {
	case *rtiv1.LocalLRCOperation_AttributeUpdate:
		return body.AttributeUpdate.GetSequence(), nil
	case *rtiv1.LocalLRCOperation_Interaction:
		return body.Interaction.GetSequence(), nil
	default:
		return 0, status.Error(codes.InvalidArgument, "local LRC batch operation has no body")
	}
}

func (s *localLRCService) applyLocalLRCOperation(
	ctx context.Context,
	fed core.FederationName,
	handle core.FederateHandle,
	operation *rtiv1.LocalLRCOperation,
	wantSequence uint64,
) (uint64, error) {
	if operation == nil {
		return 0, status.Error(codes.InvalidArgument, "local LRC batch contains a nil operation")
	}
	switch body := operation.GetBody().(type) {
	case *rtiv1.LocalLRCOperation_AttributeUpdate:
		return s.applyLocalLRCAttributeUpdate(ctx, fed, handle, body.AttributeUpdate, wantSequence)
	case *rtiv1.LocalLRCOperation_Interaction:
		return s.applyLocalLRCInteraction(ctx, fed, handle, body.Interaction, wantSequence)
	default:
		return 0, status.Error(codes.InvalidArgument, "local LRC batch operation has no body")
	}
}

func (s *localLRCService) applyLocalLRCAttributeUpdate(
	ctx context.Context,
	fed core.FederationName,
	handle core.FederateHandle,
	update *rtiv1.LocalLRCAttributeUpdate,
	wantSequence uint64,
) (uint64, error) {
	sequence := update.GetSequence()
	if err := validateLocalLRCSequence(sequence, wantSequence); err != nil {
		return 0, err
	}
	// Membership fencing is held by the caller's frame lease (W2):
	// Exchange acquires once per frame before dispatching here.
	//
	// W3 fast path: hand the freshly decoded proto map straight to the
	// registry. The wire version was validated once at stream Open, the
	// operation is RO with no retraction handle, and `update` is never
	// touched again after this call — the map's ownership transfers per
	// the core.WireApplier contract (no re-boxed unary request, no
	// copyAttrMap materializations, no per-op &rtiv1.Empty{}).
	if s.wire != nil {
		if err := s.wire.UpdateAttributesWire(
			ctx, fed, handle, core.ObjectHandle(update.GetObjectHandle()),
			update.GetAttributes(), nil,
		); err != nil {
			return 0, errToStatus(ctx, err)
		}
		return sequence, nil
	}
	_, err := s.server.objService.UpdateAttributeValues(ctx,
		&rtiv1.UpdateAttributeValuesRequest{
			WireVersion:    rtiv1.WireVersion_WIRE_VERSION_V1,
			FederationName: string(fed),
			FederateHandle: uint64(handle),
			ObjectHandle:   update.GetObjectHandle(),
			Attributes:     update.GetAttributes(),
		})
	return sequence, err
}

func (s *localLRCService) applyLocalLRCInteraction(
	ctx context.Context,
	fed core.FederationName,
	handle core.FederateHandle,
	interaction *rtiv1.LocalLRCInteraction,
	wantSequence uint64,
) (uint64, error) {
	sequence := interaction.GetSequence()
	if err := validateLocalLRCSequence(sequence, wantSequence); err != nil {
		return 0, err
	}
	// Membership fencing is held by the caller's frame lease (W2):
	// Exchange acquires once per frame before dispatching here.
	//
	// W3 fast path — see applyLocalLRCAttributeUpdate. `interaction` is
	// never touched again after this call; the decoded params map's
	// ownership transfers per the core.WireApplier contract.
	if s.wire != nil {
		if err := s.wire.SendInteractionWire(
			ctx, fed, handle,
			core.InteractionClassHandle(interaction.GetInteractionClassHandle()),
			interaction.GetParameters(), nil,
		); err != nil {
			return 0, errToStatus(ctx, err)
		}
		return sequence, nil
	}
	_, err := s.server.objService.SendInteraction(ctx,
		&rtiv1.SendInteractionRequest{
			WireVersion:            rtiv1.WireVersion_WIRE_VERSION_V1,
			FederationName:         string(fed),
			FederateHandle:         uint64(handle),
			InteractionClassHandle: interaction.GetInteractionClassHandle(),
			Parameters:             interaction.GetParameters(),
		})
	return sequence, err
}

func sendLocalLRCAckIfDue(
	stream rtiv1.LocalLRCService_ExchangeServer,
	committed, lastAck uint64,
	ackEvery uint32,
) (uint64, error) {
	if committed-lastAck < uint64(ackEvery) {
		return lastAck, nil
	}
	if err := stream.Send(&rtiv1.LocalLRCAck{CommittedThrough: committed}); err != nil {
		return lastAck, err
	}
	return committed, nil
}

func sendLocalLRCAckBeforeError(
	stream rtiv1.LocalLRCService_ExchangeServer,
	committed, lastAck uint64,
	operationErr error,
) error {
	if committed > lastAck {
		if err := stream.Send(&rtiv1.LocalLRCAck{CommittedThrough: committed}); err != nil {
			return err
		}
	}
	return operationErr
}

func (s *localLRCService) validateOpen(
	ctx context.Context,
	fed core.FederationName,
	handle core.FederateHandle,
	expectedGeneration uint64,
) error {
	if s.server.membership == nil {
		return status.Error(codes.FailedPrecondition, "local LRC requires membership fencing")
	}
	generation, ok := s.server.membership.GenerationFor(fed)
	if !ok {
		return errToStatus(ctx, core.ErrFederationNotFound)
	}
	if generation != expectedGeneration {
		return errToStatus(ctx, core.ErrFederationGenerationMismatch)
	}
	return errToStatus(ctx, s.server.membership.ValidateMember(fed, handle))
}

// localLRCFrameFencer resolves the membership fencing tier once at
// Exchange open (W2) — leaser (value lease, allocation-free), guard
// (bound release closure, compatibility fallback), or plain validator —
// so the per-frame hot path pays neither a type assertion nor a
// closure allocation. The unary/confirmed paths keep their existing
// per-op guard acquisition; this fencer is the LocalLRC stream path
// only.
type localLRCFrameFencer struct {
	leaser     core.FederationMembershipLeaser
	guard      core.FederationMembershipGuard
	membership core.FederationMembershipValidator
	fed        core.FederationName
	handle     core.FederateHandle
}

func newLocalLRCFrameFencer(
	membership core.FederationMembershipValidator,
	fed core.FederationName,
	handle core.FederateHandle,
) localLRCFrameFencer {
	f := localLRCFrameFencer{membership: membership, fed: fed, handle: handle}
	if leaser, ok := membership.(core.FederationMembershipLeaser); ok {
		f.leaser = leaser
	} else if guard, ok := membership.(core.FederationMembershipGuard); ok {
		f.guard = guard
	}
	return f
}

// localLRCFrameLease is one held frame fence. At most one of lease /
// release is populated; the zero value (validator-only tier) ends as a
// no-op. end MUST be called exactly once per successful acquire.
type localLRCFrameLease struct {
	lease   core.MemberLease
	release func()
}

func (l localLRCFrameLease) end() {
	if l.release != nil {
		l.release()
		return
	}
	l.lease.Release()
}

// acquire fences one received frame. Validation errors come back
// already mapped through errToStatus.
func (f *localLRCFrameFencer) acquire(ctx context.Context) (localLRCFrameLease, error) {
	switch {
	case f.leaser != nil:
		lease, err := f.leaser.AcquireMemberLease(f.fed, f.handle)
		if err != nil {
			return localLRCFrameLease{}, errToStatus(ctx, err)
		}
		return localLRCFrameLease{lease: lease}, nil
	case f.guard != nil:
		release, err := f.guard.AcquireMember(f.fed, f.handle)
		if err != nil {
			return localLRCFrameLease{}, errToStatus(ctx, err)
		}
		return localLRCFrameLease{release: release}, nil
	default:
		if err := f.membership.ValidateMember(f.fed, f.handle); err != nil {
			return localLRCFrameLease{}, errToStatus(ctx, err)
		}
		return localLRCFrameLease{}, nil
	}
}

func clampLocalLRCAckEvery(requested uint32) uint32 {
	if requested == 0 {
		return defaultLocalLRCAckEvery
	}
	if requested > maxLocalLRCAckEvery {
		return maxLocalLRCAckEvery
	}
	return requested
}

func clampLocalLRCBatchOperations(requested uint32) uint32 {
	if requested == 0 {
		return defaultLocalLRCBatchOperations
	}
	if requested > maxLocalLRCBatchOperations {
		return maxLocalLRCBatchOperations
	}
	return requested
}

func validateLocalLRCSequence(got, want uint64) error {
	if got == want {
		return nil
	}
	return status.Error(codes.FailedPrecondition,
		fmt.Sprintf("local LRC sequence %d, expected %d", got, want))
}
