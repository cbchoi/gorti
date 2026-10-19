package eventlog

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"google.golang.org/protobuf/proto"
)

// ErrReplayDivergence indicates the replayed log produced a different
// byte sequence than the source log. This is a fatal determinism failure;
// the replayer surfaces it so callers can capture and bisect.
var ErrReplayDivergence = errors.New("eventlog: replay diverged from source")

// Replayer dispatches supported records through the supplied federation and
// object services. Other records pass through to the destination writer.
// With CapturedBuffer, each emitted frame must equal the original Reader
// frame. Header metadata is validated separately and excluded from comparison.
// Byte equality for a passed-through record does not verify its service effects.
type ReplayerOf[T core.LogicalTimeScalar] struct {
	source        core.EventLogReader
	federation    core.FederationStore
	objects       core.ObjectRegistryOf[T]
	capturingSink *Writer
	capturedBuf   *bytes.Buffer // observation tap on the sink
}

// ReplayerOptions bundles Replayer dependencies.
//
// Source and CapturingSink must be non-nil. Missing service dependencies cause
// their records to pass through without applying the recorded operation.
type ReplayerOptionsOf[T core.LogicalTimeScalar] struct {
	// Source is the event log to replay.
	Source core.EventLogReader

	// Federation applies lifecycle records when supplied.
	Federation core.FederationStore

	// Objects applies object and interaction records when supplied.
	Objects core.ObjectRegistryOf[T]

	// CapturingSink receives the log produced during replay.
	CapturingSink *Writer

	// CapturedBuffer is the buffer written by CapturingSink. When supplied,
	// Source must yield Reader records containing the original frame bytes.
	// A nil buffer disables byte comparison, not service-level checks.
	CapturedBuffer *bytes.Buffer
}

// NewReplayer constructs a Replayer. Validates options.
//
// Required: Source, CapturingSink. Optional: Federation, Objects, CapturedBuffer.
func NewReplayerOf[T core.LogicalTimeScalar](opts ReplayerOptionsOf[T]) (*ReplayerOf[T], error) {
	if opts.Source == nil {
		return nil, errors.New("eventlog: ReplayerOptions.Source is required")
	}
	if opts.CapturingSink == nil {
		return nil, errors.New("eventlog: ReplayerOptions.CapturingSink is required")
	}
	return &ReplayerOf[T]{
		source:        opts.Source,
		federation:    opts.Federation,
		objects:       opts.Objects,
		capturingSink: opts.CapturingSink,
		capturedBuf:   opts.CapturedBuffer,
	}, nil
}

// Replay consumes every event from the source, dispatches it through the
// live RTI components (Federation + Objects), and verifies the captured
// output matches the source byte-for-byte from offset HeaderSize onward.
//
// Returns ErrReplayDivergence (wrapped with a context message naming
// the first divergent offset) if the captured body differs. Returns
// the underlying error for I/O failures.
//
// # Header-byte caveat
//
// Version 1 used the metadata slot for a wall-clock CreatedAtNs value;
// version 2 uses it for federation Generation. Replayer keeps the original
// body-only comparison so legacy captures and independently constructed
// destinations follow the same contract. Callers that require matching
// generation provenance validate Header() before replay.
func (r *ReplayerOf[T]) Replay(ctx context.Context) error {
	// Compare each original frame before releasing its source record, so
	// verification does not retain another complete copy of a large journal.
	expectedLen := HeaderSize
	if err := r.compareCaptured(expectedLen); err != nil {
		return err
	}

	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("eventlog: replay context: %w", err)
		}

		rec, err := r.source.Next(ctx)
		switch {
		case errors.Is(err, io.EOF):
			// Clean end of source. Compare and return.
			return r.compareCaptured(expectedLen)
		case err != nil:
			return fmt.Errorf("eventlog: replay read source: %w", err)
		}

		dispatchErr := r.dispatch(ctx, rec)
		halted := errors.Is(dispatchErr, errEventFederationHalted)
		if dispatchErr != nil && !halted {
			return dispatchErr
		}
		if r.capturedBuf != nil {
			var err error
			expectedLen, err = r.compareRecord(expectedLen, rec)
			if err != nil {
				return err
			}
		}
		if halted {
			if r.capturedBuf != nil {
				if _, err := r.source.Next(ctx); !errors.Is(err, io.EOF) {
					if err != nil {
						return fmt.Errorf("eventlog: read after halt: %w", err)
					}
					return fmt.Errorf("%w: source contains records after terminal halt", ErrReplayDivergence)
				}
			}
			return r.compareCaptured(expectedLen)
		}
	}
}

