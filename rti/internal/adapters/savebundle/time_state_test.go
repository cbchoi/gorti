package savebundle

import (
	"bytes"
	"context"
	"encoding/json"
	"math"
	"reflect"
	"testing"
	stdtime "time"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/object"
	savepoint "github.com/cbchoi/gorti/rti/internal/hla/save_restore"
	timepkg "github.com/cbchoi/gorti/rti/internal/hla/time"
)

var _ savepoint.ManagerSnapshotter = Time{}
var _ savepoint.PreparedSnapshotter = Time{}

type timeStateSent struct {
	fed   core.FederationName
	dst   core.FederateHandle
	event core.OutboundEvent
}
type timeStateOutbox struct{ sent []timeStateSent }

func (o *timeStateOutbox) Send(_ context.Context, fed core.FederationName, dst core.FederateHandle, event core.OutboundEvent) error {
	o.sent = append(o.sent, timeStateSent{fed, dst, event})
	return nil
}

func newTimeStateCodec(t *testing.T) (Time, *core.ManualClock, *timeStateOutbox) {
	t.Helper()
	clock := core.NewManualClock(stdtime.Unix(2000, 0))
	out := &timeStateOutbox{}
	m, err := timepkg.New(timepkg.Options{Clock: clock, Outbox: out, StallTimeout: 15 * stdtime.Second})
	if err != nil {
		t.Fatal(err)
	}
	return Time{Manager: m}, clock, out
}

func timeStateCallbacks() []*object.CallbackEvent {
	times := []core.LogicalTime{8, 3, core.PositiveInfinity}
	return []*object.CallbackEvent{
		{Sequence: 91, Kind: object.CallbackReflect, Object: 44, Attributes: map[uint64][]byte{9: {0, 255}, 1: {1}}, LogicalTime: &times[0], UserSuppliedTag: []byte{0, 254}},
		{Sequence: 92, Kind: object.CallbackReceive, InteractionClass: 33, Parameters: map[uint64][]byte{3: {255, 0}}, LogicalTime: &times[1], UserSuppliedTag: []byte{255, 0}},
		{Sequence: 93, Kind: object.CallbackRemove, Object: 44, LogicalTime: &times[2], UserSuppliedTag: []byte{0, 253}},
	}
}

func TestTimeStateCodecRoundTripTSOAndDeterminism(t *testing.T) {
	s, clock, out := newTimeStateCodec(t)
	ctx := context.Background()
	if err := s.Manager.EnableConstrained(ctx, "fed", 2); err != nil {
		t.Fatal(err)
	}
	if len(out.sent) != 1 {
		t.Fatalf("constraint activation callbacks: %v", out.sent)
	}
	if _, ok := out.sent[0].event.(*timepkg.TimeConstrainedEnabled); !ok {
		t.Fatalf("unexpected activation callback: %T", out.sent[0].event)
	}
	out.sent = nil
	events := timeStateCallbacks()
	for _, e := range events {
		if err := s.Manager.BufferTSOWithRetraction(ctx, "fed", 2, *e.LogicalTime, e, 7, e.Sequence+100); err != nil {
			t.Fatal(err)
		}
	}
	data, err := s.Marshal("fed")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte(`"+Inf"`)) {
		t.Fatalf("infinite logical time not encoded: %s", data)
	}
	for i := 0; i < 10; i++ {
		got, err := s.Marshal("fed")
		if err != nil || !bytes.Equal(data, got) {
			t.Fatalf("nondeterministic snapshot: %s, %v", got, err)
		}
	}
	commit, err := s.PrepareRestore("fed", data)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Unmarshal("fed", nil); err != nil {
		t.Fatal(err)
	}
	clock.Advance(stdtime.Hour)
	commit()
	round, err := s.Marshal("fed")
	if err != nil || !bytes.Equal(data, round) {
		t.Fatalf("round trip changed payload: %s, %v", round, err)
	}
	if err := s.Manager.EnableAsynchronousDelivery(ctx, "fed", 2); err != nil {
		t.Fatal(err)
	}
	if len(out.sent) != 0 {
		t.Fatal("restored future TSO queue escaped through asynchronous delivery")
	}
	if err := s.Manager.DisableConstrained(ctx, "fed", 2); err != nil {
		t.Fatal(err)
	}
	if len(out.sent) != 3 {
		t.Fatalf("got %d callbacks", len(out.sent))
	}
	for i, sent := range out.sent {
		if sent.fed != "fed" || sent.dst != 2 || !reflect.DeepEqual(sent.event, events[i]) {
			t.Fatalf("event %d: %+v, want %+v", i, sent, events[i])
		}
	}
}

