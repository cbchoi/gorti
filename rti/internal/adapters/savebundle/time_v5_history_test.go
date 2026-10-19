package savebundle

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func TestTimeV5ReceiverStrictHistoryAndLegacyFallback(t *testing.T) {
	for _, tc := range []struct {
		name     string
		version  int
		present  bool
		received any
		want     *core.LogicalTime
	}{
		{"legacy v3", 3, false, nil, receiveTime(2)},
		{"legacy v4", 4, false, nil, receiveTime(2)},
		{"explicit strict", 5, true, 2, receiveTime(2)},
		{"explicit absence", 5, true, nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _, out := newTimeStateCodec(t)
			data := timeV5Fixture(t, func(w, f map[string]any) {
				w["version"] = tc.version
				delete(f, "flush_minimum")
				f["pending_request"], f["requested_time"], f["mode"] = false, 0, 0
				f["strict_zero_lookahead"] = true
				if tc.present {
					f["preserved_receive_time"] = tc.received
				}
			})
			if err := s.Unmarshal("fed", data); err != nil {
				t.Fatal(err)
			}
			state, err := s.Manager.ExportState("fed", nil)
			if err != nil {
				t.Fatal(err)
			}
			got := state.Federates[0].PreservedReceiveTime
			if (got == nil) != (tc.want == nil) || got != nil && *got != *tc.want {
				t.Fatalf("receiver history = %v, want %v", got, tc.want)
			}
			round, err := s.Marshal("fed")
			if err != nil || !bytes.Contains(round, []byte(`"version":5`)) {
				t.Fatalf("new history format: %s %v", round, err)
			}
			if tc.want == nil && !bytes.Contains(round, []byte(`"preserved_receive_time":null`)) {
				t.Fatal("explicit absence lost")
			}
			rebound, err := s.RewriteFederates(round, func(h core.FederateHandle) (core.FederateHandle, error) { return h + 10, nil })
			if err != nil {
				t.Fatal(err)
			}
			if err := s.Unmarshal("fed", rebound); err != nil {
				t.Fatal(err)
			}
			state, err = s.Manager.ExportState("fed", nil)
			if err != nil || state.Federates[0].Handle != 11 {
				t.Fatal("receiver history rebind failed")
			}
			got = state.Federates[0].PreservedReceiveTime
			if (got == nil) != (tc.want == nil) || got != nil && *got != *tc.want {
				t.Fatal("rebind changed receiver history")
			}
			if len(out.sent) != 0 {
				t.Fatal("restoring history emitted callback")
			}
		})
	}
}

func receiveTime(v core.LogicalTime) *core.LogicalTime { return &v }

func TestTimeV5ReceiverStrictHistoryRejectsMalformed(t *testing.T) {
	for name, mutate := range map[string]func(map[string]any, map[string]any){
		"future":                   func(_ map[string]any, f map[string]any) { f["preserved_receive_time"] = 3 },
		"negative":                 func(_ map[string]any, f map[string]any) { f["preserved_receive_time"] = -1 },
		"not constrained":          func(_ map[string]any, f map[string]any) { f["constrained"] = false },
		"no regulation state":      func(_ map[string]any, f map[string]any) { f["has_regulation_state"] = false },
		"no advance state":         func(_ map[string]any, f map[string]any) { f["has_advance_state"] = false },
		"legacy extension":         func(w map[string]any, _ map[string]any) { w["version"] = 4 },
		"ambiguous strict history": func(_ map[string]any, f map[string]any) { delete(f, "preserved_receive_time") },
	} {
		t.Run(name, func(t *testing.T) {
			s, _, _ := newTimeStateCodec(t)
			data := timeV5Fixture(t, func(w, f map[string]any) {
				f["strict_zero_lookahead"] = true
				f["preserved_receive_time"] = 2
				mutate(w, f)
			})
			if commit, err := s.PrepareRestore("fed", data); err == nil || commit != nil {
				t.Fatal("malformed strict history accepted")
			}
		})
	}
}

func TestTimeV5RetainsLegacyVersionSelection(t *testing.T) {
	for version := 1; version <= 4; version++ {
		t.Run(stringMustJSON(t, version), func(t *testing.T) {
			s, _, _ := newTimeStateCodec(t)
			data := timeV5Fixture(t, func(w, f map[string]any) {
				w["version"] = version
				delete(f, "flush_minimum")
				f["pending_request"], f["requested_time"], f["mode"], f["constrained"] = false, 0, 0, false
				if version == 2 {
					f["regulating"] = true
					f["lookahead"] = 1
					f["requested_lookahead"] = 0
					f["lookahead_origin_time"] = 2
					f["lookahead_origin_value"] = 1
				}
				if version == 3 {
					f["strict_zero_lookahead"] = true
				}
			})
			if version == 4 {
				var err error
				data, err = json.Marshal(roSnapshotDocument(t))
				if err != nil {
					t.Fatal(err)
				}
			}
			if err := s.Unmarshal("fed", data); err != nil {
				t.Fatal(err)
			}
			round, err := s.Marshal("fed")
			if err != nil {
				t.Fatal(err)
			}
			var doc map[string]any
			if err := json.Unmarshal(round, &doc); err != nil {
				t.Fatal(err)
			}
			if doc["version"] != float64(version) {
				t.Fatalf("legacy version bumped: %s", round)
			}
		})
	}
}
