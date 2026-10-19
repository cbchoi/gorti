package savepoint

import (
	"context"
	"math"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/adapters/savebundle"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	timepkg "github.com/cbchoi/gorti/rti/internal/hla/time"
)

func TestReviewInteger64ScheduledSaveRestoresPendingLookaheadAndAdvance(t *testing.T) {
	for _, base := range []int64{1<<53 + 1, math.MaxInt64 - 3} {
		ctx := context.Background()
		out := &fakeOutbox{}
		tm, err := timepkg.NewInteger(timepkg.IntegerOptions{Clock: core.NewRealClock(), Outbox: out})
		if err != nil {
			t.Fatal(err)
		}
		if err := tm.ImportState("integer", &timepkg.IntegerManagerState{StallTimeout: timepkg.DefaultStallTimeout, Federates: []timepkg.IntegerFederateState{{Handle: 1, HasRegulationState: true, HasAdvanceState: true, Regulating: true, Constrained: true, CurrentTime: base, Lookahead: 4}}}, nil); err != nil {
			t.Fatal(err)
		}
		if err := tm.ModifyLookahead(ctx, "integer", 1, 1); err != nil {
			t.Fatal(err)
		}
		codec := savebundle.IntegerTime{Manager: tm}
		coordinator, err := NewOf(OptionsOf[int64]{
			Outbox: out, BundleStore: newMemStore(),
			Members:       func(core.FederationName) []core.FederateHandle { return []core.FederateHandle{1} },
			ScheduledTime: tm.ReserveScheduledSave, ResumeTime: tm.ResumeScheduledSave,
			ManagerSnapshots: map[string]ManagerSnapshotter{"time": codec},
		})
		if err != nil {
			t.Fatal(err)
		}
		target := base + 3
		if err := coordinator.RequestFederationSave(ctx, "integer", "pending", &target); err != nil {
			t.Fatal(err)
		}
		metrics := coordinator.QueryMOMMetrics("integer", 1)
		if metrics.NextSaveTime == nil || *metrics.NextSaveTime != target || metrics.FederateState != 1 {
			t.Fatalf("pending save metrics=%+v target=%d", metrics, target)
		}
		*metrics.NextSaveTime = 0
		if err := tm.TimeAdvanceRequest(ctx, "integer", 1, target); err != nil {
			t.Fatal(err)
		}
		if got := tm.Metrics("integer", 1); !got.Advancing || got.CurrentTime != base {
			t.Fatalf("scheduled-save crossing committed prematurely: %+v", got)
		}
		if err := coordinator.ProgressScheduledSave(ctx, "integer"); err != nil {
			t.Fatal(err)
		}
		var initiation *InitiateSaveEventOf[int64]
		for _, callback := range out.Sent() {
			if event, ok := callback.Event.(*InitiateSaveEventOf[int64]); ok {
				initiation = event
			}
		}
		if initiation == nil || initiation.SaveTime() == nil || *initiation.SaveTime() != target {
			t.Fatal("actual initiation callback lost exact time")
		}
		if err := coordinator.FederateSaveBegun(ctx, "integer", 1); err != nil {
			t.Fatal(err)
		}
		if err := coordinator.FederateSaveComplete(ctx, "integer", 1); err != nil {
			t.Fatal(err)
		}
		manifest, err := coordinator.LoadManifest("integer", "pending")
		if err != nil || manifest.SaveTime == nil || *manifest.SaveTime != target || manifest.LogicalTimeImplementationName != "HLAinteger64Time" {
			t.Fatalf("manifest=%+v error=%v", manifest, err)
		}
		if err := coordinator.ProgressScheduledSave(ctx, "integer"); err != nil {
			t.Fatal(err)
		}
		if got := tm.Metrics("integer", 1); got.Advancing || got.CurrentTime != target || got.Lookahead == nil || *got.Lookahead != 1 {
			t.Fatalf("released advance=%+v target=%d", got, target)
		}
		history := coordinator.QueryMOMMetrics("integer", 1)
		if !history.LastSaveExists || history.LastSaveTime == nil || *history.LastSaveTime != target {
			t.Fatalf("successful save history rounded: %+v", history)
		}
		if err := coordinator.RequestFederationRestore(ctx, "integer", "pending"); err != nil {
			t.Fatal(err)
		}
		if err := coordinator.FederateRestoreComplete(ctx, "integer", 1); err != nil {
			t.Fatal(err)
		}
		state, err := tm.ExportState("integer", nil)
		if err != nil || len(state.Federates) != 1 {
			t.Fatalf("restored state=%+v error=%v", state, err)
		}
		entry := state.Federates[0]
		if entry.CurrentTime != base || !entry.PendingRequest || entry.RequestedTime != target || entry.RequestedLookahead == nil || *entry.RequestedLookahead != 1 || entry.LookaheadOriginTime != base || entry.LookaheadOriginValue != 4 || entry.Lookahead != 4 {
			t.Fatalf("restored pending state=%+v base=%d target=%d", entry, base, target)
		}
		if err := tm.RetryPendingDeliveries(ctx, "integer"); err != nil {
			t.Fatal(err)
		}
		if got := tm.Metrics("integer", 1); got.Advancing || got.CurrentTime != target || got.Lookahead == nil || *got.Lookahead != 1 {
			t.Fatalf("restored advance resume=%+v", got)
		}
	}
}
