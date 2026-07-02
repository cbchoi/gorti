package grpc

import (
	"context"

	"github.com/cbchoi/gorti/rti/internal/core"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/genproto/rti/v1"
	timepkg "github.com/cbchoi/gorti/rti/internal/time"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
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
	outbox core.Outbox
}

func newStreamService(outbox core.Outbox) *streamService {
	return &streamService{outbox: outbox}
}

// Events is the server-streaming RPC. One goroutine per federate
// connection, no worker pool (per docs/agent-a-rti-core.md §7).
func (s *streamService) Events(req *rtiv1.EventsRequest, stream rtiv1.StreamService_EventsServer) error {
	if !validWireVersion(req.GetWireVersion()) {
		return status.Error(codes.FailedPrecondition, "unsupported wire version")
	}
	sub, ok := s.outbox.(SubscribableOutbox)
	if !ok {
		return status.Error(codes.Unimplemented, "outbox does not support subscription (W4 wiring pending)")
	}

	fed := core.FederationName(req.GetFederationName())
	h := core.FederateHandle(req.GetFederateHandle())

	ch, cancel, err := sub.Subscribe(stream.Context(), fed, h)
	if err != nil {
		return errToStatus(stream.Context(), err)
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

// federateEventCarrier is a duck-typed interface satisfied by adapters in
// rti/internal/object that wrap the generated *rtiv1.FederateEvent. Using
// an interface here avoids a hard import dependency between transport/grpc
// and the object package.
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
		// §8.22 (M37 Agent EA) — same layering rationale as the two
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
