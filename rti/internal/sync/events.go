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

// announceOutbound is a placeholder OutboundEvent for
// announceSynchronizationPoint until the proto FederateEvent oneof is
// extended. The fakeOutbox in spec tests counts emissions; production
// transport wiring is the M8 W2 follow-up.
type announceOutbound struct {
	pb    *rtiv1.FederateEvent
	label string
	tag   []byte
}

func announceEvent(label string, tag []byte) *announceOutbound {
	return &announceOutbound{
		pb:    &rtiv1.FederateEvent{},
		label: label,
		tag:   tag,
	}
}

// Seq satisfies core.OutboundEvent.
func (o *announceOutbound) Seq() uint64 {
	if o == nil || o.pb == nil {
		return 0
	}
	return o.pb.Seq
}

// Label / Tag expose the sync-point identifiers carried by the
// announce envelope; used by tests + future gRPC handler wiring.
func (o *announceOutbound) Label() string { return o.label }
func (o *announceOutbound) Tag() []byte   { return o.tag }

// synchronizedOutbound is the symmetric placeholder for
// federationSynchronized.
type synchronizedOutbound struct {
	pb    *rtiv1.FederateEvent
	label string
}

func synchronizedEvent(label string) *synchronizedOutbound {
	return &synchronizedOutbound{
		pb:    &rtiv1.FederateEvent{},
		label: label,
	}
}

func (o *synchronizedOutbound) Seq() uint64 {
	if o == nil || o.pb == nil {
		return 0
	}
	return o.pb.Seq
}
func (o *synchronizedOutbound) Label() string { return o.label }
