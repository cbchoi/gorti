package savepoint

import "github.com/cbchoi/gorti/rti/internal/hla/core"

type SaveFailureReason int32

const (
	SaveFailureNone SaveFailureReason = iota
	RTIUnableToSave
	FederateReportedFailureDuringSave
	FederateResignedDuringSave
	RTIDetectedFailureDuringSave
	SaveTimeCannotBeHonored
	SaveAborted
)

type RestoreFailureReason int32

const (
	RestoreFailureNone RestoreFailureReason = iota
	RTIUnableToRestore
	FederateReportedFailureDuringRestore
	FederateResignedDuringRestore
	RTIDetectedFailureDuringRestore
	RestoreAborted
)

type eventKind uint8

const (
	evtSaveRequested eventKind = iota + 1
	evtFederationSaved
	evtFederationNotSaved
	evtRestoreRequested
	evtFederationRestored
)

type eventRecord struct {
	seq   uint64
	kind  eventKind
	label string
}

func (e *eventRecord) Seq() uint64 {
	if e == nil {
		return 0
	}
	return e.seq
}
func (e *eventRecord) SetSeq(seq uint64) { e.seq = seq }
func (e *eventRecord) Kind() eventKind   { return e.kind }
func (e *eventRecord) Label() string     { return e.label }

type callbackBase struct {
	sequence uint64
	label    string
}

func (e *callbackBase) Seq() uint64 {
	if e == nil {
		return 0
	}
	return e.sequence
}
func (e *callbackBase) Label() string {
	if e == nil {
		return ""
	}
	return e.label
}

// InitiateSaveEvent represents initiateFederateSave.
type InitiateSaveEventOf[T core.LogicalTimeScalar] struct {
	callbackBase
	saveTime *T
}

func initiateFederateSaveEventOf[T core.LogicalTimeScalar](label string, saveTime *T) *InitiateSaveEventOf[T] {
	var copied *T
	if saveTime != nil {
		value := *saveTime
		copied = &value
	}
	return &InitiateSaveEventOf[T]{callbackBase: callbackBase{label: label}, saveTime: copied}
}
func (e *InitiateSaveEventOf[T]) SaveTime() *T {
	if e == nil || e.saveTime == nil {
		return nil
	}
	value := *e.saveTime
	return &value
}

// SavedEvent represents federationSaved.
type SavedEvent struct{ callbackBase }

func federationSavedEvent(label string) *SavedEvent { return &SavedEvent{callbackBase{label: label}} }

// NotSavedEvent represents federationNotSaved.
type NotSavedEvent struct {
	callbackBase
	Reason SaveFailureReason
}

func federationNotSavedEvent(label string, reason SaveFailureReason) *NotSavedEvent {
	return &NotSavedEvent{callbackBase{label: label}, reason}
}

// InitiateRestoreEvent represents initiateFederateRestore.
type InitiateRestoreEvent struct {
	callbackBase
	FederateHandle core.FederateHandle
	FederateName   string
	ClientState    []byte
}
type initiateFederateRestoreOutbound = InitiateRestoreEvent

func initiateFederateRestoreEvent(label string, handle core.FederateHandle, name string) *InitiateRestoreEvent {
	return &InitiateRestoreEvent{callbackBase: callbackBase{label: label}, FederateHandle: handle, FederateName: name}
}

// RestoredEvent represents federationRestored.
type RestoredEvent struct {
	callbackBase
	// Nonzero only after the production identity/queue transition commits.
	FederateHandle core.FederateHandle
	CallbackEpoch  uint64
	// Positive only when complete saved/current producer history proves a hint.
	// It is not a minimum admissible ID: unused lower caller IDs remain legal.
	NextRetractionHandle uint64
}

func federationRestoredEvent(label string) *RestoredEvent {
	return &RestoredEvent{callbackBase: callbackBase{label: label}}
}

// NotRestoredEvent represents federationNotRestored.
type NotRestoredEvent struct {
	callbackBase
	Reason RestoreFailureReason
}

func federationNotRestoredEvent(label string, reason RestoreFailureReason) *NotRestoredEvent {
	return &NotRestoredEvent{callbackBase{label: label}, reason}
}

// RestoreRequestSucceededEvent represents requestFederationRestoreSucceeded.
type RestoreRequestSucceededEvent struct{ callbackBase }

func requestFederationRestoreSucceededEvent(label string) *RestoreRequestSucceededEvent {
	return &RestoreRequestSucceededEvent{callbackBase{label: label}}
}

// RestoreRequestFailedEvent represents requestFederationRestoreFailed.
type RestoreRequestFailedEvent struct {
	callbackBase
	Reason string
}

func requestFederationRestoreFailedEvent(label, reason string) *RestoreRequestFailedEvent {
	return &RestoreRequestFailedEvent{callbackBase: callbackBase{label: label}, Reason: reason}
}

// RestoreBegunEvent represents federationRestoreBegun.
type RestoreBegunEvent struct{ sequence uint64 }

func federationRestoreBegunEvent() *RestoreBegunEvent { return &RestoreBegunEvent{} }
func (e *RestoreBegunEvent) Seq() uint64 {
	if e == nil {
		return 0
	}
	return e.sequence
}
