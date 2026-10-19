package grpc

import (
	"context"
	"fmt"
	"github.com/cbchoi/gorti/rti/internal/adapters/auth/session"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/declaration"
	"github.com/cbchoi/gorti/rti/internal/hla/mom"
	objectpkg "github.com/cbchoi/gorti/rti/internal/hla/object"
	"github.com/cbchoi/gorti/rti/internal/hla/ownership"
	savepoint "github.com/cbchoi/gorti/rti/internal/hla/save_restore"
	syncpkg "github.com/cbchoi/gorti/rti/internal/hla/synchronization"
	timepkg "github.com/cbchoi/gorti/rti/internal/hla/time"
	stdgrpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
)

const (
	grpcDefaultMaxReceiveMessageSize = 4 << 20
	eventBatchSizeSafetyMargin       = 64 << 10
	maxEventBatchSerializedSize      = grpcDefaultMaxReceiveMessageSize - eventBatchSizeSafetyMargin
	// maxCoalescedEventBatchEvents caps how many callback events the
	// greedy drain in sendAvailableEventBatches concatenates into one
	// wire frame. The contractual outbox admission batch stays 32; this
	// cap only bounds how many already-ready 32-event batches share a
	// frame (W7: 64 -> 256, i.e. up to 8 batches per frame instead of 2).
	// Grant frame-cut semantics are unaffected: a TimeAdvanceGrant still
	// ends the current wire frame immediately.
	maxCoalescedEventBatchEvents = 256
)

// SubscribableOutbox is the M2 forward-declaration of the richer Outbox
// the StreamService.Events handler needs. core.Outbox is push-only by
// design; the streaming handler must register itself as a subscriber and
// drain a channel of OutboundEvents.
//
// W4 (cmd/rtid wiring) supplies the production implementation. For tests
// here, a fake satisfies the interface with an in-memory channel.
//
// Cancel returns any error encountered while tearing down the
// subscription; the handler returns its own loop error first, so the
// cancel error is best-effort.
type SubscribableOutbox interface {
	core.Outbox
	Subscribe(ctx context.Context, fed core.FederationName, h core.FederateHandle) (<-chan []core.OutboundEvent, func() error, error)
}

// streamService binds rtiv1.StreamServiceServer to a core.Outbox. If the
// outbox additionally satisfies SubscribableOutbox, Events drains the
// federate's outbound channel and forwards each event onto the gRPC
// server stream. Otherwise Events returns Unimplemented.
type streamService struct {
	rtiv1.UnimplementedStreamServiceServer
	outbox            core.Outbox
	membership        core.FederationMembershipValidator
	sessions          *session.Manager
	receipts          *CallbackReceipts
	ownershipReceipts *OwnershipCallbackReceipts
}

func newStreamService(outbox core.Outbox, memberships ...core.FederationMembershipValidator) *streamService {
	var membership core.FederationMembershipValidator
	if len(memberships) > 0 {
		membership = memberships[0]
	}
	return &streamService{outbox: outbox, membership: membership}
}

// Events is the server-streaming RPC. One goroutine per federate
// connection, with no worker pool.
func (s *streamService) Events(req *rtiv1.EventsRequest, stream rtiv1.StreamService_EventsServer) error {
	if !validWireVersion(req.GetWireVersion()) {
		return status.Error(codes.FailedPrecondition, "unsupported wire version")
	}
	if s.sessions != nil {
		done, err := s.sessions.Open(stream.Context(), core.FederationName(req.GetFederationName()), core.FederateHandle(req.GetFederateHandle()))
		if err != nil {
			return sessionErrorStatus(stream.Context(), err)
		}
		defer done()
	}
	var receiptStream *receiptEventsStream
	if s.receipts != nil {
		receiptStream = &receiptEventsStream{StreamService_EventsServer: stream, receipts: s.receipts, fed: core.FederationName(req.GetFederationName()), handle: core.FederateHandle(req.GetFederateHandle())}
		stream = receiptStream
	}
	if s.ownershipReceipts != nil {
		stream = &ownershipReceiptEventsStream{StreamService_EventsServer: stream, receipts: s.ownershipReceipts, fed: core.FederationName(req.GetFederationName()), handle: core.FederateHandle(req.GetFederateHandle())}
	}
	ch, cancel, err := s.subscribe(req, stream)
	if err != nil {
		return err
	}
	defer func() { _ = cancel() }()
	if s.receipts != nil {
		defer func() {
			s.receipts.markDisconnected(receiptStream.fed, receiptStream.handle)
		}()
		for _, event := range s.receipts.pendingRedelivery(core.FederationName(req.GetFederationName()), core.FederateHandle(req.GetFederateHandle())) {
			if err := stream.Send(event); err != nil {
				return err
			}
		}
	}

	return forwardEvents(ch, stream)
}

