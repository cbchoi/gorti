package savebundle

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	stdtime "time"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/object"
	timepkg "github.com/cbchoi/gorti/rti/internal/hla/time"
)

// Time persists time-management state and queued object callbacks. Version 1
// uses JSON numbers for finite logical times and the string "+Inf" for the
// product final-time sentinel. Version 2 adds a pending lookahead reduction;
// Version 3 adds strict TAR/NMR grant history at the current logical time.
// Version 4 adds a separate receive-order queue. Version 5 adds delivered
// FQR/next-message minima, pending time-mode enables and receiver frontiers.
// States without extensions retain Version 1. All five versions can be restored.
// This does not alter the public wire protocol.
type TimeOf[T core.LogicalTimeScalar] struct{ Manager *timepkg.ManagerOf[T] }

type timeSnapshotNumber core.LogicalTime

func (t timeSnapshotNumber) MarshalJSON() ([]byte, error) {
	if math.IsInf(float64(t), 1) {
		return []byte(`"+Inf"`), nil
	}
	if math.IsNaN(float64(t)) || t < 0 {
		return nil, fmt.Errorf("invalid saved logical time %v", t)
	}
	return json.Marshal(float64(t))
}

func (t *timeSnapshotNumber) UnmarshalJSON(data []byte) error {
	if bytes.Equal(data, []byte(`"+Inf"`)) {
		*t = timeSnapshotNumber(core.PositiveInfinity)
		return nil
	}
	var number float64
	if bytes.Equal(data, []byte("null")) {
		return fmt.Errorf("null saved logical time")
	}
	if err := json.Unmarshal(data, &number); err != nil {
		return err
	}
	if math.IsNaN(number) || math.IsInf(number, 0) || number < 0 {
		return fmt.Errorf("invalid saved logical time %v", number)
	}
	*t = timeSnapshotNumber(number)
	return nil
}

type timeSnapshotOf[N core.LogicalTimeScalar] struct {
	LogicalTimeImplementationName string                      `json:"logical_time_implementation_name,omitempty"`
	Version                       uint32                      `json:"version"`
	Halted                        bool                        `json:"halted"`
	StallTimeout                  stdtime.Duration            `json:"stall_timeout_ns"`
	Federates                     []timeFederateSnapshotOf[N] `json:"federates"`
}

type timeFederateSnapshotOf[N core.LogicalTimeScalar] struct {
	Handle               core.FederateHandle       `json:"handle"`
	HasRegulationState   bool                      `json:"has_regulation_state"`
	Regulating           bool                      `json:"regulating"`
	Constrained          bool                      `json:"constrained"`
	RegulationPending    bool                      `json:"regulation_pending,omitempty"`
	RegulationLookahead  N                         `json:"regulation_lookahead,omitempty"`
	ConstrainedPending   bool                      `json:"constrained_pending,omitempty"`
	PreservedReceiveTime *N                        `json:"preserved_receive_time,omitempty"`
	ReceivedTSOTime      *N                        `json:"received_tso_time,omitempty"`
	Lookahead            N                         `json:"lookahead"`
	RequestedLookahead   *N                        `json:"requested_lookahead,omitempty"`
	LookaheadOriginTime  N                         `json:"lookahead_origin_time,omitempty"`
	LookaheadOriginValue N                         `json:"lookahead_origin_value,omitempty"`
	HasAdvanceState      bool                      `json:"has_advance_state"`
	CurrentTime          N                         `json:"current_time"`
	StrictZeroLookahead  bool                      `json:"strict_zero_lookahead,omitempty"`
	PendingRequest       bool                      `json:"pending_request"`
	RequestedTime        N                         `json:"requested_time"`
	Mode                 timepkg.AdvanceMode       `json:"mode"`
	FlushMinimum         *N                        `json:"flush_minimum,omitempty"`
	HasPendingSince      bool                      `json:"has_pending_since"`
	PendingAge           stdtime.Duration          `json:"pending_age_ns"`
	AsynchronousDelivery bool                      `json:"asynchronous_delivery"`
	Elapsed              *timepkg.ElapsedTimeState `json:"elapsed,omitempty"`
	Queue                []timeQueuedSnapshotOf[N] `json:"queue"`
	ROQueue              timeROQueueSnapshotOf[N]  `json:"ro_queue,omitempty"`
	hasVersion5Fields    bool
	hasReceiveHistory    bool
}

