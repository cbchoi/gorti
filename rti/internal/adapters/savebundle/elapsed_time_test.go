package savebundle

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"
	stdtime "time"

	timepkg "github.com/cbchoi/gorti/rti/internal/hla/time"
)

func elapsedCodecMeasurement(t *testing.T, s Time, granted, advancing stdtime.Duration) timepkg.ElapsedMeasurement {
	t.Helper()
	x, err := s.Manager.MeasureElapsed("fed", 1)
	if err != nil {
		t.Fatal(err)
	}
	if x.Granted != granted || x.Advancing != advancing {
		t.Fatalf("elapsed after codec: granted=%v advancing=%v; want %v, %v", x.Granted, x.Advancing, granted, advancing)
	}
	return x
}

func TestElapsedTimeCodecRoundTripPrecisionBaselinesAndRestart(t *testing.T) {
	s, clock, out := newTimeStateCodec(t)
	ctx := context.Background()
	if err := s.Manager.OnFederateJoined("fed", 1); err != nil {
		t.Fatal(err)
	}
	clock.Advance(1750001)
	x := elapsedCodecMeasurement(t, s, 1750001, 0)
	if err := s.Manager.AcknowledgeElapsed("fed", 1, x, timepkg.ElapsedAttributes{Granted: true}); err != nil {
		t.Fatal(err)
	}
	if err := s.Manager.EnableConstrained(ctx, "fed", 1); err != nil {
		t.Fatal(err)
	}
	if err := s.Manager.EnableRegulation(ctx, "fed", 2, 0); err != nil {
		t.Fatal(err)
	}
	if len(out.sent) != 2 {
		t.Fatalf("activation callbacks: %v", out.sent)
	}
	if _, ok := out.sent[0].event.(*timepkg.TimeConstrainedEnabled); !ok {
		t.Fatalf("unexpected constraint callback: %T", out.sent[0].event)
	}
	if _, ok := out.sent[1].event.(*timepkg.TimeRegulationEnabled); !ok {
		t.Fatalf("unexpected regulation callback: %T", out.sent[1].event)
	}
	out.sent = nil
	if err := s.Manager.TimeAdvanceRequest(ctx, "fed", 1, 10); err != nil {
		t.Fatal(err)
	}
	clock.Advance(2500002)
	x = elapsedCodecMeasurement(t, s, 750001, 2500002)
	if err := s.Manager.AcknowledgeElapsed("fed", 1, x, timepkg.ElapsedAttributes{Advancing: true}); err != nil {
		t.Fatal(err)
	}
	clock.Advance(600000)
	stale := elapsedCodecMeasurement(t, s, 750001, 1100002)
	data, err := s.Marshal("fed")
	if err != nil {
		t.Fatal(err)
	}
	var wire timeSnapshot
	if err := json.Unmarshal(data, &wire); err != nil {
		t.Fatal(err)
	}
	history := wire.Federates[0].Elapsed
	if history == nil || *history != (timepkg.ElapsedTimeState{Granted: 1750001, Advancing: 3100002, GrantedPublished: 1000000, AdvancingPublished: 2000000}) {
		t.Fatalf("codec lost independent nanosecond durations/baselines: %+v", history)
	}
	var document struct {
		Federates []struct {
			Elapsed map[string]json.RawMessage `json:"elapsed"`
		} `json:"federates"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	fields := document.Federates[0].Elapsed
	if len(fields) != 5 {
		t.Fatalf("elapsed wire contains non-duration process metadata: %v", fields)
	}
	for key, want := range map[string]string{"Granted": "1750001", "Advancing": "3100002", "GrantedPublished": "1000000", "AdvancingPublished": "2000000", "Invalid": "false"} {
		if string(fields[key]) != want {
			t.Fatalf("%s=%s want %s", key, fields[key], want)
		}
	}
	commit, err := s.PrepareRestore("fed", data)
	if err != nil {
		t.Fatal(err)
	}
	clock.Advance(24 * stdtime.Hour)
	commit()
	commit()
	if err := s.Manager.AcknowledgeElapsed("fed", 1, stale, timepkg.ElapsedAttributes{Granted: true, Advancing: true}); !errors.Is(err, timepkg.ErrElapsedStaleMeasurement) {
		t.Fatal(err)
	}
	elapsedCodecMeasurement(t, s, 750001, 1100002)
	round, err := s.Marshal("fed")
	if err != nil || !bytes.Equal(data, round) {
		t.Fatalf("codec restore changed durations: %s %v", round, err)
	}
	if len(out.sent) != 0 {
		t.Fatal("restore replayed a grant callback")
	}
	restarted, restartClock, _ := newTimeStateCodec(t)
	restartClock.Set(stdtime.Unix(8000000, 0))
	if err := restarted.Unmarshal("fed", data); err != nil {
		t.Fatal(err)
	}
	restartClock.Advance(stdtime.Millisecond)
	elapsedCodecMeasurement(t, restarted, 750001, 2100002)
}

func mutateElapsedWire(t *testing.T, data []byte, mutate func(map[string]json.RawMessage)) []byte {
	t.Helper()
	var document map[string]json.RawMessage
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	var federates []map[string]json.RawMessage
	if err := json.Unmarshal(document["federates"], &federates); err != nil {
		t.Fatal(err)
	}
	mutate(federates[0])
	encoded, err := json.Marshal(federates)
	if err != nil {
		t.Fatal(err)
	}
	document["federates"] = encoded
	encoded, err = json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func TestElapsedTimeCodecLegacyMissingHistoryRemainsUnavailable(t *testing.T) {
	s, clock, _ := newTimeStateCodec(t)
	if err := s.Manager.OnFederateJoined("fed", 1); err != nil {
		t.Fatal(err)
	}
	clock.Advance(stdtime.Second)
	data, err := s.Marshal("fed")
	if err != nil {
		t.Fatal(err)
	}
	legacy := mutateElapsedWire(t, data, func(f map[string]json.RawMessage) { delete(f, "elapsed") })
	if err := s.Unmarshal("fed", legacy); err != nil {
		t.Fatal(err)
	}
	clock.Advance(stdtime.Hour)
	if _, err := s.Manager.MeasureElapsed("fed", 1); !errors.Is(err, timepkg.ErrElapsedCoverage) {
		t.Fatal("legacy fabricated since-join history", err)
	}
	if err := s.Manager.OnFederateJoined("fed", 1); !errors.Is(err, timepkg.ErrElapsedCoverage) {
		t.Fatal("legacy history was lazily initialized", err)
	}
	round, err := s.Marshal("fed")
	if err != nil {
		t.Fatal(err)
	}
	var wire timeSnapshot
	if err := json.Unmarshal(round, &wire); err != nil {
		t.Fatal(err)
	}
	if wire.Federates[0].Elapsed != nil {
		t.Fatal("legacy history fabricated during export")
	}
}

func TestElapsedTimeCodecRejectsInvalidHistoryWithoutMutation(t *testing.T) {
	s, clock, _ := newTimeStateCodec(t)
	if err := s.Manager.OnFederateJoined("fed", 1); err != nil {
		t.Fatal(err)
	}
	clock.Advance(2500001)
	valid, err := s.Marshal("fed")
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{
		"negative granted":          `{"Granted":-1}`,
		"negative advancing":        `{"Advancing":-1}`,
		"negative baseline":         `{"GrantedPublished":-1000000}`,
		"future baseline":           `{"Granted":1000000,"GrantedPublished":2000000}`,
		"future advancing baseline": `{"Advancing":1000000,"AdvancingPublished":2000000}`,
		"fractional baseline":       `{"Granted":2000000,"GrantedPublished":1}`,
		"duration overflow":         `{"Granted":9223372036854775808}`,
		"fractional nanosecond":     `{"Granted":1.5}`,
		"invalid failure type":      `{"Invalid":"true"}`,
		"process timestamp":         `{"Granted":1000000,"last":"2026-09-20"}`,
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			invalid := mutateElapsedWire(t, valid, func(f map[string]json.RawMessage) { f["elapsed"] = json.RawMessage(raw) })
			if commit, err := s.PrepareRestore("fed", invalid); err == nil || commit != nil {
				t.Fatalf("invalid elapsed state accepted: %s", invalid)
			}
			current, err := s.Marshal("fed")
			if err != nil || !bytes.Equal(current, valid) {
				t.Fatal("invalid prepare mutated live state", err)
			}
		})
	}
}

func TestElapsedTimeCodecCoverageLossSurvivesRestart(t *testing.T) {
	s, clock, _ := newTimeStateCodec(t)
	if err := s.Manager.OnFederateJoined("fed", 1); err != nil {
		t.Fatal(err)
	}
	clock.Advance(stdtime.Second)
	elapsedCodecMeasurement(t, s, stdtime.Second, 0)
	clock.Advance(-2 * stdtime.Second)
	if _, err := s.Manager.MeasureElapsed("fed", 1); !errors.Is(err, timepkg.ErrElapsedCoverage) {
		t.Fatal(err)
	}
	data, err := s.Marshal("fed")
	if err != nil {
		t.Fatal(err)
	}
	restarted, _, _ := newTimeStateCodec(t)
	if err := restarted.Unmarshal("fed", data); err != nil {
		t.Fatal(err)
	}
	if _, err := restarted.Manager.MeasureElapsed("fed", 1); !errors.Is(err, timepkg.ErrElapsedCoverage) {
		t.Fatal("coverage loss silently recovered", err)
	}
	round, err := restarted.Marshal("fed")
	if err != nil || !bytes.Equal(data, round) {
		t.Fatal("coverage marker lost", err)
	}
}