// EventBatches forwards each outbox batch as one or more size-bounded gRPC
// messages. Events remains available for older clients; both methods consume
// the same ordered outbox subscription and have identical callback semantics.
func (s *streamService) EventBatches(req *rtiv1.EventsRequest, stream rtiv1.StreamService_EventBatchesServer) error {
	if !validWireVersion(req.GetWireVersion()) {
		return status.Error(codes.FailedPrecondition, "unsupported wire version")
	}
	if s.sessions != nil {
		done, err := s.sessions.Open(stream.Context(), core.FederationName(req.GetFederationName()), core.FederateHandle(req.GetFederateHandle()))
		if err != nil {
			return sessionErrorStatus(stream.Context(), err)
		}
		defer done()
	}
	var receiptStream *receiptBatchesStream
	if s.receipts != nil {
		receiptStream = &receiptBatchesStream{StreamService_EventBatchesServer: stream, receipts: s.receipts, fed: core.FederationName(req.GetFederationName()), handle: core.FederateHandle(req.GetFederateHandle())}
		stream = receiptStream
	}
	if s.ownershipReceipts != nil {
		stream = &ownershipReceiptBatchesStream{StreamService_EventBatchesServer: stream, receipts: s.ownershipReceipts, fed: core.FederationName(req.GetFederationName()), handle: core.FederateHandle(req.GetFederateHandle())}
	}
	ch, cancel, err := s.subscribe(req, stream)
	if err != nil {
		return err
	}
	defer func() { _ = cancel() }()
	if err := stream.Send(&rtiv1.FederateEventBatch{Ready: true}); err != nil {
		return err
	}
	if s.receipts != nil {
		defer func() {
			s.receipts.markDisconnected(receiptStream.fed, receiptStream.handle)
		}()
		for _, event := range s.receipts.pendingRedelivery(core.FederationName(req.GetFederationName()), core.FederateHandle(req.GetFederateHandle())) {
			if err := stream.Send(&rtiv1.FederateEventBatch{Events: []*rtiv1.FederateEvent{event}}); err != nil {
				return err
			}
		}
	}

	return forwardEventBatches(ch, stream)
}

// sendAvailableEventBatches combines only source batches that are already
// queued. It therefore reduces transport frames under load without adding a
// batching delay at low traffic rates. A time grant ends the current wire
// batch so callbacks that follow it remain observably after the grant.
func sendAvailableEventBatches(
	first []core.OutboundEvent,
	ch <-chan []core.OutboundEvent,
	stream rtiv1.StreamService_EventBatchesServer,
	scratch []core.OutboundEvent,
) (bool, error) {
	pending := scratch[:0]
	batch := first

	for {
		if err := stream.Context().Err(); err != nil {
			return false, err
		}
		for _, evt := range batch {
			pending = append(pending, evt)
			isGrant := isTimeGrantEvent(evt)
			if len(pending) == maxCoalescedEventBatchEvents || isGrant {
				if err := sendEventBatchChunks(pending, stream); err != nil {
					return false, err
				}
				pending = pending[:0]
			}
		}

		select {
		case <-stream.Context().Done():
			return false, stream.Context().Err()
		case next, alive := <-ch:
			if !alive {
				if err := sendEventBatchChunks(pending, stream); err != nil {
					return false, err
				}
				return true, nil
			}
			batch = next
		default:
			if err := sendEventBatchChunks(pending, stream); err != nil {
				return false, err
			}
			return false, nil
		}
	}
}

