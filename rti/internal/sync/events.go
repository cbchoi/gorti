package sync

import (
	rtiv1 "github.com/cbchoi/gorti/rti/internal/genproto/rti/v1"
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/cbchoi/gorti/rti/internal/core"
)

// eventKind tags the sync-transition variant carried by eventRecord.
//
// Cut-1 limitation: the proto Event oneof (rtiv1.Event.Body) does not
// yet carry sync-point variants, so the on-disk WAL representation is
// a placeholder empty Event. The kind tag is preserved here so that an
// in-memory permissive log (used by spec tests) can distinguish
// transitions; production-grade replay determinism (FR-SYN-4) is
// tracked as the M8 W2 follow-up that extends the proto.
type eventKind uint8

const (
	evtRegistered eventKind = iota + 1
	evtAchieved
	evtSynchronized
)

// eventRecord adapts a sync-transition into core.EventRecord +
// proto.Message so the eventlog Writer's marshaling path (which prefers
// proto.Message) accepts it. The marshaled bytes are an empty
// rtiv1.Event with only Seq populated — matching the existing
// "fallback" branch in writer.go for non-proto records.
type eventRecord struct {
	pb       *rtiv1.Event
	kind     eventKind
	label    string
	federate core.FederateHandle
}

// ensureProto lazily allocates the underlying empty proto.
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

// Kind returns the in-memory transition tag (zero for a wire-only record).
func (e *eventRecord) Kind() eventKind { return e.kind }

// Label returns the sync-point label this record refers to.
func (e *eventRecord) Label() string { return e.label }

// Federate returns the federate handle for evtAchieved records (zero
// for evtRegistered / evtSynchronized).
func (e *eventRecord) Federate() core.FederateHandle { return e.federate }

// proto.Message implementation — delegates to a lazily-allocated empty
// proto so the eventlog Writer's proto.Marshal path succeeds.
func (e *eventRecord) Reset()                            { e.ensureProto().Reset() }
func (e *eventRecord) String() string                    { return e.ensureProto().String() }
func (e *eventRecord) ProtoReflect() protoreflect.Message { return e.ensureProto().ProtoReflect() }

// announceOutbound is the OutboundEvent for announceSynchronizationPoint
// (§4.6). The wire-side proto carries the typed
// FederateEvent_SyncAnnounced oneof variant added in M12 W2 follow-up;
// the in-package fields below mirror the proto for tests that want to
// inspect without unwrapping.
type announceOutbound struct {
	pb       *rtiv1.FederateEvent
	label    string
	tag      []byte
	required []core.FederateHandle
}

func announceEvent(label string, tag []byte, required []core.FederateHandle) *announceOutbound {
	pbReq := make([]uint64, 0, len(required))
	for _, h := range required {
		pbReq = append(pbReq, uint64(h))
	}
	pb := &rtiv1.FederateEvent{
		Event: &rtiv1.FederateEvent_SyncAnnounced{
			SyncAnnounced: &rtiv1.SynchronizationPointAnnounced{
				Label:              label,
				Tag:                append([]byte(nil), tag...),
				RequiredFederates:  pbReq,
			},
		},
	}
	return &announceOutbound{
		pb:       pb,
		label:    label,
		tag:      tag,
		required: append([]core.FederateHandle(nil), required...),
	}
}

// Seq satisfies core.OutboundEvent.
func (o *announceOutbound) Seq() uint64 {
	if o == nil || o.pb == nil {
		return 0
	}
	return o.pb.Seq
}

// Inner exposes the underlying proto for the gRPC stream multiplexer
// (transport/grpc/stream.go's federateEventCarrier interface).
func (o *announceOutbound) Inner() *rtiv1.FederateEvent { return o.pb }

// Label / Tag / Required expose the sync-point identifiers carried by
// the announce envelope; used by tests that match on the in-package
// fields rather than unwrapping the proto.
func (o *announceOutbound) Label() string                   { return o.label }
func (o *announceOutbound) Tag() []byte                     { return o.tag }
func (o *announceOutbound) Required() []core.FederateHandle { return o.required }

// synchronizedOutbound is the OutboundEvent for federationSynchronized
// (§4.7). Carries the typed FederateEvent_SyncSynchronized variant.
type synchronizedOutbound struct {
	pb    *rtiv1.FederateEvent
	label string
}

func synchronizedEvent(label string) *synchronizedOutbound {
	return &synchronizedOutbound{
		pb: &rtiv1.FederateEvent{
			Event: &rtiv1.FederateEvent_SyncSynchronized{
				SyncSynchronized: &rtiv1.FederationSynchronized{Label: label},
			},
		},
		label: label,
	}
}

func (o *synchronizedOutbound) Seq() uint64 {
	if o == nil || o.pb == nil {
		return 0
	}
	return o.pb.Seq
}
func (o *synchronizedOutbound) Inner() *rtiv1.FederateEvent { return o.pb }
func (o *synchronizedOutbound) Label() string               { return o.label }
