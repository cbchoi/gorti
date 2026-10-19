package time

import (
	"context"
	"reflect"
	"testing"
	stdtime "time"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func TestScheduledSaveIndependentParticipantsAndAvailableBoundaries(t *testing.T) {
	for _, mode := range []AdvanceMode{ModeTAR, ModeNER, ModeTARA, ModeNMRA, ModeFQR} {
		t.Run("Float64/"+mode.String(), func(t *testing.T) { scheduledParticipantsContract[core.LogicalTime](t, mode) })
		t.Run("Integer64/"+mode.String(), func(t *testing.T) { scheduledParticipantsContract[int64](t, mode) })
	}
}

func scheduledParticipantsContract[T core.LogicalTimeScalar](t *testing.T, mode AdvanceMode) {
	ctx := context.Background()
	out := &scheduledAnyOutbox{}
	m, err := NewOf[T](OptionsOf[T]{Outbox: out, Clock: core.NewManualClock(stdtime.Unix(1, 0))})
	if err != nil {
		t.Fatal(err)
	}
	if err = m.EnableRegulation(ctx, "fed", 1, 1); err != nil {
		t.Fatal(err)
	}
	for _, h := range []core.FederateHandle{2, 3} {
		if err = m.EnableConstrained(ctx, "fed", h); err != nil {
			t.Fatal(err)
		}
	}
	r, err := m.PrepareScheduledSave("fed", 10, []core.FederateHandle{1, 2, 3, 4}, false)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Release()
	r.Commit()
	if err = m.TimeAdvanceRequest(ctx, "fed", 1, 20); err != nil {
		t.Fatal(err)
	}
	if err = m.dispatchAdvance(ctx, "fed", 2, 10, mode); err != nil {
		t.Fatal(err)
	}
	ready, err := r.Ready(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !strictAdvance(mode) {
		if len(ready) != 0 {
			t.Fatalf("available equality initiated save: %v", ready)
		}
		if err = m.dispatchAdvance(ctx, "fed", 2, 11, mode); err != nil {
			t.Fatal(err)
		}
		ready, err = r.Ready(ctx)
		if err != nil {
			t.Fatal(err)
		}
	}
	if !reflect.DeepEqual(ready, []core.FederateHandle{2}) {
		t.Fatalf("first constrained waits for slower: %v", ready)
	}
	if err := r.Pause(ready); err != nil {
		t.Fatal(err)
	}
	if err = m.dispatchAdvance(ctx, "fed", 3, 11, mode); err != nil {
		t.Fatal(err)
	}
	ready, err = r.Ready(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(ready, []core.FederateHandle{1, 2, 3, 4}) {
		t.Fatalf("nonconstrained boundary: %v", ready)
	}
}

func TestScheduledSaveInvalidPreparedReplacementKeepsBarrier(t *testing.T) {
	m, _ := newScheduledTM(t)
	r, err := m.PrepareScheduledSave("fed", 10, []core.FederateHandle{1, 2}, false)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Release()
	r.Commit()
	if candidate, err := m.PrepareScheduledSave("fed", -1, []core.FederateHandle{1, 2}, true); err == nil {
		candidate.Release()
		t.Fatal("invalid replacement accepted")
	}
	if _, err := r.Ready(context.Background()); err != nil {
		t.Fatalf("old barrier lost after invalid replacement: %v", err)
	}
	candidate, err := m.PrepareScheduledSave("fed", 12, []core.FederateHandle{1, 2}, true)
	if err != nil {
		t.Fatal(err)
	}
	candidate.Release()
	if _, err := r.Ready(context.Background()); err != nil {
		t.Fatalf("old barrier lost after cancelled prepare: %v", err)
	}
}
