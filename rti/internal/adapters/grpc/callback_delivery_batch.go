package grpc

import (
	"context"
	"fmt"
	"time"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

const callbackDeliveryBatchLimit = 64

type preparedCallbackDelivery struct {
	key   string
	entry *callbackReceipt
}

type callbackDeliveryBatch struct {
	receipts   *CallbackReceipts
	fed        core.FederationName
	events     []*rtiv1.FederateEvent
	deliveries []preparedCallbackDelivery
}

// Only homogeneous bounded RO frames use this path. Mixed/control/recovery
// frames keep their original per-event ordering and handle transitions.
func (r *CallbackReceipts) prepareDataDeliveryBatch(fed core.FederationName, h core.FederateHandle, events []*rtiv1.FederateEvent) (*callbackDeliveryBatch, bool, error) {
	if len(events) < 2 || len(events) > callbackDeliveryBatchLimit {
		return nil, false, nil
	}
	for _, event := range events {
		if !ordinaryROData(event) || len(event.GetCallbackReceipt()) != 32 {
			return nil, false, nil
		}
	}
	batch := &callbackDeliveryBatch{receipts: r, fed: fed, events: events[:0], deliveries: make([]preparedCallbackDelivery, 0, len(events))}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, event := range events {
		prepared, skip, err := r.preparedDeliveryLocked(fed, h, event)
		if err != nil {
			return batch, true, err
		}
		if !skip {
			batch.events = append(batch.events, event)
			batch.deliveries = append(batch.deliveries, prepared)
		}
	}
	return batch, true, nil
}

func (b *callbackDeliveryBatch) commit(sent bool) {
	if len(b.deliveries) == 0 {
		return
	}
	r := b.receipts
	// Transport errors remain per-ticket operations: authenticated early entry
	// or outcome reports can have advanced different members of the frame.
	if !sent {
		for _, prepared := range b.deliveries {
			r.commit(prepared.key, prepared.entry, false)
		}
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if r.opts.AcquireService != nil {
		release, err := r.opts.AcquireService(ctx, b.fed)
		if err != nil || release == nil {
			if err == nil {
				err = fmt.Errorf("MOM: callback admission returned no release")
			}
			// Treat the failed shared attempt as the first ticket's attempt.
			// A transient failure must not invalidate otherwise admissible peers.
			first := b.deliveries[0]
			r.invalidateDelivery(first.key, first.entry, err)
			cancel()
			for _, prepared := range b.deliveries[1:] {
				r.commit(prepared.key, prepared.entry, true)
			}
			return
		}
		defer release()
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, prepared := range b.deliveries {
		if r.entries[prepared.key] != prepared.entry {
			continue
		}
		prepared.entry.sent = true
		r.publishCheckpointLocked(prepared.entry)
	}
}
