package time

import (
	"context"
	"errors"
	"math"
	"reflect"
	"sync"
	"testing"
	stdtime "time"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

type persistentTestEvent struct {
	sequence uint64
	data     []byte
}

func (e *persistentTestEvent) Seq() uint64 { return e.sequence }

func clonePersistentTestEvent(event core.OutboundEvent) (core.OutboundEvent, error) {
	e, ok := event.(*persistentTestEvent)
	if !ok || e == nil {
		return nil, errors.New("unsupported event")
	}
	return &persistentTestEvent{e.sequence, append([]byte(nil), e.data...)}, nil
}

func persistentTestManager(t *testing.T) (*Manager, *core.ManualClock) {
	t.Helper()
	clock := core.NewManualClock(stdtime.Unix(1234, 0))
	m, err := New(Options{Clock: clock, Outbox: nopOutbox{}, StallTimeout: 10 * stdtime.Second})
	if err != nil {
		t.Fatal(err)
	}
	return m, clock
}

func TestPersistentTimeStateRoundTripAndIsolation(t *testing.T) {
	m, clock := persistentTestManager(t)
	ctx := context.Background()
	for _, fed := range []core.FederationName{"fed", "other"} {
		if err := m.EnableRegulation(ctx, fed, 9, 0.5); err != nil {
			t.Fatal(err)
		}
		if err := m.EnableConstrained(ctx, fed, 2); err != nil {
			t.Fatal(err)
		}
		if err := m.TimeAdvanceRequest(ctx, fed, 2, core.PositiveInfinity); err != nil {
			t.Fatal(err)
		}
	}
	if err := m.EnableAsynchronousDelivery(ctx, "fed", 9); err != nil {
		t.Fatal(err)
	}
	if err := m.BufferTSOWithRetraction(ctx, "fed", 2, 6, &persistentTestEvent{23, []byte{0, 255}}, 9, 77); err != nil {
		t.Fatal(err)
	}
	clock.Advance(3 * stdtime.Second)
	state, err := m.ExportState("fed", clonePersistentTestEvent)
	if err != nil {
		t.Fatal(err)
	}
	other, err := m.ExportState("other", clonePersistentTestEvent)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Federates) != 2 || state.Federates[0].Handle != 2 || state.Federates[1].Handle != 9 {
		t.Fatalf("not sorted: %+v", state)
	}
	entry := state.Federates[0]
	if !entry.Constrained || !entry.PendingRequest || entry.Mode != ModeTAR || entry.PendingAge != 3*stdtime.Second || !math.IsInf(float64(entry.RequestedTime), 1) {
		t.Fatalf("lost request: %+v", entry)
	}
	if !state.Federates[1].Regulating || !state.Federates[1].AsynchronousDelivery || state.Federates[1].Lookahead != 0.5 {
		t.Fatalf("lost regulation: %+v", state.Federates[1])
	}
	if entry.Queue[0].Recipient != 2 || entry.Queue[0].Timestamp != 6 || entry.Queue[0].Sender != 9 || entry.Queue[0].RetractionHandle != 77 {
		t.Fatalf("lost queue: %+v", entry.Queue)
	}

	commit, err := m.PrepareImportState("fed", state, clonePersistentTestEvent)
	if err != nil {
		t.Fatal(err)
	}
	state.Federates[0].Queue[0].Event.(*persistentTestEvent).data[0] = 42
	state.Federates[1].Lookahead = 99
	if err := m.ImportState("fed", nil, clonePersistentTestEvent); err != nil {
		t.Fatal(err)
	}
	clock.Advance(stdtime.Hour)
	commit()
	restored, err := m.ExportState("fed", clonePersistentTestEvent)
	if err != nil {
		t.Fatal(err)
	}
	if restored.Federates[0].PendingAge != 3*stdtime.Second || restored.Federates[1].Lookahead != 0.5 || restored.Federates[0].Queue[0].Event.(*persistentTestEvent).data[0] != 0 {
		t.Fatalf("restore aliased input or counted downtime: %+v", restored)
	}
	if got := m.RetractMessage("fed", 9, 77); got != 1 {
		t.Fatalf("retraction removed %d", got)
	}
	if restored.Federates[0].Queue[0].Event.(*persistentTestEvent).data[0] != 0 {
		t.Fatal("export aliased live queue")
	}
	// Compare the other federation at the same clock epoch, excluding only its
	// naturally advancing pending age.
	gotOther, err := m.ExportState("other", clonePersistentTestEvent)
	if err != nil {
		t.Fatal(err)
	}
	gotOther.Federates[0].PendingAge = other.Federates[0].PendingAge
	if !reflect.DeepEqual(gotOther, other) {
		t.Fatalf("other federation changed: %+v", gotOther)
	}
}

