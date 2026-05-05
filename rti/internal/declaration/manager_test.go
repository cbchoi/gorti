package declaration

import (
	"context"
	"math/rand"
	"reflect"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/core"
)

func TestManager_PublishObjectClass_RecordsPublisher(t *testing.T) {
	t.Parallel()
	mgr := New()
	ctx := context.Background()

	if err := mgr.PublishObjectClassAttributes(ctx, "fed", 1, 7, []core.AttributeHandle{2, 3}); err != nil {
		t.Fatalf("PublishObjectClassAttributes: %v", err)
	}

	got := mgr.PublishersFor(ctx, "fed", 7, 2)
	want := []core.FederateHandle{1}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("PublishersFor(7,2) = %v, want %v", got, want)
	}

	got = mgr.PublishersFor(ctx, "fed", 7, 3)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("PublishersFor(7,3) = %v, want %v", got, want)
	}
}

func TestManager_SubscribeObjectClass_RecordsSubscriber(t *testing.T) {
	t.Parallel()
	mgr := New()
	ctx := context.Background()

	if err := mgr.SubscribeObjectClassAttributes(ctx, "fed", 5, 9, []core.AttributeHandle{2}); err != nil {
		t.Fatalf("SubscribeObjectClassAttributes: %v", err)
	}

	got := mgr.SubscribersFor(ctx, "fed", 9, []core.AttributeHandle{2})
	want := []core.FederateHandle{5}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("SubscribersFor = %v, want %v", got, want)
	}
}

func TestManager_SubscribersFor_DeterministicSortedOrder(t *testing.T) {
	t.Parallel()
	// Subscribe in jumbled order; SubscribersFor must always return ascending.
	jumbled := []core.FederateHandle{42, 3, 17, 1, 9, 100, 8, 25}
	want := []core.FederateHandle{1, 3, 8, 9, 17, 25, 42, 100}

	// Run several seeded shuffles to defend against accidental insert-order leak.
	rng := rand.New(rand.NewSource(0xC0FFEE))
	for trial := 0; trial < 5; trial++ {
		shuffled := make([]core.FederateHandle, len(jumbled))
		copy(shuffled, jumbled)
		rng.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })

		mgr := New()
		ctx := context.Background()
		for _, h := range shuffled {
			if err := mgr.SubscribeObjectClassAttributes(ctx, "fed", h, 9, []core.AttributeHandle{2}); err != nil {
				t.Fatalf("SubscribeObjectClassAttributes: %v", err)
			}
		}

		got := mgr.SubscribersFor(ctx, "fed", 9, []core.AttributeHandle{2})
		if !reflect.DeepEqual(got, want) {
			t.Errorf("trial %d shuffled=%v: SubscribersFor = %v, want %v", trial, shuffled, got, want)
		}
	}
}

func TestManager_SubscribersFor_AnyMatchingAttribute(t *testing.T) {
	t.Parallel()
	mgr := New()
	ctx := context.Background()

	if err := mgr.SubscribeObjectClassAttributes(ctx, "fed", 5, 7, []core.AttributeHandle{2}); err != nil {
		t.Fatalf("SubscribeObjectClassAttributes: %v", err)
	}

	got := mgr.SubscribersFor(ctx, "fed", 7, []core.AttributeHandle{2, 3, 4})
	want := []core.FederateHandle{5}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("SubscribersFor superset = %v, want %v", got, want)
	}
}

func TestManager_SubscribersFor_DisjointAttrs_NoMatch(t *testing.T) {
	t.Parallel()
	mgr := New()
	ctx := context.Background()

	if err := mgr.SubscribeObjectClassAttributes(ctx, "fed", 5, 7, []core.AttributeHandle{2}); err != nil {
		t.Fatalf("SubscribeObjectClassAttributes: %v", err)
	}

	got := mgr.SubscribersFor(ctx, "fed", 7, []core.AttributeHandle{3, 4})
	if len(got) != 0 {
		t.Errorf("SubscribersFor disjoint attrs = %v, want []", got)
	}
}

func TestManager_PublishObjectClass_IdempotentRepeats(t *testing.T) {
	t.Parallel()
	mgr := New()
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		if err := mgr.PublishObjectClassAttributes(ctx, "fed", 1, 7, []core.AttributeHandle{2}); err != nil {
			t.Fatalf("PublishObjectClassAttributes (iter %d): %v", i, err)
		}
	}
	got := mgr.PublishersFor(ctx, "fed", 7, 2)
	want := []core.FederateHandle{1}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("after 3x publish PublishersFor = %v, want %v", got, want)
	}
}

