package savebundle

import (
	"bytes"
	"encoding/json"
	"testing"
)

func timeV5Fixture(t *testing.T, mutate func(map[string]any, map[string]any)) []byte {
	t.Helper()
	wire := map[string]any{
		"version": 5, "halted": false, "stall_timeout_ns": 10000000000,
	}
	federate := map[string]any{
		"handle": 1, "has_regulation_state": true, "constrained": true,
		"has_advance_state": true, "current_time": 2,
		"pending_request": true, "requested_time": 10, "mode": 5,
		"flush_minimum": 4,
	}
	wire["federates"] = []any{federate}
	if mutate != nil {
		mutate(wire, federate)
	}
	data, err := json.Marshal(wire)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestTimeV5FlushMinimumRoundTrip(t *testing.T) {
	s, _, out := newTimeStateCodec(t)
	data := timeV5Fixture(t, nil)
	if err := s.Unmarshal("fed", data); err != nil {
		t.Fatal(err)
	}
	round, err := s.Marshal("fed")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(round, []byte(`"version":5`)) || !bytes.Contains(round, []byte(`"flush_minimum":4`)) {
		t.Fatalf("lost FQR prefix state: %s", round)
	}
	if len(out.sent) != 0 {
		t.Fatal("restore emitted callbacks")
	}
	if err := s.Unmarshal("fed", round); err != nil {
		t.Fatal(err)
	}
	last, err := s.Marshal("fed")
	if err != nil || !bytes.Equal(round, last) {
		t.Fatalf("unstable round trip: %s, %v", last, err)
	}
}

func TestTimeV5RejectsMalformedBeforeMutation(t *testing.T) {
	cases := map[string]func(map[string]any, map[string]any){
		"before current": func(_ map[string]any, f map[string]any) { f["flush_minimum"] = 1 },
		"negative":       func(_ map[string]any, f map[string]any) { f["flush_minimum"] = -1 },
		"null":           func(_ map[string]any, f map[string]any) { f["flush_minimum"] = nil },
		"not FQR":        func(_ map[string]any, f map[string]any) { f["mode"] = 3 },
		"not pending": func(_ map[string]any, f map[string]any) {
			f["pending_request"], f["requested_time"], f["mode"] = false, 0, 0
		},
		"no advance state": func(_ map[string]any, f map[string]any) { f["has_advance_state"] = false },
		"legacy extension": func(w map[string]any, _ map[string]any) { w["version"] = 4 },
		"legacy null extension": func(w map[string]any, f map[string]any) {
			w["version"], f["flush_minimum"] = 4, nil
		},
		"legacy case-folded null extension": func(w map[string]any, f map[string]any) {
			w["version"] = 4
			delete(f, "flush_minimum")
			f["FLUSH_MINIMUM"] = nil
		},
		"v5 without extension": func(_ map[string]any, f map[string]any) { delete(f, "flush_minimum") },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			s, _, _ := newTimeStateCodec(t)
			before, err := s.Marshal("fed")
			if err != nil {
				t.Fatal(err)
			}
			commit, err := s.PrepareRestore("fed", timeV5Fixture(t, mutate))
			if err == nil || commit != nil {
				t.Fatal("malformed state accepted")
			}
			after, err := s.Marshal("fed")
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("failed prepare changed live state")
			}
		})
	}
}

func TestTimeV5PendingEnableRoundTrip(t *testing.T) {
	for _, mode := range []string{"regulation", "constrained", "both"} {
		t.Run(mode, func(t *testing.T) {
			s, _, out := newTimeStateCodec(t)
			data := timeV5Fixture(t, func(_ map[string]any, f map[string]any) {
				delete(f, "flush_minimum")
				f["constrained"], f["pending_request"], f["requested_time"], f["mode"] = false, false, 0, 0
				if mode != "constrained" {
					f["regulation_pending"], f["regulation_lookahead"] = true, 0.5
				}
				if mode != "regulation" {
					f["constrained_pending"] = true
				}
			})
			if err := s.Unmarshal("fed", data); err != nil {
				t.Fatal(err)
			}
			round, err := s.Marshal("fed")
			if err != nil {
				t.Fatal(err)
			}
			for _, field := range []string{"regulation_pending", "regulation_lookahead", "constrained_pending"} {
				var original, restored map[string]any
				_ = json.Unmarshal(data, &original)
				_ = json.Unmarshal(round, &restored)
				a := original["federates"].([]any)[0].(map[string]any)[field]
				b := restored["federates"].([]any)[0].(map[string]any)[field]
				if a != b {
					t.Fatalf("%s changed: %v -> %v", field, a, b)
				}
			}
			if len(out.sent) != 0 {
				t.Fatal("restore emitted pending-enable callback")
			}
		})
	}
}