func (f timeFederateSnapshotOf[N]) MarshalJSON() ([]byte, error) {
	type plain timeFederateSnapshotOf[N]
	data, err := json.Marshal(plain(f))
	if err != nil || !f.hasReceiveHistory || f.PreservedReceiveTime != nil {
		return data, err
	}
	// Explicit absence distinguishes new receiver history from legacy snapshots
	// that shared the sender's strict zero-lookahead flag.
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return nil, err
	}
	fields["preserved_receive_time"] = json.RawMessage("null")
	return json.Marshal(fields)
}

func (f *timeFederateSnapshotOf[N]) UnmarshalJSON(data []byte) error {
	type plain timeFederateSnapshotOf[N]
	var decoded plain
	if err := decodeSnapshotJSON(data, &decoded); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	if integerScalar[N]() {
		for _, name := range []string{"handle", "lookahead", "current_time", "requested_time"} {
			if err := requireSnapshotScalarField(fields, name); err != nil {
				return err
			}
		}
	}
	for rawKey, value := range fields {
		key := strings.ToLower(rawKey)
		switch key {
		case "flush_minimum", "regulation_pending", "regulation_lookahead", "constrained_pending", "preserved_receive_time", "received_tso_time":
			decoded.hasVersion5Fields = true
			if key == "preserved_receive_time" {
				decoded.hasReceiveHistory = true
				continue
			}
			if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
				return fmt.Errorf("time snapshot: null %s", key)
			}
		}
	}
	*f = timeFederateSnapshotOf[N](decoded)
	return nil
}

type timeROQueueSnapshotOf[N core.LogicalTimeScalar] []timeQueuedSnapshotOf[N]

func (q *timeROQueueSnapshotOf[N]) UnmarshalJSON(data []byte) error {
	// Preserve field presence even for null, so an older version cannot hide
	// a receive-order extension behind an empty value.
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		*q = timeROQueueSnapshotOf[N]{}
		return nil
	}
	type plain timeROQueueSnapshotOf[N]
	return decodeSnapshotJSON(data, (*plain)(q))
}

type timeQueuedSnapshotOf[N core.LogicalTimeScalar] struct {
	Destination      core.FederateHandle    `json:"destination"`
	Timestamp        N                      `json:"timestamp"`
	Sender           core.FederateHandle    `json:"sender"`
	RetractionHandle uint64                 `json:"retraction_handle"`
	ReceiveOrder     bool                   `json:"receive_order,omitempty"`
	Event            timeEventSnapshotOf[N] `json:"event"`
}

// Kind tags a closed union. RO queues accept reflect, receive and remove;
// TSO queues also support internal deletion markers. The complete callback
// envelope is retained, including binary tags and the original timestamp.
type timeEventSnapshotOf[N core.LogicalTimeScalar] struct {
	Metadata         *core.ObjectCallbackMetadata `json:"metadata,omitempty"`
	Kind             string                       `json:"kind"`
	Sequence         uint64                       `json:"sequence"`
	Object           core.ObjectHandle            `json:"object"`
	ObjectClass      core.ObjectClassHandle       `json:"object_class"`
	ObjectName       string                       `json:"object_name"`
	Attributes       map[uint64][]byte            `json:"attributes"`
	AttributeHandles []core.AttributeHandle       `json:"attribute_handles"`
	InteractionClass core.InteractionClassHandle  `json:"interaction_class"`
	Parameters       map[uint64][]byte            `json:"parameters"`
	LogicalTime      *N                           `json:"logical_time"`
	UserSuppliedTag  []byte                       `json:"user_supplied_tag"`
	ObjectNames      []string                     `json:"object_names"`
	CollidingNames   []string                     `json:"colliding_names"`
}

