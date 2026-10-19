package grpc

import (
	"context"
	"fmt"
	"slices"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/object"
	"google.golang.org/protobuf/proto"
)

type TransportationCheckpointBinding = object.TransportationCheckpointBinding

func cloneTransportationBinding(b *TransportationCheckpointBinding) *TransportationCheckpointBinding {
	if b == nil {
		return nil
	}
	cloned := *b
	cloned.Attributes = slices.Clone(b.Attributes)
	cloned.ActualAttributes = slices.Clone(b.ActualAttributes)
	return &cloned
}

func validateTransportationCheckpointBody(event *rtiv1.FederateEvent, binding *TransportationCheckpointBinding) error {
	if event == nil || hasUnknownCallbackFields(event.ProtoReflect()) {
		return fmt.Errorf("transportation checkpoint: missing or unknown body")
	}
	body := event.GetTransportationChangeConfirmed()
	if body == nil {
		if binding != nil {
			return fmt.Errorf("transportation checkpoint: binding without confirmation")
		}
		return nil
	}
	if err := validateTransportationConfirmation(body); err != nil {
		return err
	}
	if binding == nil {
		return nil
	}
	if err := object.ValidateTransportationCheckpointBinding(*binding); err != nil {
		return err
	}
	if uint64(binding.Object) != body.ObjectHandle || uint64(binding.Interaction) != body.InteractionClassHandle || binding.Transportation != protoTransportTypeToCore(body.TransportType) {
		return fmt.Errorf("transportation checkpoint: intent differs from callback body")
	}
	for _, attr := range binding.Attributes {
		if !slices.Contains(body.AttributeHandles, uint64(attr)) {
			return fmt.Errorf("transportation checkpoint: retained attribute outside original callback")
		}
	}
	return nil
}

// Snapshot must be captured before taking the general ledger mutex. A real
// source capability cannot be saved without a matching live domain projection.
func CaptureTransportationCheckpointBinding(snapshot object.TransportationCheckpointSnapshot, recipient core.FederateHandle, event *rtiv1.FederateEvent, outcomeOnly bool, retained ...*TransportationCheckpointBinding) (*TransportationCheckpointBinding, error) {
	if recipient == 0 || len(retained) > 1 || event == nil {
		return nil, fmt.Errorf("transportation checkpoint: invalid capture arguments")
	}
	var binding *TransportationCheckpointBinding
	if len(retained) == 1 {
		binding = cloneTransportationBinding(retained[0])
	}
	body := event.GetTransportationChangeConfirmed()
	if body == nil {
		return nil, validateTransportationCheckpointBody(event, binding)
	}
	// A finished application result is reconciled centrally. Its domain ticket
	// may already be retired or evicted; it must never be activated again.
	if outcomeOnly {
		return nil, validateTransportationCheckpointBody(event, nil)
	}
	var err error
	binding, err = captureTransportationAuthority(snapshot, recipient, event, binding, outcomeOnly)
	if err != nil {
		return nil, err
	}
	if err := validateTransportationCheckpointBody(event, binding); err != nil {
		return nil, err
	}
	return binding, nil
}

// Strip is also used by canonical callback-body comparison: nested authority
// must not change invocation identity or authorize a future execution.
func StripTransportationCheckpointEvent(event *rtiv1.FederateEvent, binding *TransportationCheckpointBinding) (*rtiv1.FederateEvent, error) {
	if err := validateTransportationCheckpointBody(event, binding); err != nil {
		return nil, err
	}
	cloned := proto.Clone(event).(*rtiv1.FederateEvent)
	if body := cloned.GetTransportationChangeConfirmed(); body != nil {
		body.Ticket, body.FederationGeneration, body.CallbackEpoch = nil, 0, 0
	}
	return cloned, nil
}

// Validate before any restore allocation. Grammar-only zero-ticket descriptors
// are permitted by capture, but a real replay confirmation requires a binding.
func ValidateTransportationCheckpointEvent(event *rtiv1.FederateEvent, binding *TransportationCheckpointBinding, outcomeOnly ...bool) error {
	if len(outcomeOnly) > 1 {
		return fmt.Errorf("transportation checkpoint: ambiguous outcome phase")
	}
	finished := len(outcomeOnly) == 1 && outcomeOnly[0]
	if err := validateTransportationCheckpointBody(event, binding); err != nil {
		return err
	}
	if body := event.GetTransportationChangeConfirmed(); body != nil {
		if (!finished && binding == nil) || len(body.Ticket) != 0 || body.FederationGeneration != 0 || body.CallbackEpoch != 0 {
			return fmt.Errorf("transportation checkpoint: missing binding or persisted nested authority")
		}
	}
	return nil
}

