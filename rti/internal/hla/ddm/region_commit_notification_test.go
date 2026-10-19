package ddm

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func TestRegionCommitNotificationAfterAdmissionAndUnlock(t *testing.T) {
	m, regions := groupedFixture(t)
	ctx := context.Background()
	var guard sync.Mutex
	var admission error
	m.SetUpdateRateHooks(func(context.Context, core.FederationName, bool) error { return admission }, nil, func() func() { guard.Lock(); return guard.Unlock })
	calls := 0
	m.SetOnRegionStateChanged(func(_ context.Context, fed core.FederationName) {
		calls++
		for name, lock := range map[string]*sync.Mutex{"subscription": &m.subscriptionMu, "bridge": &guard} {
			if !lock.TryLock() {
				t.Fatalf("%s held during committed notification", name)
			}
			lock.Unlock()
		}
		if !m.mu.TryLock() {
			t.Fatal("DDM state held during notification")
		}
		m.mu.Unlock()
		if fed != "fed" {
			t.Fatalf("federation=%s", fed)
		}
	})
	groups := []core.AttributeRegionGroup{{Attributes: []core.AttributeHandle{1}, Regions: regions[:1]}}
	if err := m.AssociateRegionGroupsForUpdates(ctx, "fed", 1, 9, groups); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("successful commit notifications=%d", calls)
	}
	admission = errors.New("reject change")
	if err := m.UnassociateRegionGroupsForUpdates(ctx, "fed", 1, 9, groups); !errors.Is(err, admission) {
		t.Fatalf("admission=%v", err)
	}
	if calls != 1 || len(m.ExportState("fed").ObjectPublications) != 1 {
		t.Fatal("rollback emitted notification or lost state")
	}
	admission = nil
	if err := m.CommitRegionModifications(ctx, "fed", 1, regions[:1]); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("region commit notifications=%d", calls)
	}
}
