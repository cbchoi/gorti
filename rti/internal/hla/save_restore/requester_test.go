package savepoint

import (
	"context"
	"errors"
	"testing"
	stdtime "time"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	timepkg "github.com/cbchoi/gorti/rti/internal/hla/time"
)

func TestRequesterAwareSaveUsesExactTimeBackendBothScalars(t *testing.T) {
	t.Run("Float64", func(t *testing.T) { requesterSaveContract[core.LogicalTime](t) })
	t.Run("Integer64", func(t *testing.T) { requesterSaveContract[int64](t) })
}

func requesterSaveContract[T core.LogicalTimeScalar](t *testing.T) {
	ctx := context.Background()
	out := &fakeOutbox{}
	tm, err := timepkg.NewOf[T](timepkg.OptionsOf[T]{Outbox: out, Clock: core.NewManualClock(stdtime.Unix(1, 0))})
	if err != nil {
		t.Fatal(err)
	}
	members := []core.FederateHandle{1, 2}
	m, err := NewOf[T](OptionsOf[T]{Outbox: out, BundleStore: newMemStore(), Members: func(core.FederationName) []core.FederateHandle { return members }, ScheduledTime: tm.ReserveScheduledSave, ValidateSaveRequest: tm.ValidateScheduledSaveRequest})
	if err != nil {
		t.Fatal(err)
	}
	var target T = 10
	if err := m.RequestFederationSaveFrom(ctx, "fed", 1, "invalid", &target); !errors.Is(err, core.ErrSaveTimeNotAllowed) {
		t.Fatalf("requester identity dropped: %v", err)
	}
	if m.Snapshot("fed").SaveState != StateIdle || len(out.Sent()) != 0 {
		t.Fatal("invalid requester changed save or callback state")
	}
	if err := tm.EnableRegulation(ctx, "fed", 1, 1); err != nil {
		t.Fatal(err)
	}
	if err := tm.EnableConstrained(ctx, "fed", 2); err != nil {
		t.Fatal(err)
	}
	before := len(out.Sent())
	if err := m.RequestFederationSaveFrom(ctx, "fed", 1, "legal", &target); err != nil {
		t.Fatal(err)
	}
	if m.Snapshot("fed").SaveLabel != "legal" || len(out.Sent()) != before {
		t.Fatalf("wrong typed admission: %+v callbacks=%d", m.Snapshot("fed"), len(out.Sent()))
	}
	if err := m.AbortSave(ctx, "fed"); err != nil {
		t.Fatal(err)
	}
}