func (s TimeOf[T]) Marshal(fed core.FederationName) ([]byte, error) {
	if integerScalar[T]() {
		return marshalTimeOf[T, integerSnapshotNumber](s, fed)
	}
	return marshalTimeOf[T, timeSnapshotNumber](s, fed)
}

func marshalTimeOf[T, N core.LogicalTimeScalar](s TimeOf[T], fed core.FederationName) ([]byte, error) {
	if integerScalar[T]() != integerScalar[N]() {
		return nil, fmt.Errorf("time snapshot: scalar representation mismatch")
	}
	if s.Manager == nil {
		return nil, fmt.Errorf("time snapshot: manager is required")
	}
	state, err := s.Manager.ExportState(fed, cloneTimeEventOf[T, N])
	if err != nil {
		return nil, err
	}
	if state == nil {
		return nil, nil
	}
	wire := timeSnapshotOf[N]{Version: 1, Halted: state.Halted, StallTimeout: state.StallTimeout}
	if integerScalar[T]() {
		wire.Version = 6
		wire.LogicalTimeImplementationName = checkpointHLAinteger64Time
	}
	for _, entry := range state.Federates {
		if err := appendTimeFederateOf[T, N](&wire, entry); err != nil {
			return nil, err
		}
	}
	return json.Marshal(wire)
}

// Unmarshal is the checked immediate path. Present empty bytes clear saved time
// state; a missing manifest entry must be handled by the save coordinator.
func (s TimeOf[T]) Unmarshal(fed core.FederationName, data []byte) error {
	commit, err := s.PrepareRestore(fed, data)
	if err != nil {
		return err
	}
	commit()
	return nil
}

func (s TimeOf[T]) PrepareRestore(fed core.FederationName, data []byte) (func(), error) {
	if integerScalar[T]() {
		return prepareTimeOf[T, integerSnapshotNumber](s, fed, data)
	}
	return prepareTimeOf[T, timeSnapshotNumber](s, fed, data)
}

func prepareTimeOf[T, N core.LogicalTimeScalar](s TimeOf[T], fed core.FederationName, data []byte) (func(), error) {
	if integerScalar[T]() != integerScalar[N]() {
		return nil, fmt.Errorf("time snapshot: scalar representation mismatch")
	}
	if s.Manager == nil {
		return nil, fmt.Errorf("time snapshot: manager is required")
	}
	if len(data) == 0 {
		return s.Manager.PrepareImportState(fed, nil, cloneTimeEventOf[T, N])
	}
	var wire timeSnapshotOf[N]
	if err := decodeSnapshotJSON(data, &wire); err != nil {
		return nil, fmt.Errorf("time snapshot for %q: %w", fed, err)
	}
	if err := validateTimeSnapshotVersionOf[N](wire); err != nil {
		return nil, err
	}
	state := &timepkg.ManagerStateOf[T]{Halted: wire.Halted, StallTimeout: wire.StallTimeout}
	for _, record := range wire.Federates {
		entry, err := decodeTimeFederateOf[T, N](record, wire.Version)
		if err != nil {
			return nil, err
		}
		state.Federates = append(state.Federates, entry)
	}
	return s.Manager.PrepareImportState(fed, state, cloneTimeEventOf[T, N])
}

func cloneTimeEventOf[T core.LogicalTimeScalar, N core.LogicalTimeScalar](event core.OutboundEvent) (core.OutboundEvent, error) {
	// A missing timestamp is allowed only for a classified RO callback. The
	// enclosing queue is validated separately, so TSO cannot adopt this escape.
	e, ok := event.(*object.CallbackEventOf[T])
	ro := ok && e != nil && e.LogicalTime == nil && e.Metadata != nil && e.Metadata.ReceivedOrder == core.OrderReceive
	wire, err := encodeTimeEventModeOf[T, N](event, ro)
	if err != nil {
		return nil, err
	}
	return decodeTimeEventModeOf[T, N](wire, ro)
}