func TestPersistentTimeStateStallBudgetAndHalt(t *testing.T) {
	m, clock := persistentTestManager(t)
	ctx := context.Background()
	_ = m.EnableRegulation(ctx, "fed", 1, 1)
	_ = m.EnableConstrained(ctx, "fed", 2)
	_ = m.TimeAdvanceRequest(ctx, "fed", 2, 20)
	clock.Advance(3 * stdtime.Second)
	state, err := m.ExportState("fed", clonePersistentTestEvent)
	if err != nil {
		t.Fatal(err)
	}
	state.StallTimeout = 8 * stdtime.Second
	clock.Advance(stdtime.Hour)
	if err := m.ImportState("fed", state, clonePersistentTestEvent); err != nil {
		t.Fatal(err)
	}
	clock.Advance(4 * stdtime.Second)
	if m.CheckStalls(ctx) != 0 {
		t.Fatal("restore consumed saved stall budget")
	}
	clock.Advance(stdtime.Second)
	if m.CheckStalls(ctx) != 1 {
		t.Fatal("restored stall timeout was lost")
	}
	halted, err := m.ExportState("fed", clonePersistentTestEvent)
	if err != nil || !halted.Halted {
		t.Fatalf("halt snapshot: %+v, %v", halted, err)
	}
	if err := m.ImportState("fed", nil, clonePersistentTestEvent); err != nil {
		t.Fatal(err)
	}
	if err := m.ImportState("fed", halted, clonePersistentTestEvent); err != nil {
		t.Fatal(err)
	}
	if err := m.EnableConstrained(ctx, "fed", 3); !errors.Is(err, core.ErrFederationHalted) {
		t.Fatalf("halt not restored: %v", err)
	}
	if err := m.ImportState("fed", nil, clonePersistentTestEvent); err != nil {
		t.Fatal(err)
	}
	if state, err := m.ExportState("fed", clonePersistentTestEvent); err != nil || state != nil {
		t.Fatalf("empty did not clear: %+v, %v", state, err)
	}
}

func TestPersistentTimeStateAllModesAndInfinity(t *testing.T) {
	for _, mode := range []AdvanceMode{ModeNER, ModeNMRA, ModeTAR, ModeTARA, ModeFQR} {
		t.Run(mode.String(), func(t *testing.T) {
			m, _ := persistentTestManager(t)
			state := &ManagerState{StallTimeout: stdtime.Second, Federates: []FederateState{{Handle: 1, HasAdvanceState: true, CurrentTime: core.PositiveInfinity, PendingRequest: true, RequestedTime: core.PositiveInfinity, Mode: mode, HasPendingSince: true}}}
			if err := m.ImportState("fed", state, clonePersistentTestEvent); err != nil {
				t.Fatal(err)
			}
			got, err := m.ExportState("fed", clonePersistentTestEvent)
			if err != nil || !reflect.DeepEqual(got, state) {
				t.Fatalf("round trip: %+v, %v", got, err)
			}
		})
	}
}

