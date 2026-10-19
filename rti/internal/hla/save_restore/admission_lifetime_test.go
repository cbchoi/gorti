package savepoint

import (
	"fmt"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func TestCheckpointAdmissionReclaimsIdleNames(t *testing.T) {
	m, err := New(Options{Outbox: &fakeOutbox{}, BundleStore: newMemStore()})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 1000; i++ {
		fed := core.FederationName(fmt.Sprintf("unknown-%d", i))
		release, err := m.AcquireService(fed)
		if err != nil {
			t.Fatal(err)
		}
		release()
		control := m.AcquireCheckpoint(fed)
		control()
		m.saves[fed] = &activeSave{}
		if release, err := m.AcquireService(fed); err == nil || release != nil {
			t.Fatal("active protocol admitted a service")
		}
		delete(m.saves, fed)
	}
	count := 0
	m.admission.Range(func(_, _ any) bool { count++; return true })
	if count != 0 {
		t.Fatalf("idle admission entries=%d", count)
	}
}

func TestCheckpointAdmissionRetainsOneLockForWaiters(t *testing.T) {
	m, err := New(Options{Outbox: &fakeOutbox{}, BundleStore: newMemStore()})
	if err != nil {
		t.Fatal(err)
	}
	first := m.retainAdmission("fed")
	first.lock.RLock()
	waiter := m.retainAdmission("fed")
	first.lock.RUnlock()
	m.releaseAdmission("fed", first)
	third := m.retainAdmission("fed")
	if first != waiter || waiter != third {
		t.Fatal("same federation acquired distinct locks")
	}
	m.releaseAdmission("fed", waiter)
	m.releaseAdmission("fed", third)
	if _, ok := m.admission.Load(core.FederationName("fed")); ok {
		t.Fatal("last reference retained lock")
	}
}
