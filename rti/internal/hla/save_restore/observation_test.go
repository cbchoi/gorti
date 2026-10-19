package savepoint

import (
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func TestObservationAdmissionDuringCheckpointProtocols(t *testing.T) {
	for _, restore := range []bool{false, true} {
		m, err := New(Options{Outbox: &transitionOutbox{}, BundleStore: newMemStore()})
		if err != nil {
			t.Fatal(err)
		}
		if restore {
			m.restore["f"] = &activeRestore{state: RestoreInitiated}
		} else {
			m.saves["f"] = &activeSave{state: StateInitiated}
		}
		observer, ok := any(m).(interface {
			AcquireObservation(core.FederationName) func()
		})
		if !ok {
			t.Fatal("phase-safe callback observation admission unavailable")
		}
		if release, err := m.AcquireService("f"); err == nil {
			release()
			t.Fatal("ordinary mutation admitted during active protocol")
		}
		release := observer.AcquireObservation("f")
		started := make(chan struct{})
		acquired := make(chan struct{})
		finished := make(chan struct{})
		go func() { close(started); unlock := m.AcquireCheckpoint("f"); close(acquired); unlock(); close(finished) }()
		<-started
		select {
		case <-acquired:
			t.Fatal("checkpoint passed active observation")
		case <-time.After(15 * time.Millisecond):
		}
		release()
		release()
		select {
		case <-finished:
		case <-time.After(time.Second):
			t.Fatal("observation did not release checkpoint admission")
		}
		m.admissionMu.Lock()
		_, retained := m.admission.Load(core.FederationName("f"))
		m.admissionMu.Unlock()
		if retained {
			t.Fatal("observation lease leaked")
		}
	}
}