func (s *streamService) subscribe(
	req *rtiv1.EventsRequest,
	stream stdgrpc.ServerStream,
) (<-chan []core.OutboundEvent, func() error, error) {
	sub, ok := s.outbox.(SubscribableOutbox)
	if !ok {
		return nil, nil, status.Error(codes.Unimplemented, "outbox does not support subscription (W4 wiring pending)")
	}

	fed := core.FederationName(req.GetFederationName())
	h := core.FederateHandle(req.GetFederateHandle())
	membership := s.membershipForStream(stream)
	var release func()
	if membership != nil {
		var err error
		if guard, ok := membership.(core.FederationMembershipGuard); ok {
			release, err = guard.AcquireMember(fed, h)
		} else {
			err = membership.ValidateMember(fed, h)
		}
		if err != nil {
			return nil, nil, errToStatus(stream.Context(), err)
		}
		if release != nil {
			defer release()
		}
		if req.ExpectedFederationGeneration != nil {
			generation, exists := membership.GenerationFor(fed)
			if !exists {
				return nil, nil, errToStatus(stream.Context(), core.ErrFederationNotFound)
			}
			if generation != req.GetExpectedFederationGeneration() {
				return nil, nil, errToStatus(stream.Context(), core.ErrFederationGenerationMismatch)
			}
		}
	}

	if s.sessions != nil {
		md, _ := metadata.FromIncomingContext(stream.Context())
		if len(md.Get(session.MetadataKey)) != 0 {
			if release == nil {
				return nil, nil, status.Error(codes.FailedPrecondition, "authenticated callback subscription requires membership admission")
			}
			// Open's earlier check does not pin membership through subscription.
			// Recheck under this lease so a restore/rejoin cannot replace its owner.
			if err := s.sessions.Authenticate(stream.Context(), fed, h); err != nil {
				return nil, nil, err
			}
		}
	}
	ch, cancel, err := sub.Subscribe(stream.Context(), fed, h)
	if err != nil {
		return nil, nil, errToStatus(stream.Context(), err)
	}
	return ch, cancel, nil
}

func (s *streamService) membershipForStream(stream stdgrpc.ServerStream) core.FederationMembershipValidator {
	if s.membership != nil {
		return s.membership
	}

	var wrapped stdgrpc.ServerStream
	switch typed := stream.(type) {
	case *stdgrpc.GenericServerStream[rtiv1.EventsRequest, rtiv1.FederateEvent]:
		wrapped = typed.ServerStream
	case *stdgrpc.GenericServerStream[rtiv1.EventsRequest, rtiv1.FederateEventBatch]:
		wrapped = typed.ServerStream
	}
	membershipStream, ok := wrapped.(*membershipServerStream)
	if !ok || membershipStream.server == nil {
		return nil
	}
	return membershipStream.server.membership
}

func sendEventBatchChunks(
	batch []core.OutboundEvent,
	stream rtiv1.StreamService_EventBatchesServer,
) error {
	if len(batch) == 0 {
		return nil
	}
	wireBatch := &rtiv1.FederateEventBatch{Events: newWireEventSlice(len(batch))}
	wireEvents := make([]core.OutboundEvent, 0, len(batch))
	wireSize := 0
	for i, evt := range batch {
		pb, err := toDispatchFederateEvent(evt)
		if err != nil {
			return status.Error(codes.Internal, err.Error())
		}
		if pb == nil {
			continue
		}
		entrySize := federateEventBatchEntrySize(pb)
		if entrySize > maxEventBatchSerializedSize {
			return status.Error(codes.ResourceExhausted, fmt.Sprintf(
				"callback event seq %d serialized size %d exceeds safe batch limit %d",
				pb.GetSeq(), entrySize, maxEventBatchSerializedSize,
			))
		}

		if len(wireBatch.Events) > 0 && wireSize+entrySize > maxEventBatchSerializedSize {
			if err := sendKnowledgeCheckedBatch(wireBatch, wireEvents, stream); err != nil {
				return err
			}
			wireBatch = &rtiv1.FederateEventBatch{Events: newWireEventSlice(len(batch) - i)}
			wireEvents = wireEvents[:0]
			wireSize = 0
		}
		wireBatch.Events = append(wireBatch.Events, pb)
		wireEvents = append(wireEvents, evt)
		wireSize += entrySize
	}
	if len(wireBatch.Events) == 0 {
		return nil
	}
	return sendKnowledgeCheckedBatch(wireBatch, wireEvents, stream)
}

// newWireEventSlice preallocates a wire batch's Events backing array for
// the events still to be framed (W6c). Outbox-fed batches never exceed
// maxCoalescedEventBatchEvents, so the cap covers the common case in one
// allocation; longer test-only batches simply grow past it.
func newWireEventSlice(remaining int) []*rtiv1.FederateEvent {
	if remaining > maxCoalescedEventBatchEvents {
		remaining = maxCoalescedEventBatchEvents
	}
	return make([]*rtiv1.FederateEvent, 0, remaining)
}

