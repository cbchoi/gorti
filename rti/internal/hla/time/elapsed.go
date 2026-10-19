package time

import (
	"errors"
	"fmt"
	"math"
	stdtime "time"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

var ErrElapsedCoverage = errors.New("time: elapsed history unavailable")
var ErrElapsedStaleMeasurement = errors.New("time: stale elapsed measurement")

// ElapsedAttributes selects the independently published MOM attributes.
type ElapsedAttributes struct{ Granted, Advancing bool }

// ElapsedMeasurement observes intervals without consuming them.
// The opaque token fences acknowledgements across resign, restore and managers.
// Values include the fractional millisecond carried from earlier publications.
type ElapsedMeasurement struct {
	Granted, Advancing                   stdtime.Duration
	owner                                *elapsedAccounting
	grantedPublished, advancingPublished stdtime.Duration
}

// ElapsedTimeState is optional persistent history, in nanoseconds. It contains
// no process-local timestamp. Nil means unknown history (legacy snapshots), not
// a zero interval. Invalid marks lost clock/overflow coverage until a known
// snapshot is restored or a new membership starts.
type ElapsedTimeState struct {
	Granted, Advancing                   stdtime.Duration
	GrantedPublished, AdvancingPublished stdtime.Duration
	Invalid                              bool
}

type elapsedAccounting struct {
	ElapsedTimeState
	last stdtime.Time
}

// OnFederateJoined must run at the actual join boundary, before time services
// or MOM publication. Repeated calls never reset existing history. It cannot
// backfill an existing member whose join was not observed.
func (m *ManagerOf[T]) OnFederateJoined(fed core.FederationName, h core.FederateHandle) error {
	if fed == "" || h == 0 {
		return fmt.Errorf("time: elapsed tracking requires membership identity")
	}
	ext := extOfOf[T](m)
	ext.mu.Lock()
	defer ext.mu.Unlock()
	if ns := ext.getLocked(fed, h); ns != nil {
		if ns.elapsed == nil {
			return ErrElapsedCoverage
		}
		return nil
	}
	ns := ext.getOrCreateLocked(fed, h)
	ns.elapsed = &elapsedAccounting{last: m.opts.Clock.Now()}
	return nil
}

func (a *elapsedAccounting) accrue(now stdtime.Time, advancing bool) {
	if a == nil || a.Invalid {
		return
	}
	delta := now.Sub(a.last)
	if delta < 0 || (delta == stdtime.Duration(math.MaxInt64) && a.last.Add(delta).Before(now)) {
		a.Invalid = true
		return
	}
	total := &a.Granted
	if advancing {
		total = &a.Advancing
	}
	if delta > stdtime.Duration(math.MaxInt64)-*total {
		a.Invalid = true
		return
	}
	*total += delta
	a.last = now
}

// MeasureElapsed observes the server time-manager state, not callback invocation
// in the SDK. Advancing starts at accepted request admission and ends when the
// successful grant enqueue completes under the state lock. Failed enqueues and
// partial grants that leave the request pending remain advancing. This is the
// product's server-state measurement contract, not a full IEEE conformance claim.
func (m *ManagerOf[T]) MeasureElapsed(fed core.FederationName, h core.FederateHandle) (ElapsedMeasurement, error) {
	ext := extOfOf[T](m)
	ext.mu.Lock()
	defer ext.mu.Unlock()
	ns := ext.getLocked(fed, h)
	if ns == nil || ns.elapsed == nil {
		return ElapsedMeasurement{}, ErrElapsedCoverage
	}
	a := ns.elapsed
	a.accrue(m.opts.Clock.Now(), ns.pendingNER)
	if a.Invalid {
		return ElapsedMeasurement{}, ErrElapsedCoverage
	}
	granted, advancing := a.Granted-a.GrantedPublished, a.Advancing-a.AdvancingPublished
	return ElapsedMeasurement{
		Granted: granted, Advancing: advancing, owner: a,
		grantedPublished:   a.GrantedPublished + granted.Truncate(stdtime.Millisecond),
		advancingPublished: a.AdvancingPublished + advancing.Truncate(stdtime.Millisecond),
	}, nil
}

// AcknowledgeElapsed consumes only the whole milliseconds represented in a
// successful HLAinteger32BE attribute update. Independent baselines retain the
// fractional remainder. Duplicate/out-of-order acknowledgements cannot roll
// baselines back or consume time after the measurement. Publishers must serialize
// measure/publish/ack transactions; arbitrarily many query readers are allowed.
func (m *ManagerOf[T]) AcknowledgeElapsed(fed core.FederationName, h core.FederateHandle, measured ElapsedMeasurement, published ElapsedAttributes) error {
	ext := extOfOf[T](m)
	ext.mu.Lock()
	defer ext.mu.Unlock()
	ns := ext.getLocked(fed, h)
	if ns == nil || ns.elapsed == nil || ns.elapsed != measured.owner {
		return ErrElapsedStaleMeasurement
	}
	a := ns.elapsed
	if a.Invalid {
		return ErrElapsedCoverage
	}
	if (published.Granted && measured.Granted/stdtime.Millisecond > math.MaxInt32) ||
		(published.Advancing && measured.Advancing/stdtime.Millisecond > math.MaxInt32) {
		return fmt.Errorf("time: elapsed publication exceeds HLAinteger32BE")
	}
	if published.Granted {
		a.GrantedPublished = max(a.GrantedPublished, measured.grantedPublished)
	}
	if published.Advancing {
		a.AdvancingPublished = max(a.AdvancingPublished, measured.advancingPublished)
	}
	return nil
}

func validateElapsedState(s *ElapsedTimeState) error {
	if s == nil {
		return nil
	}
	if s.Granted < 0 || s.Advancing < 0 || s.GrantedPublished < 0 || s.AdvancingPublished < 0 ||
		s.GrantedPublished > s.Granted || s.AdvancingPublished > s.Advancing ||
		s.GrantedPublished%stdtime.Millisecond != 0 || s.AdvancingPublished%stdtime.Millisecond != 0 {
		return fmt.Errorf("invalid elapsed history")
	}
	return nil
}