// errEventFederationHalted is the sentinel that signals the replay loop
// to exit cleanly because an EventFederationHalted record was observed.
// It never propagates to the caller — Replay traps it and returns nil.
var errEventFederationHalted = errors.New("eventlog: federation halted (replay terminal)")

// dispatch applies supported protobuf records and passes other records to the
// writer. Strict comparison still checks their original encoding afterward.
func (r *ReplayerOf[T]) dispatch(ctx context.Context, rec core.EventRecord) error {
	pre, ok := rec.(protoEventRecord)
	if !ok {
		// Source isn't W1B's eventRecord adapter; we can't get at the
		// proto body. Passthrough using a synthetic *rtiv1.Event{Seq:N}
		// — same as the writer's synthetic path. This branch is
		// defensive; in practice every Reader returns *eventRecord.
		return r.passthrough(ctx, &rtiv1.Event{Seq: rec.Seq()})
	}
	pb := pre.ProtoEvent()
	if pb == nil {
		return r.passthrough(ctx, &rtiv1.Event{Seq: rec.Seq()})
	}
	return r.dispatchProtoEvent(ctx, pb)
}

// dispatchProtoEvent handles a real *rtiv1.Event by type-switching on
// the Body oneof. When Federation/Objects are nil, or the body is the
// empty oneof, this falls back to passthrough.
//
// # Cut-1 dispatch table
//
// FederateJoined    -> Federation.JoinFederation; assigned handle MUST
//
//	equal recorded handle, else ErrReplayDivergence.
//
// FederateResigned  -> Federation.ResignFederation with recorded handle/action.
// ObjectRegistered  -> ObjectRegistrationReplay; restores recorded identity
// without inventing reservation events or invoking public Register.
//
// AttributeUpdated  -> Objects.UpdateAttributes (ts may be nil for RO).
// InteractionSent   -> Objects.SendInteraction.
// ObjectDeleted     -> cut-2 deferred (passthrough; a future delete
//
//	handler will re-emit the event).
//
// TimeAdvance*      -> M3 (passthrough — the time manager is M3 work).
// EventFederationHalted -> terminal; dispatch returns the halted
//
//	sentinel so the replay loop exits cleanly.
//
// When dispatch is impossible (nil components) we fall back to
// passthrough, preserving the determinism contract for tests that
// don't wire the live components.
func (r *ReplayerOf[T]) dispatchProtoEvent(ctx context.Context, pb *rtiv1.Event) error {
	body := pb.GetBody()
	if body == nil {
		// Empty Event (e.g. spec fixture). Passthrough.
		return r.passthrough(ctx, pb)
	}
	switch b := body.(type) {
	case *rtiv1.Event_FedJoined:
		return r.replayFedJoined(ctx, pb)
	case *rtiv1.Event_FedResigned:
		return r.replayFedResigned(ctx, pb)
	case *rtiv1.Event_ObjRegistered:
		return r.replayObjRegistered(ctx, pb)
	case *rtiv1.Event_AttrUpdated:
		return r.replayAttrUpdated(ctx, b.AttrUpdated)
	case *rtiv1.Event_InterSent:
		return r.replayInterSent(ctx, b.InterSent)
	case *rtiv1.Event_ObjDeleted:
		// Cut-2: ObjectRegistry has no Delete in cut-1. Passthrough so
		// the captured byte stream still matches.
		return r.passthrough(ctx, pb)
	case *rtiv1.Event_TimeRequested, *rtiv1.Event_TimeGranted:
		// M3: time manager not present. Passthrough.
		return r.passthrough(ctx, pb)
	case *rtiv1.Event_Halted:
		// Terminal record. Re-emit so the captured stream contains it,
		// then signal the loop to stop.
		if err := r.passthrough(ctx, pb); err != nil {
			return err
		}
		return errEventFederationHalted
	default:
		return fmt.Errorf("eventlog: replay unknown Event body type %T", body)
	}
}

