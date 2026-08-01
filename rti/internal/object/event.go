package object

import (
	rtiv1 "github.com/cbchoi/gorti/rti/internal/genproto/rti/v1"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// eventRecord adapts *rtiv1.Event to core.EventRecord. The adapter exists
// because the generated proto exposes Seq as a field; core.EventRecord
// requires a Seq() method. The adapter also implements proto.Message so
// the eventlog Writer can marshal it directly via the proto.Message
// branch in writer.go.
type eventRecord struct {
	pb *rtiv1.Event
}

// Seq satisfies core.EventRecord.
func (e *eventRecord) Seq() uint64 {
	if e == nil || e.pb == nil {
		return 0
	}
	return e.pb.Seq
}

// SetSeq is exposed for in-package tests that emulate the event log's seq
// assignment without the unsafe-pointer reflection path used by the real
// Writer. Production never calls it directly.
func (e *eventRecord) SetSeq(seq uint64) { e.pb.Seq = seq }

// Inner returns the underlying generated proto. Used by tests that want
// to inspect the event body without owning the adapter type.
func (e *eventRecord) Inner() *rtiv1.Event { return e.pb }

// proto.Message implementation — delegates to the embedded proto so the
// real eventlog Writer marshals the adapter correctly.
func (e *eventRecord) Reset()                             { e.pb.Reset() }
func (e *eventRecord) String() string                     { return e.pb.String() }
func (e *eventRecord) ProtoReflect() protoreflect.Message { return e.pb.ProtoReflect() }

// outboundEvent adapts *rtiv1.FederateEvent to core.OutboundEvent. The
// downstream stream multiplexer (Wave 3C) extracts the proto via Inner()
// and writes it on the federate's outbound stream.
type outboundEvent struct {
	pb *rtiv1.FederateEvent
}

// Seq satisfies core.OutboundEvent.
func (o *outboundEvent) Seq() uint64 {
	if o == nil || o.pb == nil {
		return 0
	}
	return o.pb.Seq
}

// Inner returns the underlying proto for the stream multiplexer.
func (o *outboundEvent) Inner() *rtiv1.FederateEvent { return o.pb }