func TestTimeStateCodecRestoredQueuePrecedesGrant(t *testing.T) {
	s, _, out := newTimeStateCodec(t)
	ctx := context.Background()
	_ = s.Manager.EnableRegulation(ctx, "fed", 1, 1)
	_ = s.Manager.EnableConstrained(ctx, "fed", 2)
	if len(out.sent) != 2 {
		t.Fatalf("activation callbacks: %v", out.sent)
	}
	if _, ok := out.sent[0].event.(*timepkg.TimeRegulationEnabled); !ok {
		t.Fatalf("unexpected regulation callback: %T", out.sent[0].event)
	}
	if _, ok := out.sent[1].event.(*timepkg.TimeConstrainedEnabled); !ok {
		t.Fatalf("unexpected constraint callback: %T", out.sent[1].event)
	}
	out.sent = nil
	e := timeStateCallbacks()[1]
	_ = s.Manager.BufferTSOWithRetraction(ctx, "fed", 2, *e.LogicalTime, e, 1, 99)
	_ = s.Manager.TimeAdvanceRequest(ctx, "fed", 2, 5)
	data, err := s.Marshal("fed")
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Unmarshal("fed", nil)
	if err := s.Unmarshal("fed", data); err != nil {
		t.Fatal(err)
	}
	if len(out.sent) != 0 {
		t.Fatal("restore emitted premature callback")
	}
	if err := s.Manager.ModifyLookahead(ctx, "fed", 1, 10); err != nil {
		t.Fatal(err)
	}
	if len(out.sent) != 2 {
		t.Fatalf("got %d deliveries", len(out.sent))
	}
	if !reflect.DeepEqual(out.sent[0].event, e) {
		t.Fatal("TSO did not precede grant")
	}
	grant, ok := out.sent[1].event.(*timepkg.TimeAdvanceGrant)
	if !ok || grant.Time != 5 || out.sent[1].dst != 2 {
		t.Fatalf("grant lost: %+v", out.sent[1])
	}
}

func TestTimeStateCodecRejectsCorruptWithoutMutation(t *testing.T) {
	s, _, _ := newTimeStateCodec(t)
	_ = s.Manager.EnableConstrained(context.Background(), "fed", 2)
	e := timeStateCallbacks()[0]
	_ = s.Manager.BufferTSO(context.Background(), "fed", 2, *e.LogicalTime, e)
	valid, err := s.Marshal("fed")
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string][]byte{
		"syntax": []byte(`{`), "null": []byte(`null`), "missing version": []byte(`{}`),
		"future version":       bytes.Replace(valid, []byte(`"version":1`), []byte(`"version":5`), 1),
		"unknown field":        bytes.Replace(valid, []byte(`"version":1`), []byte(`"version":1,"unknown":1`), 1),
		"extra document":       append(append([]byte(nil), valid...), []byte(`{}`)...),
		"unknown event":        bytes.Replace(valid, []byte(`"reflect"`), []byte(`"unsupported"`), 1),
		"missing object":       bytes.Replace(valid, []byte(`"object":44`), []byte(`"object":0`), 1),
		"wrong union kind":     bytes.Replace(valid, []byte(`"reflect"`), []byte(`"receive"`), 1),
		"duplicate version":    bytes.Replace(valid, []byte(`"version":1`), []byte(`"version":2,"version":1`), 1),
		"duplicate event kind": bytes.Replace(valid, []byte(`"kind":"reflect"`), []byte(`"kind":"unsupported","kind":"reflect"`), 1),
		"timestamp mismatch":   bytes.Replace(valid, []byte(`"timestamp":8`), []byte(`"timestamp":9`), 1),
		"nan":                  bytes.Replace(valid, []byte(`"timestamp":8`), []byte(`"timestamp":"NaN"`), 1),
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			if bytes.Equal(valid, data) {
				t.Fatal("corruption fixture had no effect")
			}
			commit, err := s.PrepareRestore("fed", data)
			if err == nil || commit != nil {
				t.Fatalf("invalid snapshot accepted: %s", data)
			}
			got, _ := s.Marshal("fed")
			if !bytes.Equal(got, valid) {
				t.Fatal("prepare changed live state")
			}
		})
	}
}

