package core

// ConveyedRegionEvent identifies callbacks carrying immutable update-region
// copies. Delivery acquires one recipient-scoped lease per callback reservation,
// not per queue attempt, and releases it only at invocation outcome or teardown.
// Empty results do not require a callback receipt.
type ConveyedRegionEvent interface {
	OutboundEvent
	ConveyedRegionCopies() []ConveyedRegionCopy
}