func encodeTimeEventOf[T core.LogicalTimeScalar, N core.LogicalTimeScalar](event core.OutboundEvent) (timeEventSnapshotOf[N], error) {
	return encodeTimeEventModeOf[T, N](event, false)
}

func encodeTimeEventModeOf[T core.LogicalTimeScalar, N core.LogicalTimeScalar](event core.OutboundEvent, receiveOrder bool) (timeEventSnapshotOf[N], error) {
	if integerScalar[T]() != integerScalar[N]() {
		return timeEventSnapshotOf[N]{}, fmt.Errorf("time snapshot: scalar representation mismatch")
	}
	e, ok := event.(*object.CallbackEventOf[T])
	if !ok || e == nil {
		return timeEventSnapshotOf[N]{}, fmt.Errorf("time snapshot: unsupported queued event %T", event)
	}
	kind, err := encodeTimeCallbackKind(e.Kind, receiveOrder)
	if err != nil {
		return timeEventSnapshotOf[N]{}, err
	}
	if err := validateTimeCallbackShape(e.Kind, e.Object, e.InteractionClass, e.LogicalTime, receiveOrder); err != nil {
		return timeEventSnapshotOf[N]{}, err
	}
	var timestamp *N
	if e.LogicalTime != nil {
		t := N(*e.LogicalTime)
		timestamp = &t
	}
	metadata, err := cloneCallbackMetadata(e.Metadata)
	if err != nil {
		return timeEventSnapshotOf[N]{}, err
	}
	if err := validateDelayedSubscriptionMetadata(kind, metadata); err != nil {
		return timeEventSnapshotOf[N]{}, err
	}
	if err := validateMOMPointQueuedEvent(kind, metadata, receiveOrder, e.LogicalTime != nil); err != nil {
		return timeEventSnapshotOf[N]{}, err
	}
	if receiveOrder && !validROMetadata(metadata) {
		return timeEventSnapshotOf[N]{}, fmt.Errorf("time snapshot: RO callback requires receive-order metadata without retraction")
	}
	return timeEventSnapshotOf[N]{
		Metadata: metadata,
		Kind:     kind, Sequence: e.Sequence, Object: e.Object, ObjectClass: e.ObjectClass, ObjectName: e.ObjectName,
		Attributes: cloneTimeBytesMap(e.Attributes), AttributeHandles: cloneTimeSlice(e.AttributeHandles),
		InteractionClass: e.InteractionClass, Parameters: cloneTimeBytesMap(e.Parameters), LogicalTime: timestamp,
		UserSuppliedTag: cloneTimeSlice(e.UserSuppliedTag), ObjectNames: cloneTimeSlice(e.ObjectNames), CollidingNames: cloneTimeSlice(e.CollidingNames),
	}, nil
}

func decodeTimeEventOf[T core.LogicalTimeScalar, N core.LogicalTimeScalar](wire timeEventSnapshotOf[N]) (*object.CallbackEventOf[T], error) {
	return decodeTimeEventModeOf[T, N](wire, false)
}