func TestTimeStateCodecRejectsUnsupportedOrNilEvents(t *testing.T) {
	for _, event := range []core.OutboundEvent{&timepkg.TimeAdvanceGrant{}, (*object.CallbackEvent)(nil), &object.CallbackEvent{Kind: object.CallbackDiscover}, nil} {
		s, _, _ := newTimeStateCodec(t)
		_ = s.Manager.BufferTSO(context.Background(), "fed", 2, 8, event)
		if _, err := s.Marshal("fed"); err == nil {
			t.Fatalf("silently accepted %T", event)
		}
	}
}

func TestTimeStateCodecInfinityCurrentAndRequest(t *testing.T) {
	s, _, _ := newTimeStateCodec(t)
	state := &timepkg.ManagerState{StallTimeout: stdtime.Minute, Federates: []timepkg.FederateState{{Handle: 4, HasAdvanceState: true, CurrentTime: core.PositiveInfinity, PendingRequest: true, RequestedTime: core.PositiveInfinity, Mode: timepkg.ModeNER, HasPendingSince: true}}}
	if err := s.Manager.ImportState("fed", state, cloneTimeEvent); err != nil {
		t.Fatal(err)
	}
	data, err := s.Marshal("fed")
	if err != nil {
		t.Fatal(err)
	}
	var document any
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	if err := s.Unmarshal("fed", data); err != nil {
		t.Fatal(err)
	}
	got, err := s.Manager.ExportState("fed", cloneTimeEvent)
	if err != nil || !math.IsInf(float64(got.Federates[0].CurrentTime), 1) || !math.IsInf(float64(got.Federates[0].RequestedTime), 1) {
		t.Fatalf("infinity lost: %+v, %v", got, err)
	}
}

func TestTimeStateCodecDeepCopiesCallbackPayloads(t *testing.T) {
	s, _, _ := newTimeStateCodec(t)
	e := timeStateCallbacks()[0]
	e.ObjectClass = 10
	e.ObjectName = "named"
	e.AttributeHandles = []core.AttributeHandle{1, 9}
	e.Parameters = map[uint64][]byte{3: {0, 7}}
	e.ObjectNames = []string{"one"}
	e.CollidingNames = []string{"two"}
	_ = s.Manager.BufferTSO(context.Background(), "fed", 2, *e.LogicalTime, e)
	before, err := s.Marshal("fed")
	if err != nil {
		t.Fatal(err)
	}
	mutate := func(state *timepkg.ManagerState) {
		callback := state.Federates[0].Queue[0].Event.(*object.CallbackEvent)
		callback.Attributes[9][0] = 55
		callback.Parameters[3][0] = 55
		callback.UserSuppliedTag[0] = 55
		callback.AttributeHandles[0] = 55
		callback.ObjectNames[0] = "changed"
		callback.CollidingNames[0] = "changed"
		*callback.LogicalTime = 55
		callback.Sequence = 55
	}
	exported, err := s.Manager.ExportState("fed", cloneTimeEvent)
	if err != nil {
		t.Fatal(err)
	}
	mutate(exported)
	current, err := s.Marshal("fed")
	if err != nil || !bytes.Equal(current, before) {
		t.Fatalf("export shared callback payload: %s, %v", current, err)
	}
	exported, err = s.Manager.ExportState("fed", cloneTimeEvent)
	if err != nil {
		t.Fatal(err)
	}
	commit, err := s.Manager.PrepareImportState("fed", exported, cloneTimeEvent)
	if err != nil {
		t.Fatal(err)
	}
	mutate(exported)
	_ = s.Unmarshal("fed", nil)
	commit()
	current, err = s.Marshal("fed")
	if err != nil || !bytes.Equal(current, before) {
		t.Fatalf("prepared image shared callback payload: %s, %v", current, err)
	}
}

func TestTimeStateCodecEmptyClearsOnlyTarget(t *testing.T) {
	s, _, _ := newTimeStateCodec(t)
	ctx := context.Background()
	for _, fed := range []core.FederationName{"fed", "other"} {
		_ = s.Manager.EnableRegulation(ctx, fed, 1, 0.5)
		_ = s.Manager.EnableAsynchronousDelivery(ctx, fed, 1)
	}
	other, _ := s.Marshal("other")
	commit, err := s.PrepareRestore("fed", []byte{})
	if err != nil {
		t.Fatal(err)
	}
	if data, _ := s.Marshal("fed"); len(data) == 0 {
		t.Fatal("prepare mutated live state")
	}
	commit()
	if data, err := s.Marshal("fed"); err != nil || len(data) != 0 {
		t.Fatalf("empty did not clear: %s, %v", data, err)
	}
	if data, err := s.Marshal("other"); err != nil || !bytes.Equal(data, other) {
		t.Fatalf("other federation changed: %s, %v", data, err)
	}
}