func TestManager_PerFederationIsolation(t *testing.T) {
	t.Parallel()
	mgr := New()
	ctx := context.Background()

	if err := mgr.SubscribeObjectClassAttributes(ctx, "fedA", 5, 7, []core.AttributeHandle{2}); err != nil {
		t.Fatalf("SubscribeObjectClassAttributes(fedA): %v", err)
	}
	if err := mgr.PublishObjectClassAttributes(ctx, "fedA", 9, 7, []core.AttributeHandle{2}); err != nil {
		t.Fatalf("PublishObjectClassAttributes(fedA): %v", err)
	}
	if err := mgr.PublishInteractionClass(ctx, "fedA", 4, 11); err != nil {
		t.Fatalf("PublishInteractionClass(fedA): %v", err)
	}
	if err := mgr.SubscribeInteractionClass(ctx, "fedA", 6, 11); err != nil {
		t.Fatalf("SubscribeInteractionClass(fedA): %v", err)
	}

	if got := mgr.SubscribersFor(ctx, "fedB", 7, []core.AttributeHandle{2}); len(got) != 0 {
		t.Errorf("fedB SubscribersFor leaked: got %v", got)
	}
	if got := mgr.PublishersFor(ctx, "fedB", 7, 2); len(got) != 0 {
		t.Errorf("fedB PublishersFor leaked: got %v", got)
	}
	if got := mgr.InteractionPublishersFor(ctx, "fedB", 11); len(got) != 0 {
		t.Errorf("fedB InteractionPublishersFor leaked: got %v", got)
	}
	if got := mgr.InteractionSubscribersFor(ctx, "fedB", 11); len(got) != 0 {
		t.Errorf("fedB InteractionSubscribersFor leaked: got %v", got)
	}

	// And the original fedA state is intact.
	if got := mgr.SubscribersFor(ctx, "fedA", 7, []core.AttributeHandle{2}); !reflect.DeepEqual(got, []core.FederateHandle{5}) {
		t.Errorf("fedA SubscribersFor = %v, want [5]", got)
	}
}

func TestManager_UnpublishObjectClass_Removes(t *testing.T) {
	t.Parallel()
	mgr := New()
	ctx := context.Background()

	_ = mgr.PublishObjectClassAttributes(ctx, "fed", 1, 7, []core.AttributeHandle{2, 3})
	if err := mgr.UnpublishObjectClassAttributes(ctx, "fed", 1, 7, []core.AttributeHandle{2}); err != nil {
		t.Fatalf("UnpublishObjectClassAttributes: %v", err)
	}

	if got := mgr.PublishersFor(ctx, "fed", 7, 2); len(got) != 0 {
		t.Errorf("PublishersFor(7,2) after unpublish = %v, want []", got)
	}
	// attr 3 still published.
	if got := mgr.PublishersFor(ctx, "fed", 7, 3); !reflect.DeepEqual(got, []core.FederateHandle{1}) {
		t.Errorf("PublishersFor(7,3) = %v, want [1]", got)
	}
}

func TestManager_UnsubscribeObjectClass_Removes(t *testing.T) {
	t.Parallel()
	mgr := New()
	ctx := context.Background()

	_ = mgr.SubscribeObjectClassAttributes(ctx, "fed", 5, 7, []core.AttributeHandle{2, 3})
	if err := mgr.UnsubscribeObjectClassAttributes(ctx, "fed", 5, 7, []core.AttributeHandle{2, 3}); err != nil {
		t.Fatalf("UnsubscribeObjectClassAttributes: %v", err)
	}

	if got := mgr.SubscribersFor(ctx, "fed", 7, []core.AttributeHandle{2, 3}); len(got) != 0 {
		t.Errorf("SubscribersFor after unsub = %v, want []", got)
	}
}

