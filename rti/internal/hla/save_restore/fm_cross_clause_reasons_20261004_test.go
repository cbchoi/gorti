package savepoint

import (
	"context"
	"errors"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func TestFMCrossClause20261004InstructedResignNotifiesOnlySurvivors(t *testing.T) {
	f := newScheduleFixture(t)
	ctx := context.Background()
	target := core.LogicalTime(10)
	if err := f.m.RequestFederationSave(ctx, "fed", "member-resigned", &target); err != nil {
		t.Fatal(err)
	}
	f.ready = true
	if err := progressSchedule(t, f.m); err != nil {
		t.Fatal(err)
	}
	for _, h := range f.members {
		found := false
		for _, delivery := range f.out.Sent() {
			if event, ok := delivery.Event.(*InitiateSaveEvent); ok && delivery.Federate == h && event.Label() == "member-resigned" {
				found = true
			}
		}
		if !found {
			t.Fatal("participant was not instructed")
		}
	}
	f.members = []core.FederateHandle{1}
	if err := f.m.FederateSaveBegun(ctx, "fed", 1); err != nil {
		t.Fatal(err)
	}
	if err := f.m.FederateSaveComplete(ctx, "fed", 1); !errors.Is(err, ErrScheduledSaveParticipantsChanged) {
		t.Fatalf("changed instructed census: %v", err)
	}
	failed := 0
	for _, delivery := range f.out.Sent() {
		if event, ok := delivery.Event.(*NotSavedEvent); ok {
			if delivery.Federate != 1 || event.Label() != "member-resigned" || event.Reason != FederateResignedDuringSave {
				t.Fatalf("wrong failure recipient/reason: %+v %+v", delivery, event)
			}
			failed++
		}
	}
	if failed != 1 || f.m.QuerySaveState("fed", "member-resigned") != StateNotSaved || f.store.Exists("fed", "member-resigned") || f.released != 1 {
		t.Fatalf("failed=%d released=%d", failed, f.released)
	}
}

func TestFMCrossClause20261004UnhonorableTimeNotifiesCensusAndReleases(t *testing.T) {
	f := newScheduleFixture(t)
	readinessFailure := errors.New("accepted time boundary can no longer be honored")
	f.m.opts.ScheduledTime = func(core.FederationName, core.LogicalTime, []core.FederateHandle) (func() (bool, error), func(), error) {
		return func() (bool, error) { return false, readinessFailure }, func() { f.released++ }, nil
	}
	target := core.LogicalTime(10)
	if err := f.m.RequestFederationSave(context.Background(), "fed", "time-failure", &target); err != nil {
		t.Fatal(err)
	}
	if err := progressSchedule(t, f.m); !errors.Is(err, readinessFailure) {
		t.Fatalf("progress error=%v", err)
	}
	seen := map[core.FederateHandle]int{}
	for _, delivery := range f.out.Sent() {
		event, ok := delivery.Event.(*NotSavedEvent)
		if !ok || event.Label() != "time-failure" || event.Reason != SaveTimeCannotBeHonored {
			t.Fatalf("wrong timed failure event: %#v", delivery.Event)
		}
		seen[delivery.Federate]++
	}
	if len(seen) != 2 || seen[1] != 1 || seen[2] != 1 || f.released != 1 || f.m.QuerySaveState("fed", "time-failure") != StateNotSaved || f.store.Exists("fed", "time-failure") {
		t.Fatalf("census=%v released=%d", seen, f.released)
	}
}