// Slack constants for the W6b conservative entry-size bound. Each term
// over-covers the exact protobuf wire bytes it stands in for, so the
// bound is provably >= proto.Size for the covered event shapes:
//
//   - federateEventEnvelopeSizeSlack covers every non-payload byte of a
//     Reflect/Receive FederateEvent: seq (tag + varint <= 11), the oneof
//     submessage framing (tag 1 + length varint <= 5), two uint64 handle
//     fields (<= 11 each), and the optional logical-time double
//     (tag + fixed64 = 9). Worst case 48 <= 64.
//   - federateEventValueSizeSlack covers one map<uint64, bytes> entry's
//     framing: entry tag (1) + entry length varint (<= 5) + key
//     tag + varint (<= 11) + value tag (1) + value length varint (<= 5).
//     Worst case 23 <= 32.
const (
	federateEventEnvelopeSizeSlack = 64
	federateEventValueSizeSlack    = 32
)

// federateEventBatchEntrySize returns a conservative upper bound on the
// serialized size of one FederateEventBatch.events entry (W6b). For the
// hot callback shapes (Reflect / Receive) the bound is the sum of the
// payload byte lengths plus fixed slack — no reflective proto.Size tree
// walk. Every other event type, and any bound crossing
// maxEventBatchSerializedSize/2 (where over-estimation could split or
// reject frames that actually fit), falls back to the exact walk, so the
// per-event rejection threshold is always evaluated on exact sizes.
func federateEventBatchEntrySize(event *rtiv1.FederateEvent) int {
	bound := federateEventEnvelopeSizeSlack
	switch body := event.GetEvent().(type) {
	case *rtiv1.FederateEvent_Reflect:
		for _, v := range body.Reflect.GetAttributes() {
			bound += federateEventValueSizeSlack + len(v)
		}
	case *rtiv1.FederateEvent_Receive:
		for _, v := range body.Receive.GetParameters() {
			bound += federateEventValueSizeSlack + len(v)
		}
	default:
		return exactFederateEventBatchEntrySize(event)
	}
	if bound > maxEventBatchSerializedSize/2 {
		return exactFederateEventBatchEntrySize(event)
	}
	return protowire.SizeTag(1) + protowire.SizeBytes(bound)
}

// exactFederateEventBatchEntrySize is the precise serialized size of one
// batch entry: repeated-field tag + length-delimited event bytes.
func exactFederateEventBatchEntrySize(event *rtiv1.FederateEvent) int {
	return protowire.SizeTag(1) + protowire.SizeBytes(proto.Size(event))
}

// federateEventCarrier is a duck-typed interface satisfied by adapters in
// rti/internal/object that wrap the generated *rtiv1.FederateEvent. Using
// an interface here avoids a hard import dependency between transport/grpc
// and the object package. Inner transfers an event that must remain immutable
// until the synchronous stream Send that consumes its batch returns.
type federateEventCarrier interface {
	Inner() *rtiv1.FederateEvent
}

// toFederateEvent extracts the generated proto from a core.OutboundEvent.
// Returns an error rather than panicking when the event does not expose
// the proto, so the handler can surface a clean Internal status.
//
// The generated *rtiv1.FederateEvent does not satisfy core.OutboundEvent
// directly (Seq is a field, not a method). Producers wrap it in an
// adapter that satisfies federateEventCarrier; the registry's
// outboundEvent in rti/internal/object is the canonical example.
//
// M21 TASK-204b: time-management events (*time.TimeAdvanceGrant,
// *time.FederationHalted) do NOT implement federateEventCarrier —
// the time package must not import the generated proto (layering).
// They are translated here via type-switch instead.
func toFederateEvent(evt core.OutboundEvent) (*rtiv1.FederateEvent, error) {
	if c, ok := evt.(federateEventCarrier); ok {
		return c.Inner(), nil
	}
	switch v := evt.(type) {
	case core.LocalValidationBarrierEvent:
		return &rtiv1.FederateEvent{Event: &rtiv1.FederateEvent_LocalValidationBarrier{LocalValidationBarrier: &rtiv1.LocalValidationBarrier{SnapshotId: v.SnapshotID, FederateHandle: uint64(v.Federate), CallbackEpoch: v.CallbackEpoch}}}, nil
	case *declaration.AdvisoryEvent:
		return encodeDeclarationAdvisory(v)
	case *syncpkg.AnnounceEvent, *syncpkg.SynchronizedEvent, *syncpkg.RegistrationSucceededEvent, *syncpkg.RegistrationFailedEvent:
		return encodeSynchronizationCallback(evt)
	case *objectpkg.CallbackEvent:
		return encodeObjectCallback(v)
	case *objectpkg.CallbackEventOf[int64]:
		return encodeObjectCallback(v)
	case *core.UpdateRateAdvisory:
		return encodeUpdateRateAdvisory(v)
	case *ownership.CallbackEvent:
		return encodeOwnershipCallback(v)
	case *savepoint.InitiateSaveEvent, *savepoint.InitiateSaveEventOf[int64], *savepoint.SavedEvent, *savepoint.NotSavedEvent, *savepoint.InitiateRestoreEvent, *savepoint.RestoredEvent, *savepoint.NotRestoredEvent, *savepoint.RestoreRequestSucceededEvent, *savepoint.RestoreRequestFailedEvent, *savepoint.RestoreBegunEvent:
		return encodeSavepointCallback(evt)
	case *mom.InteractionEvent, *mom.DiscoverEvent, *mom.ReflectEvent:
		return encodeMOMCallback(evt)
	case *timepkg.TimeAdvanceGrant, *timepkg.TimeAdvanceGrantOf[int64], *timepkg.TimeRegulationEnabled, *timepkg.TimeRegulationEnabledOf[int64], *timepkg.TimeConstrainedEnabled, *timepkg.TimeConstrainedEnabledOf[int64], *timepkg.FederationHalted, *timepkg.RequestRetraction:
		return encodeTimeCallback(evt)
	}
	return nil, errOutboundEventNotConvertible
}