// passthrough appends pb directly into CapturingSink. The wrapper used
// is protoRecord, which satisfies both proto.Message (via the embedded
// *rtiv1.Event) and core.EventRecord (via the Seq() method). The
// writer's reflection-based assignSeq finds Event.Seq through the
// embedded pointer and assigns the next monotonic value; for passthrough
// to produce byte-equal output, that assignment MUST match pb.GetSeq().
//
// Because the writer assigns nextSeq starting at 1 and increments by 1
// per Append, and because the source's seqs are also monotonic from 1,
// passthrough is byte-equal whenever the replay processes events in the
// same order as the source — which it always does (Reader iterates in
// stored order).
func (r *ReplayerOf[T]) passthrough(ctx context.Context, pb *rtiv1.Event) error {
	rec := &protoRecord{Event: pb}
	return r.capturingSink.Append(ctx, r.capturingSink.opts.Federation, rec)
}

// replayFedJoined dispatches a FederateJoined event. If Federation is
// nil, passes through.
func (r *ReplayerOf[T]) replayFedJoined(ctx context.Context, pb *rtiv1.Event) error {
	if r.federation == nil {
		return r.passthrough(ctx, pb)
	}
	j := pb.GetFedJoined()
	modules := make([]core.FOMModule, len(j.GetAdditionalFomModules()))
	for i, module := range j.GetAdditionalFomModules() {
		modules[i] = core.FOMModule{Path: module.GetPath(), XML: append([]byte(nil), module.GetXml()...)}
	}
	assigned, err := r.federation.JoinFederation(ctx, core.JoinFederationRequest{
		Federation:   r.capturingSink.opts.Federation,
		FederateName: j.GetFederateName(),
		FederateType: j.GetFederateType(), AdditionalFOMModules: modules,
	})
	if err != nil {
		return fmt.Errorf("eventlog: replay JoinFederation %q: %w", j.GetFederateName(), err)
	}
	if uint64(assigned) != j.GetFederateHandle() {
		return fmt.Errorf("%w: FederateJoined handle: live assigned %d, source recorded %d (federate %q)",
			ErrReplayDivergence, assigned, j.GetFederateHandle(), j.GetFederateName())
	}
	return nil
}

// replayFedResigned dispatches a FederateResigned event.
func (r *ReplayerOf[T]) replayFedResigned(ctx context.Context, pb *rtiv1.Event) error {
	j := pb.GetFedResigned()
	if j == nil {
		return errors.New("eventlog: replay nil FederateResigned body")
	}
	action, err := replayResignAction(j.GetAction())
	if err != nil {
		return fmt.Errorf("eventlog: replay ResignFederation handle %d: %w", j.GetFederateHandle(), err)
	}
	if r.federation == nil {
		return r.passthrough(ctx, pb)
	}
	if err := r.federation.ResignFederation(ctx,
		r.capturingSink.opts.Federation,
		core.FederateHandle(j.GetFederateHandle()),
		action,
	); err != nil {
		return fmt.Errorf("eventlog: replay ResignFederation handle %d: %w", j.GetFederateHandle(), err)
	}
	return nil
}

