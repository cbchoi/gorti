package grpc

import (
	"fmt"
)

// callbackCapacityPressure is transient and is issued before any member of a
// mandatory fanout is prepared. Only an explicitly uncommitted wire operation
// may wait and retry it; other services retain their normal failure behavior.
type callbackCapacityPressure struct{ ready <-chan struct{} }

func (*callbackCapacityPressure) Error() string { return "MOM: callback receipt capacity exceeded" }

type callbackCredits struct {
	receipts  *CallbackReceipts
	remaining int // guarded by receipts.mu
}

func (r *CallbackReceipts) reserveCredits(count int) (*callbackCredits, error) {
	return r.reserveCallbackCredits(count, false)
}

func (r *CallbackReceipts) reserveCallbackCredits(count int, data bool) (*callbackCredits, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	limit := r.opts.MaxPending
	if data {
		limit -= r.opts.ControlReserve
	}
	if count > limit {
		return nil, fmt.Errorf("MOM: callback fanout %d exceeds receipt capacity %d", count, limit)
	}
	if count > 0 && count > limit-r.pending-r.reserved {
		if r.capacityChanged == nil {
			r.capacityChanged = make(chan struct{})
		}
		return nil, &callbackCapacityPressure{ready: r.capacityChanged}
	}
	r.reserved += count
	return &callbackCredits{receipts: r, remaining: count}, nil
}

func (c *callbackCredits) release() {
	r := c.receipts
	r.mu.Lock()
	defer r.mu.Unlock()
	if c.remaining != 0 {
		r.reserved -= c.remaining
		c.remaining = 0
		r.notifyCapacityLocked()
	}
}

func (r *CallbackReceipts) notifyCapacityLocked() {
	if r.capacityChanged != nil {
		close(r.capacityChanged)
		r.capacityChanged = nil
	}
}

func (r *CallbackReceipts) retirePendingLocked() {
	r.pending--
	r.notifyCapacityLocked()
}