func encodeSynchronizationCallback(evt core.OutboundEvent) (*rtiv1.FederateEvent, error) {
	switch v := evt.(type) {
	case *syncpkg.AnnounceEvent:
		required := make([]uint64, 0, len(v.Required()))
		for _, h := range v.Required() {
			required = append(required, uint64(h))
		}
		return &rtiv1.FederateEvent{Seq: v.Seq(), Event: &rtiv1.FederateEvent_SyncAnnounced{SyncAnnounced: &rtiv1.SynchronizationPointAnnounced{Label: v.Label(), Tag: v.Tag(), RequiredFederates: required}}}, nil
	case *syncpkg.SynchronizedEvent:
		failed := make([]uint64, 0, len(v.FailedToSync()))
		for _, h := range v.FailedToSync() {
			failed = append(failed, uint64(h))
		}
		return &rtiv1.FederateEvent{Seq: v.Seq(), Event: &rtiv1.FederateEvent_SyncSynchronized{SyncSynchronized: &rtiv1.FederationSynchronized{Label: v.Label(), FailedToSync: failed}}}, nil
	case *syncpkg.RegistrationSucceededEvent:
		return &rtiv1.FederateEvent{Seq: v.Seq(), Event: &rtiv1.FederateEvent_SyncRegistrationSucceeded{SyncRegistrationSucceeded: &rtiv1.SyncRegistrationSucceeded{Label: v.Label()}}}, nil
	case *syncpkg.RegistrationFailedEvent:
		reason := rtiv1.SyncPointFailureReason_SYNC_POINT_FAILURE_REASON_UNSPECIFIED
		switch v.Reason() {
		case syncpkg.RegistrationFailureLabelNotUnique:
			reason = rtiv1.SyncPointFailureReason_SYNC_POINT_FAILURE_REASON_LABEL_NOT_UNIQUE
		case syncpkg.RegistrationFailureSetMemberNotJoined:
			reason = rtiv1.SyncPointFailureReason_SYNC_POINT_FAILURE_REASON_SET_MEMBER_NOT_JOINED
		}
		return &rtiv1.FederateEvent{Seq: v.Seq(), Event: &rtiv1.FederateEvent_SyncRegistrationFailed{SyncRegistrationFailed: &rtiv1.SyncRegistrationFailed{Label: v.Label(), Reason: reason}}}, nil
	}
	return nil, errOutboundEventNotConvertible
}

