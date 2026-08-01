package grpc

import (
	"context"
	"fmt"

	"github.com/cbchoi/gorti/rti/internal/core"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/genproto/rti/v1"
	timepkg "github.com/cbchoi/gorti/rti/internal/time"
	stdgrpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
)

const (
	grpcDefaultMaxReceiveMessageSize = 4 << 20
	eventBatchSizeSafetyMargin       = 64 << 10
	maxEventBatchSerializedSize      = grpcDefaultMaxReceiveMessageSize - eventBatchSizeSafetyMargin
	// maxCoalescedEventBatchEvents caps how many callback events the
	// greedy drain in sendAvailableEventBatches concatenates into one
	// wire frame. The contractual outbox admission batch stays 32; this
	// cap only bounds how many already-ready 32-event batches share a
	// frame (W7: 64 -> 256, i.e. up to 8 batches per frame instead of 2).
	// Grant frame-cut semantics are unaffected: a TimeAdvanceGrant still
	// ends the current wire frame immediately.
	maxCoalescedEventBatchEvents = 256
)

// SubscribableOutbox is the M2 forward-declaration of the richer Outbox
// the StreamService.Events handler needs. core.Outbox is push-only by
// design; the streaming handler must register itself as a subscriber and
// drain a channel of OutboundEvents.
//
// W4 (cmd/rtid wiring) supplies the production implementation. For tests
// here, a fake satisfies the interface with an in-memory channel.
//
// Cancel returns any error encountered while tearing down the
// subscription; the handler returns its own loop error first, so the
// cancel error is best-effort.
type SubscribableOutbox interface {
	core.Outbox
	Subscribe(ctx context.Context, fed core.FederationName, h core.FederateHandle) (<-chan []core.OutboundEvent, func() error, error)
}

// streamService binds rtiv1.StreamServiceServer to a core.Outbox. If the
// outbox additionally satisfies SubscribableOutbox, Events drains the
// federate's outbound channel and forwards each event onto the gRPC
// server stream. Otherwise Events returns Unimplemented.
type streamService struct {
	rtiv1.UnimplementedStreamServiceServer
	outbox     core.Outbox
	membership core.FederationMembershipValidator
}

func newStreamService(outbox core.Outbox, memberships ...core.FederationMembershipValidator) *streamService {
	var membership core.FederationMembershipValidator
	if len(memberships) > 0 {
		membership = memberships[0]
	}
	return &streamService{outbox: outbox, membership: membership}
}

// Events is the server-streaming RPC. One goroutine per federate
// connection, with no worker pool.
func (s *streamService) Events(req *rtiv1.EventsRequest, stream rtiv1.StreamService_EventsServer) error {
	if !validWireVersion(req.GetWireVersion()) {
		return status.Error(codes.FailedPrecondition, "unsupported wire version")
	}
	ch, cancel, err := s.subscribe(req, stream)
	if err != nil {
		return err
	}
	defer func() { _ = cancel() }()

	for {
		select {
		case batch, alive := <-ch:
			if !alive {
				return nil
			}
			for _, evt := range batch {
				pb, err := toFederateEvent(evt)
				if err != nil {
					return status.Error(codes.Internal, err.Error())
				}
				if err := stream.Send(pb); err != nil {
					return err
				}
			}
		case <-stream.Context().Done():
			return stream.Context().Err()
		}
	}
}