func replayResignAction(action rtiv1.ResignAction) (core.ResignAction, error) {
	switch action {
	case rtiv1.ResignAction_RESIGN_ACTION_UNSPECIFIED:
		// Legacy captures and fixtures omitted the action when only
		// unconditional divestiture was supported. Keep that default
		// exclusively for UNSPECIFIED, never for unknown enum values.
		return core.ResignActionUnconditionallyDivestAttributes, nil
	case rtiv1.ResignAction_RESIGN_ACTION_UNCONDITIONALLY_DIVEST_ATTRIBUTES:
		return core.ResignActionUnconditionallyDivestAttributes, nil
	case rtiv1.ResignAction_RESIGN_ACTION_DELETE_THEN_DIVEST:
		return core.ResignActionDeleteThenDivest, nil
	case rtiv1.ResignAction_RESIGN_ACTION_CANCEL_THEN_DELETE:
		return core.ResignActionCancelThenDelete, nil
	case rtiv1.ResignAction_RESIGN_ACTION_CANCEL_PENDING_OWNERSHIP:
		return core.ResignActionCancelPendingOwnership, nil
	case rtiv1.ResignAction_RESIGN_ACTION_NO_ACTION:
		return core.ResignActionNoAction, nil
	case rtiv1.ResignAction_RESIGN_ACTION_DELETE_OBJECTS:
		return core.ResignActionDeleteObjects, nil
	default:
		return 0, fmt.Errorf("unknown recorded resign action %d: %w", action, core.ErrResignActionUnsupported)
	}
}

// replayObjRegistered uses recovery admission rather than the public federate
// service. Reservation lifetimes cannot be reconstructed from registration alone.
func (r *ReplayerOf[T]) replayObjRegistered(ctx context.Context, pb *rtiv1.Event) error {
	o := pb.GetObjRegistered()
	if o == nil {
		return errors.New("eventlog: replay nil ObjectRegistered body")
	}
	if r.objects == nil {
		return r.passthrough(ctx, pb)
	}
	recovery, ok := r.objects.(core.ObjectRegistrationReplay)
	if !ok {
		return errors.New("eventlog: object registry does not support registration replay")
	}
	err := recovery.ReplayObjectRegistration(ctx, r.capturingSink.opts.Federation, core.ObjectRegistrationRecord{
		Object: core.ObjectHandle(o.GetObjectHandle()), Class: core.ObjectClassHandle(o.GetObjectClassHandle()),
		Registrant: core.FederateHandle(o.GetOwnerFederateHandle()), Name: o.GetObjectName(), WallNS: pb.GetWallNs(),
	})
	if errors.Is(err, core.ErrObjectRegistrationReplayState) {
		return fmt.Errorf("%w: ObjectRegistered %q: %w", ErrReplayDivergence, o.GetObjectName(), err)
	}
	if err != nil {
		return fmt.Errorf("eventlog: replay registration %q: %w", o.GetObjectName(), err)
	}
	return nil
}

// replayAttrUpdated dispatches an AttributeUpdated event.
func (r *ReplayerOf[T]) replayAttrUpdated(ctx context.Context, a *rtiv1.AttributeUpdated) error {
	if r.objects == nil {
		return r.passthrough(ctx, &rtiv1.Event{Body: &rtiv1.Event_AttrUpdated{AttrUpdated: a}})
	}
	attrs := make(map[core.AttributeHandle][]byte, len(a.GetAttributes()))
	for k, v := range a.GetAttributes() {
		attrs[core.AttributeHandle(k)] = v
	}
	ts, err := decodeReplayTimestamp[T](a.LogicalTime, a.LogicalTimeValue)
	if err != nil {
		return err
	}
	if err := r.objects.UpdateAttributes(ctx,
		r.capturingSink.opts.Federation,
		core.FederateHandle(a.GetProducerFederateHandle()),
		core.ObjectHandle(a.GetObjectHandle()),
		attrs,
		ts,
	); err != nil {
		return fmt.Errorf("eventlog: replay UpdateAttributes obj %d: %w", a.GetObjectHandle(), err)
	}
	return nil
}