func decodeTimeEventModeOf[T core.LogicalTimeScalar, N core.LogicalTimeScalar](wire timeEventSnapshotOf[N], receiveOrder bool) (*object.CallbackEventOf[T], error) {
	if integerScalar[T]() != integerScalar[N]() {
		return nil, fmt.Errorf("time snapshot: scalar representation mismatch")
	}
	kind, err := decodeTimeCallbackKind(wire.Kind, receiveOrder)
	if err != nil {
		return nil, err
	}
	if err := validateTimeCallbackShape(kind, wire.Object, wire.InteractionClass, wire.LogicalTime, receiveOrder); err != nil {
		return nil, err
	}
	var timestamp *T
	if wire.LogicalTime != nil {
		t := T(*wire.LogicalTime)
		timestamp = &t
	}
	metadata, err := cloneCallbackMetadata(wire.Metadata)
	if err != nil {
		return nil, err
	}
	if err := validateDelayedSubscriptionMetadata(wire.Kind, metadata); err != nil {
		return nil, err
	}
	if err := validateMOMPointQueuedEvent(wire.Kind, metadata, receiveOrder, wire.LogicalTime != nil); err != nil {
		return nil, err
	}
	if receiveOrder && !validROMetadata(metadata) {
		return nil, fmt.Errorf("time snapshot: RO callback requires receive-order metadata without retraction")
	}
	return &object.CallbackEventOf[T]{
		Metadata: metadata,
		Kind:     kind, Sequence: wire.Sequence, Object: wire.Object, ObjectClass: wire.ObjectClass, ObjectName: wire.ObjectName,
		Attributes: cloneTimeBytesMap(wire.Attributes), AttributeHandles: cloneTimeSlice(wire.AttributeHandles),
		InteractionClass: wire.InteractionClass, Parameters: cloneTimeBytesMap(wire.Parameters), LogicalTime: timestamp,
		UserSuppliedTag: cloneTimeSlice(wire.UserSuppliedTag), ObjectNames: cloneTimeSlice(wire.ObjectNames), CollidingNames: cloneTimeSlice(wire.CollidingNames),
	}, nil
}

func validROMetadata(m *core.ObjectCallbackMetadata) bool {
	return m != nil && m.ReceivedOrder == core.OrderReceive && m.RetractionHandle == 0
}

func validateTimeSnapshotVersionOf[N core.LogicalTimeScalar](wire timeSnapshotOf[N]) error {
	if integerScalar[N]() {
		if wire.Version != 6 || wire.LogicalTimeImplementationName != checkpointHLAinteger64Time {
			return fmt.Errorf("time snapshot: integer time requires version 6 and HLAinteger64Time")
		}
	} else if wire.Version < 1 || wire.Version > 5 || wire.LogicalTimeImplementationName != "" && wire.LogicalTimeImplementationName != checkpointHLAfloat64Time {
		return fmt.Errorf("time snapshot: unsupported version %d", wire.Version)
	}
	hasVersion5 := false
	for _, f := range wire.Federates {
		newState := hasVersion5TimeState(f)
		hasVersion5 = hasVersion5 || newState
		if err := validateTimeFederateVersion(f, wire.Version, newState); err != nil {
			return err
		}
	}
	if wire.Version == 5 && !hasVersion5 {
		return fmt.Errorf("time snapshot: version 5 has no FQR/enable extension")
	}
	return nil
}

func decodeTimeDeliveryOf[T core.LogicalTimeScalar, N core.LogicalTimeScalar](q timeQueuedSnapshotOf[N], receiveOrder bool) (core.MessageDeliveryOf[T], error) {
	if q.ReceiveOrder != receiveOrder || receiveOrder && (q.Timestamp != 0 || q.RetractionHandle != 0) {
		return core.MessageDeliveryOf[T]{}, fmt.Errorf("time snapshot: invalid queued delivery order, timestamp or retraction")
	}
	e, err := decodeTimeEventModeOf[T, N](q.Event, receiveOrder)
	if err != nil {
		return core.MessageDeliveryOf[T]{}, err
	}
	if !receiveOrder && (e.LogicalTime == nil || *e.LogicalTime != T(q.Timestamp)) {
		return core.MessageDeliveryOf[T]{}, fmt.Errorf("time snapshot: queue and callback timestamps differ")
	}
	return core.MessageDeliveryOf[T]{Recipient: q.Destination, Timestamp: T(q.Timestamp), Event: e, Sender: q.Sender, RetractionHandle: q.RetractionHandle, ReceiveOrder: receiveOrder}, nil
}