// EventBatches forwards each outbox batch as one or more size-bounded gRPC
// messages. Events remains available for older clients; both methods consume
// the same ordered outbox subscription and have identical callback semantics.
func (s *streamService) EventBatches(req *rtiv1.EventsRequest, stream rtiv1.StreamService_EventBatchesServer) error {
	if !validWireVersion(req.GetWireVersion()) {
		return status.Error(codes.FailedPrecondition, "unsupported wire version")
	}
	ch, cancel, err := s.subscribe(req, stream)
	if err != nil {
		return err
	}
	defer func() { _ = cancel() }()
	if err := stream.Send(&rtiv1.FederateEventBatch{Ready: true}); err != nil {
		return err
	}

	scratch := make([]core.OutboundEvent, 0, maxCoalescedEventBatchEvents)
	for {
		select {
		case batch, alive := <-ch:
			if !alive {
				return nil
			}
			closed, err := sendAvailableEventBatches(batch, ch, stream, scratch)
			if err != nil {
				return err
			}
			if closed {
				return nil
			}
		case <-stream.Context().Done():
			return stream.Context().Err()
		}
	}
}

// sendAvailableEventBatches combines only source batches that are already
// queued. It therefore reduces transport frames under load without adding a
// batching delay at low traffic rates. A time grant ends the current wire
// batch so callbacks that follow it remain observably after the grant.
func sendAvailableEventBatches(
	first []core.OutboundEvent,
	ch <-chan []core.OutboundEvent,
	stream rtiv1.StreamService_EventBatchesServer,
	scratch []core.OutboundEvent,
) (bool, error) {
	pending := scratch[:0]
	batch := first

	for {
		if err := stream.Context().Err(); err != nil {
			return false, err
		}
		for _, evt := range batch {
			pending = append(pending, evt)
			_, isGrant := evt.(*timepkg.TimeAdvanceGrant)
			if len(pending) == maxCoalescedEventBatchEvents || isGrant {
				if err := sendEventBatchChunks(pending, stream); err != nil {
					return false, err
				}
				pending = pending[:0]
			}
		}

		select {
		case <-stream.Context().Done():
			return false, stream.Context().Err()
		case next, alive := <-ch:
			if !alive {
				if err := sendEventBatchChunks(pending, stream); err != nil {
					return false, err
				}
				return true, nil
			}
			batch = next
		default:
			if err := sendEventBatchChunks(pending, stream); err != nil {
				return false, err
			}
			return false, nil
		}
	}
}

func (s *streamService) subscribe(
	req *rtiv1.EventsRequest,
	stream stdgrpc.ServerStream,
) (<-chan []core.OutboundEvent, func() error, error) {
	sub, ok := s.outbox.(SubscribableOutbox)
	if !ok {
		return nil, nil, status.Error(codes.Unimplemented, "outbox does not support subscription (W4 wiring pending)")
	}

	fed := core.FederationName(req.GetFederationName())
	h := core.FederateHandle(req.GetFederateHandle())
	membership := s.membershipForStream(stream)
	var release func()
	if membership != nil {
		var err error
		if guard, ok := membership.(core.FederationMembershipGuard); ok {
			release, err = guard.AcquireMember(fed, h)
		} else {
			err = membership.ValidateMember(fed, h)
		}
		if err != nil {
			return nil, nil, errToStatus(stream.Context(), err)
		}
		if release != nil {
			defer release()
		}
		if req.ExpectedFederationGeneration != nil {
			generation, exists := membership.GenerationFor(fed)
			if !exists {
				return nil, nil, errToStatus(stream.Context(), core.ErrFederationNotFound)
			}
			if generation != req.GetExpectedFederationGeneration() {
				return nil, nil, errToStatus(stream.Context(), core.ErrFederationGenerationMismatch)
			}
		}
	}

	ch, cancel, err := sub.Subscribe(stream.Context(), fed, h)
	if err != nil {
		return nil, nil, errToStatus(stream.Context(), err)
	}
	return ch, cancel, nil
}

func (s *streamService) membershipForStream(stream stdgrpc.ServerStream) core.FederationMembershipValidator {
	if s.membership != nil {
		return s.membership
	}

	var wrapped stdgrpc.ServerStream
	switch typed := stream.(type) {
	case *stdgrpc.GenericServerStream[rtiv1.EventsRequest, rtiv1.FederateEvent]:
		wrapped = typed.ServerStream
	case *stdgrpc.GenericServerStream[rtiv1.EventsRequest, rtiv1.FederateEventBatch]:
		wrapped = typed.ServerStream
	}
	membershipStream, ok := wrapped.(*membershipServerStream)
	if !ok || membershipStream.server == nil {
		return nil
	}
	return membershipStream.server.membership
}

