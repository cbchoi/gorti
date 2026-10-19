package savebundle

import (
	"bytes"
	"context"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/object"
	timepkg "github.com/cbchoi/gorti/rti/internal/hla/time"
)

func TestTimeV5ReceivedTSOFrontierRoundTrip(t *testing.T) {
	for _, mode := range []string{"granted", "ordinary advance", "implicit enable"} {
		t.Run(mode, func(t *testing.T) {
			s, _, out := newTimeStateCodec(t)
			data := timeV5Fixture(t, func(_ map[string]any, f map[string]any) {
				delete(f, "flush_minimum")
				f["received_tso_time"] = 4
				f["pending_request"], f["requested_time"], f["mode"] = false, 0, 0
				switch mode {
				case "granted":
					f["current_time"] = 4
				case "ordinary advance":
					f["pending_request"], f["requested_time"], f["mode"] = true, 10, 3
				case "implicit enable":
					f["regulation_pending"], f["regulation_lookahead"] = true, 1
				}
			})
			if err := s.Unmarshal("fed", data); err != nil {
				t.Fatal(err)
			}
			state, err := s.Manager.ExportState("fed", nil)
			if err != nil || state.Federates[0].ReceivedTSOTime == nil || *state.Federates[0].ReceivedTSOTime != 4 {
				t.Fatalf("frontier lost: %+v %v", state, err)
			}
			*state.Federates[0].ReceivedTSOTime = 99
			round, err := s.Marshal("fed")
			if err != nil || !bytes.Contains(round, []byte(`"received_tso_time":4`)) {
				t.Fatalf("frontier export aliases runtime: %s %v", round, err)
			}
			if err := s.Unmarshal("fed", round); err != nil {
				t.Fatal(err)
			}
			if len(out.sent) != 0 {
				t.Fatal("restore emitted callbacks")
			}
		})
	}
}

func TestTimeV5ReceivedTSOFrontierRejectsMalformed(t *testing.T) {
	for name, mutate := range map[string]func(map[string]any, map[string]any){
		"speculative FQR":     func(_ map[string]any, _ map[string]any) {},
		"not constrained":     func(_ map[string]any, f map[string]any) { f["constrained"] = false },
		"no advance state":    func(_ map[string]any, f map[string]any) { f["has_advance_state"] = false },
		"no regulation state": func(_ map[string]any, f map[string]any) { f["has_regulation_state"] = false },
		"beyond request": func(_ map[string]any, f map[string]any) {
			f["mode"], f["received_tso_time"] = 3, 11
			delete(f, "flush_minimum")
		},
		"future while granted": func(_ map[string]any, f map[string]any) {
			f["mode"], f["pending_request"], f["requested_time"] = 0, false, 0
			delete(f, "flush_minimum")
		},
		"negative":     func(_ map[string]any, f map[string]any) { f["received_tso_time"] = -1 },
		"null":         func(_ map[string]any, f map[string]any) { f["received_tso_time"] = nil },
		"legacy field": func(w map[string]any, f map[string]any) { w["version"] = 4; delete(f, "flush_minimum") },
	} {
		t.Run(name, func(t *testing.T) {
			s, _, _ := newTimeStateCodec(t)
			data := timeV5Fixture(t, func(w, f map[string]any) { f["received_tso_time"] = 4; mutate(w, f) })
			if commit, err := s.PrepareRestore("fed", data); err == nil || commit != nil {
				t.Fatal("malformed delivery frontier accepted")
			}
		})
	}
}

func TestTimeV5RestoredFQRPrefixDoesNotRaiseGrant(t *testing.T) {
	s, _, out := newTimeStateCodec(t)
	minimum, timestamp := core.LogicalTime(4), core.LogicalTime(8)
	state := &timepkg.ManagerState{StallTimeout: 15000000000, Federates: []timepkg.FederateState{{
		Handle: 1, HasRegulationState: true, Constrained: true, HasAdvanceState: true,
		CurrentTime: 2, PendingRequest: true, RequestedTime: 10, Mode: timepkg.ModeFQR, FlushMinimum: &minimum,
		Queue: []core.TSOBufferedDelivery{{Recipient: 1, Timestamp: timestamp, Sender: 2, Event: &object.CallbackEvent{Kind: object.CallbackReceive, InteractionClass: 1, Sequence: 2, LogicalTime: &timestamp}}},
	}}}
	if err := s.Manager.ImportState("fed", state, cloneTimeEvent); err != nil {
		t.Fatal(err)
	}
	data, err := s.Marshal("fed")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Unmarshal("fed", data); err != nil {
		t.Fatal(err)
	}
	if err := s.Manager.ResumeScheduledSave(context.Background(), "fed"); err != nil {
		t.Fatal(err)
	}
	if len(out.sent) != 2 {
		t.Fatalf("expected remaining message then grant, got %v", out.sent)
	}
	if e, ok := out.sent[0].event.(*object.CallbackEvent); !ok || e.Sequence != 2 {
		t.Fatal("remaining FQR message was not delivered")
	}
	if e, ok := out.sent[1].event.(*timepkg.TimeAdvanceGrant); !ok || e.Time != 4 {
		t.Fatalf("lost already delivered minimum: %+v", out.sent[1].event)
	}
	if err := s.Manager.ResumeScheduledSave(context.Background(), "fed"); err != nil {
		t.Fatal(err)
	}
	if len(out.sent) != 2 {
		t.Fatal("restored retry duplicated callback")
	}
}