// replayInterSent dispatches an InteractionSent event.
func (r *ReplayerOf[T]) replayInterSent(ctx context.Context, i *rtiv1.InteractionSent) error {
	if r.objects == nil {
		return r.passthrough(ctx, &rtiv1.Event{Body: &rtiv1.Event_InterSent{InterSent: i}})
	}
	params := make(map[core.ParameterHandle][]byte, len(i.GetParameters()))
	for k, v := range i.GetParameters() {
		params[core.ParameterHandle(k)] = v
	}
	ts, err := decodeReplayTimestamp[T](i.LogicalTime, i.LogicalTimeValue)
	if err != nil {
		return err
	}
	if err := r.objects.SendInteraction(ctx,
		r.capturingSink.opts.Federation,
		core.FederateHandle(i.GetProducerFederateHandle()),
		core.InteractionClassHandle(i.GetInteractionClassHandle()),
		params,
		ts,
	); err != nil {
		return fmt.Errorf("eventlog: replay SendInteraction class %d: %w", i.GetInteractionClassHandle(), err)
	}
	return nil
}

// compareCaptured rejects missing/extra bytes, including a polluted initial
// sink. Record contents are checked independently against retained source bytes.
func (r *ReplayerOf[T]) compareCaptured(expectedLen int) error {
	if r.capturedBuf == nil {
		return nil
	}
	if actual := r.capturedBuf.Len(); actual != expectedLen {
		return fmt.Errorf("%w: captured length %d, expected %d", ErrReplayDivergence, actual, expectedLen)
	}
	return nil
}

func (r *ReplayerOf[T]) compareRecord(offset int, rec core.EventRecord) (int, error) {
	original, ok := rec.(*eventRecord)
	if !ok {
		return offset, errors.New("eventlog: byte comparison requires original Reader records")
	}
	body := original.rawBody
	bodySize := uint64(len(body))
	if bodySize > uint64(^uint32(0)) {
		return offset, fmt.Errorf("%w: record body exceeds uint32 length", ErrReplayDivergence)
	}
	end := offset + 4 + len(body)
	captured := r.capturedBuf.Bytes()
	if end > len(captured) {
		return offset, fmt.Errorf("%w: captured record missing bytes at offset %d", ErrReplayDivergence, offset-HeaderSize)
	}
	var prefix [4]byte
	binary.LittleEndian.PutUint32(prefix[:], uint32(bodySize))
	for i := range prefix {
		if captured[offset+i] != prefix[i] {
			return offset, fmt.Errorf("%w: byte mismatch at body offset %d", ErrReplayDivergence, offset+i-HeaderSize)
		}
	}
	for i, expected := range body {
		if captured[offset+4+i] != expected {
			return offset, fmt.Errorf("%w: byte mismatch at body offset %d", ErrReplayDivergence, offset+4+i-HeaderSize)
		}
	}
	return end, nil
}

// protoEventRecord is the type-asserted shape Reader-returned records
// satisfy. Defined locally so the replayer doesn't reach into Reader's
// private types.
type protoEventRecord interface {
	core.EventRecord
	ProtoEvent() *rtiv1.Event
}

// protoRecord is the writer-input wrapper used during passthrough. It
// satisfies proto.Message (via the embedded *rtiv1.Event) and
// core.EventRecord (via Seq()). The writer's reflection-based
// assignSeq finds and sets the embedded Event's Seq field; proto.Marshal
// emits the wire bytes.
//
// This mirrors the productionEvent test wrapper in
// proto_record_test.go — the production write path uses the same shape.
type protoRecord struct {
	*rtiv1.Event
}

func (p *protoRecord) Seq() uint64 { return p.Event.GetSeq() }

// Compile-time assertion that protoRecord satisfies both interfaces.
var (
	_ core.EventRecord = (*protoRecord)(nil)
	_ proto.Message    = (*protoRecord)(nil)
)
