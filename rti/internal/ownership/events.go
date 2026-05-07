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
func (e *eventRecord) Reset()                             { e.ensureProto().Reset() }
func (e *eventRecord) String() string                     { return e.ensureProto().String() }
func (e *eventRecord) ProtoReflect() protoreflect.Message { return e.ensureProto().ProtoReflect() }

// attrsToWire converts attribute handles to the proto's repeated
// uint64 field; sorted-stable ordering is the caller's responsibility
// (the manager already sorts before fan-out).
func attrsToWire(attrs []core.AttributeHandle) []uint64 {
	out := make([]uint64, 0, len(attrs))
	for _, a := range attrs {
		out = append(out, uint64(a))
	}
	return out
}

// assumptionOutbound is the OutboundEvent for
// requestAttributeOwnershipAssumption (§7.3). M12 W2: the proto
// FederateEvent oneof now carries the typed
// RequestAttributeOwnershipAssumption variant; the gRPC stream
// multiplexer extracts the proto via Inner().
type assumptionOutbound struct {
	pb       *rtiv1.FederateEvent
	obj      core.ObjectHandle
	attrs    []core.AttributeHandle
	tag      []byte
	divester core.FederateHandle
}

func assumptionEvent(obj core.ObjectHandle, attrs []core.AttributeHandle, tag []byte, divester core.FederateHandle) *assumptionOutbound {
	attrsCopy := append([]core.AttributeHandle(nil), attrs...)
	return &assumptionOutbound{
		pb: &rtiv1.FederateEvent{
			Event: &rtiv1.FederateEvent_OwnershipAssumption{
				OwnershipAssumption: &rtiv1.RequestAttributeOwnershipAssumption{
					ObjectHandle:      uint64(obj),
					AttributeHandles:  attrsToWire(attrsCopy),
					DivestingFederate: uint64(divester),
					Tag:               append([]byte(nil), tag...),
				},
			},
		},
		obj:      obj,
		attrs:    attrsCopy,
		tag:      tag,
		divester: divester,
	}
}

func (o *assumptionOutbound) Seq() uint64 {
	if o == nil || o.pb == nil {
		return 0
	}
	return o.pb.Seq
}
func (o *assumptionOutbound) Inner() *rtiv1.FederateEvent { return o.pb }

// divestNotificationOutbound — RequestDivestitureConfirmation. Fires on
// the divesting federate when an acquirer completes the transfer.
type divestNotificationOutbound struct {
	pb    *rtiv1.FederateEvent
	obj   core.ObjectHandle
	attrs []core.AttributeHandle
}

func divestNotificationEvent(obj core.ObjectHandle, attrs []core.AttributeHandle) *divestNotificationOutbound {
	attrsCopy := append([]core.AttributeHandle(nil), attrs...)
	return &divestNotificationOutbound{
		pb: &rtiv1.FederateEvent{
			Event: &rtiv1.FederateEvent_OwnershipDivestConfirmed{
				OwnershipDivestConfirmed: &rtiv1.RequestDivestitureConfirmation{
					ObjectHandle:     uint64(obj),
					AttributeHandles: attrsToWire(attrsCopy),
				},
			},
		},
		obj:   obj,
		attrs: attrsCopy,
	}
}

func (o *divestNotificationOutbound) Seq() uint64 {
	if o == nil || o.pb == nil {
		return 0
	}
	return o.pb.Seq
}
func (o *divestNotificationOutbound) Inner() *rtiv1.FederateEvent { return o.pb }

// acquireNotificationOutbound — AttributeOwnershipAcquisitionNotification.
// Fires on the new owner when a transfer completes.
type acquireNotificationOutbound struct {
	pb       *rtiv1.FederateEvent
	obj      core.ObjectHandle
	attrs    []core.AttributeHandle
	newOwner core.FederateHandle
}

func acquireNotificationEvent(obj core.ObjectHandle, attrs []core.AttributeHandle, newOwner core.FederateHandle) *acquireNotificationOutbound {
	attrsCopy := append([]core.AttributeHandle(nil), attrs...)
	return &acquireNotificationOutbound{
		pb: &rtiv1.FederateEvent{
			Event: &rtiv1.FederateEvent_OwnershipAcquired{
				OwnershipAcquired: &rtiv1.AttributeOwnershipAcquisitionNotification{
					ObjectHandle:     uint64(obj),
					AttributeHandles: attrsToWire(attrsCopy),
					OwningFederate:   uint64(newOwner),
				},
			},
		},
		obj:      obj,
		attrs:    attrsCopy,
		newOwner: newOwner,
	}
}

func (o *acquireNotificationOutbound) Seq() uint64 {
	if o == nil || o.pb == nil {
		return 0
	}
	return o.pb.Seq
}
func (o *acquireNotificationOutbound) Inner() *rtiv1.FederateEvent { return o.pb }