func encodeSavepointCallback(evt core.OutboundEvent) (*rtiv1.FederateEvent, error) {
	switch v := evt.(type) {
	case *savepoint.InitiateSaveEvent:
		return encodeSaveInitiate(v)
	case *savepoint.InitiateSaveEventOf[int64]:
		return encodeSaveInitiate(v)
	case *savepoint.SavedEvent:
		return &rtiv1.FederateEvent{Seq: v.Seq(), Event: &rtiv1.FederateEvent_SaveCompleted{SaveCompleted: &rtiv1.FederationSaved{Label: v.Label()}}}, nil
	case *savepoint.NotSavedEvent:
		reason := int32(v.Reason)
		return &rtiv1.FederateEvent{Seq: v.Seq(), Event: &rtiv1.FederateEvent_SaveFailed{SaveFailed: &rtiv1.FederationNotSaved{Label: v.Label(), Reason: &reason}}}, nil
	case *savepoint.InitiateRestoreEvent:
		return &rtiv1.FederateEvent{Seq: v.Seq(), Event: &rtiv1.FederateEvent_RestoreInitiate{RestoreInitiate: &rtiv1.InitiateFederateRestore{Label: v.Label(), FederateHandle: uint64(v.FederateHandle), FederateName: v.FederateName, ClientState: append([]byte(nil), v.ClientState...)}}}, nil
	case *savepoint.RestoredEvent:
		body := &rtiv1.FederationRestored{Label: v.Label()}
		if v.FederateHandle != 0 {
			handle := uint64(v.FederateHandle)
			body.FederateHandle = &handle
		}
		if v.CallbackEpoch != 0 {
			epoch := v.CallbackEpoch
			body.CallbackEpoch = &epoch
		}
		if v.NextRetractionHandle != 0 {
			next := v.NextRetractionHandle
			body.NextRetractionHandle = &next
		}
		return &rtiv1.FederateEvent{Seq: v.Seq(), Event: &rtiv1.FederateEvent_RestoreCompleted{RestoreCompleted: body}}, nil
	case *savepoint.NotRestoredEvent:
		reason := int32(v.Reason)
		return &rtiv1.FederateEvent{Seq: v.Seq(), Event: &rtiv1.FederateEvent_RestoreFailed{RestoreFailed: &rtiv1.FederationNotRestored{Label: v.Label(), Reason: &reason}}}, nil
	case *savepoint.RestoreRequestSucceededEvent:
		return &rtiv1.FederateEvent{Seq: v.Seq(), Event: &rtiv1.FederateEvent_RestoreRequestSucceeded{RestoreRequestSucceeded: &rtiv1.RequestFederationRestoreSucceeded{Label: v.Label()}}}, nil
	case *savepoint.RestoreRequestFailedEvent:
		return &rtiv1.FederateEvent{Seq: v.Seq(), Event: &rtiv1.FederateEvent_RestoreRequestFailed{RestoreRequestFailed: &rtiv1.RequestFederationRestoreFailed{Label: v.Label(), Reason: v.Reason}}}, nil
	case *savepoint.RestoreBegunEvent:
		return &rtiv1.FederateEvent{Seq: v.Seq(), Event: &rtiv1.FederateEvent_RestoreBegun{RestoreBegun: &rtiv1.FederationRestoreBegun{}}}, nil
	}
	return nil, errOutboundEventNotConvertible
}

func encodeMOMCallback(evt core.OutboundEvent) (*rtiv1.FederateEvent, error) {
	switch v := evt.(type) {
	case *mom.InteractionEvent:
		params := make(map[uint64][]byte, len(v.Parameters))
		for h, value := range v.Parameters {
			params[uint64(h)] = append([]byte(nil), value...)
		}
		return &rtiv1.FederateEvent{Seq: v.Seq(), Event: &rtiv1.FederateEvent_Receive{Receive: &rtiv1.ReceiveInteraction{InteractionClassHandle: uint64(v.Class), Parameters: params, Metadata: momCallbackMetadata()}}}, nil
	case *mom.DiscoverEvent:
		return &rtiv1.FederateEvent{Seq: v.Seq(), Event: &rtiv1.FederateEvent_Discover{Discover: &rtiv1.DiscoverObjectInstance{ObjectHandle: uint64(v.Object), ObjectClassHandle: uint64(v.Class), ObjectName: v.Name}}}, nil
	case *mom.ReflectEvent:
		attrs := make(map[uint64][]byte, len(v.Attributes))
		for h, value := range v.Attributes {
			attrs[uint64(h)] = append([]byte(nil), value...)
		}
		return &rtiv1.FederateEvent{Seq: v.Seq(), Event: &rtiv1.FederateEvent_Reflect{Reflect: &rtiv1.ReflectAttributeValues{ObjectHandle: uint64(v.Object), ObjectClassHandle: uint64(v.Class), Attributes: attrs, Metadata: momCallbackMetadata()}}}, nil
	}
	return nil, errOutboundEventNotConvertible
}

