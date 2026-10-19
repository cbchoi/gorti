package ownership

import "github.com/cbchoi/gorti/rti/internal/hla/core"

type eventKind uint8

const (
	evtUnconditionalDivest eventKind = iota + 1
	evtNegotiatedDivest
	evtAcquire
	evtTransferred
	evtCancelDivest
	evtCancelAcquire
	evtDivestIfWanted
	evtReleaseDenied
)

type eventRecord struct {
	seq   uint64
	kind  eventKind
	obj   core.ObjectHandle
	attrs []core.AttributeHandle
	from  core.FederateHandle
	to    core.FederateHandle
}

func (e *eventRecord) Seq() uint64 {
	if e == nil {
		return 0
	}
	return e.seq
}
func (e *eventRecord) SetSeq(seq uint64) { e.seq = seq }
func (e *eventRecord) Kind() eventKind   { return e.kind }

// CallbackKind identifies an ownership-management callback.
type CallbackKind uint8

const (
	CallbackAssumption CallbackKind = iota + 1
	CallbackDivestitureConfirmation
	CallbackAcquisitionNotification
	CallbackReleaseRequest
	CallbackUnavailable
	CallbackAcquisitionCancellation
)

// CallbackEvent is the transport-neutral ownership callback envelope.
type CallbackEvent struct {
	Sequence                 uint64
	Kind                     CallbackKind
	Object                   core.ObjectHandle
	Attributes               []core.AttributeHandle
	Tag                      []byte
	Federate                 core.FederateHandle
	CallbackID               uint64
	CallbackScope            core.OwnershipCallbackScope
	RequireInvocationReceipt bool
	RetainedProjection       *CallbackProjection `json:"retainedProjection,omitempty"`
	identity                 *callbackIdentity
}

// CallbackProjection preserves the recipient's class view for a mandatory
// outcome whose instance can be deleted before invocation or retry.
type CallbackProjection struct {
	RegisteredClass core.ObjectClassHandle
	KnownClass      core.ObjectClassHandle
	Attributes      []core.AttributeHandle
}

// ProjectedOwnershipCallback leaves registered-class attributes in the ledger.
// The detached wire-facing event shares only its precommit identity cell.
func (e *CallbackEvent) ProjectedOwnershipCallback() core.OutboundEvent {
	if e.RetainedProjection == nil {
		return e
	}
	cloned := *e
	cloned.Attributes = append([]core.AttributeHandle(nil), e.RetainedProjection.Attributes...)
	cloned.Tag = append([]byte(nil), e.Tag...)
	return &cloned
}

// Reservations may project a callback before its transaction assigns an ID.
// Copies retain this cell; it is filled under the commit lock before queue
// Commit publishes any event, and is immutable thereafter.
type callbackIdentity struct {
	scope  core.OwnershipCallbackScope
	id     uint64
	strict bool
}

func (e *CallbackEvent) OwnershipCallbackIdentity() (core.OwnershipCallbackScope, uint64, bool) {
	if e.identity != nil {
		return e.identity.scope, e.identity.id, e.identity.strict
	}
	return e.CallbackScope, e.CallbackID, e.RequireInvocationReceipt
}

func (e *CallbackEvent) Seq() uint64 {
	if e == nil {
		return 0
	}
	return e.Sequence
}

func newCallback(kind CallbackKind, obj core.ObjectHandle, attrs []core.AttributeHandle, tag []byte, federate core.FederateHandle) *CallbackEvent {
	return &CallbackEvent{Kind: kind, Object: obj, Attributes: append([]core.AttributeHandle(nil), attrs...), Tag: append([]byte(nil), tag...), Federate: federate, identity: &callbackIdentity{}}
}

func assumptionEvent(obj core.ObjectHandle, attrs []core.AttributeHandle, tag []byte, divester core.FederateHandle) *CallbackEvent {
	return newCallback(CallbackAssumption, obj, attrs, tag, divester)
}

func divestNotificationEvent(obj core.ObjectHandle, attrs []core.AttributeHandle) *CallbackEvent {
	return newCallback(CallbackDivestitureConfirmation, obj, attrs, nil, 0)
}

func acquireNotificationEvent(obj core.ObjectHandle, attrs []core.AttributeHandle, newOwner core.FederateHandle, tags ...[]byte) *CallbackEvent {
	var tag []byte
	if len(tags) > 0 {
		tag = tags[0]
	}
	return newCallback(CallbackAcquisitionNotification, obj, attrs, tag, newOwner)
}

func releaseRequestEvent(obj core.ObjectHandle, attrs []core.AttributeHandle, tag []byte) *CallbackEvent {
	return newCallback(CallbackReleaseRequest, obj, attrs, tag, 0)
}

func unavailableEvent(obj core.ObjectHandle, attrs []core.AttributeHandle) *CallbackEvent {
	return newCallback(CallbackUnavailable, obj, attrs, nil, 0)
}

func cancellationEvent(obj core.ObjectHandle, attrs []core.AttributeHandle) *CallbackEvent {
	return newCallback(CallbackAcquisitionCancellation, obj, attrs, nil, 0)
}