func TestTimeV5RestoredEnableUsesDeliveredFrontier(t *testing.T) {
	s, _, out := newTimeStateCodec(t)
	data := timeV5Fixture(t, func(_ map[string]any, f map[string]any) {
		delete(f, "flush_minimum")
		f["pending_request"], f["requested_time"], f["mode"] = false, 0, 0
		f["regulation_pending"], f["regulation_lookahead"], f["received_tso_time"] = true, 1, 4
	})
	if err := s.Unmarshal("fed", data); err != nil {
		t.Fatal(err)
	}
	if err := s.Manager.ResumeScheduledSave(context.Background(), "fed"); err != nil {
		t.Fatal(err)
	}
	if len(out.sent) != 1 {
		t.Fatalf("activation callbacks: %v", out.sent)
	}
	if e, ok := out.sent[0].event.(*timepkg.TimeRegulationEnabled); !ok || e.Time != 4 {
		t.Fatalf("activation crossed restored delivery frontier: %+v", out.sent[0].event)
	}
	if err := s.Manager.ResumeScheduledSave(context.Background(), "fed"); err != nil {
		t.Fatal(err)
	}
	if len(out.sent) != 1 {
		t.Fatal("restored enable emitted twice")
	}
}

func TestTimeV5RestoredNextMessageDeliveredMinimum(t *testing.T) {
	for _, mode := range []timepkg.AdvanceMode{timepkg.ModeNER, timepkg.ModeNMRA} {
		t.Run(stringMustJSON(t, mode), func(t *testing.T) {
			s, _, out := newTimeStateCodec(t)
			minimum, later := core.LogicalTime(4), core.LogicalTime(8)
			state := &timepkg.ManagerState{StallTimeout: 15000000000, Federates: []timepkg.FederateState{{
				Handle: 1, HasRegulationState: true, Constrained: true, HasAdvanceState: true,
				CurrentTime: 2, PendingRequest: true, RequestedTime: 10, Mode: mode,
				FlushMinimum: &minimum, ReceivedTSOTime: &minimum,
				Queue: []core.TSOBufferedDelivery{{Recipient: 1, Timestamp: later, Sender: 2, Event: &object.CallbackEvent{Kind: object.CallbackReceive, InteractionClass: 1, Sequence: 2, LogicalTime: &later}}},
			}}}
			if err := s.Manager.ImportState("fed", state, cloneTimeEvent); err != nil {
				t.Fatal(err)
			}
			data, err := s.Marshal("fed")
			if err != nil {
				t.Fatal(err)
			}
			if err := s.Unmarshal("fed", data); err != nil {
				t.Fatal(err)
			}
			if len(out.sent) != 0 {
				t.Fatal("restore replayed the already delivered prefix")
			}
			if err := s.Manager.ResumeScheduledSave(context.Background(), "fed"); err != nil {
				t.Fatal(err)
			}
			if len(out.sent) != 1 {
				t.Fatalf("only retained-target grant expected: %v", out.sent)
			}
			if grant, ok := out.sent[0].event.(*timepkg.TimeAdvanceGrant); !ok || grant.Time != 4 {
				t.Fatalf("lost delivered next-message target: %+v", out.sent[0].event)
			}
			got, err := s.Manager.ExportState("fed", cloneTimeEvent)
			if err != nil || len(got.Federates[0].Queue) != 1 || got.Federates[0].Queue[0].Timestamp != 8 || got.Federates[0].FlushMinimum != nil {
				t.Fatalf("remaining queue/history changed: %+v %v", got, err)
			}
			if err := s.Manager.ResumeScheduledSave(context.Background(), "fed"); err != nil {
				t.Fatal(err)
			}
			if len(out.sent) != 1 {
				t.Fatal("resume duplicated next-message grant")
			}
		})
	}
}