func TestPersistentTimeStateValidationIsAtomic(t *testing.T) {
	cases := map[string]func(*ManagerState){
		"zero handle":       func(s *ManagerState) { s.Federates[0].Handle = 0 },
		"duplicate":         func(s *ManagerState) { s.Federates = append(s.Federates, s.Federates[0]) },
		"lookahead":         func(s *ManagerState) { s.Federates[0].Lookahead = core.PositiveInfinity },
		"nan":               func(s *ManagerState) { s.Federates[0].CurrentTime = core.LogicalTime(math.NaN()) },
		"negative infinity": func(s *ManagerState) { s.Federates[0].CurrentTime = core.LogicalTime(math.Inf(-1)) },
		"mode":              func(s *ManagerState) { s.Federates[0].Mode = 99 },
		"negative age":      func(s *ManagerState) { s.Federates[0].PendingAge = -1 },
		"negative timeout":  func(s *ManagerState) { s.StallTimeout = -1 },
		"destination": func(s *ManagerState) {
			s.Federates[0].Queue = []core.TSOBufferedDelivery{{Recipient: 2, Timestamp: 1, Event: &persistentTestEvent{}}}
		},
		"unknown event": func(s *ManagerState) {
			s.Federates[0].Queue = []core.TSOBufferedDelivery{{Recipient: 1, Timestamp: 1, Event: &TimeAdvanceGrant{}}}
		},
	}
	for name, corrupt := range cases {
		t.Run(name, func(t *testing.T) {
			m, _ := persistentTestManager(t)
			_ = m.EnableRegulation(context.Background(), "fed", 8, 0.5)
			before, _ := m.ExportState("fed", clonePersistentTestEvent)
			state := &ManagerState{StallTimeout: stdtime.Second, Federates: []FederateState{{Handle: 1, HasRegulationState: true, Regulating: true, HasAdvanceState: true}}}
			corrupt(state)
			commit, err := m.PrepareImportState("fed", state, clonePersistentTestEvent)
			if err == nil || commit != nil {
				t.Fatalf("invalid state accepted: %v", err)
			}
			after, _ := m.ExportState("fed", clonePersistentTestEvent)
			if !reflect.DeepEqual(before, after) {
				t.Fatal("validation mutated live state")
			}
		})
	}
}

func TestPersistentTimeStateSerializesWithEvaluator(t *testing.T) {
	for _, operation := range []string{"export", "commit"} {
		t.Run(operation, func(t *testing.T) {
			m, _ := persistentTestManager(t)
			commit, err := m.PrepareImportState("fed", nil, clonePersistentTestEvent)
			if err != nil {
				t.Fatal(err)
			}
			lock := extOf(m).evaluatorLock("fed")
			lock.Lock()
			done := make(chan error, 1)
			go func() {
				if operation == "export" {
					_, err := m.ExportState("fed", clonePersistentTestEvent)
					done <- err
				} else {
					commit()
					done <- nil
				}
			}()
			select {
			case err := <-done:
				lock.Unlock()
				t.Fatalf("bypassed evaluator lease: %v", err)
			case <-stdtime.After(20 * stdtime.Millisecond):
			}
			lock.Unlock()
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-stdtime.After(2 * stdtime.Second):
				t.Fatal("state operation deadlocked")
			}
			if extOf(m).evaluatorLock("fed") != lock {
				t.Fatal("restore replaced evaluator lock")
			}
		})
	}
}

func TestPersistentTimeStateConcurrentCopiesAndIdempotentCommit(t *testing.T) {
	m, _ := persistentTestManager(t)
	state := &ManagerState{StallTimeout: stdtime.Second, Federates: []FederateState{{Handle: 1, HasRegulationState: true, Regulating: true, Lookahead: 1}}}
	commit, err := m.PrepareImportState("fed", state, clonePersistentTestEvent)
	if err != nil {
		t.Fatal(err)
	}
	commit()
	if err := m.ModifyLookahead(context.Background(), "fed", 1, 2); err != nil {
		t.Fatal(err)
	}
	commit()
	got, err := m.ExportState("fed", clonePersistentTestEvent)
	if err != nil || got.Federates[0].Lookahead != 2 {
		t.Fatalf("commit applied twice: %+v, %v", got, err)
	}

	var workers sync.WaitGroup
	for i := 0; i < 4; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for j := 0; j < 30; j++ {
				if err := m.ImportState("fed", state, clonePersistentTestEvent); err != nil {
					t.Error(err)
					return
				}
				if _, err := m.ExportState("fed", clonePersistentTestEvent); err != nil {
					t.Error(err)
					return
				}
				m.ShouldDeliverNow("fed", 1, 3)
				m.QueryLITS("fed", 1)
				m.CheckStalls(context.Background())
			}
		}()
	}
	workers.Wait()
	_ = m.EnableRegulation(context.Background(), "other", 2, 1)
	other, _ := m.ExportState("other", clonePersistentTestEvent)
	if other.StallTimeout != 10*stdtime.Second {
		t.Fatal("restored timeout leaked to another federation")
	}
	m.OnFederationDestroyed("fed")
	_ = m.EnableRegulation(context.Background(), "fed", 1, 1)
	got, err = m.ExportState("fed", clonePersistentTestEvent)
	if err != nil || got.StallTimeout != 10*stdtime.Second {
		t.Fatalf("destroy retained timeout override: %+v, %v", got, err)
	}
}
