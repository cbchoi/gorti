package declaration

import (
	"context"
	"math/rand"
	"reflect"
	"sort"
	"sync"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/core"
)

// TestManager_Concurrent_NoRace stresses Manager from many goroutines doing
// random Publish/Subscribe/SubscribersFor operations. Run under -race to
// catch any unsynchronized access; the assertion at the end checks that the
// final SubscribersFor for the canonical (cls, attr) matches the union of
// goroutines that subscribed.
func TestManager_Concurrent_NoRace(t *testing.T) {
	t.Parallel()
	const (
		numGoroutines = 50
		opsPerG       = 200
		fed           = core.FederationName("fed")
		cls           = core.ObjectClassHandle(7)
		attr          = core.AttributeHandle(2)
	)

	mgr := New()
	ctx := context.Background()

	// Each goroutine deterministically subscribes to the canonical (cls, attr)
	// at index opsPerG/2 — that guarantees a known final-state union without
	// taking a lock around the test itself.
	expectedSubs := map[core.FederateHandle]struct{}{}
	var expMu sync.Mutex

	var wg sync.WaitGroup
	for g := 0; g < numGoroutines; g++ {
		wg.Add(1)
		go func(seed int) {
			defer wg.Done()
			rng := rand.New(rand.NewSource(int64(seed) + 1))
			h := core.FederateHandle(seed + 1)
			for i := 0; i < opsPerG; i++ {
				switch rng.Intn(6) {
				case 0:
					_ = mgr.PublishObjectClassAttributes(ctx, fed, h, cls,
						[]core.AttributeHandle{attr, core.AttributeHandle(rng.Intn(5) + 1)})
				case 1:
					_ = mgr.UnpublishObjectClassAttributes(ctx, fed, h, cls,
						[]core.AttributeHandle{core.AttributeHandle(rng.Intn(5) + 1)})
				case 2:
					_ = mgr.SubscribeObjectClassAttributes(ctx, fed, h,
						core.ObjectClassHandle(rng.Intn(3)+1),
						[]core.AttributeHandle{core.AttributeHandle(rng.Intn(5) + 1)})
				case 3:
					_ = mgr.PublishInteractionClass(ctx, fed, h, core.InteractionClassHandle(rng.Intn(3)+1))
				case 4:
					_ = mgr.SubscribeInteractionClass(ctx, fed, h, core.InteractionClassHandle(rng.Intn(3)+1))
				case 5:
					_ = mgr.SubscribersFor(ctx, fed, cls, []core.AttributeHandle{attr, 3, 4})
				}
				if i == opsPerG/2 {
					if err := mgr.SubscribeObjectClassAttributes(ctx, fed, h, cls, []core.AttributeHandle{attr}); err != nil {
						t.Errorf("goroutine %d Subscribe: %v", seed, err)
						return
					}
					expMu.Lock()
					expectedSubs[h] = struct{}{}
					expMu.Unlock()
				}
			}
		}(g)
	}
	wg.Wait()

	got := mgr.SubscribersFor(ctx, fed, cls, []core.AttributeHandle{attr})

	wantHandles := make([]core.FederateHandle, 0, len(expectedSubs))
	for h := range expectedSubs {
		wantHandles = append(wantHandles, h)
	}
	sort.Slice(wantHandles, func(i, j int) bool { return wantHandles[i] < wantHandles[j] })

	if !reflect.DeepEqual(got, wantHandles) {
		t.Errorf("final SubscribersFor mismatch.\n got=%v\nwant=%v", got, wantHandles)
	}

	// Sanity: confirm sorted ordering one more time from a clean read path.
	for i := 1; i < len(got); i++ {
		if got[i] <= got[i-1] {
			t.Errorf("SubscribersFor not sorted: index %d got %d after %d", i, got[i], got[i-1])
		}
	}
}

// TestManager_Concurrent_ReadersDuringWrites verifies that read methods do not
// observe corrupted state while writers are mutating. The race detector
// remains the primary check; this adds an integrity assertion on returned
// slice membership.
func TestManager_Concurrent_ReadersDuringWrites(t *testing.T) {
	t.Parallel()
	const (
		numWriters = 20
		numReaders = 20
		opsPerG    = 100
		fed        = core.FederationName("fed")
		cls        = core.InteractionClassHandle(11)
	)

	mgr := New()
	ctx := context.Background()

	var wg sync.WaitGroup
	for g := 0; g < numWriters; g++ {
		wg.Add(1)
		go func(seed int) {
			defer wg.Done()
			h := core.FederateHandle(seed + 1)
			for i := 0; i < opsPerG; i++ {
				_ = mgr.SubscribeInteractionClass(ctx, fed, h, cls)
				if i%3 == 0 {
					_ = mgr.UnsubscribeInteractionClass(ctx, fed, h, cls)
				}
			}
		}(g)
	}
	for g := 0; g < numReaders; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < opsPerG; i++ {
				subs := mgr.InteractionSubscribersFor(ctx, fed, cls)
				for j := 1; j < len(subs); j++ {
					if subs[j] <= subs[j-1] {
						t.Errorf("reader observed non-sorted slice: %v", subs)
						return
					}
				}
			}
		}()
	}
	wg.Wait()
}
