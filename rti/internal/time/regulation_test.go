package time

import (
	"context"
	"errors"
	"math"
	"sync"
	"testing"
	stdtime "time"

	"github.com/cbchoi/gorti/rti/internal/core"
)

// nopOutbox satisfies core.Outbox for constructor tests; it never records.
type nopOutbox struct{}

func (nopOutbox) Send(_ context.Context, _ core.FederationName, _ core.FederateHandle, _ core.OutboundEvent) error {
	return nil
}

func zeroTime() stdtime.Time { return stdtime.Unix(0, 0) }

// Internal unit tests for the per-federate regulation state machine.
// These exercise the helpers that back Manager's exported methods. The
// acceptance contract lives in rti/spec/M3/
// regulation_test.go; these tests are component-owned.

func TestStateStore_EnableRegulation_Happy(t *testing.T) {
	s := newStateStore()
	if err := s.enableRegulation("fed", 1, core.LogicalTime(2.5)); err != nil {
		t.Fatalf("enableRegulation: %v", err)
	}
	got := s.snapshot("fed", 1)
	if !got.regulating {
		t.Errorf("regulating = false, want true")
	}
	if got.lookahead != core.LogicalTime(2.5) {
		t.Errorf("lookahead = %v, want 2.5", got.lookahead)
	}
}

func TestStateStore_EnableRegulation_Twice(t *testing.T) {
	s := newStateStore()
	_ = s.enableRegulation("fed", 1, core.LogicalTime(0))
	err := s.enableRegulation("fed", 1, core.LogicalTime(0))
	if !errors.Is(err, core.ErrTimeAlreadyRegulating) {
		t.Errorf("re-enable: err = %v, want ErrTimeAlreadyRegulating", err)
	}
}

func TestStateStore_EnableRegulation_RejectsNegative(t *testing.T) {
	s := newStateStore()
	err := s.enableRegulation("fed", 1, core.LogicalTime(-0.0001))
	if !errors.Is(err, core.ErrTimeInvalidLookahead) {
		t.Errorf("negative: err = %v, want ErrTimeInvalidLookahead", err)
	}
}

func TestStateStore_EnableRegulation_RejectsNaN(t *testing.T) {
	s := newStateStore()
	err := s.enableRegulation("fed", 1, core.LogicalTime(math.NaN()))
	if !errors.Is(err, core.ErrTimeInvalidLookahead) {
		t.Errorf("NaN: err = %v, want ErrTimeInvalidLookahead", err)
	}
}

func TestStateStore_EnableRegulation_AcceptsZero(t *testing.T) {
	s := newStateStore()
	if err := s.enableRegulation("fed", 1, core.LogicalTime(0)); err != nil {
		t.Errorf("zero lookahead: err = %v, want nil", err)
	}
}

func TestStateStore_DisableRegulation_NotRegulating(t *testing.T) {
	s := newStateStore()
	err := s.disableRegulation("fed", 7)
	if !errors.Is(err, core.ErrTimeNotRegulating) {
		t.Errorf("disable cold: err = %v, want ErrTimeNotRegulating", err)
	}
}

func TestStateStore_DisableRegulation_AfterDisable(t *testing.T) {
	s := newStateStore()
	_ = s.enableRegulation("fed", 1, core.LogicalTime(1))
	if err := s.disableRegulation("fed", 1); err != nil {
		t.Errorf("first disable: %v", err)
	}
	err := s.disableRegulation("fed", 1)
	if !errors.Is(err, core.ErrTimeNotRegulating) {
		t.Errorf("second disable: err = %v, want ErrTimeNotRegulating", err)
	}
}

func TestStateStore_EnableConstrained_Happy(t *testing.T) {
	s := newStateStore()
	if err := s.enableConstrained("fed", 1); err != nil {
		t.Fatalf("enableConstrained: %v", err)
	}
	got := s.snapshot("fed", 1)
	if !got.constrained {
		t.Errorf("constrained = false, want true")
	}
}

