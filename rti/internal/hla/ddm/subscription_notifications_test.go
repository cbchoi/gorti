package ddm

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func subscribeNotificationFixture(ctx context.Context, m *Manager, grouped bool, attrs []core.AttributeHandle, regions []RegionHandle) error {
	options := core.SubscriptionOptions{Active: true}
	if grouped {
		return m.SubscribeObjectClassAttributeRegionGroups(ctx, "fed", 1, 1, []core.AttributeRegionGroup{{Attributes: attrs, Regions: regions}}, options)
	}
	return m.SubscribeObjectClassAttributesWithRegionsAndOptions(ctx, "fed", 1, 1, attrs, regions, options)
}

func TestObjectSubscribedPostCommitReleasesLocksAndAllowsReentry(t *testing.T) {
	for _, grouped := range []bool{false, true} {
		name := "flat"
		if grouped {
			name = "grouped"
		}
		t.Run(name, func(t *testing.T) {
			m, regions := groupedFixture(t)
			var rate sync.Mutex
			m.SetUpdateRateHooks(nil, nil, func() func() {
				rate.Lock()
				return rate.Unlock
			})
			calls := 0
			m.opts.OnObjectSubscribed = func(ctx context.Context, fed core.FederationName, sub core.FederateHandle, cls core.ObjectClassHandle, attrs []core.AttributeHandle) {
				calls++
				if !m.subscriptionMu.TryLock() {
					t.Error("postcommit hook retained subscription lock")
					return
				}
				m.subscriptionMu.Unlock()
				if !rate.TryLock() {
					t.Error("postcommit hook retained rate guard")
					return
				}
				rate.Unlock()
				if !m.mu.TryLock() {
					t.Error("postcommit hook retained DDM state lock")
					return
				}
				m.mu.Unlock()
				if fed != "fed" || sub != 1 || cls != 1 || !slices.Equal(attrs, []core.AttributeHandle{1, 2}) {
					t.Errorf("postcommit identity/attributes = %s/%d/%d/%v", fed, sub, cls, attrs)
				}
				if got := len(m.ExportState(fed).ObjectSubscriptions); got != 2 {
					t.Errorf("callback could not observe committed subscription: %d rows", got)
				}
				if err := m.UnsubscribeObjectClassAttributeRegionGroups(ctx, fed, sub, cls, []core.AttributeRegionGroup{{Attributes: attrs, Regions: regions[:1]}}); err != nil {
					t.Errorf("postcommit reentrant unsubscription: %v", err)
				}
				attrs[0] = 999
			}
			input := []core.AttributeHandle{2, 1, 2}
			if err := subscribeNotificationFixture(context.Background(), m, grouped, input, regions[:1]); err != nil {
				t.Fatal(err)
			}
			if calls != 1 || len(m.ExportState("fed").ObjectSubscriptions) != 0 {
				t.Fatalf("notifications=%d, reentrant unsubscription did not commit", calls)
			}
			if !slices.Equal(input, []core.AttributeHandle{2, 1, 2}) {
				t.Fatalf("postcommit callback mutated caller attributes: %v", input)
			}
		})
	}
}

func TestObjectSubscribedPostCommitSkipsNoOpsAndRollback(t *testing.T) {
	for _, grouped := range []bool{false, true} {
		name := "flat"
		if grouped {
			name = "grouped"
		}
		t.Run(name, func(t *testing.T) {
			m, regions := groupedFixture(t)
			calls := 0
			m.opts.OnObjectSubscribed = func(context.Context, core.FederationName, core.FederateHandle, core.ObjectClassHandle, []core.AttributeHandle) {
				calls++
			}
			ctx := context.Background()
			if err := subscribeNotificationFixture(ctx, m, grouped, []core.AttributeHandle{1}, nil); err != nil {
				t.Fatal(err)
			}
			if err := subscribeNotificationFixture(ctx, m, grouped, nil, regions[:1]); err != nil {
				t.Fatal(err)
			}
			if err := subscribeNotificationFixture(ctx, m, grouped, []core.AttributeHandle{1}, []RegionHandle{999}); err == nil {
				t.Fatal("invalid region subscription accepted")
			}
			failure := errors.New("advisory admission rejected")
			m.opts.OnObjectSubscriptionInterest = func(context.Context, core.FederationName, core.FederateHandle, core.ObjectClassHandle, []core.AttributeHandle) error {
				return failure
			}
			if err := subscribeNotificationFixture(ctx, m, grouped, []core.AttributeHandle{1, 2}, regions[:1]); !errors.Is(err, failure) {
				t.Fatalf("advisory rejection = %v", err)
			}
			if calls != 0 || len(m.ExportState("fed").ObjectSubscriptions) != 0 {
				t.Fatalf("failed/no-op subscription notification=%d or rollback leaked state", calls)
			}
		})
	}
}
