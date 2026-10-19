package time

import (
	"context"
	"errors"
	"math"
	"testing"
	stdtime "time"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func TestScheduledSaveRequesterPreconditionsBothScalars(t *testing.T) {
	t.Run("Float64", func(t *testing.T) { scheduledRequesterContract[core.LogicalTime](t) })
	t.Run("Integer64", func(t *testing.T) { scheduledRequesterContract[int64](t) })
}

func scheduledRequesterContract[T core.LogicalTimeScalar](t *testing.T) {
	t.Helper()
	out := &scheduledAnyOutbox{}
	m, err := NewOf[T](OptionsOf[T]{Outbox: out, Clock: core.NewManualClock(stdtime.Unix(1, 0))})
	if err != nil {
		t.Fatal(err)
	}
	members := []core.FederateHandle{1, 2}
	check := func(h core.FederateHandle, at T, want error) {
		t.Helper()
		before := len(out.events)
		err := m.ValidateScheduledSaveRequest("fed", h, at, members)
		if !errors.Is(err, want) {
			t.Fatalf("requester %d at %v: %v, want %v", h, at, err, want)
		}
		if len(out.events) != before {
			t.Fatal("validation emitted a callback")
		}
	}
	check(3, 10, core.ErrFederateNotJoined)
	check(1, 10, core.ErrSaveTimeNotAllowed)
	check(1, -1, core.ErrTimeInvalidLogicalTime)
	if err := m.EnableRegulation(context.Background(), "fed", 2, 1); err != nil {
		t.Fatal(err)
	}
	check(1, 1, core.ErrTimeRequestInPast)
	check(1, 2, nil)
	check(2, 0, core.ErrTimeInvalidLogicalTime)
	check(2, 1, nil)
	if err := m.EnableConstrained(context.Background(), "fed", 1); err != nil {
		t.Fatal(err)
	}
	check(2, 0, core.ErrTimeInvalidLogicalTime)
	// Validation observes actual constrained time without modifying its pending
	// advance or granting a synthetic checkpoint time.
	check(2, 10, nil)
}

func TestScheduledSaveIntegerRequesterOverflowIsInvalidNotFinal(t *testing.T) {
	out := &scheduledAnyOutbox{}
	m, err := NewOf[int64](OptionsOf[int64]{Outbox: out, Clock: core.NewManualClock(stdtime.Unix(1, 0))})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := m.EnableRegulation(ctx, "fed", 1, 1); err != nil {
		t.Fatal(err)
	}
	if err := m.TimeAdvanceRequest(ctx, "fed", 1, math.MaxInt64); err != nil {
		t.Fatal(err)
	}
	if err := m.ValidateScheduledSaveRequest("fed", 1, math.MaxInt64, []core.FederateHandle{1, 2}); !errors.Is(err, core.ErrTimeInvalidLogicalTime) {
		t.Fatalf("overflow silently became finite final: %v", err)
	}
	if err := m.ValidateScheduledSaveRequest("fed", 2, math.MaxInt64, []core.FederateHandle{1, 2}); !errors.Is(err, core.ErrTimeRequestInPast) {
		t.Fatalf("unbounded GALT treated as finite MaxInt64: %v", err)
	}
	if got := m.Metrics("fed", 1).CurrentTime; got != math.MaxInt64 {
		t.Fatalf("invalid save mutated current time: %v", got)
	}
}
