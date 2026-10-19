package grpc

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"

	wire "github.com/cbchoi/gorti/rti/internal/adapters/protobuf"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/object"
	"github.com/cbchoi/gorti/rti/internal/hla/ownership"
	"github.com/cbchoi/gorti/rti/pkg/logicaltime"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

const CallbackCheckpointKey = "callback_pending"
const maxCallbackCheckpointBytes = 32 << 20

// CallbackCheckpoint stores invocation phases across server, transport and client
// queues. Capabilities are deliberately replaced, never persisted.
type CallbackCheckpoint struct {
	Receipts       func() *CallbackReceipts
	Ownership      func(core.FederationName) *ownership.ManagerState
	Transportation func(core.FederationName) object.TransportationCheckpointSnapshot
}
type callbackCheckpointImage struct {
	Version    uint32                       `json:"version"`
	Deliveries []callbackCheckpointDelivery `json:"deliveries"`
}
type callbackCheckpointDelivery struct {
	Recipient      core.FederateHandle              `json:"recipient"`
	Order          uint64                           `json:"order"`
	Event          []byte                           `json:"event"`
	Identity       string                           `json:"identity,omitempty"`
	Phase          string                           `json:"phase,omitempty"`
	Outcome        *CallbackCheckpointOutcome       `json:"outcome,omitempty"`
	Ownership      *OwnershipCheckpointBinding      `json:"ownership,omitempty"`
	Transportation *TransportationCheckpointBinding `json:"transportation,omitempty"`
}

type CallbackCheckpointOutcome struct {
	Success   bool   `json:"success"`
	Exception string `json:"exception"`
}

