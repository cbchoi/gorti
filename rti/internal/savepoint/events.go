package savepoint

import (
	"github.com/cbchoi/gorti/rti/internal/core"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/genproto/rti/v1"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// eventKind tags the save/restore-transition variant carried by
// eventRecord.
//
// Cut-1 limitation: the proto Event oneof (rtiv1.Event.Body) does not
// yet carry save/restore variants, so the on-disk WAL representation is
// a placeholder empty Event. The kind tag is preserved here so an
// in-memory permissive log (used by spec tests) can distinguish
// transitions; production-grade replay determinism for save/restore
// transitions (FR-SR-5 across the protocol layer, not just the data
// snapshot) is tracked as the M9 W2 follow-up that extends the proto.
type eventKind uint8

const (
	evtSaveRequested eventKind = iota + 1
	evtFederationSaved
	evtFederationNotSaved
	evtRestoreRequested
	evtFederationRestored
)

// eventRecord adapts a save/restore-transition into core.EventRecord +
// proto.Message so the eventlog Writer's marshaling path (which prefers
// proto.Message) accepts it. The marshaled bytes are an empty
// rtiv1.Event with only Seq populated — matching the existing
// "fallback" branch in writer.go for non-proto records.
type eventRecord struct {
	pb    *rtiv1.Event
	kind  eventKind
	label string
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

// Kind returns the in-memory transition tag (zero for a wire-only record).
func (e *eventRecord) Kind() eventKind { return e.kind }

// Label returns the save/restore label the record refers to.
func (e *eventRecord) Label() string { return e.label }

// proto.Message implementation — delegates to a lazily-allocated empty
// proto so the eventlog Writer's proto.Marshal path succeeds.
func (e *eventRecord) Reset()                             { e.ensureProto().Reset() }
func (e *eventRecord) String() string                     { return e.ensureProto().String() }
func (e *eventRecord) ProtoReflect() protoreflect.Message { return e.ensureProto().ProtoReflect() }

// --- Outbound events -----------------------------------------------------
//
// M12 W2: the proto FederateEvent oneof now carries save callback
// variants (InitiateFederateSave / FederationSaved / FederationNotSaved
// at tags 40/41/42). Save outbound types populate the typed bodies and
// expose Inner() so the gRPC stream multiplexer ships them through.
// Restore-side variants (initiateFederateRestore, federationRestored)
// remain placeholder envelopes — the SDK currently observes restore
// transitions via Query and the cut-3 spec test for M12 also uses
// Query; we ship the save half this cut and revisit restore once the
// SDK callback path actually consumes it.

// initiateFederateSaveOutbound is the OutboundEvent for
// initiateFederateSave (§4.8).
type initiateFederateSaveOutbound struct {
	pb       *rtiv1.FederateEvent
	label    string
	saveTime *core.LogicalTime
}

func initiateFederateSaveEvent(label string, saveTime *core.LogicalTime) *initiateFederateSaveOutbound {
	body := &rtiv1.InitiateFederateSave{Label: label}
	if saveTime != nil {
		t := float64(*saveTime)
		body.SaveTime = &t
	}
	return &initiateFederateSaveOutbound{
		pb: &rtiv1.FederateEvent{
			Event: &rtiv1.FederateEvent_SaveInitiate{SaveInitiate: body},
		},
		label:    label,
		saveTime: saveTime,
	}
}

func (o *initiateFederateSaveOutbound) Seq() uint64 {
	if o == nil || o.pb == nil {
		return 0
	}
	return o.pb.Seq
}
func (o *initiateFederateSaveOutbound) Inner() *rtiv1.FederateEvent { return o.pb }

// Label / SaveTime expose the §4.8 callback identifiers; used by tests
// that match on the in-package fields rather than unwrapping the proto.
func (o *initiateFederateSaveOutbound) Label() string               { return o.label }
func (o *initiateFederateSaveOutbound) SaveTime() *core.LogicalTime { return o.saveTime }

// federationSavedOutbound — federationSaved (§4.9).
type federationSavedOutbound struct {
	pb    *rtiv1.FederateEvent
	label string
}

func federationSavedEvent(label string) *federationSavedOutbound {
	return &federationSavedOutbound{
		pb: &rtiv1.FederateEvent{
			Event: &rtiv1.FederateEvent_SaveCompleted{
				SaveCompleted: &rtiv1.FederationSaved{Label: label},
			},
		},
		label: label,
	}
}

func (o *federationSavedOutbound) Seq() uint64 {
	if o == nil || o.pb == nil {
		return 0
	}
	return o.pb.Seq
}
func (o *federationSavedOutbound) Inner() *rtiv1.FederateEvent { return o.pb }
func (o *federationSavedOutbound) Label() string               { return o.label }

// federationNotSavedOutbound — federationNotSaved (§4.9 failure half).
type federationNotSavedOutbound struct {
	pb    *rtiv1.FederateEvent
	label string
}

func federationNotSavedEvent(label string) *federationNotSavedOutbound {
	return &federationNotSavedOutbound{
		pb: &rtiv1.FederateEvent{
			Event: &rtiv1.FederateEvent_SaveFailed{
				SaveFailed: &rtiv1.FederationNotSaved{Label: label},
			},
		},
		label: label,
	}
}

func (o *federationNotSavedOutbound) Seq() uint64 {
	if o == nil || o.pb == nil {
		return 0
	}
	return o.pb.Seq
}
func (o *federationNotSavedOutbound) Inner() *rtiv1.FederateEvent { return o.pb }
func (o *federationNotSavedOutbound) Label() string               { return o.label }

// initiateFederateRestoreOutbound — initiateFederateRestore (§4.13).
// M17.25 wires the wire-format payload that the M17 Cut-3 placeholder
// left empty. M37 Agent EA adds the federate NAME (§4.26 carries the
// name alongside the pre-save handle; empty when unknown).
type initiateFederateRestoreOutbound struct {
	pb    *rtiv1.FederateEvent
	label string
}

func initiateFederateRestoreEvent(
	label string, federateHandle core.FederateHandle, federateName string,
) *initiateFederateRestoreOutbound {
	return &initiateFederateRestoreOutbound{
		pb: &rtiv1.FederateEvent{
			Event: &rtiv1.FederateEvent_RestoreInitiate{
				RestoreInitiate: &rtiv1.InitiateFederateRestore{
					Label:          label,
					FederateHandle: uint64(federateHandle),
					FederateName:   federateName,
				},
			},
		},
		label: label,
	}
}

func (o *initiateFederateRestoreOutbound) Seq() uint64 {
	if o == nil || o.pb == nil {
		return 0
	}
	return o.pb.Seq
}
func (o *initiateFederateRestoreOutbound) Inner() *rtiv1.FederateEvent { return o.pb }
func (o *initiateFederateRestoreOutbound) Label() string               { return o.label }

// federationRestoredOutbound — federationRestored (§4.14).
type federationRestoredOutbound struct {
	pb    *rtiv1.FederateEvent
	label string
}

func federationRestoredEvent(label string) *federationRestoredOutbound {
	return &federationRestoredOutbound{
		pb: &rtiv1.FederateEvent{
			Event: &rtiv1.FederateEvent_RestoreCompleted{
				RestoreCompleted: &rtiv1.FederationRestored{Label: label},
			},
		},
		label: label,
	}
}

func (o *federationRestoredOutbound) Seq() uint64 {
	if o == nil || o.pb == nil {
		return 0
	}
	return o.pb.Seq
}
func (o *federationRestoredOutbound) Inner() *rtiv1.FederateEvent { return o.pb }
func (o *federationRestoredOutbound) Label() string               { return o.label }

// federationNotRestoredOutbound — federationNotRestored (§4.14 failure
// half). M17.25 (Cut-4).
type federationNotRestoredOutbound struct {
	pb    *rtiv1.FederateEvent
	label string
}

func federationNotRestoredEvent(label string) *federationNotRestoredOutbound {
	return &federationNotRestoredOutbound{
		pb: &rtiv1.FederateEvent{
			Event: &rtiv1.FederateEvent_RestoreFailed{
				RestoreFailed: &rtiv1.FederationNotRestored{Label: label},
			},
		},
		label: label,
	}
}

func (o *federationNotRestoredOutbound) Seq() uint64 {
	if o == nil || o.pb == nil {
		return 0
	}
	return o.pb.Seq
}
func (o *federationNotRestoredOutbound) Inner() *rtiv1.FederateEvent { return o.pb }
func (o *federationNotRestoredOutbound) Label() string               { return o.label }

// requestFederationRestoreSucceededOutbound — requestFederationRestoreSucceeded
// (§4.25 success half). Targets the REQUESTING federate only. M37 Agent EA.
type requestFederationRestoreSucceededOutbound struct {
	pb    *rtiv1.FederateEvent
	label string
}

func requestFederationRestoreSucceededEvent(label string) *requestFederationRestoreSucceededOutbound {
	return &requestFederationRestoreSucceededOutbound{
		pb: &rtiv1.FederateEvent{
			Event: &rtiv1.FederateEvent_RestoreRequestSucceeded{
				RestoreRequestSucceeded: &rtiv1.RequestFederationRestoreSucceeded{Label: label},
			},
		},
		label: label,
	}
}

func (o *requestFederationRestoreSucceededOutbound) Seq() uint64 {
	if o == nil || o.pb == nil {
		return 0
	}
	return o.pb.Seq
}
func (o *requestFederationRestoreSucceededOutbound) Inner() *rtiv1.FederateEvent { return o.pb }
func (o *requestFederationRestoreSucceededOutbound) Label() string               { return o.label }

// requestFederationRestoreFailedOutbound — requestFederationRestoreFailed
// (§4.25 failure half). Targets the REQUESTING federate only. M37 Agent EA.
type requestFederationRestoreFailedOutbound struct {
	pb    *rtiv1.FederateEvent
	label string
}

func requestFederationRestoreFailedEvent(label, reason string) *requestFederationRestoreFailedOutbound {
	return &requestFederationRestoreFailedOutbound{
		pb: &rtiv1.FederateEvent{
			Event: &rtiv1.FederateEvent_RestoreRequestFailed{
				RestoreRequestFailed: &rtiv1.RequestFederationRestoreFailed{
					Label:  label,
					Reason: reason,
				},
			},
		},
		label: label,
	}
}

func (o *requestFederationRestoreFailedOutbound) Seq() uint64 {
	if o == nil || o.pb == nil {
		return 0
	}
	return o.pb.Seq
}
func (o *requestFederationRestoreFailedOutbound) Inner() *rtiv1.FederateEvent { return o.pb }
func (o *requestFederationRestoreFailedOutbound) Label() string               { return o.label }

// federationRestoreBegunOutbound — federationRestoreBegun (§4.26).
// Broadcast to every joined federate when the restore leaves idle,
// BEFORE the per-federate initiateFederateRestore events. M37 Agent EA.
type federationRestoreBegunOutbound struct {
	pb *rtiv1.FederateEvent
}

func federationRestoreBegunEvent() *federationRestoreBegunOutbound {
	return &federationRestoreBegunOutbound{
		pb: &rtiv1.FederateEvent{
			Event: &rtiv1.FederateEvent_RestoreBegun{
				RestoreBegun: &rtiv1.FederationRestoreBegun{},
			},
		},
	}
}

func (o *federationRestoreBegunOutbound) Seq() uint64 {
	if o == nil || o.pb == nil {
		return 0
	}
	return o.pb.Seq
}
func (o *federationRestoreBegunOutbound) Inner() *rtiv1.FederateEvent { return o.pb }