type TransportationCheckpointRestorer interface {
	ReserveTransportationCheckpoint(context.Context, []object.TransportationCheckpointRestore) (*object.TransportationCheckpointReservation, error)
}

type TransportationCheckpointReplay struct {
	Event       *rtiv1.FederateEvent
	Binding     *TransportationCheckpointBinding
	Scope       core.TransportationScope
	OutcomeOnly bool
}

type TransportationCheckpointReplayReservation struct {
	events []*rtiv1.FederateEvent
	domain *object.TransportationCheckpointReservation
}

func (r *TransportationCheckpointReplayReservation) Events() []*rtiv1.FederateEvent {
	result := make([]*rtiv1.FederateEvent, len(r.events))
	for i, event := range r.events {
		result[i] = proto.Clone(event).(*rtiv1.FederateEvent)
	}
	return result
}

func (r *TransportationCheckpointReplayReservation) Commit() {
	if r.domain != nil {
		r.domain.Commit()
	}
}

func (r *TransportationCheckpointReplayReservation) Release() {
	if r.domain != nil {
		r.domain.Release()
	}
}

func (r *TransportationCheckpointReplayReservation) Rollback() {
	if r.domain != nil {
		r.domain.Rollback()
	}
}

// All rows are validated before batch allocation. Call only after domain import
// under exclusive checkpoint admission. Scope uses planned epochs, not an
// outbox lookup. Parent central preflight still proves every finished outcome.
func PrepareTransportationCheckpointEvents(ctx context.Context, restorer TransportationCheckpointRestorer, rows []TransportationCheckpointReplay, prepared ...*object.TransportationCheckpointReservation) (*TransportationCheckpointReplayReservation, error) {
	if len(prepared) > 1 {
		return nil, fmt.Errorf("transportation checkpoint: ambiguous prepared reservation")
	}
	result := &TransportationCheckpointReplayReservation{events: make([]*rtiv1.FederateEvent, len(rows))}
	requests, indices, err := prepareTransportationCheckpointRequests(rows, result.events)
	if err != nil {
		return nil, err
	}
	if len(requests) == 0 {
		if len(prepared) == 1 && prepared[0] != nil && len(prepared[0].Changes()) != 0 {
			return nil, fmt.Errorf("transportation checkpoint: unexpected prepared tickets")
		}
		return result, nil
	}
	if restorer == nil && len(prepared) == 0 {
		return nil, fmt.Errorf("transportation checkpoint: live reservation port unavailable")
	}
	var reservation *object.TransportationCheckpointReservation
	if len(prepared) == 1 {
		reservation = prepared[0]
	} else {
		reservation, err = restorer.ReserveTransportationCheckpoint(ctx, requests)
	}
	if err != nil {
		return nil, err
	}
	if reservation == nil || len(reservation.Changes()) != len(indices) {
		if reservation != nil {
			reservation.Release()
		}
		return nil, fmt.Errorf("transportation checkpoint: incomplete reservation")
	}
	result.domain = reservation
	if err := applyTransportationCheckpointChanges(result, reservation, requests, indices); err != nil {
		return nil, err
	}
	return result, nil
}
func validateTransportationConfirmation(body *rtiv1.TransportationChange) error {
	if (body.ObjectHandle == 0) == (body.InteractionClassHandle == 0) || body.ObjectHandle == ^uint64(0) || body.InteractionClassHandle == ^uint64(0) || (body.InteractionClassHandle != 0 && len(body.AttributeHandles) != 0) || (body.ObjectHandle != 0 && len(body.AttributeHandles) == 0) || (body.TransportType != rtiv1.TransportationType_TRANSPORTATION_TYPE_RELIABLE && body.TransportType != rtiv1.TransportationType_TRANSPORTATION_TYPE_BEST_EFFORT) {
		return fmt.Errorf("transportation checkpoint: invalid confirmation target")
	}
	return validateTransportationConfirmationAttributes(body.AttributeHandles)
}