func encodeTimeCallback(evt core.OutboundEvent) (*rtiv1.FederateEvent, error) {
	switch v := evt.(type) {
	case *timepkg.TimeAdvanceGrant:
		return encodeTimeGrant(v)
	case *timepkg.TimeAdvanceGrantOf[int64]:
		return encodeTimeGrant(v)
	case *timepkg.TimeRegulationEnabled:
		return encodeRegulationEnabled(v)
	case *timepkg.TimeRegulationEnabledOf[int64]:
		return encodeRegulationEnabled(v)
	case *timepkg.TimeConstrainedEnabled:
		return encodeConstrainedEnabled(v)
	case *timepkg.TimeConstrainedEnabledOf[int64]:
		return encodeConstrainedEnabled(v)
	case *timepkg.FederationHalted:
		return &rtiv1.FederateEvent{
			Seq: v.Seq(),
			Event: &rtiv1.FederateEvent_Halted{
				Halted: &rtiv1.FederationHalted{
					Cause: v.Cause,
				},
			},
		}, nil
	case *timepkg.RequestRetraction:
		// §8.22 (M37) — same layering rationale as the two
		// cases above: the time package must not import the proto.
		return &rtiv1.FederateEvent{
			Seq: v.Seq(),
			Event: &rtiv1.FederateEvent_RetractionRequested{
				RetractionRequested: &rtiv1.RequestRetraction{
					SenderFederate:          uint64(v.Sender),
					MessageRetractionHandle: v.RetractionHandle,
				},
			},
		}, nil
	}
	return nil, errOutboundEventNotConvertible
}

func encodeDeclarationAdvisory(v *declaration.AdvisoryEvent) (*rtiv1.FederateEvent, error) {
	out := &rtiv1.FederateEvent{Seq: v.Seq()}
	switch v.Kind {
	case declaration.AdvisoryStartRegistration:
		out.Event = &rtiv1.FederateEvent_StartRegistration{StartRegistration: &rtiv1.StartRegistrationForObjectClass{ObjectClassHandle: uint64(v.ObjectClass)}}
	case declaration.AdvisoryStopRegistration:
		out.Event = &rtiv1.FederateEvent_StopRegistration{StopRegistration: &rtiv1.StopRegistrationForObjectClass{ObjectClassHandle: uint64(v.ObjectClass)}}
	case declaration.AdvisoryTurnInteractionsOn:
		out.Event = &rtiv1.FederateEvent_TurnInteractionsOn{TurnInteractionsOn: &rtiv1.TurnInteractionsOn{InteractionClassHandle: uint64(v.InteractionClass)}}
	case declaration.AdvisoryTurnInteractionsOff:
		out.Event = &rtiv1.FederateEvent_TurnInteractionsOff{TurnInteractionsOff: &rtiv1.TurnInteractionsOff{InteractionClassHandle: uint64(v.InteractionClass)}}
	default:
		return nil, errOutboundEventNotConvertible
	}
	return out, nil
}

func encodeOwnershipCallback(v *ownership.CallbackEvent) (*rtiv1.FederateEvent, error) {
	attrs := make([]uint64, 0, len(v.Attributes))
	for _, a := range v.Attributes {
		attrs = append(attrs, uint64(a))
	}
	out := &rtiv1.FederateEvent{Seq: v.Seq()}
	switch v.Kind {
	case ownership.CallbackAssumption:
		out.Event = &rtiv1.FederateEvent_OwnershipAssumption{OwnershipAssumption: &rtiv1.RequestAttributeOwnershipAssumption{ObjectHandle: uint64(v.Object), AttributeHandles: attrs, DivestingFederate: uint64(v.Federate), Tag: append([]byte(nil), v.Tag...)}}
	case ownership.CallbackDivestitureConfirmation:
		out.Event = &rtiv1.FederateEvent_OwnershipDivestConfirmed{OwnershipDivestConfirmed: &rtiv1.RequestDivestitureConfirmation{ObjectHandle: uint64(v.Object), AttributeHandles: attrs}}
	case ownership.CallbackAcquisitionNotification:
		out.Event = &rtiv1.FederateEvent_OwnershipAcquired{OwnershipAcquired: &rtiv1.AttributeOwnershipAcquisitionNotification{ObjectHandle: uint64(v.Object), AttributeHandles: attrs, OwningFederate: uint64(v.Federate), Tag: append([]byte(nil), v.Tag...)}}
	case ownership.CallbackReleaseRequest:
		out.Event = &rtiv1.FederateEvent_OwnershipReleaseRequested{OwnershipReleaseRequested: &rtiv1.RequestAttributeOwnershipRelease{ObjectHandle: uint64(v.Object), AttributeHandles: attrs, Tag: append([]byte(nil), v.Tag...)}}
	case ownership.CallbackUnavailable:
		out.Event = &rtiv1.FederateEvent_OwnershipUnavailable{OwnershipUnavailable: &rtiv1.AttributeOwnershipUnavailable{ObjectHandle: uint64(v.Object), AttributeHandles: attrs}}
	case ownership.CallbackAcquisitionCancellation:
		out.Event = &rtiv1.FederateEvent_OwnershipAcquisitionCancelled{OwnershipAcquisitionCancelled: &rtiv1.ConfirmAttributeOwnershipAcquisitionCancellation{ObjectHandle: uint64(v.Object), AttributeHandles: attrs}}
	default:
		return nil, errOutboundEventNotConvertible
	}
	if scope, id, strict := v.OwnershipCallbackIdentity(); strict {
		out.OwnershipReceipt = &rtiv1.OwnershipCallbackReceipt{FederationGeneration: scope.FederationGeneration, CallbackEpoch: scope.CallbackEpoch, CallbackId: id}
	}
	return out, nil
}