func (o *CallbackCheckpointOutcome) UnmarshalJSON(data []byte) error {
	var fields struct {
		Success   *bool   `json:"success"`
		Exception *string `json:"exception"`
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(&fields); err != nil {
		return err
	}
	if fields.Success == nil || fields.Exception == nil {
		return fmt.Errorf("callback checkpoint: incomplete outcome evidence")
	}
	if *fields.Success && *fields.Exception != "" || !*fields.Success && *fields.Exception == "" {
		return fmt.Errorf("callback checkpoint: contradictory outcome evidence")
	}
	o.Success, o.Exception = *fields.Success, *fields.Exception
	return nil
}

func (s CallbackCheckpoint) Marshal(fed core.FederationName) ([]byte, error) {
	if s.Receipts == nil || s.Receipts() == nil {
		return nil, fmt.Errorf("callback checkpoint: receipt ledger unavailable")
	}
	r := s.Receipts()
	var owned *ownership.ManagerState
	if s.Ownership != nil {
		owned = s.Ownership(fed)
	}
	var transportation object.TransportationCheckpointSnapshot
	if s.Transportation != nil {
		transportation = s.Transportation(fed)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	image := callbackCheckpointImage{Version: 2}
	for ticket, entry := range r.entries {
		if entry.fed == fed && r.discardObsoleteLocked(ticket, entry) {
			continue
		}
		if !entry.includedInCheckpoint(fed) {
			continue
		}
		if entry.entryWorking != nil || entry.outcomeWorking != nil {
			return nil, fmt.Errorf("callback checkpoint: invocation transition is still in progress")
		}
		row, err := marshalCallbackCheckpointDelivery(ticket, entry, owned, transportation)
		if err != nil {
			return nil, err
		}
		image.Deliveries = append(image.Deliveries, row)
	}
	sort.Slice(image.Deliveries, func(i, j int) bool { return image.Deliveries[i].Order < image.Deliveries[j].Order })
	data, err := json.Marshal(image)
	if len(data) > maxCallbackCheckpointBytes {
		return nil, fmt.Errorf("callback checkpoint: size limit exceeded")
	}
	return data, err
}

func marshalCallbackCheckpointDelivery(ticket string, entry *callbackReceipt, owned *ownership.ManagerState, transportation object.TransportationCheckpointSnapshot) (callbackCheckpointDelivery, error) {
	identity := entry.recoveryIdentity
	if len(identity) == 0 {
		sum := sha256.Sum256([]byte(ticket))
		identity = sum[:]
	}
	row := callbackCheckpointDelivery{Recipient: entry.h, Order: entry.checkpointOrder, Identity: hex.EncodeToString(identity), Phase: callbackPhasePending}
	if entry.reported {
		row.Outcome = &CallbackCheckpointOutcome{Success: entry.success, Exception: entry.exception}
	}
	if entry.applied {
		row.Phase = callbackPhaseCompleted
	} else {
		if entry.event == nil || !checkpointReportable(entry.event) {
			return callbackCheckpointDelivery{}, fmt.Errorf("callback checkpoint: missing portable callback body")
		}
		if entry.entryApplied {
			row.Phase = callbackPhaseOutcome
		}
		if err := marshalCallbackCheckpointBody(&row, entry.event, owned, transportation, entry.ownershipBinding); err != nil {
			return callbackCheckpointDelivery{}, err
		}
	}
	return row, nil
}

func marshalCallbackCheckpointBody(row *callbackCheckpointDelivery, source *rtiv1.FederateEvent, owned *ownership.ManagerState, transportation object.TransportationCheckpointSnapshot, retained *OwnershipCheckpointBinding) error {
	binding, err := CaptureOwnershipCheckpointBinding(owned, row.Recipient, source, row.Phase == callbackPhaseOutcome, retained)
	if err != nil {
		return err
	}
	row.Ownership = binding
	row.Transportation, err = CaptureTransportationCheckpointBinding(transportation, row.Recipient, source, row.Phase == callbackPhaseOutcome)
	if err != nil {
		return err
	}
	event := proto.Clone(source).(*rtiv1.FederateEvent)
	if event.GetTransportationChangeConfirmed() != nil {
		event, err = StripTransportationCheckpointEvent(event, row.Transportation)
		if err != nil {
			return err
		}
	}
	event.CallbackReceipt, event.OwnershipReceipt, event.CallbackRecovery = nil, nil, nil
	payload, err := proto.MarshalOptions{Deterministic: true}.Marshal(event)
	if err != nil {
		return err
	}
	row.Event = payload
	return nil
}

func decodeCallbackCheckpoint(data []byte) (callbackCheckpointImage, error) {
	if len(data) == 0 {
		return callbackCheckpointImage{Version: 1}, nil
	}
	if len(data) > maxCallbackCheckpointBytes {
		return callbackCheckpointImage{}, fmt.Errorf("callback checkpoint: size limit exceeded")
	}
	if err := rejectCallbackCheckpointDuplicateKeys(json.NewDecoder(bytes.NewReader(data)), 0); err != nil {
		return callbackCheckpointImage{}, err
	}
	var image callbackCheckpointImage
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(&image); err != nil {
		return image, err
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return image, fmt.Errorf("callback checkpoint: trailing data")
	}
	if image.Version != 1 && image.Version != 2 {
		return image, fmt.Errorf("callback checkpoint: unsupported version")
	}
	var previous uint64
	identities := map[string]bool{}
	ownershipIDs := map[[2]uint64]bool{}
	for _, delivery := range image.Deliveries {
		if !checkpointControlHandle(uint64(delivery.Recipient)) || delivery.Order <= previous {
			return image, fmt.Errorf("callback checkpoint: invalid recipient or order")
		}
		previous = delivery.Order
		if err := validateCallbackCheckpointMetadata(image.Version, delivery, identities, ownershipIDs); err != nil {
			return image, err
		}
		if delivery.Phase == callbackPhaseCompleted {
			continue
		}
		if err := validateCallbackCheckpointEvent(image.Version, delivery); err != nil {
			return image, err
		}
	}
	return image, nil
}

func validateCallbackCheckpointMetadata(version uint32, delivery callbackCheckpointDelivery, identities map[string]bool, ownershipIDs map[[2]uint64]bool) error {
	if version == 1 {
		if delivery.Identity != "" || delivery.Phase != "" || delivery.Outcome != nil || delivery.Ownership != nil || delivery.Transportation != nil {
			return fmt.Errorf("callback checkpoint: v1 recovery metadata")
		}
		return nil
	}

	if err := validateCallbackCheckpointIdentity(delivery, identities); err != nil {
		return err
	}
	if delivery.Ownership != nil {
		key := [2]uint64{uint64(delivery.Recipient), delivery.Ownership.CallbackID}
		if key[1] == 0 || ownershipIDs[key] {
			return fmt.Errorf("callback checkpoint: duplicate or invalid ownership binding")
		}
		ownershipIDs[key] = true
	}
	return nil
}

func validateCallbackCheckpointEvent(version uint32, delivery callbackCheckpointDelivery) error {
	event := &rtiv1.FederateEvent{}
	if err := proto.Unmarshal(delivery.Event, event); err != nil {
		return err
	}
	canonical, err := proto.MarshalOptions{Deterministic: true}.Marshal(event)
	if err != nil || !bytes.Equal(canonical, delivery.Event) {
		return fmt.Errorf("callback checkpoint: noncanonical or duplicate wire fields")
	}
	order, data := dataCallbackOrder(event)
	validBody := checkpointReportable(event)
	if version == 1 {
		validBody = data || event.GetDiscover() != nil
	}
	if !validBody || (data && order != core.OrderReceive && order != core.OrderTimestamp) || hasStoredCallbackCapability(event) {
		return fmt.Errorf("callback checkpoint: invalid data or stored capability")
	}
	if event.GetTransportationChangeConfirmed() != nil || delivery.Transportation != nil {
		if err := ValidateTransportationCheckpointEvent(event, delivery.Transportation, delivery.Phase == callbackPhaseOutcome); err != nil {
			return err
		}
	}
	return nil
}

func rejectCallbackCheckpointDuplicateKeys(d *json.Decoder, depth int) error {
	if depth > 8 {
		return fmt.Errorf("callback checkpoint: excessive JSON nesting")
	}
	token, err := d.Token()
	if err != nil {
		return err
	}
	delim, container := token.(json.Delim)
	if !container {
		return nil
	}
	switch delim {
	case '{':
		keys := map[string]bool{}
		for d.More() {
			key, err := d.Token()
			if err != nil {
				return err
			}
			name, ok := key.(string)
			if !ok || name != strings.ToLower(name) || keys[name] {
				return fmt.Errorf("callback checkpoint: duplicate or invalid JSON key")
			}
			keys[name] = true
			if err := rejectCallbackCheckpointDuplicateKeys(d, depth+1); err != nil {
				return err
			}
		}
	case '[':
		for d.More() {
			if err := rejectCallbackCheckpointDuplicateKeys(d, depth+1); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("callback checkpoint: invalid JSON container")
	}
	_, err = d.Token()
	return err
}

func hasUnknownCallbackFields(m protoreflect.Message) bool {
	if len(m.GetUnknown()) != 0 {
		return true
	}
	unknown := false
	m.Range(func(f protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		switch {
		case f.IsMap() && f.MapValue().Kind() == protoreflect.MessageKind:
			v.Map().Range(func(_ protoreflect.MapKey, x protoreflect.Value) bool {
				unknown = hasUnknownCallbackFields(x.Message())
				return !unknown
			})
		case f.IsList() && f.Kind() == protoreflect.MessageKind:
			for i := 0; i < v.List().Len() && !unknown; i++ {
				unknown = hasUnknownCallbackFields(v.List().Get(i).Message())
			}
		case f.Kind() == protoreflect.MessageKind && !f.IsMap() && !f.IsList():
			unknown = hasUnknownCallbackFields(v.Message())
		}
		return !unknown
	})
	return unknown
}

func (CallbackCheckpoint) Unmarshal(core.FederationName, []byte) error {
	return fmt.Errorf("callback checkpoint requires coordinated restore transition")
}
func (CallbackCheckpoint) PrepareRestoreParticipants(_ core.FederationName, data []byte, participants []core.RestoreParticipant) ([]core.RestoreParticipant, error) {
	image, err := decodeCallbackCheckpoint(data)
	if err != nil {
		return nil, err
	}
	known := map[core.FederateHandle]bool{}
	for _, p := range participants {
		known[p.SavedHandle] = true
	}
	for _, d := range image.Deliveries {
		if !known[d.Recipient] {
			return nil, fmt.Errorf("callback checkpoint: receiver is not a restore participant")
		}
	}
	return append([]core.RestoreParticipant(nil), participants...), nil
}

func (CallbackCheckpoint) RewriteFederates(data []byte, rewrite func(core.FederateHandle) (core.FederateHandle, error)) ([]byte, error) {
	if len(data) == 0 {
		return nil, nil
	}
	image, err := decodeCallbackCheckpoint(data)
	if err != nil {
		return nil, err
	}
	for i := range image.Deliveries {
		d := &image.Deliveries[i]
		d.Recipient, err = rewrite(d.Recipient)
		if err != nil {
			return nil, err
		}
		if d.Phase == callbackPhaseCompleted {
			continue
		}
		event := &rtiv1.FederateEvent{}
		if err = proto.Unmarshal(d.Event, event); err != nil {
			return nil, err
		}
		if err := rewriteCheckpointProducer(event, rewrite); err != nil {
			return nil, err
		}
		if err := rewriteCheckpointControlFederates(event, rewrite); err != nil {
			return nil, err
		}
		d.Event, err = proto.MarshalOptions{Deterministic: true}.Marshal(event)
		if err != nil {
			return nil, err
		}
	}
	return json.Marshal(image)
}

// PrepareReplay installs fresh, unadmitted tickets before any restored queue is
// published. Exclusive checkpoint admission must span preparation and commit.
func (r *CallbackReceipts) PrepareCheckpointReplay(ctx context.Context, fed core.FederationName, data []byte, participants []core.RestoreParticipant) (map[core.FederateHandle][]core.OutboundEvent, *CallbackReplayReservation, error) {
	return r.PrepareCheckpointReplayWithOwnership(ctx, fed, data, participants, nil)
}

func (r *CallbackReceipts) PrepareCheckpointReplayWithOwnership(ctx context.Context, fed core.FederationName, data []byte, participants []core.RestoreParticipant, owned *ownership.ManagerState) (map[core.FederateHandle][]core.OutboundEvent, *CallbackReplayReservation, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if _, err := (CallbackCheckpoint{}).PrepareRestoreParticipants(fed, data, participants); err != nil {
		return nil, nil, err
	}
	image, err := decodeCallbackCheckpoint(data)
	if err != nil {
		return nil, nil, err
	}
	replay := &CallbackReplayReservation{receipts: r}
	preparedEvents := map[uint64]*rtiv1.FederateEvent{}
	for _, d := range image.Deliveries {
		if d.Phase == callbackPhaseCompleted {
			continue
		}
		event := &rtiv1.FederateEvent{}
		if err = proto.Unmarshal(d.Event, event); err != nil {
			return nil, nil, err
		}
		event, err = PrepareOwnershipCheckpointEvent(owned, d.Recipient, event, d.Ownership, d.Phase == callbackPhaseOutcome)
		if err != nil {
			return nil, nil, err
		}
		preparedEvents[d.Order] = event
	}
	pending := map[core.FederateHandle][]core.OutboundEvent{}
	credit, err := r.checkReplayCapacity(fed, image)
	if err != nil {
		return nil, nil, err
	}
	for _, d := range image.Deliveries {
		event := &rtiv1.FederateEvent{}
		identity, _ := hex.DecodeString(d.Identity)
		if d.Phase == callbackPhaseCompleted {
			ticket, err := r.prepareCompletedReplay(fed, d, event, identity)
			if err != nil {
				replay.Release()
				return nil, nil, err
			}
			replay.tickets = append(replay.tickets, ticket)
			pending[d.Recipient] = append(pending[d.Recipient], &requiredCallbackEvent{event: event})
			continue
		}
		event = preparedEvents[d.Order]
		if image.Version == 2 {
			event.CallbackRecovery = &rtiv1.CallbackRecovery{InvocationIdentity: identity, OutcomeOnly: d.Phase == callbackPhaseOutcome}
		}
		ticket, err := r.preparePendingReplay(fed, d, event, identity, credit)
		if err != nil {
			replay.Release()
			return nil, nil, err
		}
		replay.tickets = append(replay.tickets, ticket)
		event.CallbackReceipt = bytes.Clone(ticket)
		if event.GetTransportationChangeConfirmed() != nil {
			replay.transportation = append(replay.transportation, callbackReplayTransportation{recipient: d.Recipient, binding: d.Transportation, event: event, outcomeOnly: d.Phase == callbackPhaseOutcome})
		}
		pending[d.Recipient] = append(pending[d.Recipient], &requiredCallbackEvent{event: event})
	}
	return pending, replay, nil
}

type CallbackReplayReservation struct {
	mu             sync.Mutex
	receipts       *CallbackReceipts
	tickets        [][]byte
	done           bool
	transportation []callbackReplayTransportation
}

func (p *CallbackReplayReservation) Commit() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.done {
		return nil
	}
	p.done = true
	return nil
}
func (p *CallbackReplayReservation) Admit() {
	p.receipts.mu.Lock()
	defer p.receipts.mu.Unlock()
	for _, ticket := range p.tickets {
		if entry := p.receipts.entries[string(ticket)]; entry != nil {
			entry.admitted = true
		}
	}
}
func (p *CallbackReplayReservation) Release() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.done {
		return
	}
	p.receipts.mu.Lock()
	defer p.receipts.mu.Unlock()
	for _, ticket := range p.tickets {
		key := string(ticket)
		if entry := p.receipts.entries[key]; entry != nil && entry.restorePrepared {
			delete(p.receipts.entries, key)
			if !entry.applied {
				p.receipts.retirePendingLocked()
			}
		}
	}
	p.done = true
}

var _ core.TransitionSnapshotter = CallbackCheckpoint{}
var _ core.FederateSnapshotRebinder = CallbackCheckpoint{}

func (s CallbackCheckpoint) ValidateCheckpoint(_ core.FederationName, model core.FOMHandle, roster []core.FederateHandle, factory string, data []byte) error {
	participants := make([]core.RestoreParticipant, len(roster))
	for i, h := range roster {
		participants[i].SavedHandle = h
	}
	if _, err := s.PrepareRestoreParticipants("", data, participants); err != nil {
		return err
	}
	return s.ValidateModel(data, model, factory)
}

func (CallbackCheckpoint) ValidateConveyedRegions(data, ddmState []byte) error {
	image, err := decodeCallbackCheckpoint(data)
	if err != nil {
		return err
	}
	var regions rtiv1.DDMManagerState
	if err = proto.Unmarshal(ddmState, &regions); err != nil {
		return err
	}
	leases := map[[2]uint64]bool{}
	for _, region := range regions.ConveyedRegions {
		for _, recipient := range region.Recipients {
			leases[[2]uint64{region.Designator, recipient.FederateHandle}] = recipient.Active
		}
	}
	for _, d := range image.Deliveries {
		if d.Phase == callbackPhaseCompleted {
			continue
		}
		event := &rtiv1.FederateEvent{}
		if err = proto.Unmarshal(d.Event, event); err != nil {
			return err
		}
		var metadata *rtiv1.ObjectCallbackMetadata
		switch {
		case event.GetReflect() != nil:
			metadata = event.GetReflect().Metadata
		case event.GetReceive() != nil:
			metadata = event.GetReceive().Metadata
		case event.GetRemove() != nil:
			metadata = event.GetRemove().Metadata
		}
		seen := map[uint64]bool{}
		for _, h := range metadata.GetReceivedRegions().GetRegionHandles() {
			active, present := leases[[2]uint64{h, uint64(d.Recipient)}]
			if seen[h] || !present || active != (d.Phase == callbackPhaseOutcome) {
				return fmt.Errorf("callback checkpoint: missing, wrong-phase or duplicate copied-region lease")
			}
			seen[h] = true
		}
	}
	return nil
}

func (CallbackCheckpoint) ValidateModel(data []byte, model core.FOMHandle, factory string) error {
	image, err := decodeCallbackCheckpoint(data)
	if err != nil {
		return err
	}
	if len(image.Deliveries) == 0 {
		return nil
	}
	names, ok := model.(core.FOMHandleNameLookup)
	if !ok {
		return fmt.Errorf("callback checkpoint: saved model lookup unavailable")
	}
	var kind logicaltime.Representation
	switch factory {
	case float64TimeFactory:
		kind = logicaltime.HLAfloat64Time
	case integer64TimeFactory:
		kind = logicaltime.HLAinteger64Time
	default:
		return fmt.Errorf("callback checkpoint: unknown time factory")
	}
	for _, d := range image.Deliveries {
		if d.Phase == callbackPhaseCompleted {
			continue
		}
		event := &rtiv1.FederateEvent{}
		if err = proto.Unmarshal(d.Event, event); err != nil {
			return err
		}
		if err := validateCallbackCheckpointModelEvent(event, names, kind); err != nil {
			return err
		}
	}
	return nil
}
func rewriteCheckpointProducer(event *rtiv1.FederateEvent, rewrite func(core.FederateHandle) (core.FederateHandle, error)) error {
	var metadata *rtiv1.ObjectCallbackMetadata
	switch {
	case event.GetDiscover() != nil:
		v := event.GetDiscover()
		if v.ProducingFederate != nil {
			producer, e := rewrite(core.FederateHandle(*v.ProducingFederate))
			if e != nil {
				return e
			}
			value := uint64(producer)
			v.ProducingFederate = &value
		}
	case event.GetReflect() != nil:
		metadata = event.GetReflect().Metadata
	case event.GetReceive() != nil:
		metadata = event.GetReceive().Metadata
	case event.GetRemove() != nil:
		metadata = event.GetRemove().Metadata
	}
	if metadata != nil && metadata.ProducingFederate != nil {
		producer, e := rewrite(core.FederateHandle(*metadata.ProducingFederate))
		if e != nil {
			return e
		}
		value := uint64(producer)
		metadata.ProducingFederate = &value
	}
	return nil
}

func validateCallbackCheckpointModelEvent(event *rtiv1.FederateEvent, names core.FOMHandleNameLookup, kind logicaltime.Representation) error {
	legacy, exact, err := checkpointDataModelTime(event, names)
	if err != nil {
		return err
	}
	at, err := wire.DecodeOptionalLogicalTimePair(kind, legacy, exact)
	if err != nil {
		return err
	}
	order, _ := dataCallbackOrder(event)
	if order == core.OrderTimestamp && at == nil {
		return fmt.Errorf("callback checkpoint: timestamp order has no logical time")
	}
	if err := validateCheckpointControl(event, names, kind); err != nil {
		return err
	}
	return nil
}

func checkpointDataModelTime(event *rtiv1.FederateEvent, names core.FOMHandleNameLookup) (*float64, *rtiv1.LogicalTimeValue, error) {
	var legacy *float64
	var exact *rtiv1.LogicalTimeValue
	switch {
	case event.GetDiscover() != nil:
		v := event.GetDiscover()
		if v.ObjectHandle == 0 || v.ObjectName == "" {
			return nil, nil, fmt.Errorf("callback checkpoint: invalid discovery identity")
		}
		if _, ok := names.ObjectClassName(core.ObjectClassHandle(v.ObjectClassHandle)); !ok {
			return nil, nil, fmt.Errorf("callback checkpoint: unknown discovery class")
		}
	case event.GetReceive() != nil:
		v := event.GetReceive()
		legacy, exact = v.LogicalTime, v.LogicalTimeValue
		cls := core.InteractionClassHandle(v.InteractionClassHandle)
		if _, ok := names.InteractionClassName(cls); !ok {
			return nil, nil, fmt.Errorf("callback checkpoint: unknown interaction class")
		}
		if err := validateCheckpointParameters(names, cls, v.Parameters); err != nil {
			return nil, nil, err
		}
	case event.GetReflect() != nil:
		v := event.GetReflect()
		legacy, exact = v.LogicalTime, v.LogicalTimeValue
		cls := core.ObjectClassHandle(v.ObjectClassHandle)
		if v.ObjectHandle == 0 {
			return nil, nil, fmt.Errorf("callback checkpoint: invalid reflected object")
		}
		if _, ok := names.ObjectClassName(cls); !ok {
			return nil, nil, fmt.Errorf("callback checkpoint: unknown reflected class")
		}
		for a := range v.Attributes {
			if _, ok := names.AttributeName(cls, core.AttributeHandle(a)); !ok {
				return nil, nil, fmt.Errorf("callback checkpoint: unknown attribute")
			}
		}
	case event.GetRemove() != nil:
		v := event.GetRemove()
		legacy, exact = v.LogicalTime, v.LogicalTimeValue
		if v.ObjectHandle == 0 {
			return nil, nil, fmt.Errorf("callback checkpoint: invalid removed object")
		}
	}
	return legacy, exact, nil
}
func (entry *callbackReceipt) includedInCheckpoint(fed core.FederationName) bool {
	return (entry.required || entry.entryApplied || entry.applied) &&
		entry.fed == fed && !entry.checkpointExcluded && (entry.admitted || entry.sent)
}

func validateCallbackCheckpointIdentity(delivery callbackCheckpointDelivery, identities map[string]bool) error {
	identity, err := hex.DecodeString(delivery.Identity)
	if err != nil || len(identity) != 32 || delivery.Identity != strings.ToLower(delivery.Identity) || identities[delivery.Identity] {
		return fmt.Errorf("callback checkpoint: invalid or duplicate invocation identity")
	}
	identities[delivery.Identity] = true
	if delivery.Phase != callbackPhasePending && delivery.Phase != callbackPhaseOutcome && delivery.Phase != callbackPhaseCompleted {
		return fmt.Errorf("callback checkpoint: invalid invocation phase")
	}
	if delivery.Phase == callbackPhasePending && delivery.Outcome != nil {
		return fmt.Errorf("callback checkpoint: pending body has an outcome")
	}
	if delivery.Phase == callbackPhaseCompleted {
		if len(delivery.Event) != 0 || delivery.Ownership != nil || delivery.Transportation != nil {
			return fmt.Errorf("callback checkpoint: completed callback carries body")
		}
		return nil
	}
	return nil
}

func hasStoredCallbackCapability(event *rtiv1.FederateEvent) bool {
	return len(event.CallbackReceipt) != 0 || event.OwnershipReceipt != nil ||
		event.CallbackRecovery != nil || hasUnknownCallbackFields(event.ProtoReflect())
}

func validateCheckpointParameters(names core.FOMHandleNameLookup, cls core.InteractionClassHandle, parameters map[uint64][]byte) error {
	for p := range parameters {
		if _, ok := names.ParameterName(cls, core.ParameterHandle(p)); !ok {
			return fmt.Errorf("callback checkpoint: unknown parameter")
		}
	}
	return nil
}

func (r *CallbackReceipts) checkReplayCapacity(fed core.FederationName, image callbackCheckpointImage) (int, error) {
	r.mu.Lock()
	credit := 0
	for _, entry := range r.entries {
		if entry.fed == fed && !entry.applied && !entry.restoreEligible && !entry.restorePrepared {
			credit++
		}
	}
	needed, completed := 0, 0
	for _, d := range image.Deliveries {
		if d.Phase != callbackPhaseCompleted {
			needed++
		} else {
			completed++
		}
	}
	if completed > r.opts.MaxCompleted-r.pinnedCompletedOtherLocked(fed) {
		r.mu.Unlock()
		return 0, fmt.Errorf("callback checkpoint: completed receipt capacity unavailable")
	}
	if needed > r.opts.MaxPending-(r.pending+r.reserved-credit) {
		r.mu.Unlock()
		return 0, fmt.Errorf("callback checkpoint: replay receipt capacity unavailable")
	}
	r.mu.Unlock()
	return credit, nil
}

func (r *CallbackReceipts) prepareCompletedReplay(fed core.FederationName, d callbackCheckpointDelivery, event *rtiv1.FederateEvent, identity []byte) ([]byte, error) {
	ticket := make([]byte, 32)
	if _, err := rand.Read(ticket); err != nil {
		return nil, err
	}
	r.mu.Lock()
	if r.pinnedCompletedOtherLocked(fed) >= r.opts.MaxCompleted || r.entries[string(ticket)] != nil || r.nextCheckpointOrder == ^uint64(0) {
		r.mu.Unlock()
		return nil, fmt.Errorf("callback checkpoint: completed identity capacity unavailable")
	}
	r.nextCheckpointOrder++
	event.CallbackRecovery = &rtiv1.CallbackRecovery{InvocationIdentity: identity, OutcomeOnly: true, Completed: true}
	entry := &callbackReceipt{fed: fed, h: d.Recipient, event: proto.Clone(event).(*rtiv1.FederateEvent), applied: true, reported: d.Outcome != nil, entryApplied: true, required: true, restorePrepared: true, checkpointOrder: r.nextCheckpointOrder, recoveryIdentity: bytes.Clone(identity)}
	if d.Outcome != nil {
		entry.success = d.Outcome.Success
		entry.exception = d.Outcome.Exception
	}
	r.entries[string(ticket)] = entry
	r.mu.Unlock()
	return ticket, nil
}

func (r *CallbackReceipts) preparePendingReplay(fed core.FederationName, d callbackCheckpointDelivery, event *rtiv1.FederateEvent, identity []byte, credit int) ([]byte, error) {
	ticket := make([]byte, 32)
	if _, err := rand.Read(ticket); err != nil {
		return nil, err
	}
	r.mu.Lock()
	if r.pending+r.reserved-credit >= r.opts.MaxPending || r.entries[string(ticket)] != nil || r.nextCheckpointOrder == ^uint64(0) {
		r.mu.Unlock()
		return nil, fmt.Errorf("callback checkpoint: replay receipt capacity unavailable")
	}
	r.nextCheckpointOrder++
	if len(identity) == 0 {
		sum := sha256.Sum256(ticket)
		identity = sum[:]
	}
	entry := &callbackReceipt{fed: fed, h: d.Recipient, event: proto.Clone(event).(*rtiv1.FederateEvent), required: true, restorePrepared: true, checkpointOrder: r.nextCheckpointOrder, recoveryIdentity: identity, entryApplied: d.Phase == callbackPhaseOutcome, recoveryOutcome: d.Phase == callbackPhaseOutcome}
	entry.ownershipBinding = d.Ownership
	if d.Outcome != nil {
		entry.reported = true
		entry.success = d.Outcome.Success
		entry.exception = d.Outcome.Exception
	}
	r.entries[string(ticket)] = entry
	r.pending++
	r.mu.Unlock()
	return ticket, nil
}

const (
	callbackPhasePending   = "pending"
	callbackPhaseOutcome   = "outcome"
	callbackPhaseCompleted = "completed"
)