func sendEventBatchChunks(
	batch []core.OutboundEvent,
	stream rtiv1.StreamService_EventBatchesServer,
) error {
	if len(batch) == 0 {
		return nil
	}
	wireBatch := &rtiv1.FederateEventBatch{Events: newWireEventSlice(len(batch))}
	wireSize := 0
	for i, evt := range batch {
		pb, err := toFederateEvent(evt)
		if err != nil {
			return status.Error(codes.Internal, err.Error())
		}
		entrySize := federateEventBatchEntrySize(pb)
		if entrySize > maxEventBatchSerializedSize {
			return status.Error(codes.ResourceExhausted, fmt.Sprintf(
				"callback event seq %d serialized size %d exceeds safe batch limit %d",
				pb.GetSeq(), entrySize, maxEventBatchSerializedSize,
			))
		}

		if len(wireBatch.Events) > 0 && wireSize+entrySize > maxEventBatchSerializedSize {
			if err := stream.Send(wireBatch); err != nil {
				return err
			}
			wireBatch = &rtiv1.FederateEventBatch{Events: newWireEventSlice(len(batch) - i)}
			wireSize = 0
		}
		wireBatch.Events = append(wireBatch.Events, pb)
		wireSize += entrySize
	}
	if len(wireBatch.Events) == 0 {
		return nil
	}
	return stream.Send(wireBatch)
}

// newWireEventSlice preallocates a wire batch's Events backing array for
// the events still to be framed (W6c). Outbox-fed batches never exceed
// maxCoalescedEventBatchEvents, so the cap covers the common case in one
// allocation; longer test-only batches simply grow past it.
func newWireEventSlice(remaining int) []*rtiv1.FederateEvent {
	if remaining > maxCoalescedEventBatchEvents {
		remaining = maxCoalescedEventBatchEvents
	}
	return make([]*rtiv1.FederateEvent, 0, remaining)
}

// Slack constants for the W6b conservative entry-size bound. Each term
// over-covers the exact protobuf wire bytes it stands in for, so the
// bound is provably >= proto.Size for the covered event shapes:
//
//   - federateEventEnvelopeSizeSlack covers every non-payload byte of a
//     Reflect/Receive FederateEvent: seq (tag + varint <= 11), the oneof
//     submessage framing (tag 1 + length varint <= 5), two uint64 handle
//     fields (<= 11 each), and the optional logical-time double
//     (tag + fixed64 = 9). Worst case 48 <= 64.
//   - federateEventValueSizeSlack covers one map<uint64, bytes> entry's
//     framing: entry tag (1) + entry length varint (<= 5) + key
//     tag + varint (<= 11) + value tag (1) + value length varint (<= 5).
//     Worst case 23 <= 32.
const (
	federateEventEnvelopeSizeSlack = 64
	federateEventValueSizeSlack    = 32
)

// federateEventBatchEntrySize returns a conservative upper bound on the
// serialized size of one FederateEventBatch.events entry (W6b). For the
// hot callback shapes (Reflect / Receive) the bound is the sum of the
// payload byte lengths plus fixed slack — no reflective proto.Size tree
// walk. Every other event type, and any bound crossing
// maxEventBatchSerializedSize/2 (where over-estimation could split or
// reject frames that actually fit), falls back to the exact walk, so the
// per-event rejection threshold is always evaluated on exact sizes.
func federateEventBatchEntrySize(event *rtiv1.FederateEvent) int {
	bound := federateEventEnvelopeSizeSlack
	switch body := event.GetEvent().(type) {
	case *rtiv1.FederateEvent_Reflect:
		for _, v := range body.Reflect.GetAttributes() {
			bound += federateEventValueSizeSlack + len(v)
		}
	case *rtiv1.FederateEvent_Receive:
		for _, v := range body.Receive.GetParameters() {
			bound += federateEventValueSizeSlack + len(v)
		}
	default:
		return exactFederateEventBatchEntrySize(event)
	}
	if bound > maxEventBatchSerializedSize/2 {
		return exactFederateEventBatchEntrySize(event)
	}
	return protowire.SizeTag(1) + protowire.SizeBytes(bound)
}

