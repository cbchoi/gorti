package mom

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/core"
)

// fakeOutbox is the package-internal stand-in for core.Outbox so unit
// tests can exercise New without depending on the spec fixtures.
type fakeOutbox struct {
	mu   sync.Mutex
	sent int
}

func (o *fakeOutbox) Send(_ context.Context, _ core.FederationName, _ core.FederateHandle, _ core.OutboundEvent) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.sent++
	return nil
}

func TestNew_RejectsNilOutbox(t *testing.T) {
	_, err := New(Options{})
	if err == nil {
		t.Fatalf("New with nil Outbox: want error, got nil")
	}
}

func TestNew_AcceptsNilEventLog(t *testing.T) {
	_, err := New(Options{Outbox: &fakeOutbox{}})
	if err != nil {
		t.Fatalf("New with nil EventLog: want success, got %v", err)
	}
}

func TestFederationCreated_RejectsEmptyName(t *testing.T) {
	mgr, _ := New(Options{Outbox: &fakeOutbox{}})
	if err := mgr.FederationCreated(context.Background(), "", nil); err == nil {
		t.Fatalf("FederationCreated with empty name: want error")
	}
}

func TestFederationCreated_PathExtraction(t *testing.T) {
	mgr, _ := New(Options{Outbox: &fakeOutbox{}})
	if err := mgr.FederationCreated(context.Background(), "fed", []core.FOMModule{
		{Path: "a.xml"},
		{Path: ""},
		{Path: "b.xml"},
	}); err != nil {
		t.Fatalf("FederationCreated: %v", err)
	}
	attrs, ok := mgr.QueryFederationAttributes("fed")
	if !ok {
		t.Fatal("federation not registered")
	}
	if len(attrs.FOMModuleNames) != 3 {
		t.Fatalf("want 3 module names, got %d (%v)", len(attrs.FOMModuleNames), attrs.FOMModuleNames)
	}
	if attrs.FOMModuleNames[0] != "a.xml" {
		t.Errorf("module[0]=%q, want a.xml", attrs.FOMModuleNames[0])
	}
	if attrs.FOMModuleNames[1] != "module-2" {
		t.Errorf("module[1]=%q, want module-2 (placeholder)", attrs.FOMModuleNames[1])
	}
	if attrs.FOMModuleNames[2] != "b.xml" {
		t.Errorf("module[2]=%q, want b.xml", attrs.FOMModuleNames[2])
	}
}

func TestFederateJoined_RejectsInvalidHandle(t *testing.T) {
	mgr, _ := New(Options{Outbox: &fakeOutbox{}})
	_ = mgr.FederationCreated(context.Background(), "fed", nil)
	if err := mgr.FederateJoined(context.Background(), "fed", core.InvalidFederateHandle, "x", "y"); err == nil {
		t.Fatalf("FederateJoined with invalid handle: want error")
	}
}

func TestFederateJoined_LazyFederationCreate(t *testing.T) {
	// Defensive: cmd/rtid is expected to call FederationCreated first,
	// but if it doesn't, FederateJoined still succeeds and lazily
	// creates an empty HLAfederation snapshot.
	mgr, _ := New(Options{Outbox: &fakeOutbox{}})
	if err := mgr.FederateJoined(context.Background(), "fed", 1, "alice", "RoleA"); err != nil {
		t.Fatalf("FederateJoined: %v", err)
	}
	if _, ok := mgr.QueryFederationAttributes("fed"); !ok {
		t.Errorf("federation snapshot not lazily created")
	}
}

func TestFederateJoined_HandlesSortedDeterministically(t *testing.T) {
	mgr, _ := New(Options{Outbox: &fakeOutbox{}})
	_ = mgr.FederationCreated(context.Background(), "fed", nil)
	for _, h := range []core.FederateHandle{3, 1, 2} {
		if err := mgr.FederateJoined(context.Background(), "fed", h, "n", "t"); err != nil {
			t.Fatalf("join %d: %v", h, err)
		}
	}
	attrs, _ := mgr.QueryFederationAttributes("fed")
	if len(attrs.FederateHandles) != 3 {
		t.Fatalf("want 3 handles, got %d", len(attrs.FederateHandles))
	}
	for i, h := range []core.FederateHandle{1, 2, 3} {
		if attrs.FederateHandles[i] != h {
			t.Errorf("handle[%d]=%d, want %d", i, attrs.FederateHandles[i], h)
		}
	}
}

func TestFederateResigned_RemovesAndUpdatesList(t *testing.T) {
	mgr, _ := New(Options{Outbox: &fakeOutbox{}})
	_ = mgr.FederationCreated(context.Background(), "fed", nil)
	for _, h := range []core.FederateHandle{1, 2, 3} {
		_ = mgr.FederateJoined(context.Background(), "fed", h, "n", "t")
	}
	if err := mgr.FederateResigned(context.Background(), "fed", 2); err != nil {
		t.Fatalf("resign: %v", err)
	}
	if _, ok := mgr.QueryFederateAttributes("fed", 2); ok {
		t.Errorf("federate 2 still queryable")
	}
	attrs, _ := mgr.QueryFederationAttributes("fed")
	for _, h := range attrs.FederateHandles {
		if h == 2 {
			t.Errorf("handle 2 still in list")
		}
	}
	if len(attrs.FederateHandles) != 2 {
		t.Errorf("want 2 handles, got %d", len(attrs.FederateHandles))
	}
}