func TestManager_UnsubscribeUnknown_NoError(t *testing.T) {
	t.Parallel()
	mgr := New()
	ctx := context.Background()
	if err := mgr.UnsubscribeObjectClassAttributes(ctx, "fed", 5, 7, []core.AttributeHandle{2}); err != nil {
		t.Errorf("UnsubscribeObjectClassAttributes on empty state: %v", err)
	}
	if err := mgr.UnpublishObjectClassAttributes(ctx, "fed", 1, 7, []core.AttributeHandle{2}); err != nil {
		t.Errorf("UnpublishObjectClassAttributes on empty state: %v", err)
	}
	if err := mgr.UnsubscribeInteractionClass(ctx, "fed", 4, 11); err != nil {
		t.Errorf("UnsubscribeInteractionClass on empty state: %v", err)
	}
	if err := mgr.UnpublishInteractionClass(ctx, "fed", 4, 11); err != nil {
		t.Errorf("UnpublishInteractionClass on empty state: %v", err)
	}
}

func TestManager_InteractionPubSub_DeterministicSortedOrder(t *testing.T) {
	t.Parallel()
	mgr := New()
	ctx := context.Background()

	if err := mgr.PublishInteractionClass(ctx, "fed", 4, 11); err != nil {
		t.Fatalf("PublishInteractionClass: %v", err)
	}
	for _, h := range []core.FederateHandle{8, 2, 6, 14, 1} {
		if err := mgr.SubscribeInteractionClass(ctx, "fed", h, 11); err != nil {
			t.Fatalf("SubscribeInteractionClass(%d): %v", h, err)
		}
	}

	pubs := mgr.InteractionPublishersFor(ctx, "fed", 11)
	if !reflect.DeepEqual(pubs, []core.FederateHandle{4}) {
		t.Errorf("InteractionPublishersFor = %v, want [4]", pubs)
	}

	subs := mgr.InteractionSubscribersFor(ctx, "fed", 11)
	want := []core.FederateHandle{1, 2, 6, 8, 14}
	if !reflect.DeepEqual(subs, want) {
		t.Errorf("InteractionSubscribersFor = %v, want %v", subs, want)
	}
}

func TestManager_InteractionPubSub_Idempotent(t *testing.T) {
	t.Parallel()
	mgr := New()
	ctx := context.Background()

	for i := 0; i < 4; i++ {
		_ = mgr.PublishInteractionClass(ctx, "fed", 4, 11)
		_ = mgr.SubscribeInteractionClass(ctx, "fed", 6, 11)
	}

	if got := mgr.InteractionPublishersFor(ctx, "fed", 11); !reflect.DeepEqual(got, []core.FederateHandle{4}) {
		t.Errorf("InteractionPublishersFor after 4x = %v, want [4]", got)
	}
	if got := mgr.InteractionSubscribersFor(ctx, "fed", 11); !reflect.DeepEqual(got, []core.FederateHandle{6}) {
		t.Errorf("InteractionSubscribersFor after 4x = %v, want [6]", got)
	}
}

func TestManager_UnsubscribeInteractionClass_Removes(t *testing.T) {
	t.Parallel()
	mgr := New()
	ctx := context.Background()

	_ = mgr.SubscribeInteractionClass(ctx, "fed", 6, 11)
	_ = mgr.SubscribeInteractionClass(ctx, "fed", 8, 11)
	if err := mgr.UnsubscribeInteractionClass(ctx, "fed", 6, 11); err != nil {
		t.Fatalf("UnsubscribeInteractionClass: %v", err)
	}

	got := mgr.InteractionSubscribersFor(ctx, "fed", 11)
	if !reflect.DeepEqual(got, []core.FederateHandle{8}) {
		t.Errorf("InteractionSubscribersFor = %v, want [8]", got)
	}
}

func TestManager_UnpublishInteractionClass_Removes(t *testing.T) {
	t.Parallel()
	mgr := New()
	ctx := context.Background()

	_ = mgr.PublishInteractionClass(ctx, "fed", 4, 11)
	if err := mgr.UnpublishInteractionClass(ctx, "fed", 4, 11); err != nil {
		t.Fatalf("UnpublishInteractionClass: %v", err)
	}

	if got := mgr.InteractionPublishersFor(ctx, "fed", 11); len(got) != 0 {
		t.Errorf("InteractionPublishersFor after unpublish = %v, want []", got)
	}
}

func TestManager_PublishersFor_MultiplePublishersSorted(t *testing.T) {
	t.Parallel()
	mgr := New()
	ctx := context.Background()

	// Publish from federates in jumbled order; PublishersFor result must be sorted.
	for _, h := range []core.FederateHandle{9, 2, 7, 1, 5} {
		_ = mgr.PublishObjectClassAttributes(ctx, "fed", h, 7, []core.AttributeHandle{2})
	}
	got := mgr.PublishersFor(ctx, "fed", 7, 2)
	want := []core.FederateHandle{1, 2, 5, 7, 9}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("PublishersFor = %v, want %v", got, want)
	}
}