func validateTransportationConfirmationAttributes(attributes []uint64) error {
	for i, attr := range attributes {
		if attr == 0 || attr == ^uint64(0) || (i > 0 && attributes[i-1] >= attr) {
			return fmt.Errorf("transportation checkpoint: invalid confirmation attributes")
		}
	}
	return nil
}

func captureTransportationAuthority(snapshot object.TransportationCheckpointSnapshot, recipient core.FederateHandle, event *rtiv1.FederateEvent, binding *TransportationCheckpointBinding, outcomeOnly bool) (*TransportationCheckpointBinding, error) {
	body := event.GetTransportationChangeConfirmed()
	if len(body.Ticket) != 0 {
		if len(body.Ticket) != 32 {
			return nil, fmt.Errorf("transportation checkpoint: malformed source ticket")
		}
		live, ok := snapshot[string(body.Ticket)]
		if !ok || live.Scope.Member.Handle != recipient || live.Scope.Member.Generation != body.FederationGeneration || live.Scope.CallbackEpoch != body.CallbackEpoch || live.Scope.Member.Incarnation == 0 {
			return nil, fmt.Errorf("transportation checkpoint: source ticket has no verified live projection")
		}
		binding = cloneTransportationBinding(&live.Binding)
	} else {
		if body.FederationGeneration != 0 || body.CallbackEpoch != 0 {
			return nil, fmt.Errorf("transportation checkpoint: scope without source ticket")
		}
		if binding != nil {
			recovery := event.GetCallbackRecovery()
			if !validTransportationOutcomeRecovery(outcomeOnly, recovery) {
				return nil, fmt.Errorf("transportation checkpoint: retained authority-free binding needs outcome recovery")
			}
		}
	}
	return binding, nil
}

func prepareTransportationCheckpointRequests(rows []TransportationCheckpointReplay, events []*rtiv1.FederateEvent) ([]object.TransportationCheckpointRestore, []int, error) {
	var requests []object.TransportationCheckpointRestore
	var indices []int
	for i, row := range rows {
		if err := ValidateTransportationCheckpointEvent(row.Event, row.Binding, row.OutcomeOnly); err != nil {
			return nil, nil, err
		}
		events[i] = proto.Clone(row.Event).(*rtiv1.FederateEvent)
		if row.Event.GetTransportationChangeConfirmed() == nil {
			continue
		}
		if recovery := row.Event.GetCallbackRecovery(); recovery != nil && (recovery.GetCompleted() || recovery.GetOutcomeOnly() != row.OutcomeOnly) {
			return nil, nil, fmt.Errorf("transportation checkpoint: contradictory recovery phase")
		}
		if row.OutcomeOnly {
			continue
		}
		requests = append(requests, object.TransportationCheckpointRestore{Scope: row.Scope, Binding: *cloneTransportationBinding(row.Binding)})
		indices = append(indices, i)
	}
	return requests, indices, nil
}

func applyTransportationCheckpointChanges(result *TransportationCheckpointReplayReservation, reservation *object.TransportationCheckpointReservation, requests []object.TransportationCheckpointRestore, indices []int) error {
	for i, change := range reservation.Changes() {
		request := requests[i]
		if len(change.Ticket) != 32 || change.Scope != request.Scope || change.Object != request.Binding.Object || change.Interaction != request.Binding.Interaction || change.Transportation != request.Binding.Transportation || !slices.Equal(change.Attributes, request.Binding.Attributes) {
			reservation.Release()
			return fmt.Errorf("transportation checkpoint: prepared ticket intent mismatch")
		}
		body := result.events[indices[i]].GetTransportationChangeConfirmed()
		body.Ticket = slices.Clone(change.Ticket)
		body.FederationGeneration, body.CallbackEpoch = change.Scope.Member.Generation, change.Scope.CallbackEpoch
	}
	return nil
}
func validTransportationOutcomeRecovery(outcomeOnly bool, recovery *rtiv1.CallbackRecovery) bool {
	return outcomeOnly && recovery != nil && len(recovery.GetInvocationIdentity()) == 32 && recovery.GetOutcomeOnly() && !recovery.GetCompleted()
}