// exactFederateEventBatchEntrySize is the precise serialized size of one
// batch entry: repeated-field tag + length-delimited event bytes.
func exactFederateEventBatchEntrySize(event *rtiv1.FederateEvent) int {
	return protowire.SizeTag(1) + protowire.SizeBytes(proto.Size(event))
}

// federateEventCarrier is a duck-typed interface satisfied by adapters in
// rti/internal/object that wrap the generated *rtiv1.FederateEvent. Using
// an interface here avoids a hard import dependency between transport/grpc
// and the object package. Inner transfers an event that must remain immutable
// until the synchronous stream Send that consumes its batch returns.
type federateEventCarrier interface {
	Inner() *rtiv1.FederateEvent
}

// toFederateEvent extracts the generated proto from a core.OutboundEvent.
// Returns an error rather than panicking when the event does not expose
// the proto, so the handler can surface a clean Internal status.
//
// The generated *rtiv1.FederateEvent does not satisfy core.OutboundEvent
// directly (Seq is a field, not a method). Producers wrap it in an
// adapter that satisfies federateEventCarrier; the registry's
// outboundEvent in rti/internal/object is the canonical example.
//
// M21 TASK-204b: time-management events (*time.TimeAdvanceGrant,
// *time.FederationHalted) do NOT implement federateEventCarrier —
// the time package must not import the generated proto (layering).
// They are translated here via type-switch instead.
func toFederateEvent(evt core.OutboundEvent) (*rtiv1.FederateEvent, error) {
	if c, ok := evt.(federateEventCarrier); ok {
		return c.Inner(), nil
	}
	switch v := evt.(type) {
	case *timepkg.TimeAdvanceGrant:
		return &rtiv1.FederateEvent{
			Seq: v.Seq(),
			Event: &rtiv1.FederateEvent_Grant{
				Grant: &rtiv1.TimeAdvanceGrant{
					LogicalTime: float64(v.Time),
				},
			},
		}, nil
	case *timepkg.FederationHalted:
		return &rtiv1.FederateEvent{
			Seq: v.Seq(),
			Event: &rtiv1.FederateEvent_Halted{
				Halted: &rtiv1.FederationHalted{
					Cause: v.Cause,
				},
			},
		}, nil
	case *timepkg.RequestRetraction:
		// §8.22 (M37) — same layering rationale as the two
		// cases above: the time package must not import the proto.
		return &rtiv1.FederateEvent{
			Seq: v.Seq(),
			Event: &rtiv1.FederateEvent_RetractionRequested{
				RetractionRequested: &rtiv1.RequestRetraction{
					SenderFederate:          uint64(v.Sender),
					MessageRetractionHandle: v.RetractionHandle,
				},
			},
		}, nil
	}
	return nil, errOutboundEventNotConvertible
}

// errOutboundEventNotConvertible signals an OutboundEvent that the stream
// handler cannot serialize. Surface as Internal: this is a wiring bug,
// not a client error.
var errOutboundEventNotConvertible = errOutboundEvent("outbound event has no *rtiv1.FederateEvent payload")

// errOutboundEvent is a tiny named-string error type so the message is
// stable for tests without pulling in fmt.
type errOutboundEvent string

func (e errOutboundEvent) Error() string { return string(e) }

// Compile-time assertion that streamService implements the generated
// server interface.
var _ rtiv1.StreamServiceServer = (*streamService)(nil)