func cloneCallbackMetadata(m *core.ObjectCallbackMetadata) (*core.ObjectCallbackMetadata, error) {
	if m == nil {
		return nil, nil
	}
	if m.SentOrder > core.OrderTimestamp || m.ReceivedOrder > core.OrderTimestamp || m.Transportation > core.TransportTypeBestEffort {
		return nil, fmt.Errorf("time snapshot: invalid callback metadata enum")
	}
	if m.RetractionHandle != 0 && (m.Producer == 0 || m.SentOrder != core.OrderTimestamp) {
		return nil, fmt.Errorf("time snapshot: invalid callback retraction metadata")
	}
	for _, region := range m.Regions {
		if region == 0 {
			return nil, fmt.Errorf("time snapshot: zero received region")
		}
	}
	if err := validateConveyedCallbackMetadata(m); err != nil {
		return nil, err
	}
	if err := validateMOMPointCallbackMetadata(m); err != nil {
		return nil, err
	}
	cloned := *m
	if m.MOMAttributePoint != nil {
		point := *m.MOMAttributePoint
		cloned.MOMAttributePoint = &point
	}
	cloned.Regions = cloneTimeSlice(m.Regions)
	cloned.ConveyedRegions = cloneTimeSlice(m.ConveyedRegions)
	if m.AttributeConveyedRegions != nil {
		cloned.AttributeConveyedRegions = make(map[core.AttributeHandle][]core.ConveyedRegionCopy, len(m.AttributeConveyedRegions))
		for attr, regions := range m.AttributeConveyedRegions {
			cloned.AttributeConveyedRegions[attr] = cloneTimeSlice(regions)
		}
	}
	return &cloned, nil
}

func cloneTimeSlice[T any](src []T) []T {
	if src == nil {
		return nil
	}
	return append(make([]T, 0, len(src)), src...)
}

func cloneTimeBytesMap(src map[uint64][]byte) map[uint64][]byte {
	if src == nil {
		return nil
	}
	dst := make(map[uint64][]byte, len(src))
	for key, value := range src {
		dst[key] = cloneTimeSlice(value)
	}
	return dst
}

func appendTimeFederateOf[T, N core.LogicalTimeScalar](wire *timeSnapshotOf[N], entry timepkg.FederateStateOf[T]) error {
	record := timeFederateSnapshotOf[N]{
		Handle: entry.Handle, HasRegulationState: entry.HasRegulationState,
		Regulating: entry.Regulating, Constrained: entry.Constrained, Lookahead: N(entry.Lookahead),
		RegulationPending: entry.RegulationPending, RegulationLookahead: N(entry.RegulationLookahead), ConstrainedPending: entry.ConstrainedPending,
		HasAdvanceState: entry.HasAdvanceState, CurrentTime: N(entry.CurrentTime),
		StrictZeroLookahead: entry.StrictZeroLookahead,
		PendingRequest:      entry.PendingRequest, RequestedTime: N(entry.RequestedTime), Mode: entry.Mode,
		HasPendingSince: entry.HasPendingSince, PendingAge: entry.PendingAge, AsynchronousDelivery: entry.AsynchronousDelivery,
		Elapsed: entry.Elapsed,
	}
	marshalTimeFederateTimingOf(wire, &record, entry)
	for _, queued := range entry.Queue {
		if queued.ReceiveOrder {
			return fmt.Errorf("time snapshot: RO entry in TSO queue")
		}
		event, err := encodeTimeEventOf[T, N](queued.Event)
		if err != nil {
			return err
		}
		if T(*event.LogicalTime) != queued.Timestamp {
			return fmt.Errorf("time snapshot: queue and callback timestamps differ")
		}
		record.Queue = append(record.Queue, timeQueuedSnapshotOf[N]{Destination: queued.Recipient, Timestamp: N(queued.Timestamp), Sender: queued.Sender, RetractionHandle: queued.RetractionHandle, Event: event})
	}
	for _, queued := range entry.ROQueue {
		event, err := encodeTimeEventModeOf[T, N](queued.Event, true)
		if err != nil {
			return err
		}
		q := timeQueuedSnapshotOf[N]{Destination: queued.Recipient, Timestamp: N(queued.Timestamp), Sender: queued.Sender, RetractionHandle: queued.RetractionHandle, ReceiveOrder: queued.ReceiveOrder, Event: event}
		if _, err := decodeTimeDeliveryOf[T, N](q, true); err != nil {
			return err
		}
		record.ROQueue = append(record.ROQueue, q)
		wire.Version = max(wire.Version, 4)
	}
	wire.Federates = append(wire.Federates, record)
	return nil
}

