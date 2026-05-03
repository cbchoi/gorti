package ownership

import (
	rtiv1 "github.com/cbchoi/gorti/rti/internal/genproto/rti/v1"
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/cbchoi/gorti/rti/internal/core"
)

// eventKind tags the ownership-transition variant carried by
// eventRecord.
//
// Cut-1 limitation (matches the sync package): the proto Event oneof
// (rtiv1.Event.Body) does not yet carry ownership variants, so the
// on-disk WAL representation is a placeholder empty Event. The kind
// tag is preserved here so an in-memory permissive log (used by spec
// tests) can distinguish transitions; production-grade replay
// determinism (FR-OWN-6) is the M8 W2 follow-up that extends the proto.
type eventKind uint8

const (
	evtUnconditionalDivest eventKind = iota + 1
	evtNegotiatedDivest
	evtAcquire
	evtTransferred
	evtCancelDivest
	evtCancelAcquire
	evtDivestIfWanted
)

// eventRecord adapts an ownership-transition into core.EventRecord +
// proto.Message so the eventlog Writer's marshaling path accepts it.
// Marshaled bytes are an empty rtiv1.Event with only Seq populated.
type eventRecord struct {
	pb    *rtiv1.Event
	kind  eventKind
	obj   core.ObjectHandle
	attrs []core.AttributeHandle
	from  core.FederateHandle
	to    core.FederateHandle
}

func (e *eventRecord) ensureProto() *rtiv1.Event {
	if e.pb == nil {
		e.pb = &rtiv1.Event{}
	}
	return e.pb
}

// Seq satisfies core.EventRecord.
func (e *eventRecord) Seq() uint64 {
	if e == nil || e.pb == nil {
		return 0
	}
	return e.pb.Seq
}

// SetSeq is exposed for tests that emulate eventlog seq assignment.
func (e *eventRecord) SetSeq(seq uint64) { e.ensureProto().Seq = seq }

// Kind returns the in-memory transition tag.
func (e *eventRecord) Kind() eventKind { return e.kind }

// proto.Message implementation — delegates to the lazily-allocated empty proto.
func (e *eventRecord) Reset()                            { e.ensureProto().Reset() }
func (e *eventRecord) String() string                    { return e.ensureProto().String() }
func (e *eventRecord) ProtoReflect() protoreflect.Message { return e.ensureProto().ProtoReflect() }

// assumptionOutbound is a placeholder OutboundEvent for
// requestAttributeOwnershipAssumption until the proto FederateEvent
// oneof is extended. The fakeOutbox in spec tests counts emissions;
// production transport wiring is M8 W2 follow-up.
type assumptionOutbound struct {
	pb    *rtiv1.FederateEvent
	obj   core.ObjectHandle
	attrs []core.AttributeHandle
	tag   []byte
}

func assumptionEvent(obj core.ObjectHandle, attrs []core.AttributeHandle, tag []byte) *assumptionOutbound {
	return &assumptionOutbound{
		pb:    &rtiv1.FederateEvent{},
		obj:   obj,
		attrs: append([]core.AttributeHandle(nil), attrs...),
		tag:   tag,
	}
}

func (o *assumptionOutbound) Seq() uint64 {
	if o == nil || o.pb == nil {
		return 0
	}
	return o.pb.Seq
}

// divestNotificationOutbound — attributeOwnershipDivestitureNotification placeholder.
type divestNotificationOutbound struct {
	pb    *rtiv1.FederateEvent
	obj   core.ObjectHandle
	attrs []core.AttributeHandle
}

func divestNotificationEvent(obj core.ObjectHandle, attrs []core.AttributeHandle) *divestNotificationOutbound {
	return &divestNotificationOutbound{
		pb:    &rtiv1.FederateEvent{},
		obj:   obj,
		attrs: append([]core.AttributeHandle(nil), attrs...),
	}
}

func (o *divestNotificationOutbound) Seq() uint64 {
	if o == nil || o.pb == nil {
		return 0
	}
	return o.pb.Seq
}

// acquireNotificationOutbound — attributeOwnershipAcquisitionNotification placeholder.
type acquireNotificationOutbound struct {
	pb    *rtiv1.FederateEvent
	obj   core.ObjectHandle
	attrs []core.AttributeHandle
}

func acquireNotificationEvent(obj core.ObjectHandle, attrs []core.AttributeHandle) *acquireNotificationOutbound {
	return &acquireNotificationOutbound{
		pb:    &rtiv1.FederateEvent{},
		obj:   obj,
		attrs: append([]core.AttributeHandle(nil), attrs...),
	}
}

func (o *acquireNotificationOutbound) Seq() uint64 {
	if o == nil || o.pb == nil {
		return 0
	}
	return o.pb.Seq
}