func encodeUpdateRateAdvisory(v *core.UpdateRateAdvisory) (*rtiv1.FederateEvent, error) {
	attrs := make([]uint64, len(v.Attributes))
	for i, h := range v.Attributes {
		attrs[i] = uint64(h)
	}
	if v.Enabled {
		var rate *string
		if v.UpdateRateDesignator != nil {
			cloned := *v.UpdateRateDesignator
			rate = &cloned
		}
		return &rtiv1.FederateEvent{Seq: v.Seq(), Event: &rtiv1.FederateEvent_TurnUpdatesOn{TurnUpdatesOn: &rtiv1.TurnUpdatesOnForObjectInstance{ObjectHandle: uint64(v.Object), AttributeHandles: attrs, UpdateRateDesignator: rate}}}, nil
	}
	return &rtiv1.FederateEvent{Seq: v.Seq(), Event: &rtiv1.FederateEvent_TurnUpdatesOff{TurnUpdatesOff: &rtiv1.TurnUpdatesOffForObjectInstance{ObjectHandle: uint64(v.Object), AttributeHandles: attrs}}}, nil
}

// EncodeOutboundEvent converts a transport-neutral HLA callback into the
// versioned Protobuf wire envelope. It is exported for black-box conformance
// harnesses and non-gRPC adapters that need the canonical wire mapping.
func EncodeOutboundEvent(evt core.OutboundEvent) (*rtiv1.FederateEvent, error) {
	return toFederateEvent(evt)
}

func momCallbackMetadata() *rtiv1.ObjectCallbackMetadata {
	// This RTI sends management reports reliably without a timestamp or producing federate.
	order := rtiv1.OrderType_ORDER_TYPE_RECEIVE
	transport := rtiv1.TransportationType_TRANSPORTATION_TYPE_RELIABLE
	return &rtiv1.ObjectCallbackMetadata{SentOrder: &order, ReceivedOrder: &order, TransportationType: &transport}
}

// errOutboundEventNotConvertible signals an OutboundEvent that the stream
// handler cannot serialize. Surface as Internal: this is a wiring bug,
// not a client error.
var errOutboundEventNotConvertible = errOutboundEvent("outbound event has no *rtiv1.FederateEvent payload")

// errOutboundEvent is a tiny named-string error type so the message is
// stable for tests without pulling in fmt.
type errOutboundEvent string

func (e errOutboundEvent) Error() string { return string(e) }

// Compile-time assertion that streamService implements the generated
// server interface.
var _ rtiv1.StreamServiceServer = (*streamService)(nil)

func forwardEvents(ch <-chan []core.OutboundEvent, stream rtiv1.StreamService_EventsServer) error {
	for {
		select {
		case batch, alive := <-ch:
			if !alive {
				return nil
			}
			for _, evt := range batch {
				pb, err := toDispatchFederateEvent(evt)
				if err != nil {
					return status.Error(codes.Internal, err.Error())
				}
				if pb == nil {
					continue
				}
				if err := stream.Send(pb); err != nil {
					return err
				}
			}
		case <-stream.Context().Done():
			return stream.Context().Err()
		}
	}
}

func forwardEventBatches(ch <-chan []core.OutboundEvent, stream rtiv1.StreamService_EventBatchesServer) error {
	scratch := make([]core.OutboundEvent, 0, maxCoalescedEventBatchEvents)
	for {
		select {
		case batch, alive := <-ch:
			if !alive {
				return nil
			}
			closed, err := sendAvailableEventBatches(batch, ch, stream, scratch)
			if err != nil {
				return err
			}
			if closed {
				return nil
			}
		case <-stream.Context().Done():
			return stream.Context().Err()
		}
	}
}
