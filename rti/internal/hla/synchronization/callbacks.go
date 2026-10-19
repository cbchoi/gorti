package sync

import (
	"slices"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

type callbackKind uint8

const (
	callbackAnnounce callbackKind = iota + 1
	callbackSynchronized
	callbackRegistrationSucceeded
	callbackRegistrationFailed
)

// CallbackState is a detached durable obligation, not a transport receipt.
type CallbackState struct {
	Kind      callbackKind
	Label     string
	Recipient core.FederateHandle
	Sequence  uint64
	Epoch     uint64
	Entered   bool
	Tag       []byte
	Required  []core.FederateHandle
	Failed    []core.FederateHandle
	Reason    RegistrationFailureReason
}

func (k callbackKind) observedKind() core.SyncCallbackKind {
	switch k {
	case callbackAnnounce:
		return core.SyncCallbackAnnounced
	case callbackSynchronized:
		return core.SyncCallbackSynchronized
	case callbackRegistrationSucceeded:
		return core.SyncCallbackRegistrationSucceeded
	case callbackRegistrationFailed:
		return core.SyncCallbackRegistrationFailed
	}
	return 0
}

// InvocationState associates actual callback invocations with a label epoch.
type InvocationState struct {
	Recipient           core.FederateHandle
	Announce            uint64
	Synchronized        uint64
	AnnounceInvoked     bool
	AnnounceEntered     bool
	SynchronizedInvoked bool
}

func cloneCallbackState(state CallbackState) CallbackState {
	state.Tag = slices.Clone(state.Tag)
	state.Required = slices.Clone(state.Required)
	state.Failed = slices.Clone(state.Failed)
	return state
}

func (c CallbackState) delivery() core.OutboxDelivery {
	var event core.OutboundEvent
	switch c.Kind {
	case callbackAnnounce:
		value := announceEvent(c.Label, c.Tag, c.Required)
		value.Sequence = c.Sequence
		event = value
	case callbackSynchronized:
		value := synchronizedEvent(c.Label, c.Failed)
		value.Sequence = c.Sequence
		event = value
	case callbackRegistrationSucceeded:
		value := registrationSucceededEvent(c.Label)
		value.Sequence = c.Sequence
		event = value
	case callbackRegistrationFailed:
		value := registrationFailedEvent(c.Label, c.Reason)
		value.Sequence = c.Sequence
		event = value
	}
	return core.OutboxDelivery{Recipient: c.Recipient, Event: event}
}
