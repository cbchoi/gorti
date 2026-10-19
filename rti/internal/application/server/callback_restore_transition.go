package server

import (
	grpcadapter "github.com/cbchoi/gorti/rti/internal/adapters/grpc"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"sync"
)

type callbackReplayTransition struct {
	mu                sync.Mutex
	transition        core.OutboxReservation
	replay            *grpcadapter.CallbackReplayReservation
	done              bool
	err               error
	beforePublication func() error
	transportation    *grpcadapter.TransportationCheckpointReplayReservation
}

func (r *callbackReplayTransition) Commit() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.done {
		return r.err
	}
	if r.transportation != nil {
		r.transportation.Commit()
	}
	if r.beforePublication != nil {
		if err := r.beforePublication(); err != nil {
			r.transition.Release()
			r.replay.Release()
			if r.transportation != nil {
				r.transportation.Rollback()
			}
			r.err = err
			r.done = true
			return err
		}
	}
	// Tickets are valid before queue publication; checkpoint admission excludes
	// user reports until old identities and receipts have been retired.
	r.replay.Admit()
	r.err = r.transition.Commit()
	if r.err != nil {
		r.transition.Release()
		r.replay.Release()
		if r.transportation != nil {
			r.transportation.Rollback()
		}
	} else {
		r.err = r.replay.Commit()
	}
	r.done = true
	return r.err
}
func (r *callbackReplayTransition) Release() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.done {
		r.transition.Release()
		r.replay.Release()
		if r.transportation != nil {
			r.transportation.Release()
		}
		r.done = true
	}
}
