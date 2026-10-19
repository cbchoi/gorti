package savepoint

import (
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"testing"
)

func TestMOMMetricsPendingSaveAndRestoreAreActualState(t *testing.T) {
	m := &Manager{saves: map[core.FederationName]*activeSave{}, restore: map[core.FederationName]*activeRestore{}}
	if got := m.QueryMOMMetrics("f", 1); got.FederateState != 1 || got.NextSaveName != "" || got.NextSaveTime != nil {
		t.Fatalf("initial=%+v", got)
	}
	at := core.LogicalTime(17)
	m.saves["f"] = &activeSave{label: "pending", saveTime: &at, waiting: true, required: map[core.FederateHandle]struct{}{1: {}}}
	got := m.QueryMOMMetrics("f", 1)
	if got.FederateState != 1 || got.NextSaveName != "pending" || got.NextSaveTime == nil || *got.NextSaveTime != 17 {
		t.Fatalf("scheduled=%+v", got)
	}
	*got.NextSaveTime = 99
	if *m.saves["f"].saveTime != 17 {
		t.Fatal("save time aliased")
	}
	m.saves["f"].waiting = false
	if got := m.QueryMOMMetrics("f", 1); got.FederateState != 3 {
		t.Fatalf("saving=%+v", got)
	}
	delete(m.saves, "f")
	m.restore["f"] = &activeRestore{required: map[core.FederateHandle]struct{}{1: {}}}
	if got := m.QueryMOMMetrics("f", 1); got.FederateState != 5 {
		t.Fatalf("restoring=%+v", got)
	}
	if got := m.QueryMOMMetrics("f", 2); got.FederateState != 1 {
		t.Fatalf("nonparticipant=%+v", got)
	}
}