func TestFederationDestroyed_ClearsAll(t *testing.T) {
	mgr, _ := New(Options{Outbox: &fakeOutbox{}})
	_ = mgr.FederationCreated(context.Background(), "fed", nil)
	_ = mgr.FederateJoined(context.Background(), "fed", 1, "alice", "RoleA")
	if err := mgr.FederationDestroyed(context.Background(), "fed"); err != nil {
		t.Fatalf("destroy: %v", err)
	}
	if _, ok := mgr.QueryFederationAttributes("fed"); ok {
		t.Errorf("federation still queryable")
	}
	if _, ok := mgr.QueryFederateAttributes("fed", 1); ok {
		t.Errorf("federate 1 still queryable after federation destroy")
	}
}

func TestFederationDestroyed_UnknownFedIsNoOp(t *testing.T) {
	mgr, _ := New(Options{Outbox: &fakeOutbox{}})
	if err := mgr.FederationDestroyed(context.Background(), "ghost"); err != nil {
		t.Errorf("destroy unknown: want no error, got %v", err)
	}
}

func TestTimeStateChanged_NoOpWhenUnknown(t *testing.T) {
	mgr, _ := New(Options{Outbox: &fakeOutbox{}})
	// no federation registered — must not panic / error
	if err := mgr.TimeStateChanged(context.Background(), "ghost", 1, true, false, 1, 0); err != nil {
		t.Errorf("TimeStateChanged unknown fed: %v", err)
	}
	_ = mgr.FederationCreated(context.Background(), "fed", nil)
	if err := mgr.TimeStateChanged(context.Background(), "fed", 99, true, false, 1, 0); err != nil {
		t.Errorf("TimeStateChanged unknown federate: %v", err)
	}
}

func TestIncrementCounters(t *testing.T) {
	mgr, _ := New(Options{Outbox: &fakeOutbox{}})
	_ = mgr.FederationCreated(context.Background(), "fed", nil)
	_ = mgr.FederateJoined(context.Background(), "fed", 1, "n", "t")
	mgr.IncrementInteractionsSent("fed", 1)
	mgr.IncrementInteractionsSent("fed", 1)
	mgr.IncrementInteractionsReceived("fed", 1)
	mgr.IncrementUpdatesSent("fed", 1)
	mgr.IncrementUpdatesSent("fed", 1)
	mgr.IncrementUpdatesSent("fed", 1)
	mgr.IncrementReflectionsReceived("fed", 1)

	attrs, _ := mgr.QueryFederateAttributes("fed", 1)
	if attrs.InteractionsSent != 2 {
		t.Errorf("InteractionsSent=%d, want 2", attrs.InteractionsSent)
	}
	if attrs.InteractionsReceived != 1 {
		t.Errorf("InteractionsReceived=%d, want 1", attrs.InteractionsReceived)
	}
	if attrs.UpdatesSent != 3 {
		t.Errorf("UpdatesSent=%d, want 3", attrs.UpdatesSent)
	}
	if attrs.ReflectionsReceived != 1 {
		t.Errorf("ReflectionsReceived=%d, want 1", attrs.ReflectionsReceived)
	}
}

func TestIncrementCounters_UnknownIsNoOp(_ *testing.T) {
	mgr, _ := New(Options{Outbox: &fakeOutbox{}})
	// Must not panic on unknown federation/federate.
	mgr.IncrementInteractionsSent("ghost", 1)
	mgr.IncrementInteractionsReceived("ghost", 1)
	mgr.IncrementUpdatesSent("ghost", 1)
	mgr.IncrementReflectionsReceived("ghost", 1)
}

func TestConcurrentIncrement(t *testing.T) {
	mgr, _ := New(Options{Outbox: &fakeOutbox{}})
	_ = mgr.FederationCreated(context.Background(), "fed", nil)
	_ = mgr.FederateJoined(context.Background(), "fed", 1, "n", "t")
	var wg sync.WaitGroup
	const n = 100
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			mgr.IncrementUpdatesSent("fed", 1)
		}()
	}
	wg.Wait()
	attrs, _ := mgr.QueryFederateAttributes("fed", 1)
	if attrs.UpdatesSent != n {
		t.Errorf("UpdatesSent=%d after %d concurrent increments, want %d", attrs.UpdatesSent, n, n)
	}
}

func TestErrNotImplementedRetained(t *testing.T) {
	// Spec tests still match on this sentinel during pre-dispatch RED;
	// keep it exported and recognizable.
	if !errors.Is(ErrNotImplemented, ErrNotImplemented) {
		t.Fatal("ErrNotImplemented not self-matching")
	}
}

func TestSnapshotsAreDeepCopied(t *testing.T) {
	mgr, _ := New(Options{Outbox: &fakeOutbox{}})
	_ = mgr.FederationCreated(context.Background(), "fed", []core.FOMModule{{Path: "a.xml"}})
	_ = mgr.FederateJoined(context.Background(), "fed", 1, "n", "t")

	attrs, _ := mgr.QueryFederationAttributes("fed")
	// Mutate the returned slice — must not affect the live state.
	if len(attrs.FederateHandles) > 0 {
		attrs.FederateHandles[0] = 999
	}
	if len(attrs.FOMModuleNames) > 0 {
		attrs.FOMModuleNames[0] = "tampered"
	}

	again, _ := mgr.QueryFederationAttributes("fed")
	if len(again.FederateHandles) != 1 || again.FederateHandles[0] != 1 {
		t.Errorf("federate-handles snapshot aliased the live state: %v", again.FederateHandles)
	}
	if len(again.FOMModuleNames) != 1 || again.FOMModuleNames[0] != "a.xml" {
		t.Errorf("fom-module-names snapshot aliased the live state: %v", again.FOMModuleNames)
	}
}
