package savebundle

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func TestTimeStateCodecZeroLookaheadHistory(t *testing.T) {
	ctx := context.Background()
	s, _, _ := newTimeStateCodec(t)
	if err := s.Manager.EnableRegulation(ctx, "fed", 1, 0); err != nil {
		t.Fatal(err)
	}
	if err := s.Manager.TimeAdvanceRequest(ctx, "fed", 1, 10); err != nil {
		t.Fatal(err)
	}
	if got := s.Manager.Metrics("fed", 1); got.Advancing || got.CurrentTime != 10 {
		t.Fatalf("strict grant missing: %+v", got)
	}
	if err := s.Manager.TimeAdvanceRequestAvailable(ctx, "fed", 1, 10); err != nil {
		t.Fatal(err)
	}
	data, err := s.Marshal("fed")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte(`"version":3`)) || !bytes.Contains(data, []byte(`"strict_zero_lookahead":true`)) {
		t.Fatalf("strict history missing: %s", data)
	}
	restored, _, _ := newTimeStateCodec(t)
	if err := restored.Unmarshal("fed", data); err != nil {
		t.Fatal(err)
	}
	if err := restored.Manager.ValidateOutgoingTSO("fed", 1, 10); !errors.Is(err, core.ErrTimeInvalidLogicalTime) {
		t.Fatalf("restored equality accepted: %v", err)
	}
	if err := restored.Manager.ValidateOutgoingTSO("fed", 1, core.LogicalTime(math.Nextafter(10, math.Inf(1)))); err != nil {
		t.Fatal(err)
	}
	if err := restored.Manager.TimeAdvanceRequestAvailable(ctx, "fed", 1, 11); err != nil {
		t.Fatal(err)
	}
	if err := restored.Manager.ValidateOutgoingTSO("fed", 1, 11); err != nil {
		t.Fatalf("forward available did not release history: %v", err)
	}
	if err := restored.Unmarshal("fed", data); err != nil {
		t.Fatal(err)
	}
	for _, version := range []byte{'1', '2'} {
		bad := bytes.Replace(data, []byte(`"version":3`), append([]byte(`"version":`), version), 1)
		commit, err := restored.PrepareRestore("fed", bad)
		if err == nil || commit != nil {
			t.Fatalf("old version accepted strict history: %s", bad)
		}
		after, err := restored.Marshal("fed")
		if err != nil || !bytes.Equal(data, after) {
			t.Fatal("invalid restore changed live state")
		}
	}
}

func TestTimeStateCodecZeroLookaheadPendingAndLegacy(t *testing.T) {
	ctx := context.Background()
	s, _, _ := newTimeStateCodec(t)
	for _, h := range []core.FederateHandle{1, 2} {
		if err := s.Manager.EnableRegulation(ctx, "fed", h, 0); err != nil {
			t.Fatal(err)
		}
		if err := s.Manager.EnableConstrained(ctx, "fed", h); err != nil {
			t.Fatal(err)
		}
	}
	legacy, err := s.Marshal("fed")
	if err != nil || !bytes.Contains(legacy, []byte(`"version":1`)) {
		t.Fatalf("legacy encoding: %s, %v", legacy, err)
	}
	if err := s.Manager.NextMessageRequest(ctx, "fed", 1, 10); err != nil {
		t.Fatal(err)
	}
	if !s.Manager.Metrics("fed", 1).Advancing {
		t.Fatal("peer must hold request")
	}
	pending, err := s.Marshal("fed")
	if err != nil {
		t.Fatal(err)
	}
	restored, _, _ := newTimeStateCodec(t)
	if err := restored.Unmarshal("fed", pending); err != nil {
		t.Fatal(err)
	}
	if err := restored.Manager.ValidateOutgoingTSO("fed", 1, 10); !errors.Is(err, core.ErrTimeInvalidLogicalTime) {
		t.Fatalf("restored pending equality: %v", err)
	}
	if err := restored.Unmarshal("fed", legacy); err != nil {
		t.Fatal(err)
	}
	if err := restored.Manager.ValidateOutgoingTSO("fed", 1, 0); err != nil {
		t.Fatalf("legacy acquired strict history: %v", err)
	}
}

func TestTimeStateCodecZeroLookaheadRejectsOrphanHistory(t *testing.T) {
	s, _, _ := newTimeStateCodec(t)
	if err := s.Manager.EnableRegulation(context.Background(), "fed", 1, 0); err != nil {
		t.Fatal(err)
	}
	before, err := s.Marshal("fed")
	if err != nil {
		t.Fatal(err)
	}
	var wire timeSnapshot
	if err := json.Unmarshal(before, &wire); err != nil {
		t.Fatal(err)
	}
	wire.Version = 3
	wire.Federates[0].HasAdvanceState = false
	wire.Federates[0].StrictZeroLookahead = true
	bad, err := json.Marshal(wire)
	if err != nil {
		t.Fatal(err)
	}
	commit, err := s.PrepareRestore("fed", bad)
	if err == nil || commit != nil {
		t.Fatal("strict history without advance state accepted")
	}
	after, err := s.Marshal("fed")
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("invalid restore changed live state")
	}
}

func TestTimeStateCodecZeroLookaheadVersionAndRebinding(t *testing.T) {
	s, _, _ := newTimeStateCodec(t)
	ctx := context.Background()
	if err := s.Manager.EnableRegulation(ctx, "fed", 1, 0); err != nil {
		t.Fatal(err)
	}
	if err := s.Manager.TimeAdvanceRequest(ctx, "fed", 1, 10); err != nil {
		t.Fatal(err)
	}
	if err := s.Manager.EnableRegulation(ctx, "fed", 2, 5); err != nil {
		t.Fatal(err)
	}
	if err := s.Manager.ModifyLookahead(ctx, "fed", 2, 0); err != nil {
		t.Fatal(err)
	}
	data, err := s.Marshal("fed")
	if err != nil {
		t.Fatal(err)
	}
	var wire timeSnapshot
	if err := json.Unmarshal(data, &wire); err != nil {
		t.Fatal(err)
	}
	if wire.Version != 3 {
		t.Fatalf("later lookahead reduction downgraded version: %d", wire.Version)
	}
	rebound, err := s.RewriteFederates(data, func(h core.FederateHandle) (core.FederateHandle, error) { return h + 10, nil })
	if err != nil {
		t.Fatal(err)
	}
	restored, _, _ := newTimeStateCodec(t)
	if err := restored.Unmarshal("fed", rebound); err != nil {
		t.Fatal(err)
	}
	if err := restored.Manager.ValidateOutgoingTSO("fed", 11, 10); !errors.Is(err, core.ErrTimeInvalidLogicalTime) {
		t.Fatalf("rebound strict history missing: %v", err)
	}
	state, err := restored.Manager.ExportState("fed", nil)
	if err != nil {
		t.Fatal(err)
	}
	if state.Federates[1].RequestedLookahead == nil || *state.Federates[1].RequestedLookahead != 0 {
		t.Fatal("rebound lookahead reduction missing")
	}
}
