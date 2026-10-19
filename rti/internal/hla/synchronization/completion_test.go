package sync

import (
	"context"
	"errors"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func TestCompletionDuplicateRegistrationUsesCallback(t *testing.T) {
	m, _ := newMgr(t, Options{Members: func(core.FederationName) []core.FederateHandle { return []core.FederateHandle{1, 2} }})
	ctx := context.Background()
	if err := m.RegisterBy(ctx, "f", 1, "point", nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := m.RegisterBy(ctx, "f", 1, "point", nil, nil); err != nil {
		t.Fatalf("duplicate must be a failed confirmation, not a synchronous error: %v", err)
	}
}

func TestCompletionOutsiderCannotAchieve(t *testing.T) {
	m, _ := newMgr(t, Options{Members: func(core.FederationName) []core.FederateHandle { return []core.FederateHandle{1, 2, 3} }})
	ctx := context.Background()
	if err := m.RegisterBy(ctx, "f", 1, "point", nil, []core.FederateHandle{1, 2}); err != nil {
		t.Fatal(err)
	}
	if err := m.AchieveWith(ctx, "f", 3, "point", false); !errors.Is(err, core.ErrSyncPointNotRegistered) {
		t.Fatalf("unannounced member: %v", err)
	}
	if got := m.Snapshot("f")[0].AchievedHandles; len(got) != 0 {
		t.Fatalf("outsider changed achieved set: %v", got)
	}
}

func TestCompletionEmptySetMeansAll(t *testing.T) {
	m, _ := newMgr(t, Options{Members: func(core.FederationName) []core.FederateHandle { return []core.FederateHandle{1, 2} }})
	if err := m.RegisterBy(context.Background(), "f", 1, "point", nil, []core.FederateHandle{}); err != nil {
		t.Fatal(err)
	}
	if got := m.Snapshot("f")[0].RequiredHandles; len(got) != 2 {
		t.Fatalf("empty set did not select all members: %v", got)
	}
}
