package object

import (
	"context"
	"sync"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

type subscriptionRevisionFOM struct {
	*countingInteractionFOM
	revision uint64
	mu       sync.Mutex
	queries  int
}

func (f *subscriptionRevisionFOM) FOMRevision() uint64 { return f.revision }
func (f *subscriptionRevisionFOM) DelaySubscriptionEvaluation() (bool, bool) {
	f.mu.Lock()
	f.queries++
	f.mu.Unlock()
	return false, true
}
func (f *subscriptionRevisionFOM) queryCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.queries
}

type subscriptionRevisionRepository struct {
	*countingInteractionFOMRepo
}

func (r *subscriptionRevisionRepository) FOMHandleReference(core.FederationName) core.FOMHandleReference {
	return r
}
func (r *subscriptionRevisionRepository) CurrentFOMHandle() core.FOMHandle {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.handle
}

func TestSubscriptionEvaluationUsesCurrentRevisionWithoutRepeatedGet(t *testing.T) {
	ctx := context.Background()
	const cls core.InteractionClassHandle = 11
	base := &countingInteractionFOM{names: map[core.InteractionClassHandle]string{cls: "HLAinteractionRoot.ChatMessage"}}
	r, decl, _, _, counting := newInteractionCacheRegistry(t, base, &recordingManagementDispatcher{managerNames: map[string]bool{}})
	first := &subscriptionRevisionFOM{countingInteractionFOM: base, revision: 1}
	counting.handle = first
	r.opts.FOMs = &subscriptionRevisionRepository{counting}
	if err := decl.PublishInteractionClass(ctx, "fed", 1, cls); err != nil {
		t.Fatal(err)
	}
	if err := decl.SubscribeInteractionClass(ctx, "fed", 2, cls); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := r.SendInteraction(ctx, "fed", 1, cls, nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	second := &subscriptionRevisionFOM{countingInteractionFOM: base, revision: 2}
	counting.mu.Lock()
	counting.handle = second
	counting.mu.Unlock()
	for i := 0; i < 2; i++ {
		if err := r.SendInteraction(ctx, "fed", 1, cls, nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	if first.queryCount() != 2 || second.queryCount() != 2 {
		t.Fatalf("stale FOM policy queried: revision1=%d revision2=%d", first.queryCount(), second.queryCount())
	}
	if counting.Gets() != 1 {
		t.Fatalf("Get calls=%d, want single classification lookup", counting.Gets())
	}
	state := r.ExportState("fed")
	commit, err := r.PrepareImportState("fed", state)
	if err != nil {
		t.Fatal(err)
	}
	commit()
	if err := r.SendInteraction(ctx, "fed", 1, cls, nil, nil); err != nil {
		t.Fatal(err)
	}
	if counting.Gets() != 2 || second.queryCount() != 3 {
		t.Fatalf("restore failed to renew cache: Get=%d latest policy=%d", counting.Gets(), second.queryCount())
	}
}