func marshalTimeFederateTimingOf[T, N core.LogicalTimeScalar](wire *timeSnapshotOf[N], record *timeFederateSnapshotOf[N], entry timepkg.FederateStateOf[T]) {
	if entry.FlushMinimum != nil {
		minimum := N(*entry.FlushMinimum)
		record.FlushMinimum = &minimum
		wire.Version = max(wire.Version, 5)
	}
	if entry.PreservedReceiveTime != nil {
		received := N(*entry.PreservedReceiveTime)
		record.PreservedReceiveTime = &received
	}
	if entry.ReceivedTSOTime != nil {
		received := N(*entry.ReceivedTSOTime)
		record.ReceivedTSOTime = &received
		wire.Version = max(wire.Version, 5)
	}
	if entry.PreservedReceiveTime != nil || entry.Constrained && entry.StrictZeroLookahead {
		record.hasReceiveHistory = true
		wire.Version = max(wire.Version, 5)
	}
	if entry.RegulationPending || entry.ConstrainedPending {
		wire.Version = max(wire.Version, 5)
	}
	if entry.RequestedLookahead != nil {
		wire.Version = max(wire.Version, 2)
		target := N(*entry.RequestedLookahead)
		record.RequestedLookahead = &target
		record.LookaheadOriginTime = N(entry.LookaheadOriginTime)
		record.LookaheadOriginValue = N(entry.LookaheadOriginValue)
	}
	if entry.StrictZeroLookahead {
		wire.Version = max(wire.Version, 3)
	}
}

func decodeTimeFederateOf[T, N core.LogicalTimeScalar](record timeFederateSnapshotOf[N], version uint32) (timepkg.FederateStateOf[T], error) {
	entry := timepkg.FederateStateOf[T]{
		Handle: record.Handle, HasRegulationState: record.HasRegulationState,
		Regulating: record.Regulating, Constrained: record.Constrained, Lookahead: T(record.Lookahead),
		RegulationPending: record.RegulationPending, RegulationLookahead: T(record.RegulationLookahead), ConstrainedPending: record.ConstrainedPending,
		HasAdvanceState: record.HasAdvanceState, CurrentTime: T(record.CurrentTime),
		StrictZeroLookahead: record.StrictZeroLookahead,
		PendingRequest:      record.PendingRequest, RequestedTime: T(record.RequestedTime), Mode: record.Mode,
		HasPendingSince: record.HasPendingSince, PendingAge: record.PendingAge, AsynchronousDelivery: record.AsynchronousDelivery,
		Elapsed: record.Elapsed,
	}
	if record.FlushMinimum != nil {
		minimum := T(*record.FlushMinimum)
		entry.FlushMinimum = &minimum
	}
	if record.PreservedReceiveTime != nil {
		received := T(*record.PreservedReceiveTime)
		entry.PreservedReceiveTime = &received
	} else if version < 5 && record.Constrained && record.StrictZeroLookahead {
		received := entry.CurrentTime
		entry.PreservedReceiveTime = &received
	}
	if record.ReceivedTSOTime != nil {
		received := T(*record.ReceivedTSOTime)
		entry.ReceivedTSOTime = &received
	}
	if record.RequestedLookahead != nil {
		target := T(*record.RequestedLookahead)
		entry.RequestedLookahead = &target
	}
	entry.LookaheadOriginTime = T(record.LookaheadOriginTime)
	entry.LookaheadOriginValue = T(record.LookaheadOriginValue)
	for _, queued := range record.Queue {
		delivery, err := decodeTimeDeliveryOf[T, N](queued, false)
		if err != nil {
			return timepkg.FederateStateOf[T]{}, err
		}
		entry.Queue = append(entry.Queue, delivery)
	}
	for _, queued := range record.ROQueue {
		delivery, err := decodeTimeDeliveryOf[T, N](queued, true)
		if err != nil {
			return timepkg.FederateStateOf[T]{}, err
		}
		entry.ROQueue = append(entry.ROQueue, delivery)
	}
	return entry, nil
}