func TestManager_SubscribersFor_EmptyAttrs_ReturnsEmpty(t *testing.T) {
	t.Parallel()
	mgr := New()
	ctx := context.Background()
	_ = mgr.SubscribeObjectClassAttributes(ctx, "fed", 5, 7, []core.AttributeHandle{2})

	if got := mgr.SubscribersFor(ctx, "fed", 7, nil); len(got) != 0 {
		t.Errorf("SubscribersFor(nil attrs) = %v, want []", got)
	}
	if got := mgr.SubscribersFor(ctx, "fed", 7, []core.AttributeHandle{}); len(got) != 0 {
		t.Errorf("SubscribersFor(empty attrs) = %v, want []", got)
	}
}

func TestManager_Snapshot_RecordsPubSubPerFederate(t *testing.T) {
	t.Parallel()
	mgr := New()
	ctx := context.Background()

	// fed:demo — federate 1 publishes class 7; federate 2 subscribes class 7
	// + publishes interaction class 50; federate 3 subscribes interaction 50.
	if err := mgr.PublishObjectClassAttributes(ctx, "demo", 1, 7, []core.AttributeHandle{2, 3}); err != nil {
		t.Fatalf("PublishObjectClassAttributes: %v", err)
	}
	if err := mgr.SubscribeObjectClassAttributes(ctx, "demo", 2, 7, []core.AttributeHandle{2}); err != nil {
		t.Fatalf("SubscribeObjectClassAttributes: %v", err)
	}
	if err := mgr.PublishInteractionClass(ctx, "demo", 2, 50); err != nil {
		t.Fatalf("PublishInteractionClass: %v", err)
	}
	if err := mgr.SubscribeInteractionClass(ctx, "demo", 3, 50); err != nil {
		t.Fatalf("SubscribeInteractionClass: %v", err)
	}

	snap := mgr.Snapshot("demo")

	wantPub := []core.ObjectClassHandle{7}
	if !reflect.DeepEqual(snap.PublishedObjectClasses, wantPub) {
		t.Errorf("PublishedObjectClasses = %v, want %v", snap.PublishedObjectClasses, wantPub)
	}
	if got := len(snap.PerFederate); got != 3 {
		t.Errorf("PerFederate len = %d, want 3", got)
	}
	if pf, ok := snap.PerFederate[1]; !ok {
		t.Errorf("PerFederate[1] missing")
	} else if !reflect.DeepEqual(pf.PublishedObjectClasses, []core.ObjectClassHandle{7}) {
		t.Errorf("federate 1 PublishedObjectClasses = %v, want [7]", pf.PublishedObjectClasses)
	}
	if pf, ok := snap.PerFederate[2]; !ok {
		t.Errorf("PerFederate[2] missing")
	} else {
		if !reflect.DeepEqual(pf.SubscribedObjectClasses, []core.ObjectClassHandle{7}) {
			t.Errorf("federate 2 SubscribedObjectClasses = %v, want [7]", pf.SubscribedObjectClasses)
		}
		if !reflect.DeepEqual(pf.PublishedInteractionClasses, []core.InteractionClassHandle{50}) {
			t.Errorf("federate 2 PublishedInteractionClasses = %v, want [50]", pf.PublishedInteractionClasses)
		}
	}
	if pf, ok := snap.PerFederate[3]; !ok {
		t.Errorf("PerFederate[3] missing")
	} else if !reflect.DeepEqual(pf.SubscribedInteractionClasses, []core.InteractionClassHandle{50}) {
		t.Errorf("federate 3 SubscribedInteractionClasses = %v, want [50]", pf.SubscribedInteractionClasses)
	}
}

func TestManager_Snapshot_UnknownFederation_ReturnsEmpty(t *testing.T) {
	t.Parallel()
	mgr := New()
	snap := mgr.Snapshot("nope")
	if len(snap.PublishedObjectClasses) != 0 {
		t.Errorf("PublishedObjectClasses = %v, want empty", snap.PublishedObjectClasses)
	}
	if snap.PerFederate == nil || len(snap.PerFederate) != 0 {
		t.Errorf("PerFederate = %v, want empty non-nil map", snap.PerFederate)
	}
}