func TestStateStore_EnableConstrained_Twice(t *testing.T) {
	s := newStateStore()
	_ = s.enableConstrained("fed", 1)
	err := s.enableConstrained("fed", 1)
	if !errors.Is(err, core.ErrTimeAlreadyConstrained) {
		t.Errorf("re-enable: err = %v, want ErrTimeAlreadyConstrained", err)
	}
}

func TestStateStore_DisableConstrained_NotConstrained(t *testing.T) {
	s := newStateStore()
	err := s.disableConstrained("fed", 7)
	if !errors.Is(err, core.ErrTimeNotConstrained) {
		t.Errorf("disable cold: err = %v, want ErrTimeNotConstrained", err)
	}
}

func TestStateStore_RegulatingAndConstrainedIndependent(t *testing.T) {
	// Same federate may be both regulating and constrained, and
	// disabling one must not disturb the other.
	s := newStateStore()
	if err := s.enableRegulation("fed", 1, core.LogicalTime(1)); err != nil {
		t.Fatalf("enableRegulation: %v", err)
	}
	if err := s.enableConstrained("fed", 1); err != nil {
		t.Fatalf("enableConstrained: %v", err)
	}
	if err := s.disableRegulation("fed", 1); err != nil {
		t.Fatalf("disableRegulation: %v", err)
	}
	got := s.snapshot("fed", 1)
	if got.regulating {
		t.Errorf("regulating = true after disable")
	}
	if !got.constrained {
		t.Errorf("constrained = false after disabling regulation")
	}
}

func TestStateStore_PerFederationIsolation(t *testing.T) {
	s := newStateStore()
	_ = s.enableRegulation("fedA", 1, core.LogicalTime(1))
	err := s.disableRegulation("fedB", 1)
	if !errors.Is(err, core.ErrTimeNotRegulating) {
		t.Errorf("fedB disable: err = %v, want ErrTimeNotRegulating", err)
	}
	// fedA's state must not have changed.
	got := s.snapshot("fedA", 1)
	if !got.regulating {
		t.Errorf("fedA federate 1 lost its regulating flag")
	}
}

func TestStateStore_ConcurrentAccess(t *testing.T) {
	// The state store must tolerate parallel enable/disable on
	// distinct federates from many goroutines without data races
	// (run with `go test -race`).
	t.Parallel()
	s := newStateStore()
	const n = 64
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(h core.FederateHandle) {
			defer wg.Done()
			_ = s.enableRegulation("fed", h, core.LogicalTime(0))
			_ = s.enableConstrained("fed", h)
			_ = s.disableConstrained("fed", h)
			_ = s.disableRegulation("fed", h)
		}(core.FederateHandle(i + 1))
	}
	wg.Wait()
	// Final state of every federate should be neither regulating
	// nor constrained.
	for i := 1; i <= n; i++ {
		got := s.snapshot("fed", core.FederateHandle(i))
		if got.regulating || got.constrained {
			t.Errorf("handle %d: end state = %+v, want zero", i, got)
		}
	}
}

func TestNew_RejectsNilClock(t *testing.T) {
	outbox := nopOutbox{}
	_, err := New(Options{Outbox: outbox})
	if err == nil {
		t.Errorf("nil Clock: err = nil, want non-nil")
	}
}

func TestNew_RejectsNilOutbox(t *testing.T) {
	_, err := New(Options{Clock: core.NewFakeClock(zeroTime())})
	if err == nil {
		t.Errorf("nil Outbox: err = nil, want non-nil")
	}
}

func TestNew_AllowsNilEventLog(t *testing.T) {
	mgr, err := New(Options{
		Clock:  core.NewFakeClock(zeroTime()),
		Outbox: nopOutbox{},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if mgr == nil {
		t.Fatalf("Manager is nil")
	}
}

func TestNew_AppliesDefaultStallTimeout(t *testing.T) {
	mgr, err := New(Options{
		Clock:  core.NewFakeClock(zeroTime()),
		Outbox: nopOutbox{},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if mgr.opts.StallTimeout != DefaultStallTimeout {
		t.Errorf("StallTimeout = %v, want %v", mgr.opts.StallTimeout, DefaultStallTimeout)
	}
}
