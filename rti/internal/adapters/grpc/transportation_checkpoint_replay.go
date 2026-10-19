package grpc

import (
	"context"
	"fmt"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/object"
	"google.golang.org/protobuf/proto"
)

type callbackReplayTransportation struct {
	recipient   core.FederateHandle
	binding     *TransportationCheckpointBinding
	event       *rtiv1.FederateEvent
	outcomeOnly bool
}

// The coordinator retains membership/outbox locks here, so scopes are fully
// precomputed. Domain allocation finishes before acquiring the ledger mutex.
func (p *CallbackReplayReservation) PrepareTransportation(ctx context.Context, restorer TransportationCheckpointRestorer, scopes map[core.FederateHandle]core.TransportationScope, prepared ...*object.TransportationCheckpointReservation) (*TransportationCheckpointReplayReservation, error) {
	rows := make([]TransportationCheckpointReplay, len(p.transportation))
	for i, pending := range p.transportation {
		scope, ok := scopes[pending.recipient]
		if !ok && !pending.outcomeOnly {
			return nil, fmt.Errorf("transportation checkpoint: missing planned recipient scope")
		}
		rows[i] = TransportationCheckpointReplay{Event: pending.event, Binding: pending.binding, Scope: scope, OutcomeOnly: pending.outcomeOnly}
	}
	reservation, err := PrepareTransportationCheckpointEvents(ctx, restorer, rows, prepared...)
	if err != nil {
		return nil, err
	}
	events := reservation.Events()
	p.receipts.mu.Lock()
	for _, pending := range p.transportation {
		entry := p.receipts.entries[string(pending.event.CallbackReceipt)]
		if entry == nil || !entry.restorePrepared || entry.fed != pendingScopeFederation(scopes, pending.recipient) {
			p.receipts.mu.Unlock()
			reservation.Rollback()
			return nil, fmt.Errorf("transportation checkpoint: prepared receipt no longer reserved")
		}
	}
	for i, pending := range p.transportation {
		entry := p.receipts.entries[string(pending.event.CallbackReceipt)]
		entry.event = proto.Clone(events[i]).(*rtiv1.FederateEvent)
		entry.event.CallbackReceipt = nil
		// Restore queues retain this exact carrier pointer, not a wire copy.
		proto.Reset(pending.event)
		proto.Merge(pending.event, events[i])
	}
	p.receipts.mu.Unlock()
	return reservation, nil
}

func pendingScopeFederation(scopes map[core.FederateHandle]core.TransportationScope, recipient core.FederateHandle) core.FederationName {
	return scopes[recipient].Member.Federation
}

type TransportationCheckpointSavedRow struct {
	Recipient   core.FederateHandle
	Event       *rtiv1.FederateEvent
	Binding     *TransportationCheckpointBinding
	OutcomeOnly bool
}

func CallbackCheckpointTransportationRows(data []byte) ([]TransportationCheckpointSavedRow, error) {
	image, err := decodeCallbackCheckpoint(data)
	if err != nil {
		return nil, err
	}
	var rows []TransportationCheckpointSavedRow
	for _, row := range image.Deliveries {
		if row.Phase == callbackPhaseCompleted {
			continue
		}
		event := &rtiv1.FederateEvent{}
		if err := proto.Unmarshal(row.Event, event); err != nil {
			return nil, err
		}
		if event.GetTransportationChangeConfirmed() != nil {
			rows = append(rows, TransportationCheckpointSavedRow{Recipient: row.Recipient, Event: event, Binding: row.Transportation, OutcomeOnly: row.Phase == callbackPhaseOutcome})
		}
	}
	return rows, nil
}
