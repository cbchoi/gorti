package savebundle

import (
	"bytes"
	"context"
	"math"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func TestTimeStateCodecPreservesLookaheadReduction(t *testing.T) {
	ctx := context.Background()
	s, _, _ := newTimeStateCodec(t)
	if err := s.Manager.EnableRegulation(ctx, "fed", 1, 5); err != nil {
		t.Fatal(err)
	}
	legacy, err := s.Marshal("fed")
	if err != nil || !bytes.Contains(legacy, []byte(`"version":1`)) {
		t.Fatalf("unchanged state must retain V1: %s, %v", legacy, err)
	}
	if err := s.Manager.ModifyLookahead(ctx, "fed", 1, 2); err != nil {
		t.Fatal(err)
	}
	data, err := s.Marshal("fed")
	if err != nil || !bytes.Contains(data, []byte(`"version":2`)) || !bytes.Contains(data, []byte(`"requested_lookahead":2`)) {
		t.Fatalf("reduction missing from V2: %s, %v", data, err)
	}
	restored, _, _ := newTimeStateCodec(t)
	if err := restored.Unmarshal("fed", data); err != nil {
		t.Fatal(err)
	}
	if err := restored.Manager.TimeAdvanceRequest(ctx, "fed", 1, 2); err != nil {
		t.Fatal(err)
	}
	actual := restored.Manager.Metrics("fed", 1).Lookahead
	if actual == nil || *actual != 3 {
		t.Fatalf("restored actual lookahead = %v, want 3", actual)
	}
	if err := restored.Unmarshal("fed", legacy); err != nil {
		t.Fatal(err)
	}
	if err := restored.Manager.TimeAdvanceRequest(ctx, "fed", 1, 2); err != nil {
		t.Fatal(err)
	}
	if actual := restored.Manager.Metrics("fed", 1).Lookahead; actual == nil || *actual != 5 {
		t.Fatal("legacy snapshot incorrectly inferred a decrease")
	}
}

func TestTimeStateCodecPreservesSubULPLookaheadReduction(t *testing.T) {
	ctx := context.Background()
	s, _, _ := newTimeStateCodec(t)
	if err := s.Manager.EnableRegulation(ctx, "fed", 1, 1); err != nil {
		t.Fatal(err)
	}
	if err := s.Manager.ModifyLookahead(ctx, "fed", 1, 0.5); err != nil {
		t.Fatal(err)
	}
	delta := core.LogicalTime(math.Ldexp(1, -54))
	if err := s.Manager.TimeAdvanceRequest(ctx, "fed", 1, delta); err != nil {
		t.Fatal(err)
	}
	data, err := s.Marshal("fed")
	if err != nil {
		t.Fatal(err)
	}
	restored, _, _ := newTimeStateCodec(t)
	if err := restored.Unmarshal("fed", data); err != nil {
		t.Fatal(err)
	}
	for i := 2; i <= 4; i++ {
		if err := restored.Manager.TimeAdvanceRequest(ctx, "fed", 1, core.LogicalTime(i)*delta); err != nil {
			t.Fatal(err)
		}
	}
	actual := restored.Manager.Metrics("fed", 1).Lookahead
	if actual == nil || *actual != 1-4*delta {
		t.Fatalf("restored reduction lost accumulated advances: %v", actual)
	}
}

func TestTimeStateCodecRejectsInvalidLookaheadReduction(t *testing.T) {
	ctx := context.Background()
	s, _, _ := newTimeStateCodec(t)
	if err := s.Manager.EnableRegulation(ctx, "fed", 1, 5); err != nil {
		t.Fatal(err)
	}
	if err := s.Manager.ModifyLookahead(ctx, "fed", 1, 2); err != nil {
		t.Fatal(err)
	}
	valid, err := s.Marshal("fed")
	if err != nil {
		t.Fatal(err)
	}
	for name, bad := range map[string][]byte{
		"legacy version": bytes.Replace(valid, []byte(`"version":2`), []byte(`"version":1`), 1),
		"not a decrease": bytes.Replace(valid, []byte(`"requested_lookahead":2`), []byte(`"requested_lookahead":6`), 1),
		"not regulating": bytes.Replace(valid, []byte(`"regulating":true`), []byte(`"regulating":false`), 1),
		"negative":       bytes.Replace(valid, []byte(`"requested_lookahead":2`), []byte(`"requested_lookahead":-1`), 1),
		"missing origin": bytes.Replace(valid, []byte(`"lookahead_origin_value":5`), []byte(`"lookahead_origin_value":0`), 1),
		"future origin":  bytes.Replace(valid, []byte(`"lookahead_origin_value":5`), []byte(`"lookahead_origin_value":5,"lookahead_origin_time":1`), 1),
	} {
		t.Run(name, func(t *testing.T) {
			commit, err := s.PrepareRestore("fed", bad)
			if err == nil || commit != nil {
				t.Fatalf("invalid reduction accepted: %s", bad)
			}
			after, err := s.Marshal("fed")
			if err != nil || !bytes.Equal(valid, after) {
				t.Fatal("rejected reduction changed live state")
			}
		})
	}
}
