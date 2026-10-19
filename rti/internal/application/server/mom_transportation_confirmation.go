package server

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/federation"
	"github.com/cbchoi/gorti/rti/internal/hla/mom"
	"github.com/cbchoi/gorti/rti/pkg/logicaltime"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

type momTransportationConfirmationDependencies struct {
	Federations    *federation.Manager
	Float, Integer core.TransportationActivation
	CallbackEpoch  func(core.FederationName, core.FederateHandle) (uint64, error)
	NextSequence   func(core.FederationName) uint64
	Outbox         core.ReservableOutbox
}

type momTransportationPreparationCancellation interface {
	CancelTransportationChange(context.Context, core.TransportationScope, []byte) error
}

type momTransportationEvent struct{ event *rtiv1.FederateEvent }

func (e *momTransportationEvent) Seq() uint64 { return e.event.GetSeq() }
func (e *momTransportationEvent) Inner() *rtiv1.FederateEvent {
	return proto.Clone(e.event).(*rtiv1.FederateEvent)
}

func newMOMTransportationConfirmation(deps momTransportationConfirmationDependencies) (func(context.Context, core.FederationName, core.FederateHandle, mom.MOMServiceCommand, core.TransportType) error, error) {
	if deps.Federations == nil || deps.Float == nil || deps.Integer == nil || deps.CallbackEpoch == nil || deps.Outbox == nil {
		return nil, fmt.Errorf("MOM transportation confirmation dependencies incomplete")
	}
	for _, activation := range []core.TransportationActivation{deps.Float, deps.Integer} {
		if _, ok := activation.(momTransportationPreparationCancellation); !ok {
			return nil, status.Error(codes.Unimplemented, "MOM transportation queue rollback requires CancelTransportationChange")
		}
	}
	confirmation := &momTransportationConfirmation{deps: deps, lastSequence: make(map[core.FederationName]uint64)}
	return confirmation.confirm, nil
}

func prepareMOMTransportationChange(ctx context.Context, activation core.TransportationActivation, scope core.TransportationScope, command mom.MOMServiceCommand, transport core.TransportType) (core.TransportationChange, error) {
	if command.Target != scope.Member.Handle {
		return core.TransportationChange{}, core.ErrFederateNotJoined
	}
	switch command.Service {
	case momServiceRequestAttributeTransportationTypeChange:
		return activation.PrepareAttributeTransportationChange(ctx, scope, command.Object, command.Attributes, transport)
	case momServiceRequestInteractionTransportationTypeChange:
		return activation.PrepareInteractionTransportationChange(ctx, scope, command.InteractionClass, transport)
	default:
		return core.TransportationChange{}, fmt.Errorf("invalid MOM transportation service %q", command.Service)
	}
}

type momTransportationConfirmation struct {
	deps         momTransportationConfirmationDependencies
	commitMu     sync.Mutex
	lastSequence map[core.FederationName]uint64
}

func (c *momTransportationConfirmation) confirm(ctx context.Context, fed core.FederationName, target core.FederateHandle, command mom.MOMServiceCommand, transport core.TransportType) error {
	id, err := c.deps.Federations.MemberIdentity(fed, target)
	if err != nil {
		return err
	}
	epoch, err := c.deps.CallbackEpoch(fed, target)
	if err != nil {
		return err
	}
	activation, err := c.activation(fed)
	if err != nil {
		return err
	}
	scope := core.TransportationScope{Member: id, CallbackEpoch: epoch}
	change, err := prepareMOMTransportationChange(ctx, activation, scope, command, transport)
	if err != nil {
		return err
	}
	attributes := make([]uint64, len(change.Attributes))
	for i, attribute := range change.Attributes {
		attributes[i] = uint64(attribute)
	}
	wire := &rtiv1.TransportationChange{Ticket: slices.Clone(change.Ticket), FederationGeneration: change.Scope.Member.Generation, CallbackEpoch: change.Scope.CallbackEpoch, ObjectHandle: uint64(change.Object), AttributeHandles: attributes, InteractionClassHandle: uint64(change.Interaction), TransportType: rtiv1.TransportationType(change.Transportation)}
	// Sequence allocation and queue commit share an order. Preparation above
	// holds no registry locks, so reservation cannot invert object/queue locks.
	c.commitMu.Lock()
	defer c.commitMu.Unlock()
	sequence := c.lastSequence[fed] + 1
	if c.deps.NextSequence != nil {
		sequence = c.deps.NextSequence(fed)
	}
	if sequence == 0 || sequence <= c.lastSequence[fed] {
		err := status.Error(codes.ResourceExhausted, "MOM transportation callback sequence must increase")
		cancelErr := activation.(momTransportationPreparationCancellation).CancelTransportationChange(context.WithoutCancel(ctx), scope, change.Ticket)
		return errors.Join(err, cancelErr)
	}
	c.lastSequence[fed] = sequence
	event := &momTransportationEvent{event: &rtiv1.FederateEvent{Seq: sequence, Event: &rtiv1.FederateEvent_TransportationChangeConfirmed{TransportationChangeConfirmed: wire}}}
	// Prepare releases object locks before queue reservation. Failure cancels
	// only this scoped, inactive ticket and leaves the old transport intact.
	reservation, err := c.deps.Outbox.Reserve(ctx, fed, []core.OutboxDelivery{{Recipient: target, Event: event}})
	if err != nil || reservation == nil {
		if reservation != nil {
			reservation.Release()
		}
		if err == nil {
			err = fmt.Errorf("MOM transportation callback reservation unavailable")
		}
		cancelErr := activation.(momTransportationPreparationCancellation).CancelTransportationChange(context.WithoutCancel(ctx), scope, change.Ticket)
		return errors.Join(err, cancelErr)
	}
	defer reservation.Release()
	if err := reservation.Commit(); err != nil {
		reservation.Release()
		return errors.Join(err, activation.(momTransportationPreparationCancellation).CancelTransportationChange(context.WithoutCancel(ctx), scope, change.Ticket))
	}
	return nil
}

func (c *momTransportationConfirmation) activation(fed core.FederationName) (core.TransportationActivation, error) {
	factory, exists := c.deps.Federations.LogicalTimeImplementationFor(fed)
	if !exists {
		return nil, core.ErrFederationNotFound
	}
	activation := c.deps.Float
	if factory == logicaltime.HLAinteger64Time.String() {
		activation = c.deps.Integer
	} else if factory != logicaltime.HLAfloat64Time.String() {
		return nil, core.ErrCouldNotCreateLogicalTimeFactory
	}
	return activation, nil
}
