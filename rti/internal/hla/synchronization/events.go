package sync

import "github.com/cbchoi/gorti/rti/internal/hla/core"

type eventKind uint8

const (
	evtRegistered eventKind = iota + 1
	evtAchieved
	evtSynchronized
)

type eventRecord struct {
	seq      uint64
	kind     eventKind
	label    string
	federate core.FederateHandle
}

func (e *eventRecord) Seq() uint64 {
	if e == nil {
		return 0
	}
	return e.seq
}
func (e *eventRecord) SetSeq(seq uint64)             { e.seq = seq }
func (e *eventRecord) Kind() eventKind               { return e.kind }
func (e *eventRecord) Label() string                 { return e.label }
func (e *eventRecord) Federate() core.FederateHandle { return e.federate }

// RegistrationFailureReason is the service-domain form of the IEEE
// synchronization-point registration failure reason.
type RegistrationFailureReason uint8

const (
	RegistrationFailureLabelNotUnique RegistrationFailureReason = iota + 1
	RegistrationFailureSetMemberNotJoined
)

// AnnounceEvent represents announceSynchronizationPoint.
type AnnounceEvent struct {
	Sequence uint64
	label    string
	tag      []byte
	required []core.FederateHandle
}

func announceEvent(label string, tag []byte, required []core.FederateHandle) *AnnounceEvent {
	return &AnnounceEvent{label: label, tag: append([]byte(nil), tag...), required: append([]core.FederateHandle(nil), required...)}
}
func (e *AnnounceEvent) Seq() uint64 {
	if e == nil {
		return 0
	}
	return e.Sequence
}
func (e *AnnounceEvent) Label() string { return e.label }
func (e *AnnounceEvent) Tag() []byte   { return append([]byte(nil), e.tag...) }
func (e *AnnounceEvent) Required() []core.FederateHandle {
	return append([]core.FederateHandle(nil), e.required...)
}

// SynchronizedEvent represents federationSynchronized.
type SynchronizedEvent struct {
	Sequence uint64
	label    string
	failed   []core.FederateHandle
}

func synchronizedEvent(label string, failed []core.FederateHandle) *SynchronizedEvent {
	return &SynchronizedEvent{label: label, failed: append([]core.FederateHandle(nil), failed...)}
}
func (e *SynchronizedEvent) Seq() uint64 {
	if e == nil {
		return 0
	}
	return e.Sequence
}
func (e *SynchronizedEvent) Label() string { return e.label }
func (e *SynchronizedEvent) FailedToSync() []core.FederateHandle {
	return append([]core.FederateHandle(nil), e.failed...)
}

// RegistrationSucceededEvent represents synchronizationPointRegistrationSucceeded.
type RegistrationSucceededEvent struct {
	Sequence uint64
	label    string
}

func registrationSucceededEvent(label string) *RegistrationSucceededEvent {
	return &RegistrationSucceededEvent{label: label}
}
func (e *RegistrationSucceededEvent) Seq() uint64 {
	if e == nil {
		return 0
	}
	return e.Sequence
}
func (e *RegistrationSucceededEvent) Label() string { return e.label }

// RegistrationFailedEvent represents synchronizationPointRegistrationFailed.
type RegistrationFailedEvent struct {
	Sequence uint64
	label    string
	reason   RegistrationFailureReason
}

func registrationFailedEvent(label string, reason RegistrationFailureReason) *RegistrationFailedEvent {
	return &RegistrationFailedEvent{label: label, reason: reason}
}
func (e *RegistrationFailedEvent) Seq() uint64 {
	if e == nil {
		return 0
	}
	return e.Sequence
}
func (e *RegistrationFailedEvent) Label() string                     { return e.label }
func (e *RegistrationFailedEvent) Reason() RegistrationFailureReason { return e.reason }