func encodeTimeCallbackKind(callbackKind object.CallbackKind, receiveOrder bool) (string, error) {
	var kind string
	switch callbackKind {
	case object.CallbackReflect:
		kind = checkpointReflect
	case object.CallbackReceive:
		kind = checkpointReceive
	case object.CallbackRemove:
		kind = checkpointRemove
	case object.CallbackDeleteMarker:
		if receiveOrder {
			return "", fmt.Errorf("time snapshot: internal deletion marker in RO queue")
		}
		kind = checkpointDeleteMarker
	default:
		return "", fmt.Errorf("time snapshot: unsupported queued callback kind %d", callbackKind)
	}
	return kind, nil
}

func decodeTimeCallbackKind(wireKind string, receiveOrder bool) (object.CallbackKind, error) {
	var kind object.CallbackKind
	switch wireKind {
	case checkpointReflect:
		kind = object.CallbackReflect
	case checkpointReceive:
		kind = object.CallbackReceive
	case checkpointRemove:
		kind = object.CallbackRemove
	case checkpointDeleteMarker:
		if receiveOrder {
			return 0, fmt.Errorf("time snapshot: internal deletion marker in RO queue")
		}
		kind = object.CallbackDeleteMarker
	default:
		return 0, fmt.Errorf("time snapshot: unknown queued event kind %q", wireKind)
	}
	return kind, nil
}

func validateTimeCallbackShape[T core.LogicalTimeScalar](kind object.CallbackKind, objectHandle core.ObjectHandle, interaction core.InteractionClassHandle, timestamp *T, receiveOrder bool) error {
	if (kind == object.CallbackReceive && interaction == 0) || (kind != object.CallbackReceive && objectHandle == 0) {
		return fmt.Errorf("time snapshot: queued callback has a zero handle")
	}
	if timestamp == nil && !receiveOrder || timestamp != nil && !validSnapshotScalar(*timestamp) {
		return fmt.Errorf("time snapshot: queued callback needs a valid timestamp")
	}
	return nil
}

func hasVersion5TimeState[N core.LogicalTimeScalar](f timeFederateSnapshotOf[N]) bool {
	return f.FlushMinimum != nil || f.RegulationPending || f.ConstrainedPending || f.RegulationLookahead != 0 || f.PreservedReceiveTime != nil || f.hasReceiveHistory || f.ReceivedTSOTime != nil
}

func validateTimeFederateVersion[N core.LogicalTimeScalar](f timeFederateSnapshotOf[N], version uint32, newState bool) error {

	if version < 5 && (newState || f.hasVersion5Fields) {
		return fmt.Errorf("time snapshot: FQR/enable state requires version 5")
	}
	if version >= 5 && f.Constrained && f.StrictZeroLookahead && !f.hasReceiveHistory && f.PreservedReceiveTime == nil {
		return fmt.Errorf("time snapshot: version 5 requires explicit receiver strict history")
	}
	if version < 4 && f.ROQueue != nil {
		return fmt.Errorf("time snapshot: RO queue requires version 4")
	}
	if version < 3 && f.StrictZeroLookahead {
		return fmt.Errorf("time snapshot: strict zero-lookahead history requires version 3")
	}
	if version < 2 && f.RequestedLookahead != nil {
		return fmt.Errorf("time snapshot: requested lookahead requires version 2")
	}
	return nil
}
